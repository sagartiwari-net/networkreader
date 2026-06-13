# Own Network Fetcher (ONF) — Product Plan

> **Status:** Phase 2 (manual capture) — in progress  
> **Stack decision:** Python + CDP WebSocket (`websocket-client`) + PyInstaller  
> **Main codebase:** `own-network-fetcher/`  
> **Reference prototype:** `Friend/` (Phase 0 — read-only reference)  
> **Last updated:** June 2026

---

## ★ Current focus (manual workflow)

Abhi **automation aur parallel tasks bilkul nahi**. Aap khud Chrome mein browse karoge; ONF sirf capture + export karega.

### Double-click / launcher — 2 options

| Option | Mode | Output |
|--------|------|--------|
| **1 — Full network scan** | `--full-network` | Har website alag folder: `by_site/{domain}/network.ndjson` — request headers/body, response status/headers/body |
| **2 — Cookie scan only** | `--cookie-export` (default) | Ctrl+C par export: `exports/{domain}.json` — aapke external system ke format mein (`referer`, `includedFormats`, `cookies`, `storage`, `indexedDB`) |

Launcher: `Start ONF.bat` ya `onf.exe` double-click (menu dikhega).

### Revised roadmap (priority order)

| Phase | Focus | Status |
|-------|--------|--------|
| **1** | CDP connect, basic capture, Windows `.exe` | ✅ Done |
| **2** | 2-option menu, duplicate fix, cookie export JSON, full network per-site | 🔄 In progress |
| **3** | Browser context isolation, better per-tab tracking | ⏸ Later |
| **4** | Parallel tasks (2–6 contexts) | ⏸ Later |
| **5** | Remote server sync, login detect | ⏸ Later |
| **Last** | Browser automation (navigate, fill forms) | ⏸ Sabse last |

### Output layout (Phase 2)

```
captures/sessions/{task_id}/
├── session.json
├── cookies.ndjson              # live cookie events (option 2)
├── exports/
│   └── jasper.ai.json          # final cookie bundle (option 2, on stop)
└── by_site/
    └── app.jasper.ai/
        └── network.ndjson      # detailed traffic (option 1)
```

### Cookie export JSON contract

- HTTP only: `{"referer","includedFormats":["cookies"],"cookies":[...]}`
- localStorage only: `includedFormats:["localStorage"]`, `storage.localStorage`
- IndexedDB only: `includedFormats:["indexedDB"]`, `indexedDB.{db}.{stores}`
- Mixed: sab jo mile (`cookies`, `localStorage`, `sessionStorage`, `indexedDB`) ek hi file mein

Cookie object shape Chrome extension compatible: `domain`, `expirationDate`, `hostOnly`, `httpOnly`, `name`, `path`, `sameSite`, `secure`, `session`, `storeId`, `value`.

---

## 0. Repository Layout

```
Network Reader/                    ← workspace root
├── PLAN.md                        ← yeh document
├── Friend/                        ← Phase 0 prototype (reference only, touch mat karo)
│   ├── network_reader.py
│   ├── NETWORK_READER.md
│   └── ...
└── own-network-fetcher/           ← ★ SAARA NAYA CODE YAHAN ★
    ├── README.md
    ├── requirements.txt
    ├── pyproject.toml
    ├── config.example.yaml
    └── src/onf/                   ← Python package (Own Network Fetcher)
        ├── main.py                ← CLI entry (`python -m onf`)
        ├── config.py
        ├── browser_pool.py
        ├── capture/
        ├── storage/
        └── models/
```

| Folder | Role |
|--------|------|
| `Friend/` | Purana prototype — ideas / CDP logic reference ke liye |
| `own-network-fetcher/` | Production codebase — **shuru se likha**, modular, JSON, Playwright |

**CLI name:** `onf` (Own Network Fetcher)  
**Future `.exe`:** `onf.exe` (CMD se: `onf.exe --config config.yaml`)

---

## 1. Vision

**Own Network Fetcher (ONF)** ek **session capture aur analysis platform** banega jo RDP/Windows (aur future mein Mac) par real Chrome ke through:

