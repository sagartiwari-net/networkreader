"""Persist session JSON and optional NDJSON streams."""

from __future__ import annotations

import json
from pathlib import Path

from onf.config import CaptureMode
from onf.models.session import CookieEvent, NetworkEvent, SessionModel


class SessionWriter:
    def __init__(self, session_dir: Path, capture_mode: CaptureMode) -> None:
        self.session_dir = session_dir
        self.capture_mode = capture_mode
        self.session_file = session_dir / "session.json"
        self.cookies_file = session_dir / "cookies.ndjson"
        self.network_file = session_dir / "network.ndjson"
        self.session_dir.mkdir(parents=True, exist_ok=True)

    def append_cookie_event(self, event: CookieEvent) -> None:
        with self.cookies_file.open("a", encoding="utf-8") as handle:
            handle.write(event.model_dump_json() + "\n")

    def append_network_event(self, event: NetworkEvent) -> None:
        with self.network_file.open("a", encoding="utf-8") as handle:
            handle.write(event.model_dump_json() + "\n")

    def write_session(self, session: SessionModel) -> None:
        payload = session.model_dump(mode="json")
        self.session_file.write_text(
            json.dumps(payload, indent=2, ensure_ascii=False) + "\n",
            encoding="utf-8",
        )

    @property
    def session_path(self) -> Path:
        return self.session_file
