"""Sync CDP capture entrypoint."""

from __future__ import annotations

from onf.browser_pool import get_browser_ws_url
from onf.capture.cdp_capture import CDPCapture
from onf.config import RunConfig
from onf.logging_utils import log_info


def run_capture(config: RunConfig) -> int:
    try:
        ws_url = get_browser_ws_url(config.chrome)
    except Exception as exc:
        log_info(
            "Could not connect to Chrome debug port.\n"
            "1) Close ALL Chrome windows (Task Manager check karo)\n"
            "2) Run: ..\\scripts\\start_chrome_debug.bat\n"
            "3) Profile select karo, phir onf.exe dubara chalao\n"
            f"Details: {exc}"
        )
        return 1

    log_info(f"Connected to Chrome CDP")
    try:
        CDPCapture(ws_url, config).run()
    except Exception as exc:
        log_info(f"Capture error: {exc}")
        return 1
    return 0