- Network traffic (requests + responses) capture kare
- HTTP cookies, localStorage, sessionStorage, IndexedDB JSON mein store kare
- Multiple isolated tasks parallel chala sake (alag sites ya same site + alag credentials)
- Login journey record kare (success/fail, API flow, cookie lifecycle)
- Optional: captured data ko remote server par sync kare

**Phase 0 (reference):** `Friend/network_reader.py` — cookie-focused curl log capture (Windows CMD style). Sirf reference; naya code `own-network-fetcher/` mein.

**End goal:** Production-ready `onf.exe` jo CMD par chale, RDP par multiple Chrome sessions manage kare, aur har task ka structured JSON session export kare.

---

## 2. Requirements

### 2.1 Functional Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-1 | Chrome/Brave network requests capture karna (GET, POST, XHR, fetch, etc.) | P0 |
| FR-2 | HTTP cookies capture — request cookies + Set-Cookie response headers | P0 |
| FR-3 | Output structured JSON format mein (curl text optional) | P0 |
| FR-4 | **Browser Context** per task — tasks ke beech full isolation (cookies, storage) | P0 |
| FR-5 | 1 Chrome instance mein multiple parallel tasks (2–6 contexts) | P0 |
| FR-6 | 2–3 Chrome instances parallel (alag debug ports) | P1 |
| FR-7 | Popup / new tab login flow handle (OAuth, SSO — naya tab khulke wapas purani tab) | P0 |
| FR-8 | Manual tab switch se capture na ruke aur data mix na ho | P0 |
| FR-9 | Naya task start karna — running tasks par zero effect | P0 |
| FR-10 | Headless ya visible Chrome — user/runtime choice | P1 |
| FR-11 | Request + response capture (status, headers, body) | P1 |
| FR-12 | Login success / fail auto-detect (heuristics + optional site profiles) | P1 |
| FR-13 | Session timeline — phase-wise API journey (login → redirect → dashboard) | P1 |
| FR-14 | localStorage + sessionStorage → JSON | P2 |
| FR-15 | IndexedDB → JSON | P2 |
| FR-16 | Remote server par cookie/session sync (encrypted API) | P2 |
| FR-17 | Browser automation (navigate, form fill, wait) — optional layer | P2 |
| FR-18 | Per-domain / per-task log files + global session index | P1 |

### 2.2 Non-Functional Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| NFR-1 | **Accuracy** — CDP-based capture; koi request miss na ho (attached targets only) | P0 |
| NFR-2 | **Speed** — I/O async; heavy sites par bhi capture thread block na ho | P0 |
| NFR-3 | **Resources** — Chrome RAM limit respect; max concurrent tasks configurable | P0 |
| NFR-4 | Windows `.exe` — CMD se direct run (`onf.exe --flags`) | P1 |
| NFR-5 | Future Mac support — same codebase, OS-specific Chrome paths | P2 |
| NFR-6 | Sensitive data handling — passwords mask, cookies encrypt at rest (remote sync) | P1 |
| NFR-7 | RDP Windows par real installed Chrome se connect (`connect_over_cdp`) | P0 |
| NFR-8 | Sirf authorized accounts par use; session data secure rakho | P0 |

### 2.3 Out of Scope (initial phases)

- CAPTCHA bypass
- 2FA automation (manual step accept karna padega)
- Request replay / bot traffic generation
- Non-Chromium browsers (Firefox, Safari)

---

## 3. Technology Stack

| Layer | Choice | Reason |
|-------|--------|--------|
| Language | **Python 3.11+** | Existing codebase, fast iteration, rich ecosystem |
| Browser control | **Playwright (Python)** | Browser Context isolation, popup handling, headless/visible |
| Low-level capture | **CDP via Playwright CDPSession** | Network, Storage, IndexedDB domains |
| Async runtime | **asyncio** | Multi-task parallel capture without blocking |
| Data format | **JSON** (NDJSON for streaming logs) | Machine + human readable |
| Local config | YAML or JSON config file | Task limits, ports, paths |
| Remote sync (later) | **FastAPI** server + **httpx** client | Cookie/session upload API |
| Windows packaging | **PyInstaller** or **Nuitka** | `.exe` for CMD |
| Mac packaging (later) | PyInstaller → `.app` or standalone script | Same code, path adapters |

