# networkreader

Browser session capture tool — network, cookies, storage → JSON.

## Repo layout

| Folder | Description |
|--------|-------------|
| `own-network-fetcher/` | Main project (Python + Playwright) — **active development** |
| `Friend/` | Phase 0 reference prototype |
| `PLAN.md` | Product roadmap |
| `BUILD-EXE.txt` | How to build `onf.exe` on Windows |

## Quick start (dev)

```bash
cd own-network-fetcher
python -m venv .venv
source .venv/bin/activate   # Windows: .venv\Scripts\activate
pip install -r requirements.txt
PYTHONPATH=src python -m onf
```

Chrome must run with remote debugging:

```cmd
chrome.exe --remote-debugging-port=9222 --remote-allow-origins=*
```

## Build Windows `.exe` (RDP)

```cmd
cd own-network-fetcher
scripts\build_windows.bat
```

Output: `onf.exe` in repo root (after build on Windows only).

See `own-network-fetcher/BUILD.md` for details.
