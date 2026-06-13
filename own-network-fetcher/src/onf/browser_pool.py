"""Fetch Chrome DevTools WebSocket URL."""

from __future__ import annotations

import json
import time
from urllib.error import URLError
from urllib.request import urlopen

from onf.config import ChromeConfig
from onf.logging_utils import log_info


def get_browser_ws_url(chrome: ChromeConfig, *, wait_seconds: float = 45.0) -> str:
    version_url = f"{chrome.cdp_url}/json/version"
    log_info(f"Checking Chrome debug endpoint: {version_url}")

    deadline = time.time() + max(wait_seconds, 0)
    attempt = 0
    last_error: Exception | None = None

    while True:
        attempt += 1
        try:
            with urlopen(version_url, timeout=3) as response:
                data = json.loads(response.read().decode("utf-8"))
            ws_url = data.get("webSocketDebuggerUrl")
            if not ws_url:
                raise RuntimeError("Chrome debug endpoint found, but webSocketDebuggerUrl is missing.")
            if attempt > 1:
                log_info("Chrome debug port ready.")
            return ws_url
        except (URLError, OSError, TimeoutError, RuntimeError) as exc:
            last_error = exc
            if time.time() >= deadline:
                raise last_error from exc
            if attempt == 1:
                log_info(
                    "Chrome debug port not ready yet.\n"
                    "Alag CMD window mein chalao: scripts\\start_chrome_debug.bat\n"
                    "Profile select karo — ONF wait kar raha hai..."
                )
            time.sleep(2)
