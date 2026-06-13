"""Entry point for python -m onf and PyInstaller."""

from __future__ import annotations

import sys
import traceback


def _pause_on_crash() -> None:
    try:
        input("\nPress Enter to exit...")
    except EOFError:
        pass


if __name__ == "__main__":
    try:
        from onf.main import main

        raise SystemExit(main())
    except SystemExit:
        raise
    except Exception:
        traceback.print_exc()
        _pause_on_crash()
        raise SystemExit(1)
