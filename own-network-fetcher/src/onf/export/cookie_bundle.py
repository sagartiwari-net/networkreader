"""Cookie / storage export JSON matching external system format."""

from __future__ import annotations

from typing import Any
from urllib.parse import urlparse


def _same_site_value(raw: str | None) -> str | None:
    if not raw:
        return None
    lower = raw.lower()
    if lower in {"strict", "lax", "none"}:
        return lower
    return raw


def cdp_cookie_to_export(entry: dict[str, Any]) -> dict[str, Any]:
    domain = entry.get("domain", "")
    expires = entry.get("expires", -1)
    session = bool(entry.get("session", expires <= 0))
    return {
        "domain": domain,
        "expirationDate": None if session else float(expires),
        "hostOnly": not str(domain).startswith("."),
        "httpOnly": bool(entry.get("httpOnly", False)),
        "name": entry.get("name", ""),
        "path": entry.get("path", "/"),
        "sameSite": _same_site_value(entry.get("sameSite")),
        "secure": bool(entry.get("secure", False)),
        "session": session,
        "storeId": "0",
        "value": entry.get("value", ""),
    }


def cookies_for_referer(all_cookies: list[dict[str, Any]], referer: str) -> list[dict[str, Any]]:
    host = (urlparse(referer).hostname or "").lower()
    if not host:
        return []
    matched: list[dict[str, Any]] = []
    for raw in all_cookies:
        cookie_domain = str(raw.get("domain", "")).lower()
        bare = cookie_domain.lstrip(".")
        if host == bare or host.endswith("." + bare):
            matched.append(cdp_cookie_to_export(raw))
            continue
        if cookie_domain.startswith(".") and host.endswith(cookie_domain):
            matched.append(cdp_cookie_to_export(raw))
    return matched


def build_export_payload(
    *,
    referer: str,
    http_cookies: list[dict[str, Any]] | None = None,
    local_storage: dict[str, str] | None = None,
    session_storage: dict[str, str] | None = None,
    indexed_db: dict[str, Any] | None = None,
) -> dict[str, Any]:
    included: list[str] = []
    payload: dict[str, Any] = {"referer": referer, "includedFormats": included}

    if http_cookies is not None:
        cookies = cookies_for_referer(http_cookies, referer)
        if cookies:
            included.append("cookies")
            payload["cookies"] = cookies

    storage_block: dict[str, Any] = {}
    if local_storage:
        included.append("localStorage")
        storage_block["localStorage"] = local_storage
    if session_storage:
        included.append("sessionStorage")
        storage_block["sessionStorage"] = session_storage
    if storage_block:
        payload["storage"] = storage_block

    if indexed_db:
        included.append("indexedDB")
        payload["indexedDB"] = indexed_db

    payload["includedFormats"] = included
    return payload


def build_http_cookie_payload(referer: str, http_cookies: list[dict[str, Any]]) -> dict[str, Any]:
    return build_export_payload(referer=referer, http_cookies=http_cookies)


def build_local_storage_payload(referer: str, local_storage: dict[str, str]) -> dict[str, Any]:
    return build_export_payload(referer=referer, local_storage=local_storage)


def build_session_storage_payload(referer: str, session_storage: dict[str, str]) -> dict[str, Any]:
    return build_export_payload(referer=referer, session_storage=session_storage)


def build_indexed_db_payload(referer: str, indexed_db: dict[str, Any]) -> dict[str, Any]:
    return build_export_payload(referer=referer, indexed_db=indexed_db)


def build_all_cookie_payload(
    *,
    referer: str,
    http_cookies: list[dict[str, Any]],
    local_storage: dict[str, str] | None = None,
    session_storage: dict[str, str] | None = None,
    indexed_db: dict[str, Any] | None = None,
) -> dict[str, Any]:
    return build_export_payload(
        referer=referer,
        http_cookies=http_cookies,
        local_storage=local_storage,
        session_storage=session_storage,
        indexed_db=indexed_db,
    )
