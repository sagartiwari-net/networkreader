"""Collect cookies, DOM storage, and IndexedDB via CDP."""

from __future__ import annotations

import json
import time
from collections.abc import Callable
from typing import Any
from urllib.parse import urlparse

STORAGE_EVAL_JS = """
(() => {
  const out = { localStorage: {}, sessionStorage: {} };
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (key != null) out.localStorage[key] = localStorage.getItem(key);
    }
  } catch (e) {}
  try {
    for (let i = 0; i < sessionStorage.length; i++) {
      const key = sessionStorage.key(i);
      if (key != null) out.sessionStorage[key] = sessionStorage.getItem(key);
    }
  } catch (e) {}
  return out;
})()
"""

INDEXED_DB_PROBE_JS = """
(async () => {
  try {
    if (typeof indexedDB.databases !== "function") return [];
    const dbList = await indexedDB.databases();
    return (dbList || []).map((d) => d && d.name).filter(Boolean);
  } catch (e) {
    return [];
  }
})()
"""

INDEXED_DB_DUMP_JS = """
(async (names) => {
  const result = {};
  try {
    for (const dbName of names) {
      if (!dbName) continue;
      await new Promise((resolve) => {
        const request = indexedDB.open(dbName);
        request.onerror = () => resolve();
        request.onsuccess = () => {
          const db = request.result;
          result[dbName] = { stores: {} };
          const storeNames = [...db.objectStoreNames];
          let pending = storeNames.length;
          if (!pending) {
            db.close();
            resolve();
            return;
          }
          for (const storeName of storeNames) {
            try {
              const tx = db.transaction(storeName, "readonly");
              const store = tx.objectStore(storeName);
              const getAll = store.getAll();
              getAll.onsuccess = () => {
                result[dbName].stores[storeName] = getAll.result;
                pending -= 1;
                if (pending === 0) {
                  db.close();
                  resolve();
                }
              };
              getAll.onerror = () => {
                pending -= 1;
                if (pending === 0) {
                  db.close();
                  resolve();
                }
              };
            } catch (e) {
              pending -= 1;
              if (pending === 0) {
                db.close();
                resolve();
              }
            }
          }
        };
      });
    }
  } catch (e) {}
  return result;
})
"""


def security_origin(url: str) -> str | None:
    parsed = urlparse(url)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        return None
    port = parsed.port
    if port and not (
        (parsed.scheme == "http" and port == 80) or (parsed.scheme == "https" and port == 443)
    ):
        return f"{parsed.scheme}://{parsed.hostname}:{port}"
    return f"{parsed.scheme}://{parsed.hostname}"


