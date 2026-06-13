"""Write per-site export files under each session task folder."""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

from onf.config import CaptureMode
from onf.export.cookie_bundle import (
    build_all_cookie_payload,
    build_http_cookie_payload,
    build_indexed_db_payload,
    build_local_storage_payload,
    build_session_storage_payload,
)
from onf.models.session import CookieEvent, FullNetworkRecord, NetworkEvent, SessionModel


def safe_site_name(domain: str) -> str:
    cleaned = re.sub(r"[^a-zA-Z0-9._-]+", "_", domain.strip().lower())
    return cleaned or "unknown"


class SessionWriter:
    def __init__(self, session_dir: Path, capture_mode: CaptureMode) -> None:
        self.session_dir = session_dir
        self.capture_mode = capture_mode
        self.session_file = session_dir / "session.json"
        self.cookies_file = session_dir / "cookies.ndjson"
        self.network_file = session_dir / "network.ndjson"
        self.session_dir.mkdir(parents=True, exist_ok=True)

    def site_dir(self, domain: str) -> Path:
        path = self.session_dir / safe_site_name(domain)
        path.mkdir(parents=True, exist_ok=True)
        return path

    def write_site_json(self, domain: str, filename: str, payload: dict[str, Any]) -> Path:
        path = self.site_dir(domain) / filename
        path.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        return path

    def append_cookie_event(self, event: CookieEvent) -> None:
        with self.cookies_file.open("a", encoding="utf-8") as handle:
            handle.write(event.model_dump_json() + "\n")

    def append_network_event(self, event: NetworkEvent) -> None:
        with self.network_file.open("a", encoding="utf-8") as handle:
            handle.write(event.model_dump_json() + "\n")

    def append_full_network_record(self, record: FullNetworkRecord) -> None:
        target = self.site_dir(record.domain) / "network.ndjson"
        with target.open("a", encoding="utf-8") as handle:
            handle.write(record.model_dump_json() + "\n")

    def write_site_cookie_exports(
        self,
        *,
        domain: str,
        referer: str,
        http_cookies: list[dict[str, Any]],
        local_storage: dict[str, str],
        session_storage: dict[str, str],
        indexed_db: dict[str, Any],
    ) -> list[Path]:
        written: list[Path] = []
        self.site_dir(domain)

        http_payload = build_http_cookie_payload(referer, http_cookies)
        written.append(self.write_site_json(domain, "cookies.http.json", http_payload))

        if local_storage:
            written.append(
                self.write_site_json(
                    domain,
                    "localStorage.json",
                    build_local_storage_payload(referer, local_storage),
                )
            )

        if session_storage:
            written.append(
                self.write_site_json(
                    domain,
                    "sessionStorage.json",
                    build_session_storage_payload(referer, session_storage),
                )
            )

        if indexed_db:
            written.append(
                self.write_site_json(
                    domain,
                    "indexedDB.json",
                    build_indexed_db_payload(referer, indexed_db),
                )
            )

        all_payload = build_all_cookie_payload(
            referer=referer,
            http_cookies=http_cookies,
            local_storage=local_storage or None,
            session_storage=session_storage or None,
            indexed_db=indexed_db or None,
        )
        if not all_payload.get("includedFormats"):
            all_payload = {"referer": referer, "includedFormats": ["cookies"], "cookies": []}
        written.append(self.write_site_json(domain, "cookies.all.json", all_payload))

        return written

    def write_session(self, session: SessionModel) -> None:
        payload = session.model_dump(mode="json")
        self.session_file.write_text(
            json.dumps(payload, indent=2, ensure_ascii=False) + "\n",
            encoding="utf-8",
        )

    @property
    def session_path(self) -> Path:
        return self.session_file

    @property
    def sites_root(self) -> Path:
        return self.session_dir
