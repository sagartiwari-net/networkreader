"""Connect to an existing Chrome/Brave instance over CDP."""

from __future__ import annotations

from playwright.async_api import Browser, Playwright

from onf.config import ChromeConfig
from onf.logging_utils import log_info


async def connect_browser(playwright: Playwright, chrome: ChromeConfig) -> Browser:
    url = chrome.cdp_url
    log_info(f"Connecting to Chrome CDP at {url}")
    browser = await playwright.chromium.connect_over_cdp(url)
    context_count = len(browser.contexts)
    page_count = sum(len(context.pages) for context in browser.contexts)
    log_info(f"Connected — contexts={context_count}, pages={page_count}")
    return browser
