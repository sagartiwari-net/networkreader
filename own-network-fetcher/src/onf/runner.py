"""Async entrypoint for a capture run."""

from __future__ import annotations

from playwright.async_api import async_playwright

from onf.browser_pool import connect_browser
from onf.capture.session_capture import SessionCapture
from onf.config import RunConfig
from onf.logging_utils import log_info


async def run_capture(config: RunConfig) -> int:
    async with async_playwright() as playwright:
        try:
            browser = await connect_browser(playwright, config.chrome)
        except Exception as exc:
            log_info(
                "Could not connect to Chrome. Start Chrome with remote debugging, e.g.\n"
                f'  chrome --remote-debugging-port={config.chrome.port} '
                "--remote-allow-origins=*"
            )
            raise SystemExit(f"CDP connect failed: {exc}") from exc

        capture = SessionCapture(browser, config)
        await capture.start()

        try:
            await capture.wait_for_stop()
        except KeyboardInterrupt:
            log_info("Interrupted — saving session...")
        finally:
            await capture.finalize()

    return 0
