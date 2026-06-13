"""HTTP cookie parsing and normalization."""

from __future__ import annotations

import re
from typing import Any
from urllib.parse import urlparse


def sanitize_header_value(value: Any) -> str:
    if isinstance(value, list):
        text = ", ".join(str(item) for item in value)
    else:
        text = str(value)
    return text.replace("\r", " ").replace("\n", " ").strip()


def header_lookup(headers: dict[str, Any], name: str) -> str | None:
    target = name.lower()
    for key, value in headers.items():
        if str(key).lower().strip() == target:
            text = sanitize_header_value(value)
            return text or None
    return None


def extract_cookie_header(headers: dict[str, Any]) -> str | None:
    return header_lookup(headers, "cookie")


def parse_cookie_header(cookie_header: str) -> tuple[list[dict[str, str]], list[str]]:
    """Parse a Cookie request header into structured entries."""
    cookies: list[dict[str, str]] = []
    names: list[str] = []

    for part in cookie_header.split(";"):
        part = part.strip()
        if not part:
            continue
        if "=" in part:
            name, value = part.split("=", 1)
            name = name.strip()
            value = value.strip()
        else:
            name = part.strip()
            value = ""

        if not name:
            continue

        names.append(name)
        cookies.append({"name": name, "value": value})

    return cookies, names


def parse_set_cookie_header(set_cookie: str) -> dict[str, Any]:
    """Parse a single Set-Cookie header value."""
    parts = [piece.strip() for piece in set_cookie.split(";") if piece.strip()]
    if not parts:
        return {"name": "", "value": "", "attributes": {}}

    name_value = parts[0]
    if "=" in name_value:
        name, value = name_value.split("=", 1)
        name = name.strip()
        value = value.strip()
    else:
        name = name_value.strip()
        value = ""

    attributes: dict[str, str | bool] = {}
    for attr in parts[1:]:
        if "=" in attr:
            attr_name, attr_value = attr.split("=", 1)
            attributes[attr_name.strip().lower()] = attr_value.strip()
        else:
            attributes[attr.lower()] = True

    return {
        "name": name,
        "value": value,
        "attributes": attributes,
    }


def split_set_cookie_header_values(raw: str) -> list[str]:
    """Split merged Set-Cookie header text into individual cookie strings."""
    text = sanitize_header_value(raw)
    if not text:
        return []

    parts = re.split(r", (?=[A-Za-z_][\w-]*=)", text)
    return [part.strip() for part in parts if part.strip()]


def parse_set_cookie_headers(headers: dict[str, Any]) -> list[dict[str, Any]]:
    """Extract all Set-Cookie values from response headers."""
    raw = headers.get("set-cookie") or headers.get("Set-Cookie")
    if raw is None:
        return []

    if isinstance(raw, list):
        values: list[str] = []
        for item in raw:
            values.extend(split_set_cookie_header_values(sanitize_header_value(item)))
    else:
        values = split_set_cookie_header_values(sanitize_header_value(raw))

    return [parse_set_cookie_header(value) for value in values if value]


def safe_domain(url: str) -> str:
    try:
        host = (urlparse(url).hostname or "unknown").strip().lower()
    except Exception:
        host = "unknown"
    return host or "unknown"
