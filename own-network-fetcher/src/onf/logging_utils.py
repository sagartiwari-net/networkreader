"""Console logging helpers."""

from __future__ import annotations


def log_info(message: str) -> None:
    print(f"[INFO] {message}", flush=True)


def log_save(message: str) -> None:
    print(f"[SAVE] {message}", flush=True)


def log_skip(message: str) -> None:
    print(f"[SKIP] {message}", flush=True)
