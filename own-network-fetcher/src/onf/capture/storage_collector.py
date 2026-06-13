"""Collect cookies, DOM storage, and IndexedDB via CDP."""

from __future__ import annotations

import json
import time
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

    def __init__(self, ws: Any, send: Any, next_id_ref: list[int]) -> None:
        self.ws = ws
        self._send = send
        self._next_id = next_id_ref
        self._pending: dict[int, str] = {}

    def _next_message_id(self) -> int:
        msg_id = self._next_id[0]
        self._next_id[0] += 1
        return msg_id

    def _command(self, method: str, params: dict[str, Any] | None = None, session_id: str | None = None) -> dict[str, Any]:
        msg_id = self._next_message_id()
        message: dict[str, Any] = {"id": msg_id, "method": method}
        if params is not None:
            message["params"] = params
        if session_id:
            message["sessionId"] = session_id
        self._pending[msg_id] = method
        self.ws.send(json.dumps(message))

        deadline = time.time() + 8.0
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
            if payload.get("id") in self._pending:
                self._pending.pop(payload["id"], None)
        raise TimeoutError(f"CDP command timed out: {method}")

    def get_cookies(self) -> list[dict[str, Any]]:
        result = self._command("Storage.getCookies")
        return list(result.get("cookies", []))

    def evaluate(self, session_id: str, expression: str, *, await_promise: bool = False) -> Any:
        result = self._command(
            "Runtime.evaluate",
            {
                "expression": expression,
                "returnByValue": True,
                "awaitPromise": await_promise,
            },
            session_id=session_id,
        )
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