### Kyun Python (final decision summary)

- `Friend/` se sirf **reference** (CDP events, cookie parsing ideas) — naya code `own-network-fetcher/` mein shuru se
- Playwright = Browser Context + popup + multi-Chrome best support
- CDP accuracy Python/Node/Go mein same; Python mein development fastest
- `.exe` possible via PyInstaller → `onf.exe`; Chrome alag install (`connect_over_cdp`) → chhota binary
- Mac par same `own-network-fetcher/` codebase chalega with path changes

---

## 4. Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    CLI / .exe Entry Point                    │
│         onf.exe --config config.yaml                         │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                   Task Orchestrator                            │
│  • task queue, task_id, status (running/done/failed)         │
│  • max_concurrent_tasks, RAM-aware limits                      │
│  • naya task = naya context; running tasks untouched          │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                   Browser Pool Manager                         │
│  Chrome #1 → port 9222    Chrome #2 → port 9223  ...          │
│  headless / visible per instance                             │
└──────────────────────────┬──────────────────────────────────┘
                           │
         ┌─────────────────┼─────────────────┐
         ▼                 ▼                 ▼
┌────────────────┐ ┌────────────────┐ ┌────────────────┐
│  Task Context  │ │  Task Context  │ │  Task Context  │
│  (isolated)    │ │  (isolated)    │ │  (isolated)    │
│  + pages/popups│ │  + pages/popups│ │  + pages/popups│
└───────┬────────┘ └───────┬────────┘ └───────┬────────┘
        │                  │                  │
        └──────────────────┼──────────────────┘
                           ▼
┌─────────────────────────────────────────────────────────────┐
│              Capture Agent (per task context)                │
│  Network.*  │  Storage.*  │  DOMStorage.*  │  IndexedDB.*   │
│  All events tagged: task_id + context_id + page_id           │
└──────────────────────────┬──────────────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────────────┐
│              Session Builder → JSON files                      │
│  sessions/{task_id}/session.json                             │
│  sessions/{task_id}/network.ndjson                             │
└──────────────────────────┬──────────────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────────────┐
│         Remote Sync Client (Phase 4 — optional)              │
│         POST encrypted payload → your storage server         │
└─────────────────────────────────────────────────────────────┘
```

### Isolation rule (non-negotiable)

```
1 Task = 1 Browser Context = 1 isolated cookie/storage world
```

- Same website + alag credentials → **alag contexts** (normal tabs se nahi)
- Popup/new tab **same task ke andar** → same context (OAuth sahi kaam karega)
- Capture agent **context ke saare pages/popups** track karega

### Popup / OAuth handling

```
Task start → create context → open initial page
           → context.on("page") → har naye tab/popup par capture attach
           → sab events task_id se tag
