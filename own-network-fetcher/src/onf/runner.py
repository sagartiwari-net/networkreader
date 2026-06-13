"""Sync CDP capture entrypoint."""

from __future__ import annotations

from onf.browser_pool import get_browser_ws_url
from onf.capture.cdp_capture import CDPCapture
from onf.config import RunConfig
from onf.logging_utils import log_info


def run_capture(config: RunConfig) -> int:
    try:
        ws_url = get_browser_ws_url(
            config.chrome,
            auto_launch=config.auto_launch_chrome,
            force_restart=config.force_restart_chrome,
            wait_seconds=config.chrome_wait_seconds,
        )
    except Exception as exc:
        log_info(f"Could not connect to Chrome debug port.\n{exc}")
        return 1

    log_info(f"Connected to Chrome CDP")
    try:
        CDPCapture(ws_url, config).run()
    except Exception as exc:
        log_info(f"Capture error: {exc}")
        return 1
    return 0
