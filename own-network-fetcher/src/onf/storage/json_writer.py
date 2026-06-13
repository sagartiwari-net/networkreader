"""Persist session JSON and optional NDJSON streams."""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

from onf.config import CaptureMode
from onf.models.session import CookieEvent, FullNetworkRecord, NetworkEvent, SessionModel


def _safe_site_name(domain: str) -> str:
    cleaned = re.sub(r"[^a-zA-Z0-9._-]+", "_", domain.strip().lower())
    return cleaned or "unknown"


class SessionWriter:
    def __init__(self, session_dir: Path, capture_mode: CaptureMode) -> None:
        self.session_dir = session_dir
        self.capture_mode = capture_mode
        self.session_file = session_dir / "session.json"
        self.cookies_file = session_dir / "cookies.ndjson"
        self.network_file = session_dir / "network.ndjson"
        self.exports_dir = session_dir / "exports"
        self.by_site_dir = session_dir / "by_site"
        self.session_dir.mkdir(parents=True, exist_ok=True)

    def append_cookie_event(self, event: CookieEvent) -> None:
        with self.cookies_file.open("a", encoding="utf-8") as handle:
            handle.write(event.model_dump_json() + "\n")

    def append_network_event(self, event: NetworkEvent) -> None:
        with self.network_file.open("a", encoding="utf-8") as handle:
            handle.write(event.model_dump_json() + "\n")

    def append_full_network_record(self, record: FullNetworkRecord) -> None:
        site_dir = self.by_site_dir / _safe_site_name(record.domain)
        site_dir.mkdir(parents=True, exist_ok=True)
        target = site_dir / "network.ndjson"
        with target.open("a", encoding="utf-8") as handle:
            handle.write(record.model_dump_json() + "\n")

    def write_cookie_export(self, domain: str, payload: dict[str, Any]) -> Path:
        self.exports_dir.mkdir(parents=True, exist_ok=True)
        path = self.exports_dir / f"{_safe_site_name(domain)}.json"
        path.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        return path

    def write_session(self, session: SessionModel) -> None:
        payload = session.model_dump(mode="json")
        self.session_file.write_text(
            json.dumps(payload, indent=2, ensure_ascii=False) + "\n",
            encoding="utf-8",
        )

    @property
    def session_path(self) -> Path:
        return self.session_file