Task end   → context.close() → memory free
```

Tab switch (manual ya automatic) capture ko rok nahi karta — sirf galat task mapping se data mix hota hai.

---

## 5. Python se kaise achieve hoga (requirement mapping)

| Requirement | Python / Playwright approach |
|-------------|------------------------------|
| Network capture | `CDPSession.send("Network.enable")` + `requestWillBeSent`, `responseReceived`, `getResponseBody` |
| HTTP cookies | `Network.requestWillBeSentExtraInfo` + `Set-Cookie` from response headers; `Storage.getCookies` |
| JSON output | `dataclasses` / Pydantic models → `json.dump`; network stream as NDJSON |
| Context isolation | `browser.new_context()` per task — Playwright native |
| Multi-task parallel | `asyncio.gather()` + orchestrator queue |
| Multi Chrome | `playwright.chromium.connect_over_cdp("http://127.0.0.1:9222")` — alag ports |
| Popup login | `context.on("page", handler)` — naye page par same capture pipeline |
| Tab switch safe | Events async queue mein; task_id mapping; active tab irrelevant |
| Headless/visible | Launch flag ya RDP par visible Chrome + CDP connect |
| Login detect | POST URL patterns + status codes + response JSON heuristics + cookie delta |
| localStorage | CDP `DOMStorage.getDOMStorageItems` per origin |
| IndexedDB | CDP `IndexedDB.requestDatabaseNames` + `requestData` |
| Remote sync | `httpx.AsyncClient.post()` with API key + AES encrypted cookie payload |
| `.exe` | PyInstaller one-file; `connect_over_cdp` → system Chrome, browser bundle nahi |
| Mac | `platform.system()` path adapter; same Playwright CDP connect |

---

## 6. Implementation Phases

### Phase 0 — Reference Prototype (Done)

**Location:** `Friend/network_reader.py` — **read-only reference, edit mat karo**

**Kya hai:**
- CDP WebSocket se network request capture
- Cookie wali requests → Windows CMD curl format
- Global + per-domain text files
- Chrome/Brave auto-launch + profile clone fallback

**ONF mein kya reuse hoga (ideas only, copy-paste refactor nahi):**
- Cookie header parse logic (`extract_cookie_header`, `parse_cookie_header`)
- CDP event names (`Network.requestWillBeSent`, `requestWillBeSentExtraInfo`)
- Profile clone / debug port patterns (browser launch module mein adapt)

**Limitations (isliye naya project):**
- No Browser Context isolation
- No response body
- No JSON
- No multi-task orchestrator
- Windows-only paths
- Profile-level cookies (tab isolation nahi)

---

### Phase 1 — Foundation (In Progress)

**Location:** `own-network-fetcher/`  
**Goal:** Playwright CDP connect + JSON output + **capture modes**

**Capture modes (implemented):**

| Mode | CLI | Behaviour |
|------|-----|-----------|
| Cookie only | default | Sirf `Cookie` header wali requests + `Set-Cookie` responses save; baaki skip |
| Full | `--all-requests` | Saari requests/responses → `network.ndjson` |

**Folder structure:**

```
own-network-fetcher/
├── src/onf/
│   ├── main.py              # CLI entry — python -m onf
│   ├── config.py            # Pydantic settings
│   ├── browser_pool.py      # Chrome connect / launch (Phase 1)
│   ├── capture/
│   │   ├── network.py       # CDP network events
│   │   └── cookies.py       # HTTP cookie extract
│   ├── storage/
│   │   └── json_writer.py   # session.json + network.ndjson
│   └── models/
│       └── session.py       # Session JSON schema
├── config.example.yaml
├── requirements.txt
├── pyproject.toml
└── README.md
```

**Tasks:**
1. ~~Scaffold + package layout create karo~~ ✅
2. ~~Playwright `connect_over_cdp` se Chrome attach~~ ✅
3. ~~Cookie-only mode (default) + `--all-requests` full mode~~ ✅
4. ~~Single session → `captures/sessions/{task_id}/session.json`~~ ✅
5. ~~Windows `onf.exe` build (`scripts/build_windows.bat`, `onf.spec`)~~ ✅
6. ~~Double-click default run + pause before exit~~ ✅
7. Browser auto-launch (Friend-style) — optional, pending
8. **Next:** Phase 2 — Browser Context per task isolation

**`.exe` rule (har phase ke baad):**

```cmd
scripts\build_windows.bat
```

→ **`Network Reader/onf.exe`** (root folder) copy to RDP. Version tag bump in `src/onf/__init__.py`.

**Exit criteria:**
- ~~Ek site browse karo → JSON file mein cookies aayein~~ ✅
- ~~`onf.exe` double-click se cookie mode chale~~ ✅ (Windows build required)

**Run (development):**

```powershell
cd own-network-fetcher
pip install -r requirements.txt
playwright install chromium
python -m onf --chrome-port 9222 --output-dir ./captures --task-id test_01
```

---

### Phase 2 — Multi-Task Isolation

**Goal:** Browser Context per task + parallel run + popup handling

**Location:** `own-network-fetcher/src/onf/`

**Deliverables:**
- `task_orchestrator.py` — queue, task lifecycle, status
- `context_manager.py` — create/close context per task
- Popup handler — `context.on("page")` auto-attach capture
- Config: `max_tasks_per_chrome`, `max_chrome_instances`
- Output: `captures/sessions/{task_id}/` folder per task

**Tasks:**
1. 1 Chrome, 4 parallel contexts — alag sites, verify cookie isolation
2. Same site, 2 contexts, 2 alag logins — verify no cookie leak
3. OAuth popup flow test — popup + parent tab dono capture
4. Naya task while 4 running — zero impact test
5. Manual tab switch during capture — continuity test

**Exit criteria:**
- 4 parallel tasks stable 30+ min RDP par
- Har task ka alag JSON, koi cross-contamination nahi

---

### Phase 3 — Full Session Intelligence

**Location:** `own-network-fetcher/src/onf/`

**Goal:** Request + response + login detect + timeline

**Deliverables:**
- Response body capture (size limit configurable)
- `session.phases[]` — login_attempt, login_success, navigation, etc.
- Login heuristics engine (generic rules)
- Optional `site_profiles/` — per-site login URL + success/fail JSON paths
- Sensitive field redaction (password in POST body → `[REDACTED]`)
- Filter: skip images/fonts (resource saving)

**JSON schema (concept):**

```json
{
  "task_id": "task_20260613_001",
  "context_id": "...",
  "started_at": "2026-06-13T10:00:00Z",
  "ended_at": null,
  "status": "running",
  "target_url": "https://example.com/login",
  "phases": [
    {
      "name": "login_failed",
      "timestamp": "...",
      "trigger_request_id": "..."
    },
    {
      "name": "login_success",
      "timestamp": "...",
      "new_cookies": ["session_id", "auth_token"]
    }
  ],
  "http_cookies": [],
  "local_storage": {},
  "session_storage": {},
  "indexed_db": [],
  "summary": {
    "total_requests": 42,
    "api_calls": 12,
    "login_attempts": 2
  }
}
```

Network detail alag file: `network.ndjson` (har line ek request/response pair)

**Exit criteria:**
- Galat password → phase `login_failed` + error response saved
- Sahi login → phase `login_success` + new cookies listed
- Dashboard navigate → API list phase mein grouped

---

### Phase 4 — Storage Expansion + Remote Sync

**Location:** `own-network-fetcher/src/onf/` + optional alag server repo

**Goal:** localStorage, IndexedDB + alag server par store

**Deliverables:**
- CDP storage capture module
- Periodic snapshot (on task end + on login success)
- Remote sync client:
  - `POST /api/v1/sessions` — full session upload
  - `POST /api/v1/cookies` — cookie-only quick sync
  - API key auth + TLS
- Server-side (separate repo): FastAPI + SQLite/PostgreSQL + encryption at rest
- Retention policy (auto-delete after N days)

**Exit criteria:**
- Task end par localStorage + IndexedDB JSON mein
- Remote server par encrypted session available
- Offline mode — sync queue, retry on reconnect

---

### Phase 5 — Automation Layer (Optional)

**Location:** `own-network-fetcher/src/onf/automation/`

**Goal:** Scriptable login/navigation without manual browse

**Deliverables:**
- `automation/` module — Playwright page actions
- Task config YAML:

```yaml
task:
  id: semrush_test_1
  url: https://www.semrush.com/login
  steps:
    - fill: "#email"
      value: "${EMAIL}"
    - fill: "#password"
      value: "${PASSWORD}"
    - click: "button[type=submit]"
    - wait_for_url: "**/dashboard**"
