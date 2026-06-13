"""Launch and verify Chrome remote debugging on Windows."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
from pathlib import Path
from urllib.error import URLError
from urllib.request import urlopen

from onf.config import ChromeConfig
from onf.logging_utils import log_info


def chrome_exe_candidates() -> list[Path]:
    roots = [
        os.environ.get("ProgramFiles", r"C:\Program Files"),
        os.environ.get("ProgramFiles(x86)", r"C:\Program Files (x86)"),
        os.environ.get("LOCALAPPDATA", ""),
    ]
    rel_paths = [
        Path("Google/Chrome/Application/chrome.exe"),
        Path("Google/Chrome Beta/Application/chrome.exe"),
        Path("Chromium/Application/chrome.exe"),
    ]
    candidates: list[Path] = []
    for root in roots:
        if not root:
            continue
        for rel in rel_paths:
            path = Path(root) / rel
            if path.exists():
                candidates.append(path)
    return candidates


def find_chrome_exe() -> Path | None:
    for path in chrome_exe_candidates():
        if path.exists():
            return path
    return None


def debug_profile_dir() -> Path:
    return Path(os.environ.get("LOCALAPPDATA", "")) / "onf-chrome-debug"


def is_debug_port_ready(chrome: ChromeConfig, *, timeout_s: float = 2.0) -> bool:
    version_url = f"{chrome.cdp_url}/json/version"
    try:
        with urlopen(version_url, timeout=timeout_s) as response:
            data = json.loads(response.read().decode("utf-8"))
        return bool(data.get("webSocketDebuggerUrl"))
    except (URLError, OSError, TimeoutError, json.JSONDecodeError, ValueError):
        return False


def is_chrome_running() -> bool:
    if sys.platform != "win32":
        return False
    result = subprocess.run(
        ["tasklist", "/FI", "IMAGENAME eq chrome.exe", "/NH"],
        capture_output=True,
        text=True,
        check=False,
    )
    return "chrome.exe" in result.stdout.lower()


def kill_chrome_processes() -> None:
    if sys.platform != "win32":
        return
    log_info("Saara Chrome band ho raha hai (debug mode ke liye zaroori hai)...")
    subprocess.run(
        ["taskkill", "/F", "/IM", "chrome.exe"],
        capture_output=True,
        check=False,
    )
    time.sleep(2)


def launch_chrome_debug(chrome: ChromeConfig) -> None:
    chrome_exe = find_chrome_exe()
    if chrome_exe is None:
        raise RuntimeError(
            "Google Chrome install nahi mila. Pehle Chrome install karo."
        )

    profile = debug_profile_dir()
    profile.mkdir(parents=True, exist_ok=True)

    args = [
        str(chrome_exe),
        f"--remote-debugging-port={chrome.port}",
        "--remote-allow-origins=*",
        f'--user-data-dir={profile}',
        "--no-first-run",
        "--no-default-browser-check",
    ]
    log_info(f"Debug Chrome start ho raha hai: {chrome_exe.name} (port {chrome.port})")
    log_info(f"Profile: {profile}")
    log_info("Agar profile picker aaye to apna profile select karo.")

    creationflags = 0
    if sys.platform == "win32":
        creationflags = subprocess.DETACHED_PROCESS | subprocess.CREATE_NEW_PROCESS_GROUP

    subprocess.Popen(
        args,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        creationflags=creationflags,
    )


def ensure_chrome_debug(
    chrome: ChromeConfig,
    *,
    auto_launch: bool = True,
    initial_wait_s: float = 8.0,
    launch_wait_s: float = 90.0,
) -> None:
    """Wait for debug port; optionally kill normal Chrome and launch debug Chrome."""
    if is_debug_port_ready(chrome):
        return

    if is_chrome_running():
        log_info(
            "Chrome khula hai lekin NORMAL mode mein hai — ONF ise detect nahi kar sakta.\n"
            "ONF ko Chrome DEBUG mode chahiye (port 9222 par)."
        )
    else:
        log_info("Port 9222 par Chrome debug abhi nahi chal raha.")

    deadline = time.time() + initial_wait_s
    while time.time() < deadline:
        if is_debug_port_ready(chrome):
            log_info("Chrome debug port ready.")
            return
        time.sleep(2)

    if not auto_launch:
        raise RuntimeError("Chrome debug port not ready.")

    kill_chrome_processes()
    launch_chrome_debug(chrome)

    deadline = time.time() + launch_wait_s
    attempt = 0
    while time.time() < deadline:
        attempt += 1
        if is_debug_port_ready(chrome):
            log_info("Chrome debug port ready.")
            return
        if attempt == 1:
            log_info(
                "Profile select karo (Chrome window mein) — ONF wait kar raha hai..."
            )
        elif attempt % 5 == 0:
            remaining = max(0, int(deadline - time.time()))
            log_info(f"Abhi bhi wait... ({remaining}s baaki)")
        time.sleep(2)

    raise RuntimeError(
        "Chrome debug port start nahi hua. Browser mein ye URL kholo: "
        f"{chrome.cdp_url}/json/version — agar JSON na dikhe to scripts\\start_chrome_debug.bat chalao."
    )
