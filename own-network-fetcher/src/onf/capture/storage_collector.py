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
  try {
    const dbList = await indexedDB.databases();
    for (const meta of dbList) {
      const dbName = meta.name;
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
          }
        };
      });
    }
  } catch (e) {}
  return result;
})()
"""


def origin_of(url: str) -> str | None:
    parsed = urlparse(url)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        return None
    port = parsed.port
    if port and not ((parsed.scheme == "http" and port == 80) or (parsed.scheme == "https" and port == 443)):
        return f"{parsed.scheme}://{parsed.hostname}:{port}"
    return f"{parsed.scheme}://{parsed.hostname}"


class StorageCollector:
    """Sync CDP helper used during session finalize."""

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

    def _command(
        self,
        method: str,
        params: dict[str, Any] | None = None,
        session_id: str | None = None,
    ) -> dict[str, Any]:
        msg_id = self._send(self.ws, method, params, session_id=session_id)
        deadline = time.time() + self._timeout_s
        while time.time() < deadline:
            self.ws.settimeout(1.0)
            try:
                raw = self.ws.recv()
            except Exception:
                continue
            if not raw:
                continue
            payload = json.loads(raw)
            if payload.get("id") == msg_id:
                if payload.get("error"):
                    raise RuntimeError(f"{method} failed: {payload['error']}")
                return payload.get("result", {})
        raise TimeoutError(f"CDP command timed out: {method}")

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

    def evaluate(self, session_id: str, expression: str, *, await_promise: bool = False) -> Any:
        try:
            self._command("Runtime.enable", session_id=session_id)
        except Exception:
            pass
        result: dict[str, Any] = {}
        try:
            result = self._command(
                "Runtime.evaluate",
                {
                    "expression": expression,
                    "returnByValue": True,
                    "awaitPromise": await_promise,
                },
                session_id=session_id,
            )
        finally:
            try:
                self._command("Runtime.disable", session_id=session_id)
            except Exception:
                pass
        remote = result.get("result", {})
        if remote.get("type") == "undefined":
            return None
        if "value" in remote:
            return remote["value"]
        return None

    def collect_dom_storage(self, session_id: str) -> tuple[dict[str, str], dict[str, str]]:
        raw = self.evaluate(session_id, STORAGE_EVAL_JS)
        if not isinstance(raw, dict):
            return {}, {}
        local_storage = raw.get("localStorage") if isinstance(raw.get("localStorage"), dict) else {}
        session_storage = raw.get("sessionStorage") if isinstance(raw.get("sessionStorage"), dict) else {}
        return dict(local_storage), dict(session_storage)

    def collect_indexed_db(self, session_id: str) -> dict[str, Any]:
        raw = self.evaluate(session_id, INDEXED_DB_DUMP_JS, await_promise=True)
        return dict(raw) if isinstance(raw, dict) else {}