```

- Capture runs parallel with automation
- Manual + automated dono modes

**Exit criteria:**
- YAML-driven login + full capture JSON without manual clicks

---

### Phase 6 — Packaging & Production

**Location:** `own-network-fetcher/` — build scripts Phase 1 se active (`onf.spec`, `scripts/build_windows.bat`)

**Goal:** Production `.exe` hardening + Mac dev path

**Har phase ke baad (mandatory):**

1. `src/onf/__init__.py` version bump (`0.2.0-phase2`, etc.)
2. Windows par `scripts\build_windows.bat`
3. Root folder `onf.exe` → RDP par paste
4. Manual test: Chrome open → double-click exe → capture verify

**Deliverables:**
- ~~PyInstaller build → `dist/onf.exe`~~ ✅ (Phase 1)
- ~~`BUILD.md` + `releases/README.md`~~ ✅
- `config.example.yaml` + setup guide
- Mac build instructions (Phase 6b)
- Logging (rotating files), error recovery
- `--version`, health check, graceful shutdown

**`.exe` usage (target):**

```cmd
onf.exe --config config.yaml
onf.exe start-task --url https://example.com --task-id test_01
onf.exe status
onf.exe stop-task --task-id test_01
```

**Exit criteria:**
- Clean Windows machine par sirf Chrome + `onf.exe` se kaam chale
- Mac par `python -m onf` ya `.app` equivalent

---

## 7. Future Scope

| Area | Description | When |
|------|-------------|------|
| **Web dashboard** | Browser UI — live tasks, session viewer, cookie search | Post Phase 4 |
| **Site profile marketplace** | Pre-built login detect rules for popular tools | Phase 3+ |
| **Webhook alerts** | Login fail / new cookie / session expire notify | Phase 4+ |
| **Diff mode** | Do login attempts compare — kya response alag tha | Phase 3+ |
| **Export formats** | HAR, Postman collection, curl bundle | Phase 3+ |
| **Multi-user RDP** | Har operator ka alag task namespace | Production |
| **Go sync agent** | Sirf remote upload worker Go mein (optional scale) | If 50+ sessions |
| **Playwright trace** | Visual replay of session | Debug feature |
| **API gateway** | External tools tumhare stored cookies fetch karein (secure) | Long term |

---

## 8. Usage Guide

### 8.1 Abhi (Phase 0 — Friend folder)

**Setup:**

```powershell
cd Friend
pip install websocket-client
```

**Run (Windows RDP, Profile 1):**

```powershell
python network_reader.py --use-installed-profile --profile-directory "Profile 1" --include-sensitive
```

**Kya karo:**
1. Script chalao → Chrome khulega
2. Site kholo, login karo, pages browse karo
3. `network_requests.txt` + `domain_sessions/` mein curl logs
4. `Ctrl+C` se stop

**Limitation:** Manual browse, text output, no task isolation.

---

### 8.2 Own Network Fetcher — Single Task (Phase 1+)

```cmd
cd own-network-fetcher
onf.exe start ^
  --chrome-port 9222 ^
  --url https://example.com ^
  --task-id my_test_01 ^
  --output-dir D:\captures
