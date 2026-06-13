"""Print package version for build scripts."""

from __future__ import annotations

import re
from pathlib import Path

init_file = Path(__file__).resolve().parent.parent / "src" / "onf" / "__init__.py"
text = init_file.read_text(encoding="utf-8")
match = re.search(r'__version__ = "(.+?)"', text)
if not match:
    raise SystemExit("Could not read __version__ from onf/__init__.py")
print(match.group(1))