class StorageCollector:
    """Sync CDP helper — forwards unrelated events while waiting for responses."""

    def __init__(
        self,
        ws: Any,
        send: Callable[..., int],
        *,
        timeout_s: float = 15.0,
        on_event: Callable[[dict[str, Any]], None] | None = None,
    ) -> None:
        self.ws = ws
        self._send = send
        self._timeout_s = timeout_s
        self._on_event = on_event
        self._dom_storage_enabled: set[str] = set()
        self._runtime_enabled: set[str] = set()

    def _drain_event(self, payload: dict[str, Any], msg_id: int) -> dict[str, Any] | None:
        if payload.get("id") == msg_id:
            if payload.get("error"):
                raise RuntimeError(f"CDP error: {payload['error']}")
            return payload.get("result", {})
        return None

    def _command(
        self,
        method: str,
        params: dict[str, Any] | None = None,
        session_id: str | None = None,
        *,
        timeout_s: float | None = None,
    ) -> dict[str, Any]:
        msg_id = self._send(self.ws, method, params, session_id=session_id)
        deadline = time.time() + (timeout_s if timeout_s is not None else self._timeout_s)
        while time.time() < deadline:
            self.ws.settimeout(1.0)
            try:
                raw = self.ws.recv()
            except Exception:
                continue
            if not raw:
                continue
            payload = json.loads(raw)
            if payload.get("method") and self._on_event:
                self._on_event(payload)
                continue
            result = self._drain_event(payload, msg_id)
            if result is not None:
                return result
        raise TimeoutError(f"CDP command timed out: {method}")

    def _ensure_runtime(self, session_id: str) -> None:
        if session_id in self._runtime_enabled:
            return
        self._command("Runtime.enable", session_id=session_id, timeout_s=5.0)
        self._runtime_enabled.add(session_id)

    def _disable_runtime(self, session_id: str) -> None:
        if session_id not in self._runtime_enabled:
            return
        try:
            self._command("Runtime.disable", session_id=session_id, timeout_s=3.0)
        except Exception:
            pass
        self._runtime_enabled.discard(session_id)

    def get_cookies(self) -> list[dict[str, Any]]:
        for method in ("Storage.getCookies", "Network.getAllCookies"):
            try:
                result = self._command(method, timeout_s=8.0)
            except Exception:
                continue
            cookies = result.get("cookies", [])
            if cookies or method == "Network.getAllCookies":
                return list(cookies)
        return []

    def _ensure_dom_storage(self, session_id: str) -> None:
        if session_id in self._dom_storage_enabled:
            return
        self._command("DOMStorage.enable", session_id=session_id, timeout_s=5.0)
        self._dom_storage_enabled.add(session_id)

    def _dom_storage_items(
        self,
        session_id: str,
        *,
        origin: str,
        is_local_storage: bool,
    ) -> dict[str, str]:
        self._ensure_dom_storage(session_id)
        result = self._command(
            "DOMStorage.getDOMStorageItems",
            {
                "storageId": {
                    "securityOrigin": origin,
                    "isLocalStorage": is_local_storage,
                }
            },
            session_id=session_id,
            timeout_s=8.0,
        )
        entries = result.get("entries", [])
        items: dict[str, str] = {}
        for entry in entries:
            if isinstance(entry, list) and len(entry) >= 2:
                items[str(entry[0])] = str(entry[1])
        return items

    def collect_dom_storage_via_dom_storage(
        self,
        session_id: str,
        origin: str,
    ) -> tuple[dict[str, str], dict[str, str]]:
        local_storage = self._dom_storage_items(
            session_id,
            origin=origin,
            is_local_storage=True,
        )
        session_storage = self._dom_storage_items(
            session_id,
            origin=origin,
            is_local_storage=False,
        )
        return local_storage, session_storage

    def evaluate(
        self,
        session_id: str,
        expression: str,
        *,
        await_promise: bool = False,
        use_runtime: bool = True,
        timeout_s: float | None = None,
        args: list[Any] | None = None,
    ) -> Any:
        if use_runtime:
            self._ensure_runtime(session_id)
        expr = expression
        if args is not None:
            expr = f"({expression})({json.dumps(args)})"
        try:
            result = self._command(
                "Runtime.evaluate",
                {
                    "expression": expr,
                    "returnByValue": True,
                    "awaitPromise": await_promise,
                },
                session_id=session_id,
                timeout_s=timeout_s,
            )
        except Exception:
            return None
        if result.get("exceptionDetails"):
            return None
        remote = result.get("result", {})
        if remote.get("type") == "undefined":
            return None
        if "value" in remote:
            return remote["value"]
        return None

    def collect_dom_storage(self, session_id: str, *, origin: str | None = None) -> tuple[dict[str, str], dict[str, str]]:
        if origin:
            try:
                return self.collect_dom_storage_via_dom_storage(session_id, origin)
            except Exception:
                pass

        raw = self.evaluate(session_id, STORAGE_EVAL_JS, timeout_s=8.0)
        if not isinstance(raw, dict):
            return {}, {}
        local_storage = raw.get("localStorage") if isinstance(raw.get("localStorage"), dict) else {}
        session_storage = raw.get("sessionStorage") if isinstance(raw.get("sessionStorage"), dict) else {}
        return dict(local_storage), dict(session_storage)

    def collect_indexed_db(self, session_id: str) -> dict[str, Any]:
        try:
            self._ensure_runtime(session_id)
            names = self.evaluate(
                session_id,
                INDEXED_DB_PROBE_JS,
                await_promise=True,
                use_runtime=False,
                timeout_s=8.0,
            )
            if not isinstance(names, list) or not names:
                return {}
            raw = self.evaluate(
                session_id,
                INDEXED_DB_DUMP_JS,
                await_promise=True,
                use_runtime=False,
                timeout_s=20.0,
                args=[names],
            )
        finally:
            self._disable_runtime(session_id)

        if not isinstance(raw, dict):
            return {}
        return dict(raw)