```

Development (without `.exe`):

```powershell
cd own-network-fetcher
python -m onf --chrome-port 9222 --output-dir ./captures --task-id my_test_01
```

1. RDP par Chrome debug mode par ho (ya tool khud connect kare)
2. Tool ek Browser Context banaye, page khole
3. Tum manually login/browse karo (ya Phase 5 automation)
4. Output: `captures/sessions/my_test_01/session.json`

---

### 8.3 Future — Multiple Parallel Tasks (Phase 2+)

**config.yaml:**

```yaml
chrome:
  instances:
    - port: 9222
      headless: false
    - port: 9223
      headless: true

limits:
  max_tasks_per_chrome: 4
  max_total_tasks: 10

tasks:
  - id: semrush_user_a
    url: https://www.semrush.com
  - id: semrush_user_b
    url: https://www.semrush.com
  - id: coursera_test
    url: https://www.coursera.org
```

```cmd
cd own-network-fetcher
onf.exe run --config config.yaml
```

- Har task isolated context mein
- Popup login automatically tracked
- Naya task: `onf.exe start-task --id new_01 --url ...`

---

### 8.4 Future — Remote Cookie Store (Phase 4+)

**config.yaml:**

```yaml
remote_sync:
  enabled: true
  url: https://your-server.com/api/v1
  api_key: ${REMOTE_API_KEY}
  sync_on: task_end   # task_end | login_success | realtime
  encrypt: true
