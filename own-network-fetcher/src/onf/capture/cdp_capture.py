"""CDP WebSocket capture — lightweight, works reliably in onf.exe."""

from __future__ import annotations

import base64
import json
import signal
import time
from datetime import datetime, timezone
from typing import Any

from onf.capture.cookies import (
    extract_cookie_header,
    parse_cookie_header,
    parse_set_cookie_headers,
    safe_domain,
)
from onf.capture.storage_collector import StorageCollector
from onf.config import RunConfig
from onf.export.cookie_bundle import build_export_payload
from onf.logging_utils import log_info, log_save, log_skip
from onf.models.session import CookieEvent, FullNetworkRecord, NetworkEvent, SessionModel
from onf.storage.json_writer import SessionWriter

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
    "x-api-key",
    "x-auth-token",
}


class CDPCapture:
    def __init__(self, ws_url: str, config: RunConfig) -> None:
        self.ws_url = ws_url
        self.config = config
        self.session = SessionModel.create(
            task_id=config.task_id,
            capture_mode=config.capture_mode,
            cdp_url=config.chrome.cdp_url,
        )
        self.writer = SessionWriter(config.session_dir, config.capture_mode)
        self.next_id = 1
        self.pending_by_key: dict[tuple[str, str], dict[str, Any]] = {}
        self.response_meta: dict[tuple[str, str], dict[str, Any]] = {}
        self.attached_target_ids: set[str] = set()
        self.attached_sessions: set[str] = set()
        self.page_sessions: dict[str, str] = {}
        self.pending_command_methods: dict[int, str] = {}
        self.pending_body_requests: dict[int, tuple[str, str]] = {}
        self._dedupe_keys: set[str] = set()
        self._stop = False
        self._last_flush = time.time()

    def _send(
        self,
        ws: Any,
        method: str,
        params: dict[str, Any] | None = None,
        session_id: str | None = None,
    ) -> int:
        message: dict[str, Any] = {"id": self.next_id, "method": method}
        if params is not None:
            message["params"] = params
        if session_id:
            message["sessionId"] = session_id
        msg_id = self.next_id
        self.next_id += 1
        self.pending_command_methods[msg_id] = method
        ws.send(json.dumps(message))
        return msg_id

    def _clean_headers(self, headers: dict[str, Any]) -> dict[str, str]:
        clean: dict[str, str] = {}
        for name, value in headers.items():
            lower_name = str(name).lower()
            if not self.config.include_sensitive and lower_name in SENSITIVE_HEADERS:
                clean[str(name)] = "<redacted>"
            else:
                if isinstance(value, list):
                    clean[str(name)] = ", ".join(str(v) for v in value)
                else:
                    clean[str(name)] = str(value)
        return clean

    def _cookie_dedupe_key(self, event: CookieEvent) -> str:
        names = tuple(sorted(str(item.get("name", "")) for item in event.cookies))
        return f"{event.event_type}|{event.url}|{event.method}|{names}|{len(event.cookies)}"

    def _save_cookie_event(self, event: CookieEvent) -> None:
        dedupe_key = self._cookie_dedupe_key(event)
        if dedupe_key in self._dedupe_keys:
            return
        self._dedupe_keys.add(dedupe_key)

        self.session.cookie_events.append(event)
        self.session.summary.cookie_events_saved += 1
        self.writer.append_cookie_event(event)
        names = ", ".join(str(item.get("name", "")) for item in event.cookies[:6])
        extra = f" +{len(event.cookies) - 6} more" if len(event.cookies) > 6 else ""
        log_save(
            f"{event.event_type} | {event.domain} | {event.method or '-':<6} "
            f"{event.url[:70]} | cookies={len(event.cookies)} [{names}{extra}]"
        )

    def _emit_request_cookie(self, state: dict[str, Any]) -> None:
        method = state.get("method") or "GET"
        url = state.get("url") or ""
        if not url.startswith(("http://", "https://")):
            return

        headers = state.get("extra_headers") or state.get("headers") or {}
        cookie_header = extract_cookie_header(headers)
        if not cookie_header:
            return

        cookies, _ = parse_cookie_header(cookie_header)
        event = CookieEvent(
            timestamp=state.get("created_at", datetime.now(timezone.utc).isoformat()),
            event_type="request_cookie",
            url=url,
            method=method,
            domain=safe_domain(url),
            cookies=cookies,
            cookie_header=cookie_header,
        )
        self._save_cookie_event(event)

    def _emit_set_cookie(
        self,
        *,
        url: str,
        method: str,
        status: int | None,
        headers: dict[str, Any],
    ) -> None:
        set_cookies = parse_set_cookie_headers(headers)
        if not set_cookies:
            return
        event = CookieEvent(
            timestamp=datetime.now(timezone.utc).isoformat(),
            event_type="set_cookie",
            url=url,
            method=method,
            domain=safe_domain(url),
            cookies=set_cookies,
            status=status,
        )
        self._save_cookie_event(event)

    def _write_full_network_record(self, key: tuple[str, str], body: str | None) -> None:
        state = self.pending_by_key.get(key, {})
        meta = self.response_meta.get(key, {})
        url = meta.get("url") or state.get("url") or ""
        if not url.startswith(("http://", "https://")):
            return
        if state.get("full_record_saved"):
            return

        headers = state.get("extra_headers") or state.get("headers") or {}
        cookie_header = extract_cookie_header(headers)
        request_payload: dict[str, Any] = {
            "url": url,
            "method": state.get("method") or "GET",
            "headers": self._clean_headers(headers),
        }
        if state.get("post_data"):
            request_payload["postData"] = state["post_data"]
        if cookie_header:
            cookies, _ = parse_cookie_header(cookie_header)
            request_payload["cookies"] = cookies
            request_payload["cookieHeader"] = cookie_header

        response_payload: dict[str, Any] = {
            "status": meta.get("status"),
            "headers": self._clean_headers(meta.get("headers", {})),
            "mimeType": meta.get("mimeType"),
            "body": body,
        }
        set_cookies = parse_set_cookie_headers(meta.get("headers", {}))
        if set_cookies:
            response_payload["setCookies"] = set_cookies

        record = FullNetworkRecord(
            timestamp=state.get("created_at", datetime.now(timezone.utc).isoformat()),
            url=url,
            method=state.get("method") or "GET",
            domain=safe_domain(url),
            request=request_payload,
            response=response_payload,
        )
        self.session.summary.network_events_saved += 1
        self.writer.append_full_network_record(record)
        state["full_record_saved"] = True
        log_save(
            f"NET {record.method:<6} {record.response.get('status')} "
            f"{record.domain} {url[:80]}"
        )

    def _try_process_request(self, key: tuple[str, str]) -> None:
        state = self.pending_by_key.get(key)
        if not state or state.get("processed"):
            return

        headers = state.get("extra_headers") or state.get("headers") or {}
        cookie_header = extract_cookie_header(headers)

        if self.config.full_network:
            if not state.get("counted"):
                self.session.summary.total_requests_seen += 1
                state["counted"] = True
            return

        self.session.summary.total_requests_seen += 1
        if not cookie_header:
            self.session.summary.requests_skipped += 1
            state["processed"] = True
            return
        self._emit_request_cookie(state)
        state["processed"] = True

    def _handle_event(self, ws: Any, message: dict[str, Any]) -> None:
        method = message.get("method")
        params = message.get("params", {})
        session_id = message.get("sessionId", "")

        if method == "Target.attachedToTarget":
            sid = params.get("sessionId")
            target_info = params.get("targetInfo", {})
            target_type = target_info.get("type")
            target_url = target_info.get("url", "")
            target_id = target_info.get("targetId", "")
            if not sid:
                return
            if target_type not in CAPTURE_TARGET_TYPES:
                self._send(ws, "Target.detachFromTarget", {"sessionId": sid})
                return
            if sid in self.attached_sessions or target_id in self.attached_target_ids:
                self._send(ws, "Target.detachFromTarget", {"sessionId": sid})
                return
            self.attached_sessions.add(sid)
            if target_id:
                self.attached_target_ids.add(target_id)
            self.page_sessions[sid] = target_url
            self._send(ws, "Network.enable", session_id=sid)
            self._send(ws, "Runtime.enable", session_id=sid)
            log_info(f"Attached: {target_type} {target_url[:100]}")
            return

        if method == "Network.requestWillBeSent":
            request_id = params.get("requestId", "")
            request = params.get("request", {})
            if not request_id:
                return
            key = (session_id, request_id)
            prev = self.pending_by_key.get(key, {})
            self.pending_by_key[key] = {
                "method": request.get("method"),
                "url": request.get("url"),
                "headers": request.get("headers", {}) or {},
                "extra_headers": prev.get("extra_headers"),
                "post_data": request.get("postData"),
                "created_at": datetime.now(timezone.utc).isoformat(),
                "processed": False,
            }
            if request.get("headers"):
                self._try_process_request(key)
            return

        if method == "Network.requestWillBeSentExtraInfo":
            request_id = params.get("requestId", "")
            if not request_id:
                return
            key = (session_id, request_id)
            state = self.pending_by_key.setdefault(key, {})
            state["extra_headers"] = params.get("headers", {}) or {}
            state.setdefault("created_at", datetime.now(timezone.utc).isoformat())
            state.setdefault("processed", False)
            self._try_process_request(key)
            return

        if method == "Network.responseReceived":
            request_id = params.get("requestId", "")
            if not request_id:
                return
            response = params.get("response", {})
            key = (session_id, request_id)
            self.response_meta[key] = {
                "url": response.get("url", ""),
                "status": response.get("status"),
                "headers": response.get("headers", {}) or {},
                "mimeType": response.get("mimeType"),
            }
            if self.config.cookie_export:
                return
            if self.config.full_network:
                return
            state = self.pending_by_key.get(key, {})
            meta = self.response_meta.get(key, {})
            url = meta.get("url") or state.get("url") or ""
            if not url.startswith(("http://", "https://")):
                return
            event = NetworkEvent(
                timestamp=datetime.now(timezone.utc).isoformat(),
                url=url,
                method=state.get("method") or "GET",
                domain=safe_domain(url),
                status=meta.get("status"),
                response_headers=self._clean_headers(meta.get("headers", {})),
                has_request_cookie=bool(
                    extract_cookie_header(state.get("extra_headers") or state.get("headers") or {})
                ),
                has_set_cookie=bool(parse_set_cookie_headers(meta.get("headers", {}))),
            )
            self.session.summary.network_events_saved += 1
            self.writer.append_network_event(event)
            log_save(f"RES {meta.get('status')} {safe_domain(url)} {url[:80]}")
            return

        if method == "Network.responseReceivedExtraInfo":
            request_id = params.get("requestId", "")
            if not request_id:
                return
            key = (session_id, request_id)
            headers = params.get("headers", {}) or {}
            state = self.pending_by_key.get(key, {})
            meta = self.response_meta.get(key, {})
            url = meta.get("url") or state.get("url") or ""
            method_name = state.get("method") or "GET"
            status = meta.get("status")
            if self.config.cookie_export:
                self._emit_set_cookie(
                    url=url,
                    method=method_name,
                    status=status,
                    headers=headers,
                )
            return

        if method == "Network.loadingFinished" and self.config.full_network:
            request_id = params.get("requestId", "")
            if not request_id:
                return
            key = (session_id, request_id)
            msg_id = self._send(
                ws,
                "Network.getResponseBody",
                {"requestId": request_id},
                session_id=session_id,
            )
            self.pending_body_requests[msg_id] = key
            return

    def _handle_command_response(self, ws: Any, message: dict[str, Any]) -> None:
        msg_id = message.get("id")
        if not msg_id:
            return
        cmd = self.pending_command_methods.pop(msg_id, "")
        if cmd == "Network.getResponseBody":
            key = self.pending_body_requests.pop(msg_id, None)
            if not key:
                return
            result = message.get("result", {})
            body = result.get("body")
            if body and result.get("base64Encoded"):
                try:
                    body = base64.b64decode(body).decode("utf-8", errors="replace")
                except Exception:
                    body = str(body)
            self._write_full_network_record(key, body)
        elif cmd == "Storage.getCookies":
            self.session.cookie_jar_snapshot = message.get("result", {}).get("cookies", [])

    def _export_cookie_bundles(self, ws: Any) -> None:
        collector = StorageCollector(ws, self._send, [self.next_id])
        try:
            all_cookies = collector.get_cookies()
        except Exception as exc:
            log_skip(f"Cookie export skipped: {exc}")
            return

        self.session.cookie_jar_snapshot = all_cookies
        referers: dict[str, str] = {}
        session_for_domain: dict[str, str] = {}

        for sid, url in self.page_sessions.items():
            if not url.startswith(("http://", "https://")):
                continue
            domain = safe_domain(url)
            referers[domain] = url
            session_for_domain[domain] = sid

        if not referers:
            for event in self.session.cookie_events:
                referers.setdefault(event.domain, event.url)

        export_count = 0
        for domain, referer in referers.items():
            local_storage: dict[str, str] = {}
            session_storage: dict[str, str] = {}
            indexed_db: dict[str, Any] = {}
            sid = session_for_domain.get(domain)
            if sid:
                try:
                    local_storage, session_storage = collector.collect_dom_storage(sid)
                except Exception as exc:
                    log_skip(f"DOM storage skipped for {referer}: {exc}")
                try:
                    indexed_db = collector.collect_indexed_db(sid)
                except Exception as exc:
                    log_skip(f"IndexedDB skipped for {referer}: {exc}")

            payload = build_export_payload(
                referer=referer,
                http_cookies=all_cookies,
                local_storage=local_storage or None,
                session_storage=session_storage or None,
                indexed_db=indexed_db or None,
            )
            if not payload.get("includedFormats"):
                continue
            path = self.writer.write_cookie_export(domain, payload)
            export_count += 1
            log_info(f"Cookie export saved: {path}")

        log_info(f"Cookie export files: {export_count} site(s) in {self.writer.exports_dir}")

    def _maybe_flush(self) -> None:
        now = time.time()
        if now - self._last_flush >= self.config.flush_interval_s:
            self.writer.write_session(self.session)
            self._last_flush = now

    def _finalize(self, ws: Any | None = None) -> None:
        if ws is not None and self.config.cookie_export:
            self._export_cookie_bundles(ws)

        self.session.status = "completed"
        self.session.ended_at = datetime.now(timezone.utc)
        self.writer.write_session(self.session)
        summary = self.session.summary
        if self.config.cookie_export:
            log_info(
                "Stopped — "
                f"cookie_events={summary.cookie_events_saved}, "
                f"skipped={summary.requests_skipped}, "
                f"seen={summary.total_requests_seen}"
            )
            log_info(f"Exports folder: {self.writer.exports_dir}")
        else:
            log_info(
                "Stopped — "
                f"network_events={summary.network_events_saved}, "
                f"seen={summary.total_requests_seen}"
            )
            log_info(f"Per-site network folder: {self.writer.by_site_dir}")
        log_info(f"Session saved: {self.writer.session_path}")

    def run(self) -> None:
        try:
            import websocket  # type: ignore
        except ImportError as exc:
            raise RuntimeError(
                "Missing dependency websocket-client. Rebuild onf.exe or run: pip install websocket-client"
            ) from exc

        mode_label = (
            "cookie export (HTTP + storage snapshot on stop)"
            if self.config.cookie_export
            else "full network (detailed per-site traffic)"
        )
        log_info(f"Capture mode: {mode_label}")
        log_info(f"Session output: {self.writer.session_path}")

        try:
            ws = websocket.create_connection(self.ws_url, timeout=3, suppress_origin=True)
        except Exception as exc:
            raise RuntimeError(f"CDP WebSocket connect failed: {exc}") from exc

        def _handle_stop(*_args: Any) -> None:
            self._stop = True

        signal.signal(signal.SIGINT, _handle_stop)
        try:
            signal.signal(signal.SIGTERM, _handle_stop)
        except (AttributeError, ValueError):
            pass

        self._send(ws, "Target.setDiscoverTargets", {"discover": True})
        self._send(
            ws,
            "Target.setAutoAttach",
            {"autoAttach": True, "waitForDebuggerOnStart": False, "flatten": True},
        )
        self.writer.write_session(self.session)
        log_info("Capturing — browse in Chrome. Press Ctrl+C to stop.")

        try:
            while not self._stop:
                try:
                    ws.settimeout(1.0)
                    raw = ws.recv()
                except Exception:
                    self._maybe_flush()
                    continue
                if not raw:
                    continue
                try:
                    message = json.loads(raw)
                except json.JSONDecodeError:
                    continue
                if "method" in message:
                    self._handle_event(ws, message)
                elif "id" in message:
                    self._handle_command_response(ws, message)
                self._maybe_flush()
        except KeyboardInterrupt:
            self._stop = True
        finally:
            try:
                self._finalize(ws)
            finally:
                try:
                    ws.close()
                except Exception:
                    pass
