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


def referer_host(referer: str) -> str:
    return (urlparse(referer).hostname or "").lower()


def cookie_domain_for_referer(referer: str) -> str:
    host = referer_host(referer)
    if not host:
        return ""
    parts = host.split(".")
    if len(parts) >= 2:
        return "." + ".".join(parts[-2:])
    return host


def cookie_matches_referer(cookie_domain: str, referer: str) -> bool:
    host = referer_host(referer)
    if not host:
        return False
    bare = str(cookie_domain).lower().lstrip(".")
    if host == bare or host.endswith("." + bare):
        return True
    if str(cookie_domain).startswith(".") and host.endswith(str(cookie_domain).lower()):
        return True
    return False


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
    if not all_cookies:
        return []
    matched: list[dict[str, Any]] = []
    for raw in all_cookies:
        if cookie_matches_referer(str(raw.get("domain", "")), referer):
            matched.append(cdp_cookie_to_export(raw))
    return matched


def _attrs_get(attributes: dict[str, Any], key: str, default: Any = None) -> Any:
    for attr_key, value in attributes.items():
        if str(attr_key).lower() == key.lower():
            return value
    return default


def _event_belongs_to_site(*, event_url: str, event_domain: str, site_domain: str) -> bool:
    site = site_domain.lower()
    if event_domain.lower() == site:
        return True
    host = (urlparse(event_url).hostname or "").lower()
    return host == site


def _set_cookie_to_cdp(raw: dict[str, Any], *, fallback_domain: str) -> dict[str, Any]:
    attrs = raw.get("attributes") if isinstance(raw.get("attributes"), dict) else {}
    domain = str(attrs.get("domain") or raw.get("domain") or fallback_domain)
    expires_raw = _attrs_get(attrs, "expires")
    session = expires_raw in (None, "", True) and _attrs_get(attrs, "max-age") is None
    expires = -1 if session else -1
    if expires_raw not in (None, "", True):
        try:
            expires = float(expires_raw)
            session = expires <= 0
        except (TypeError, ValueError):
            session = True
            expires = -1

    same_site = _attrs_get(attrs, "samesite")
    return {
        "domain": domain,
        "name": str(raw.get("name", "")),
        "value": str(raw.get("value", "")),
        "path": str(_attrs_get(attrs, "path") or raw.get("path") or "/"),
        "expires": expires,
        "httpOnly": bool(_attrs_get(attrs, "httponly", False)),
        "secure": bool(_attrs_get(attrs, "secure", False)),
        "session": session,
        "sameSite": str(same_site) if same_site is not None else None,
    }


def events_to_cdp_cookies(events: list[Any], *, referer: str, site_domain: str) -> list[dict[str, Any]]:
    """Rebuild CDP-shaped cookies from live capture events for one site folder."""
    fallback_domain = cookie_domain_for_referer(referer) or site_domain
    merged: dict[tuple[str, str], dict[str, Any]] = {}

    for event in events:
        event_url = getattr(event, "url", "")
        event_domain = getattr(event, "domain", "")
        if not _event_belongs_to_site(
            event_url=event_url,
            event_domain=event_domain,
            site_domain=site_domain,
        ):
            continue

        for raw in getattr(event, "cookies", []) or []:
            if not isinstance(raw, dict):
                continue
            if getattr(event, "event_type", "") == "set_cookie":
                entry = _set_cookie_to_cdp(raw, fallback_domain=fallback_domain)
            else:
                entry = {
                    "domain": fallback_domain,
                    "name": str(raw.get("name", "")),
                    "value": str(raw.get("value", "")),
                    "path": "/",
                    "expires": -1,
                    "httpOnly": False,
                    "secure": referer.startswith("https://"),
                    "session": True,
                    "sameSite": None,
                }
            if not entry.get("name"):
                continue
            key = (str(entry.get("domain", "")).lower(), str(entry["name"]))
            merged[key] = entry

    return list(merged.values())


def merge_cdp_cookies(*sources: list[dict[str, Any]]) -> list[dict[str, Any]]:
    merged: dict[tuple[str, str], dict[str, Any]] = {}
    for source in sources:
        for raw in source:
            key = (str(raw.get("domain", "")).lower(), str(raw.get("name", "")))
            if key[1]:
                merged[key] = raw
    return list(merged.values())


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
