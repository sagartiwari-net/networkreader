# Own Network Fetcher (ONF)

Python + Playwright based session capture tool.

> Reference: `../Friend/` | Plan: `../PLAN.md` | Build: `BUILD.md`

## Status — Phase 1 (`v0.1.0-phase1`)

| Feature | Status |
|---------|--------|
| Cookie-only mode (default) | ✅ |
| Full mode (`--all-requests`) | ✅ |
| JSON + ndjson output | ✅ |
| Chrome CDP connect | ✅ |
| Windows `onf.exe` build script | ✅ |
| Multi-context isolation | Phase 2 |

---

## RDP par use (recommended — .exe)

### 1) Windows par build karo (ek baar)

```cmd
cd own-network-fetcher
scripts\build_windows.bat
```

Output: **`Network Reader\onf.exe`** (root folder — yahi copy karo)

### 2) RDP par paste karo

Root se `onf.exe` uthao ya apne folder mein paste karo, e.g. `D:\tools\onf\`

### 3) Chrome kholo (manual — debug port)

```cmd
chrome.exe --remote-debugging-port=9222 --remote-allow-origins=*
```

Already open Chrome agar debug port par hai to naya launch ki zaroorat nahi.

### 4) ONF chalao

**Double-click** `onf.exe` — default cookie mode, port 9222.

Ya CMD se:

```cmd
onf.exe
onf.exe --all-requests
onf.exe --task-id semrush_test --chrome-port 9222
onf.exe --no-pause
```

### 5) Browse + stop

Chrome mein site kholo → login/browse → **Ctrl+C** → Enter dabao window band karne se pehle.

**Output (exe ke bagal):**

```
captures/sessions/{task_id}/
├── session.json
├── cookies.ndjson
└── network.ndjson   (sirf --all-requests)
```

---

## Dev run (Mac / Windows — source)

```bash
cd own-network-fetcher
python -m venv .venv
source .venv/bin/activate        # Windows: .venv\Scripts\activate
pip install -r requirements.txt
PYTHONPATH=src python -m onf
```

---

## Capture modes

| Mode | Command | Kya save hota hai |
|------|---------|-------------------|
| Cookie only | `onf.exe` | Sirf Cookie / Set-Cookie traffic |
| Full | `onf.exe --all-requests` | Saari network requests |

---

## Har phase ke baad

Phase complete → `scripts\build_windows.bat` → `releases\onf.exe` RDP par copy.

Details: `BUILD.md` + `releases/README.md`
