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

INDEXED_DB_DUMP_JS = """
(async () => {
  const result = {};
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  try {
    let dbList = [];
    if (typeof indexedDB.databases === "function") {
      dbList = await indexedDB.databases();
    }
    if (!dbList.length) {
      return result;
    }
    for (const meta of dbList) {
      const dbName = meta && meta.name;
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
      await sleep(0);
    }
  } catch (e) {
    result.__error = String(e && e.message ? e.message : e);
  }
  return result;
})()
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
    """Sync CDP helper — prefers DOMStorage over Runtime during live capture."""

    def __init__(
        self,
        ws: Any,
        send: Callable[..., int],
        *,
        timeout_s: float = 15.0,
    ) -> None:
        self.ws = ws
        self._send = send
        self._timeout_s = timeout_s
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
            result = self._drain_event(payload, msg_id)
            if result is not None:
                return result
        raise TimeoutError(f"CDP command timed out: {method}")

    def _ensure_runtime(self, session_id: str) -> None:
        if session_id in self._runtime_enabled:
            return
        self._command("Runtime.enable", session_id=session_id)
        self._runtime_enabled.add(session_id)

    def _disable_runtime(self, session_id: str) -> None:
        if session_id not in self._runtime_enabled:
            return
        try:
            self._command("Runtime.disable", session_id=session_id, timeout_s=5.0)
        except Exception:
            pass
        self._runtime_enabled.discard(session_id)

    def get_cookies(self) -> list[dict[str, Any]]:
        for method in ("Storage.getCookies", "Network.getAllCookies"):
            try:
                result = self._command(method)
            except Exception:
                continue
            cookies = result.get("cookies", [])
            if cookies or method == "Network.getAllCookies":
                return list(cookies)
        return []

    def _ensure_dom_storage(self, session_id: str) -> None:
        if session_id in self._dom_storage_enabled:
            return
        self._command("DOMStorage.enable", session_id=session_id)
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
        try:
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
        except Exception:
            return {}, {}

    def evaluate(
        self,
        session_id: str,
        expression: str,
        *,
        await_promise: bool = False,
        use_runtime: bool = True,
        timeout_s: float | None = None,
    ) -> Any:
        if use_runtime:
            self._ensure_runtime(session_id)
        try:
            result = self._command(
                "Runtime.evaluate",
                {
                    "expression": expression,
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
            local_storage, session_storage = self.collect_dom_storage_via_dom_storage(
                session_id,
                origin,
            )
            if local_storage or session_storage:
                return local_storage, session_storage

        raw = self.evaluate(session_id, STORAGE_EVAL_JS)
        if not isinstance(raw, dict):
            return {}, {}
        local_storage = raw.get("localStorage") if isinstance(raw.get("localStorage"), dict) else {}
        session_storage = raw.get("sessionStorage") if isinstance(raw.get("sessionStorage"), dict) else {}
        return dict(local_storage), dict(session_storage)

    def collect_indexed_db(self, session_id: str) -> dict[str, Any]:
        try:
            self._ensure_runtime(session_id)
            raw = self.evaluate(
                session_id,
                INDEXED_DB_DUMP_JS,
                await_promise=True,
                use_runtime=False,
                timeout_s=45.0,
            )
        finally:
            self._disable_runtime(session_id)

        if not isinstance(raw, dict):
            return {}
        cleaned = {key: value for key, value in raw.items() if not str(key).startswith("__")}
        return cleaned
