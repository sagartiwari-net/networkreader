"""Playwright-based capture with cookie-only and full modes."""

from __future__ import annotations

import asyncio
import signal
from datetime import datetime, timezone
from typing import Any

from playwright.async_api import Browser, BrowserContext, Page, Request, Response

from onf.capture.cookies import (
    header_lookup,
    parse_cookie_header,
    parse_set_cookie_headers,
    safe_domain,
)
from onf.config import CaptureMode, RunConfig
from onf.logging_utils import log_info, log_save, log_skip
from onf.models.session import CookieEvent, NetworkEvent, SessionModel
from onf.storage.json_writer import SessionWriter

SENSITIVE_HEADERS = {
    "authorization",
    "proxy-authorization",
    "x-api-key",
    "x-auth-token",
}


class SessionCapture:
    def __init__(self, browser: Browser, config: RunConfig) -> None:
        self.browser = browser
        self.config = config
        self.session = SessionModel.create(
            task_id=config.task_id,
            capture_mode=config.capture_mode,
            cdp_url=config.chrome.cdp_url,
        )
        self.writer = SessionWriter(config.session_dir, config.capture_mode)
        self._attached_pages: set[int] = set()
        self._stop_event = asyncio.Event()
        self._flush_task: asyncio.Task[None] | None = None
        self._handlers: list[tuple[BrowserContext, Any]] = []

    async def start(self) -> None:
        mode_label = (
            "cookie-only (non-cookie traffic skipped)"
            if self.config.cookie_only
            else "full (all requests recorded)"
        )
        log_info(f"Capture mode: {mode_label}")
        log_info(f"Session output: {self.writer.session_path}")

        for context in self.browser.contexts:
            await self._attach_context(context)

        if not self._handlers:
            log_info("No browser contexts found yet — waiting for Chrome activity")

        self._flush_task = asyncio.create_task(self._periodic_flush())
        self.writer.write_session(self.session)
        log_info("Capturing — browse in Chrome. Press Ctrl+C to stop.")

    async def _attach_context(self, context: BrowserContext) -> None:
        if any(existing is context for existing, _ in self._handlers):
            return

        async def on_page(page: Page) -> None:
            await self._attach_page(page)

        context.on("page", on_page)
        self._handlers.append((context, on_page))

        for page in context.pages:
            await self._attach_page(page)

    async def _attach_page(self, page: Page) -> None:
        page_id = id(page)
        if page_id in self._attached_pages:
            return
        self._attached_pages.add(page_id)

        def on_request(request: Request) -> None:
            asyncio.create_task(self._handle_request(request))

        def on_response(response: Response) -> None:
            asyncio.create_task(self._handle_response(response))

        page.on("request", on_request)
        page.on("response", on_response)

        url = page.url or "about:blank"
        log_info(f"Attached page: {url[:120]}")

    async def _handle_request(self, request: Request) -> None:
        self.session.summary.total_requests_seen += 1

        headers = request.headers
        cookie_header = header_lookup(headers, "cookie")
        has_cookie = bool(cookie_header)

        if self.config.cookie_only and not has_cookie:
            self.session.summary.requests_skipped += 1
            return

        if self.config.capture_mode == CaptureMode.FULL:
            await self._save_network_request(request, has_cookie=has_cookie)
            return

        if not cookie_header:
            return

        cookies, _ = parse_cookie_header(cookie_header)
        event = CookieEvent(
            timestamp=datetime.now(timezone.utc).isoformat(),
            event_type="request_cookie",
            url=request.url,
            method=request.method,
            domain=safe_domain(request.url),
            cookies=cookies,
            cookie_header=cookie_header,
        )
        self._save_cookie_event(event)

    async def _handle_response(self, response: Response) -> None:
        if self.config.cookie_only:
            set_cookies = parse_set_cookie_headers(response.headers)
            if not set_cookies:
                return

            event = CookieEvent(
                timestamp=datetime.now(timezone.utc).isoformat(),
                event_type="set_cookie",
                url=response.url,
                method=response.request.method,
                domain=safe_domain(response.url),
                cookies=set_cookies,
                status=response.status,
            )
            self._save_cookie_event(event)
            return

        await self._save_network_response(response)

    async def _save_network_request(self, request: Request, *, has_cookie: bool) -> None:
        headers = self._clean_headers(request.headers)
        event = NetworkEvent(
            timestamp=datetime.now(timezone.utc).isoformat(),
            url=request.url,
            method=request.method,
            domain=safe_domain(request.url),
            request_headers=headers,
            has_request_cookie=has_cookie,
        )
        self.session.summary.network_events_saved += 1
        self.writer.append_network_event(event)
        log_save(f"REQ {request.method:<6} {safe_domain(request.url)} {request.url[:80]}")

    async def _save_network_response(self, response: Response) -> None:
        set_cookies = parse_set_cookie_headers(response.headers)
        headers = self._clean_headers(response.headers)
        event = NetworkEvent(
            timestamp=datetime.now(timezone.utc).isoformat(),
            url=response.url,
            method=response.request.method,
            domain=safe_domain(response.url),
            status=response.status,
            response_headers=headers,
            has_request_cookie=bool(header_lookup(response.request.headers, "cookie")),
            has_set_cookie=bool(set_cookies),
        )
        self.session.summary.network_events_saved += 1
        self.writer.append_network_event(event)
        log_save(f"RES {response.status} {safe_domain(response.url)} {response.url[:80]}")

    def _save_cookie_event(self, event: CookieEvent) -> None:
        self.session.cookie_events.append(event)
        self.session.summary.cookie_events_saved += 1
        self.writer.append_cookie_event(event)
        names = ", ".join(str(item.get("name", "")) for item in event.cookies[:6])
        extra = f" +{len(event.cookies) - 6} more" if len(event.cookies) > 6 else ""
        log_save(
            f"{event.event_type} | {event.domain} | {event.method or '-':<6} "
            f"{event.url[:70]} | cookies={len(event.cookies)} [{names}{extra}]"
        )

    def _clean_headers(self, headers: dict[str, str]) -> dict[str, str]:
        clean: dict[str, str] = {}
        for name, value in headers.items():
            lower_name = name.lower()
            if not self.config.include_sensitive and lower_name in SENSITIVE_HEADERS:
                clean[name] = "<redacted>"
            else:
                clean[name] = value
        return clean

    async def _snapshot_cookie_jar(self) -> None:
        if not self.browser.contexts:
            return

        context = self.browser.contexts[0]
        if not context.pages:
            return

        page = context.pages[0]
        try:
            cdp = await context.new_cdp_session(page)
            result = await cdp.send("Storage.getCookies")
            self.session.cookie_jar_snapshot = result.get("cookies", [])
            await cdp.detach()
            log_info(f"Cookie jar snapshot: {len(self.session.cookie_jar_snapshot)} cookies")
        except Exception as exc:
            log_skip(f"Cookie jar snapshot skipped: {exc}")

    async def _periodic_flush(self) -> None:
        while not self._stop_event.is_set():
            try:
                await asyncio.wait_for(self._stop_event.wait(), timeout=self.config.flush_interval_s)
            except TimeoutError:
                self.writer.write_session(self.session)

    async def wait_for_stop(self) -> None:
        loop = asyncio.get_running_loop()
        for sig in (signal.SIGINT, signal.SIGTERM):
            try:
                loop.add_signal_handler(sig, self._stop_event.set)
            except NotImplementedError:
                pass

        while not self._stop_event.is_set():
            for context in self.browser.contexts:
                await self._attach_context(context)
            try:
                await asyncio.wait_for(self._stop_event.wait(), timeout=1.0)
            except TimeoutError:
                continue

    async def finalize(self) -> None:
        self._stop_event.set()
        if self._flush_task is not None:
            self._flush_task.cancel()
            try:
                await self._flush_task
            except asyncio.CancelledError:
                pass

        await self._snapshot_cookie_jar()
        self.session.status = "completed"
        self.session.ended_at = datetime.now(timezone.utc)
        self.writer.write_session(self.session)

        summary = self.session.summary
        if self.config.cookie_only:
            log_info(
                "Stopped — "
                f"cookie_events={summary.cookie_events_saved}, "
                f"skipped={summary.requests_skipped}, "
                f"seen={summary.total_requests_seen}"
            )
        else:
            log_info(
                "Stopped — "
                f"network_events={summary.network_events_saved}, "
                f"seen={summary.total_requests_seen}"
            )
        log_info(f"Session saved: {self.writer.session_path}")
