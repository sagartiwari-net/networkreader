"""Resolve paths for dev runs and frozen .exe builds."""

from __future__ import annotations

import os
import sys
from pathlib import Path


def is_frozen() -> bool:
    return bool(getattr(sys, "frozen", False))


def app_dir() -> Path:
    """Folder containing the running app (.exe dir when frozen)."""
    if is_frozen():
        return Path(sys.executable).resolve().parent
    return Path.cwd()


def default_output_dir() -> Path:
    return app_dir() / "captures"


def configure_frozen_runtime() -> None:
    """Prepare Playwright/CDP when running as a PyInstaller bundle."""
    if not is_frozen():
        return

    # connect_over_cdp uses system Chrome — bundled browsers not required.
    os.environ.setdefault("PLAYWRIGHT_BROWSERS_PATH", "0")
