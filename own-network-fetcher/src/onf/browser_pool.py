"""Fetch Chrome DevTools WebSocket URL."""

from __future__ import annotations

import json
from urllib.error import URLError
from urllib.request import urlopen

from onf.chrome_launcher import ensure_chrome_debug, is_debug_port_ready
from onf.config import ChromeConfig
from onf.logging_utils import log_info


def get_browser_ws_url(
    chrome: ChromeConfig,
    *,
    auto_launch: bool = True,
    force_restart: bool = False,
    wait_seconds: float = 25.0,
) -> str:
    version_url = f"{chrome.cdp_url}/json/version"
    log_info(f"Checking Chrome debug endpoint: {version_url}")

    if not is_debug_port_ready(chrome):
        ensure_chrome_debug(
            chrome,
            auto_launch=auto_launch,
            force_restart=force_restart,
            launch_wait_s=wait_seconds,
        )

    try:
        with urlopen(version_url, timeout=5) as response:
            data = json.loads(response.read().decode("utf-8"))
    except (URLError, OSError, TimeoutError) as exc:
        raise RuntimeError(f"Chrome debug endpoint unreachable: {exc}") from exc

    ws_url = data.get("webSocketDebuggerUrl")
    if not ws_url:
        raise RuntimeError("Chrome debug endpoint found, but webSocketDebuggerUrl is missing.")
    return ws_url
