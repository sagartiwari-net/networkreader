#!/usr/bin/env python3
"""
Network Reader

Capture Chrome/Brave network request headers and save each request as a curl command.
Cookie values are shown in plain text (not masked).
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List, Optional, Set, Tuple
from urllib.parse import urlparse
from urllib.request import urlopen


CAPTURE_TARGET_TYPES = {
    "page",
    "iframe",
    "worker",
    "service_worker",
    "shared_worker",
}

SENSITIVE_HEADERS = {
    "authorization",
    "proxy-authorization",
    # "cookie" removed — cookie values are now shown in plain text
    "x-api-key",
    "x-auth-token",
}


def log_info(message: str) -> None:
    print(f"[INFO] {message}", flush=True)


def log_saved(message: str) -> None:
    print(f"[SAVE] {message}", flush=True)


def cmd_escape_inner(value: str) -> str:
    escaped = value.replace("\r", " ").replace("\n", " ")
    escaped = escaped.replace("^", "^^")
    escaped = escaped.replace('"', '^\\^"')
    return escaped


def cmd_quote(value: str) -> str:
    return f'^"{cmd_escape_inner(value)}^"'


def sanitize_header_value(value: Any) -> str:
    if isinstance(value, list):
        text = ", ".join(str(v) for v in value)
    else:
        text = str(value)
    return text.replace("\r", " ").replace("\n", " ").strip()


def sanitize_headers(headers: Dict[str, Any], include_sensitive: bool) -> Dict[str, str]:
    clean: Dict[str, str] = {}
    for name, raw_value in headers.items():
        lower_name = str(name).lower().strip()
        if not include_sensitive and lower_name in SENSITIVE_HEADERS:
            clean[str(name)] = "<redacted>"
            continue
        clean[str(name)] = sanitize_header_value(raw_value)
    return clean


def extract_cookie_header(headers: Dict[str, Any]) -> Optional[str]:
    for name, value in headers.items():
        if str(name).lower().strip() == "cookie":
            return sanitize_header_value(value)
    return None


def parse_cookie_header(cookie_header_value: str) -> Tuple[str, List[str]]:
    """
    Parse cookie header and return full cookie string with real values
    and a list of cookie names.
    """
    parts = [part.strip() for part in cookie_header_value.split(";") if part.strip()]
    cookie_parts: List[str] = []
    cookie_names: List[str] = []

    for part in parts:
        if "=" in part:
            name, value = part.split("=", 1)
            name = name.strip()
            value = value.strip()
        else:
            name = part.strip()
            value = ""

        if not name:
            continue

        cookie_names.append(name)
        cookie_parts.append(f"{name}={value}")  # real value, no masking

    return "; ".join(cookie_parts), cookie_names


def build_curl_command(
    *,
    url: str,
    method: str,
    headers: Dict[str, Any],
    body: Optional[str],
    include_sensitive: bool,
    cookie_only: bool,
) -> Tuple[str, List[str]]:
    clean_headers = sanitize_headers(headers, include_sensitive=include_sensitive)
    raw_cookie_header = extract_cookie_header(headers)
    actual_cookie_value = ""
    cookie_names: List[str] = []

    if raw_cookie_header:
        actual_cookie_value, cookie_names = parse_cookie_header(raw_cookie_header)

    lines = [f"curl {cmd_quote(url)} ^"]

    if method.upper() != "GET":
        lines.append(f"  -X {cmd_quote(method.upper())} ^")

    for name, value in clean_headers.items():
        if str(name).lower().strip() == "cookie":
            # Use actual cookie value (not masked)
            safe_cookie_value = actual_cookie_value or raw_cookie_header or ""
            lines.append(f"  -b {cmd_quote(safe_cookie_value)} ^")
            continue
        if cookie_only:
            continue
        lines.append(f"  -H {cmd_quote(f'{name}: {value}')} ^")

    if body and not cookie_only:
        lines.append(f"  --data-raw {cmd_quote(body)} ^")

    if lines:
        lines[-1] = lines[-1].rstrip(" ^")

    return "\n".join(lines), cookie_names


def fetch_json(url: str) -> Dict[str, Any]:
    with urlopen(url, timeout=2) as resp:
        return json.loads(resp.read().decode("utf-8"))


def get_browser_ws_url(host: str, port: int) -> str:
    data = fetch_json(f"http://{host}:{port}/json/version")
    ws = data.get("webSocketDebuggerUrl")
    if not ws:
        raise RuntimeError("Chrome debug endpoint found, but webSocketDebuggerUrl is missing.")
    return ws


def find_chrome_path(preferred: Optional[str]) -> Optional[str]:
    if preferred:
        return preferred

    candidates = [
        os.path.expandvars(r"%ProgramFiles%\Google\Chrome\Application\chrome.exe"),
        os.path.expandvars(r"%ProgramFiles(x86)%\Google\Chrome\Application\chrome.exe"),
        os.path.expandvars(r"%LocalAppData%\Google\Chrome\Application\chrome.exe"),
        os.path.expandvars(r"%ProgramFiles%\BraveSoftware\Brave-Browser\Application\brave.exe"),
        os.path.expandvars(r"%ProgramFiles(x86)%\BraveSoftware\Brave-Browser\Application\brave.exe"),
        os.path.expandvars(r"%LocalAppData%\BraveSoftware\Brave-Browser\Application\brave.exe"),
    ]
    for path in candidates:
        if path and Path(path).exists():
            return path
    return None


def infer_installed_user_data_dir(browser_path: str) -> Path:
    local_app_data = Path(os.path.expandvars(r"%LocalAppData%"))
    lower = browser_path.lower()
    if "brave" in lower:
        return local_app_data / "BraveSoftware" / "Brave-Browser" / "User Data"
    return local_app_data / "Google" / "Chrome" / "User Data"


def wait_for_debug_endpoint(host: str, port: int, timeout_s: float) -> bool:
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        try:
            get_browser_ws_url(host, port)
            return True
        except Exception:
            time.sleep(0.4)
    return False


def launch_browser_instance(
    *,
    browser_path: str,
    port: int,
    profile_root: Path,
    profile_directory: str,
) -> subprocess.Popen:
    profile_root.mkdir(parents=True, exist_ok=True)
    cmd = [
        browser_path,
        f"--remote-debugging-port={port}",
        "--remote-allow-origins=*",
        f"--user-data-dir={profile_root}",
        f"--profile-directory={profile_directory}",
        "--new-window",
        "about:blank",
    ]
    return subprocess.Popen(cmd)


def clone_profile_for_debug(
    *,
    source_user_data_dir: Path,
    profile_directory: str,
    clone_user_data_dir: Path,
) -> Path:
    source_profile_dir = source_user_data_dir / profile_directory
    if not source_profile_dir.exists():
        raise RuntimeError(f"Profile folder not found: {source_profile_dir}")

    clone_user_data_dir.mkdir(parents=True, exist_ok=True)
    target_profile_dir = clone_user_data_dir / profile_directory

    if target_profile_dir.exists():
        shutil.rmtree(target_profile_dir, ignore_errors=True)

    ignore_names = shutil.ignore_patterns(
        "Cache",
        "Code Cache",
        "GPUCache",
        "Service Worker",
        "Crashpad",
        "GrShaderCache",
        "ShaderCache",
        "DawnCache",
        "VideoDecodeStats",
    )
    shutil.copytree(source_profile_dir, target_profile_dir, ignore=ignore_names)

    local_state_src = source_user_data_dir / "Local State"
    local_state_dst = clone_user_data_dir / "Local State"
    if local_state_src.exists():
        shutil.copy2(local_state_src, local_state_dst)

    return clone_user_data_dir


def launch_chrome_if_needed(
    *,
    host: str,
    port: int,
    chrome_path: Optional[str],
    user_data_dir: Optional[str],
    use_installed_profile: bool,
    profile_directory: Optional[str],
    clone_installed_profile: bool,
    no_clone_fallback: bool,
    clone_profile_dir: Optional[str],
    no_launch: bool,
) -> Optional[subprocess.Popen]:
    try:
        get_browser_ws_url(host, port)
        log_info(f"Found running debug browser at http://{host}:{port}")
        return None
    except Exception:
        if no_launch:
            raise RuntimeError(
                f"Chrome debug endpoint not found at http://{host}:{port}.\n"
                f"Start browser manually with --remote-debugging-port={port}"
            )

    resolved_chrome = find_chrome_path(chrome_path)
    if not resolved_chrome:
        raise RuntimeError(
            "Chrome/Brave binary not found automatically. "
            "Use --chrome-path with your browser executable."
        )

    chosen_profile = profile_directory or "Default"
    launch_mode = "temporary-profile"
    profile_root: Path
    installed_root: Optional[Path] = None

    if user_data_dir:
        profile_root = Path(user_data_dir)
        launch_mode = "custom-user-data-dir"
    elif use_installed_profile or clone_installed_profile:
        installed_root = infer_installed_user_data_dir(resolved_chrome)
        if clone_installed_profile:
            clone_root = (
                Path(clone_profile_dir)
                if clone_profile_dir
                else (Path(tempfile.gettempdir()) / "network-reader-profile-clone")
            )
            log_info("Preparing cloned installed profile (forced clone mode)")
            profile_root = clone_profile_for_debug(
                source_user_data_dir=installed_root,
                profile_directory=chosen_profile,
                clone_user_data_dir=clone_root,
            )
            launch_mode = "cloned-installed-profile"
        else:
            profile_root = installed_root
            launch_mode = "installed-profile"
    else:
        profile_root = Path(tempfile.gettempdir()) / "network-reader-profile"

    log_info(f"Launching browser ({launch_mode})")
    log_info(f"User data dir: {profile_root}")
    log_info(f"Profile directory: {chosen_profile}")
    proc = launch_browser_instance(
        browser_path=resolved_chrome,
        port=port,
        profile_root=profile_root,
        profile_directory=chosen_profile,
    )

    if wait_for_debug_endpoint(host, port, timeout_s=20):
        log_info("Debug endpoint is ready.")
        return proc

    clone_fallback_allowed = (
        (use_installed_profile or user_data_dir)
        and not no_clone_fallback
        and not clone_installed_profile
    )
    if clone_fallback_allowed:
        try:
            proc.terminate()
        except Exception:
            pass

        source_root = installed_root or Path(user_data_dir)  # type: ignore[arg-type]
        clone_root = (
            Path(clone_profile_dir)
            if clone_profile_dir
            else (Path(tempfile.gettempdir()) / "network-reader-profile-clone")
        )
        log_info("Direct profile launch failed. Trying cloned profile fallback...")
        log_info(f"Clone destination: {clone_root}")
        cloned_dir = clone_profile_for_debug(
            source_user_data_dir=source_root,
            profile_directory=chosen_profile,
            clone_user_data_dir=clone_root,
        )
        proc = launch_browser_instance(
            browser_path=resolved_chrome,
            port=port,
            profile_root=cloned_dir,
            profile_directory=chosen_profile,
        )
        if wait_for_debug_endpoint(host, port, timeout_s=20):
            log_info("Debug endpoint is ready (clone fallback).")
            return proc

    profile_hint = (
        "\nTry closing all browser windows and rerun. "
        "If it still fails, run with --clone-installed-profile."
    )
    raise RuntimeError(
        "Launched browser, but debug endpoint is unavailable. "
        f"Check if another app is using port {port}.{profile_hint}"
    )


def safe_domain_from_url(url: str) -> str:
    try:
        host = (urlparse(url).hostname or "unknown").strip().lower()
    except Exception:
        host = "unknown"
    if not host:
        host = "unknown"
    safe = "".join(ch if (ch.isalnum() or ch in "._-") else "_" for ch in host)
    return safe or "unknown"


def short_path_from_url(url: str) -> str:
    try:
        parsed = urlparse(url)
        path = parsed.path or "/"
        if parsed.query:
            path = f"{path}?..."
        return path if len(path) <= 80 else path[:77] + "..."
    except Exception:
        return url[:80]


def prepend_text(path: Path, block: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists():
        existing = path.read_text(encoding="utf-8", errors="ignore")
        path.write_text(block + "\n\n" + existing, encoding="utf-8")
    else:
        path.write_text(block + "\n\n", encoding="utf-8")


class NetworkReader:
    def __init__(
        self,
        *,
        ws_url: str,
        global_out_file: Path,
        domain_dir: Path,
        include_sensitive: bool,
        cookie_only: bool,
        fallback_wait_s: float,
    ):
        self.ws_url = ws_url
        self.global_out_file = global_out_file
        self.domain_dir = domain_dir
        self.include_sensitive = include_sensitive
        self.cookie_only = cookie_only
        self.fallback_wait_s = fallback_wait_s

        self.next_id = 1
        self.pending_by_key: Dict[Tuple[str, str], Dict[str, Any]] = {}
        self.requested_target_attach: Set[str] = set()
        self.attached_sessions: Set[str] = set()
        self.pending_command_methods: Dict[int, str] = {}
        self.total_saved = 0

    def _send(
        self,
        ws: Any,
        method: str,
        params: Optional[Dict[str, Any]] = None,
        session_id: Optional[str] = None,
    ) -> int:
        msg: Dict[str, Any] = {"id": self.next_id, "method": method}
        if params is not None:
            msg["params"] = params
        if session_id:
            msg["sessionId"] = session_id

        msg_id = self.next_id
        self.next_id += 1
        self.pending_command_methods[msg_id] = method
        ws.send(json.dumps(msg))
        return msg_id

    def _save_entry(
        self,
        *,
        url: str,
        method: str,
        curl_text: str,
        created_at: str,
        cookie_names: List[str],
    ) -> None:
        domain = safe_domain_from_url(url)
        domain_file = self.domain_dir / f"{domain}.txt"

        cookie_list = ", ".join(cookie_names) if cookie_names else "none"
        block_lines = [
            f"# [{created_at}] {method.upper()} {url}",
            f"# cookies_count={len(cookie_names)} cookies={cookie_list}",
            curl_text,
        ]
        block = "\n".join(block_lines)

        prepend_text(self.global_out_file, block)
        prepend_text(domain_file, block)

        self.total_saved += 1
        now_local = datetime.now().strftime("%H:%M:%S")
        log_saved(
            f"{now_local} | {domain} | {method.upper():<6} {short_path_from_url(url)} "
            f"| cookies={len(cookie_names)} | total={self.total_saved}"
        )

    def _try_emit(self, key: Tuple[str, str]) -> None:
        state = self.pending_by_key.get(key)
        if not state or state.get("emitted"):
            return

        method = state.get("method")
        url = state.get("url")
        if not method or not url:
            return
        if not str(url).startswith(("http://", "https://")):
            state["emitted"] = True
            return

        headers = state.get("extra_headers") or state.get("headers")
        if not headers:
            return

        raw_cookie = extract_cookie_header(headers)
        if self.cookie_only and not raw_cookie:
            state["emitted"] = True
            return

        curl_text, cookie_names = build_curl_command(
            url=url,
            method=method,
            headers=headers,
            body=state.get("post_data"),
            include_sensitive=self.include_sensitive,
            cookie_only=self.cookie_only,
        )
        self._save_entry(
            url=url,
            method=method,
            curl_text=curl_text,
            created_at=state.get("created_at", datetime.now(timezone.utc).isoformat()),
            cookie_names=cookie_names,
        )
        state["emitted"] = True

    def _flush_pending_by_timeout(self) -> None:
        now = time.time()
        stale_keys = []

        for key, state in self.pending_by_key.items():
            updated_at = state.get("updated_at", now)
            emitted = state.get("emitted", False)

            if not emitted and (now - updated_at) >= self.fallback_wait_s:
                if state.get("headers"):
                    self._try_emit(key)

            if emitted and (now - updated_at) > 5:
                stale_keys.append(key)
            elif (now - updated_at) > 120:
                stale_keys.append(key)

        for key in stale_keys:
            self.pending_by_key.pop(key, None)

    def _attach_target_if_supported(self, ws: Any, target_info: Dict[str, Any]) -> None:
        target_id = target_info.get("targetId")
        target_type = target_info.get("type")
        if not target_id or target_type not in CAPTURE_TARGET_TYPES:
            return
        if target_id in self.requested_target_attach:
            return
        self.requested_target_attach.add(target_id)
        self._send(ws, "Target.attachToTarget", {"targetId": target_id, "flatten": True})

    def _handle_event(self, ws: Any, message: Dict[str, Any]) -> None:
        method = message.get("method")
        params = message.get("params", {})

        if method == "Target.targetCreated":
            self._attach_target_if_supported(ws, params.get("targetInfo", {}))
            return

        if method == "Target.attachedToTarget":
            session_id = params.get("sessionId")
            target_info = params.get("targetInfo", {})
            target_type = target_info.get("type")
            target_url = target_info.get("url", "")
            if not session_id:
                return
            if target_type not in CAPTURE_TARGET_TYPES:
                self._send(ws, "Target.detachFromTarget", {"sessionId": session_id})
                return
            if session_id in self.attached_sessions:
                return
            self.attached_sessions.add(session_id)
            self._send(ws, "Network.enable", session_id=session_id)
            log_info(f"Attached: {target_type} {target_url[:100]}")
            return

        if method == "Target.detachedFromTarget":
            session_id = params.get("sessionId")
            if session_id:
                self.attached_sessions.discard(session_id)
            return

        if method == "Network.requestWillBeSent":
            session_id = message.get("sessionId", "")
            request_id = params.get("requestId", "")
            request = params.get("request", {})
            if not request_id:
                return

            key = (session_id, request_id)
            prev = self.pending_by_key.get(key, {})
            old_extra_headers = prev.get("extra_headers")

            self.pending_by_key[key] = {
                "method": request.get("method"),
                "url": request.get("url"),
                "post_data": request.get("postData"),
                "headers": request.get("headers", {}) or {},
                "extra_headers": old_extra_headers,
                "updated_at": time.time(),
                "created_at": datetime.now(timezone.utc).isoformat(),
                "emitted": False,
            }
            self._try_emit(key)
            return

        if method == "Network.requestWillBeSentExtraInfo":
            session_id = message.get("sessionId", "")
            request_id = params.get("requestId", "")
            if not request_id:
                return
            key = (session_id, request_id)
            state = self.pending_by_key.setdefault(key, {})
            state["extra_headers"] = params.get("headers", {}) or {}
            state["updated_at"] = time.time()
            state.setdefault("created_at", datetime.now(timezone.utc).isoformat())
            state.setdefault("emitted", False)
            self._try_emit(key)
            return

    def _handle_response(self, ws: Any, message: Dict[str, Any]) -> None:
        msg_id = message.get("id")
        if not msg_id:
            return
        method = self.pending_command_methods.pop(msg_id, "")

        if method == "Target.getTargets":
            result = message.get("result", {})
            for target_info in result.get("targetInfos", []):
                self._attach_target_if_supported(ws, target_info)

    def run(self) -> None:
        try:
            import websocket  # type: ignore
        except Exception:
            print("Missing dependency: websocket-client", file=sys.stderr)
            print("Install with: pip install websocket-client", file=sys.stderr)
            raise SystemExit(1)

        self.global_out_file.parent.mkdir(parents=True, exist_ok=True)
        self.domain_dir.mkdir(parents=True, exist_ok=True)

        log_info(f"Global log: {self.global_out_file}")
        log_info(f"Domain logs dir: {self.domain_dir}")
        log_info(
            "Capture mode: "
            + ("cookie-only (cookie values shown)" if self.cookie_only else "all-requests")
        )
        log_info(
            "Sensitive headers: "
            + ("included (use carefully)" if self.include_sensitive else "redacted")
        )

        try:
            ws = websocket.create_connection(self.ws_url, timeout=1.0, suppress_origin=True)
        except Exception as exc:
            text = str(exc)
            if "403" in text and "remote-allow-origins" in text:
                print(
                    "WebSocket rejected by browser origin policy.\n"
                    "Re-run with auto-launch mode (not --no-launch), or launch Chrome with:\n"
                    "  --remote-allow-origins=*",
                    file=sys.stderr,
                )
            else:
                print(f"Failed to connect DevTools WebSocket: {exc}", file=sys.stderr)
            raise SystemExit(1)

        self._send(ws, "Target.setDiscoverTargets", {"discover": True})
        self._send(
            ws,
            "Target.setAutoAttach",
            {
                "autoAttach": True,
                "waitForDebuggerOnStart": False,
                "flatten": True,
            },
        )
        self._send(ws, "Target.getTargets")

        log_info("Capturing requests. Press Ctrl+C to stop.")

        recv_count = 0
        try:
            while True:
                try:
                    raw = ws.recv()
                except Exception:
                    self._flush_pending_by_timeout()
                    continue

                if not raw:
                    self._flush_pending_by_timeout()
                    continue

                recv_count += 1
                try:
                    message = json.loads(raw)
                except json.JSONDecodeError:
                    continue

                if "method" in message:
                    self._handle_event(ws, message)
                elif "id" in message:
                    self._handle_response(ws, message)

                if recv_count % 50 == 0:
                    self._flush_pending_by_timeout()
        except KeyboardInterrupt:
            self._flush_pending_by_timeout()
            log_info(f"Stopped. Total saved: {self.total_saved}")
        finally:
            try:
                ws.close()
            except Exception:
                pass


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Capture Chrome/Brave network headers and save as curl commands."
    )
    parser.add_argument("--host", default="127.0.0.1", help="Chrome debug host (default: 127.0.0.1)")
    parser.add_argument("--port", type=int, default=9222, help="Chrome debug port (default: 9222)")
    parser.add_argument(
        "--output",
        default="network_requests.txt",
        help="Global output text file (latest entry goes on top).",
    )
    parser.add_argument(
        "--domain-dir",
        default="domain_sessions",
        help="Directory for per-domain files (latest entry goes on top).",
    )
    parser.add_argument(
        "--include-sensitive",
        action="store_true",
        help="Include sensitive headers (authorization, x-api-key, etc.).",
    )
    parser.add_argument(
        "--all-requests",
        action="store_true",
        help="Capture all requests/headers. Default mode captures cookie-carrying requests only.",
    )
    parser.add_argument("--open-notepad", action="store_true", help="Open global output file in Notepad.")
    parser.add_argument("--chrome-path", default=None, help="Path to chrome.exe/brave.exe")
    parser.add_argument("--user-data-dir", default=None, help="Custom browser User Data dir path.")
    parser.add_argument(
        "--use-installed-profile",
        action="store_true",
        help="Use installed browser profile root (Chrome/Brave User Data).",
    )
    parser.add_argument(
        "--clone-installed-profile",
        action="store_true",
        help="Clone installed profile to a debug-safe folder before launching.",
    )
    parser.add_argument(
        "--no-clone-fallback",
        action="store_true",
        help="Disable automatic clone fallback when direct profile launch fails.",
    )
    parser.add_argument(
        "--clone-profile-dir",
        default=None,
        help="Clone destination root (default: temp folder network-reader-profile-clone).",
    )
    parser.add_argument(
        "--profile-directory",
        default="Default",
        help='Profile directory name, e.g. "Default", "Profile 1".',
    )
    parser.add_argument("--no-launch", action="store_true", help="Do not auto-launch browser if debug endpoint is unavailable.")
    parser.add_argument(
        "--fallback-wait-ms",
        type=int,
        default=500,
        help="Wait before fallback logging without extra headers (default: 500).",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    global_out_file = Path(args.output).resolve()
    domain_dir = Path(args.domain_dir).resolve()

    try:
        launch_chrome_if_needed(
            host=args.host,
            port=args.port,
            chrome_path=args.chrome_path,
            user_data_dir=args.user_data_dir,
            use_installed_profile=args.use_installed_profile,
            profile_directory=args.profile_directory,
            clone_installed_profile=args.clone_installed_profile,
            no_clone_fallback=args.no_clone_fallback,
            clone_profile_dir=args.clone_profile_dir,
            no_launch=args.no_launch,
        )
        ws_url = get_browser_ws_url(args.host, args.port)
    except Exception as exc:
        print(f"Error: {exc}", file=sys.stderr)
        return 1

    if args.open_notepad:
        try:
            subprocess.Popen(["notepad.exe", str(global_out_file)])
        except Exception:
            pass

    reader = NetworkReader(
        ws_url=ws_url,
        global_out_file=global_out_file,
        domain_dir=domain_dir,
        include_sensitive=args.include_sensitive,
        cookie_only=not args.all_requests,
        fallback_wait_s=max(args.fallback_wait_ms, 50) / 1000.0,
    )
    reader.run()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
