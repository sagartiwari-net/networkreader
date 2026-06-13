# Network Reader

Chrome/Brave request header capture kore `curl` format e save kore.

## Ki ki korbe

- Cookie thaka request save hobe (default mode).
- Prottek domain-er jonno alada file hobe.
- Latest request sobar upore, purono request niche.
- CMD-te live status show korbe (`[INFO]`, `[SAVE]`).
- Curl output Windows CMD-style e hobe (`^"` quoting + `^` line continue).
- `Cookie` header e name thakbe, value sobsomoy `**` hishebe masked hobe.
- Installed profile direct launch fail hole auto clone fallback nibe.

## 1) Dependency

```powershell
pip install websocket-client
```

## 2) Recommended run (Profile 1)

```powershell
python network_reader.py --use-installed-profile --profile-directory "Profile 1" --include-sensitive --open-notepad
```

## 3) Force clone mode (jodi direct mode repeatedly fail kore)

```powershell
python network_reader.py --clone-installed-profile --profile-directory "Profile 1" --include-sensitive --open-notepad
```

Optional clone location:

```powershell
python network_reader.py --clone-installed-profile --clone-profile-dir "C:\nr-chrome" --profile-directory "Profile 1" --include-sensitive --open-notepad
```

## 4) Output

- Global latest-first file: `network_requests.txt`
- Per-domain latest-first folder: `domain_sessions\`
  - `domain_sessions\www.semrush.com.txt`
  - `domain_sessions\api.semrush.com.txt`
- Entry example:
  - `-b "cookieA=**; cookieB=**"`
  - comment line e cookie names + count dekhabe.

## 4.1) Jodi all request capture korte chao

```powershell
python network_reader.py --all-requests --clone-installed-profile --profile-directory "Profile 1" --include-sensitive --open-notepad
```

## 5) CMD-te dekhabe

Example:

```text
[INFO] Launching browser (installed-profile)
[INFO] Direct profile launch failed. Trying cloned profile fallback...
[INFO] Debug endpoint is ready (clone fallback).
[SAVE] 14:22:10 | www.semrush.com | GET    /home/ | cookies=8 | total=17
```

## 6) Stop

`Ctrl + C`

---

## Extra notes

- `--include-sensitive` dile onno sensitive header redaction kombe, kintu cookie value sobsomoy `**`.
- Ei data sensitive, sudhu nijer authorized account/session-e use korben.
- Jodi manual browser launch koro (`--no-launch`), Chrome command-e `--remote-allow-origins=*` add korte hobe.
