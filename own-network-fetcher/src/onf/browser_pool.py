"""Fetch Chrome DevTools WebSocket URL."""

from __future__ import annotations

import json
from urllib.request import urlopen

from onf.config import ChromeConfig
from onf.logging_utils import log_info


def get_browser_ws_url(chrome: ChromeConfig) -> str:
    version_url = f"{chrome.cdp_url}/json/version"
    log_info(f"Checking Chrome debug endpoint: {version_url}")
    with urlopen(version_url, timeout=3) as response:
        data = json.loads(response.read().decode("utf-8"))
    ws_url = data.get("webSocketDebuggerUrl")
    if not ws_url:
        raise RuntimeError("Chrome debug endpoint found, but webSocketDebuggerUrl is missing.")
    return ws_url