```

Task complete → JSON + cookies encrypted upload → tumhara server store kare.

---

### 8.5 RDP Deployment Pattern (recommended)

```
┌─────────────────── RDP Windows Server ───────────────────┐
│  Chrome (visible or headless) — ports 9222, 9223         │
│  onf.exe — Task Orchestrator                             │
│  Output: own-network-fetcher/captures/                   │
└──────────────────────────┬───────────────────────────────┘
                           │ HTTPS (Phase 4)
                           ▼
┌─────────────────── Your Storage Server ──────────────────┐
│  FastAPI + DB — encrypted sessions & cookies             │
└──────────────────────────────────────────────────────────┘
```

Tum RDP se browser dekh sakte ho (visible mode) ya sirf background capture (headless).

---

## 9. Resource Planning

| Scenario | Approx RAM | Recommendation |
|----------|------------|----------------|
| 1 Chrome, 1 task | 0.6 – 1.5 GB | Safe default |
| 1 Chrome, 4 tasks | 2 – 6 GB | `max_tasks_per_chrome: 4` |
| 3 Chrome, 4 tasks each | 6 – 18 GB | High-end RDP only |
| Python orchestrator | 80 – 150 MB | Negligible vs Chrome |

**Rule:** Orchestrator mein hard limit lagao — RAM cross hone par naya task queue mein wait kare.

---

## 10. Security Checklist

- [ ] Password fields POST body mein hamesha redact
- [ ] Session JSON / cookies git mein commit mat karo
- [ ] Remote sync — HTTPS + API key + encryption at rest
- [ ] Cookie files OS-level permissions restrict karo
- [ ] Retention — purani sessions auto-delete
- [ ] Sirf authorized accounts par testing
- [ ] RDP debug port sirf localhost (9222 bind 127.0.0.1)

---

## 11. Friend/ vs own-network-fetcher/

| `Friend/` (Phase 0 — reference) | `own-network-fetcher/` (production) |
|----------------------------------|-------------------------------------|
| `network_reader.py` monolith | Modular `src/onf/` package |
| WebSocket raw CDP | Playwright + CDPSession |
| curl text output | JSON primary, curl optional |
| Profile-level capture | Browser Context per task |
| `requirements-network-reader.txt` | `requirements.txt` + Playwright |
| `NETWORK_READER.md` | `own-network-fetcher/README.md` + root `PLAN.md` |
| Edit mat karo | **Saara naya code yahan** |

**Rule:** `Friend/` sirf padhne / ideas ke liye. Koi feature add karna ho → `own-network-fetcher/src/onf/` mein likho.

---

## 12. Success Metrics

| Phase | Success looks like |
|-------|-------------------|
| 1 | JSON session file with network + cookies from one browse session |
| 2 | 4 parallel isolated tasks, no cookie leak, popup OAuth captured |
| 3 | Login fail vs success correctly phased in JSON |
| 4 | localStorage + IndexedDB in JSON; remote server has encrypted copy |
| 5 | YAML automation runs login + full capture unattended |
| 6 | `onf.exe` on clean Windows + CMD docs; Mac path adapter works |

---

## 13. Next Steps

1. ~~Phase 1 core capture + cookie modes~~ ✅
2. ~~Windows `.exe` build pipeline~~ ✅
3. **Tum (RDP):** `scripts\build_windows.bat` → `onf.exe` test with open Chrome
4. **Dev:** Phase 2 — `task_orchestrator.py` + Browser Context per task → phir naya `.exe`
5. Phase 3 — response body + login phase detection → phir naya `.exe`

---

*Yeh document har phase ke saath update hota rahega. Code sirf `own-network-fetcher/` mein likho.*
