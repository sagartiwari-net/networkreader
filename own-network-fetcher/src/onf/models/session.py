"""Session JSON schema."""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any, Literal

from pydantic import BaseModel, Field

from onf.config import CaptureMode


class SessionSummary(BaseModel):
    cookie_events_saved: int = 0
    requests_skipped: int = 0
    network_events_saved: int = 0
    total_requests_seen: int = 0


class CookieEvent(BaseModel):
    timestamp: str
    event_type: Literal["request_cookie", "set_cookie"]
    url: str
    method: str | None = None
    domain: str
    cookies: list[dict[str, Any]] = Field(default_factory=list)
    cookie_header: str | None = None
    status: int | None = None


class NetworkEvent(BaseModel):
    timestamp: str
    url: str
    method: str
    domain: str
    status: int | None = None
    request_headers: dict[str, str] = Field(default_factory=dict)
    response_headers: dict[str, str] = Field(default_factory=dict)
    has_request_cookie: bool = False
    has_set_cookie: bool = False


class SessionModel(BaseModel):
    task_id: str
    capture_mode: CaptureMode
    started_at: datetime
    ended_at: datetime | None = None
    status: str = "running"
    chrome_cdp_url: str | None = None
    cookie_events: list[CookieEvent] = Field(default_factory=list)
    cookie_jar_snapshot: list[dict[str, Any]] = Field(default_factory=list)
    local_storage: dict[str, Any] = Field(default_factory=dict)
    session_storage: dict[str, Any] = Field(default_factory=dict)
    indexed_db: list[dict[str, Any]] = Field(default_factory=list)
    phases: list[dict[str, Any]] = Field(default_factory=list)
    summary: SessionSummary = Field(default_factory=SessionSummary)

    @classmethod
    def create(cls, *, task_id: str, capture_mode: CaptureMode, cdp_url: str) -> SessionModel:
        return cls(
            task_id=task_id,
            capture_mode=capture_mode,
            started_at=datetime.now(timezone.utc),
            chrome_cdp_url=cdp_url,
        )
