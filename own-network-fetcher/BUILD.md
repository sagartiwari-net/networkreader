# Build Guide — onf.exe

## Rule (har phase ke baad)

Phase complete → Windows par build chalao → `releases/onf.exe` RDP par copy karo.

| Phase | Version tag | Build when |
|-------|-------------|------------|
| Phase 1 | `0.1.0-phase1` | Cookie mode + CDP connect ✅ |
| Phase 2 | `0.2.0-phase2` | Multi-context isolation |
| Phase 3 | `0.3.0-phase3` | Login detect + responses |
| Phase 4+ | bump version | Storage sync, etc. |

## Windows build (required for .exe)

```cmd
cd own-network-fetcher
scripts\build_windows.bat
```

Pehli baar 2–5 minute lag sakte hain (PyInstaller + Playwright bundle).

**Output location (copy ke liye):**

```
Network Reader/
├── onf.exe              ← yahan milega (root folder)
├── onf-0.1.0-phase1.exe ← versioned backup
└── own-network-fetcher/
    └── releases/onf.exe ← backup copy
```

## Dev run (Mac / Windows — bina .exe)

```bash
cd own-network-fetcher
python -m venv .venv
source .venv/bin/activate   # Windows: .venv\Scripts\activate
pip install -r requirements.txt
PYTHONPATH=src python -m onf
```

## Double-click behaviour (onf.exe)

- Koi argument nahi → **cookie-only mode**, port **9222**, output `captures\` folder (exe jahan ho wahan ke paas)
- Root par `onf.exe` rakho to captures `Network Reader\captures\` mein save honge
- Chrome pehle se open hona chahiye debug port par
- Band karne ke liye **Ctrl+C**
- Window band hone se pehle **Press Enter** (output padh sako)

CMD scripts ke liye: `onf.exe --no-pause`

## Size note

Playwright driver bundle ki wajah se `.exe` ~40–80 MB ho sakti hai. System Chrome use hota hai — alag browser install ki zaroorat nahi (`connect_over_cdp`).
