# ONF Releases

Har phase complete hone par yahan `onf.exe` aayega.

## Build (Windows par — RDP ya local)

```cmd
cd own-network-fetcher
scripts\build_windows.bat
```

Output:

- **`../onf.exe`** — workspace root folder (copy ke liye asaan)
- `releases/onf.exe` — backup copy
- `releases/onf-{version}.exe` — versioned backup

## RDP par use

1. Root se **`onf.exe`** copy karo (e.g. `D:\tools\onf\`)
2. Chrome debug mode mein kholo:

```cmd
chrome.exe --remote-debugging-port=9222 --remote-allow-origins=*
```

3. `onf.exe` par double-click karo **ya** CMD se:

```cmd
onf.exe
onf.exe --all-requests
onf.exe --chrome-port 9223 --task-id my_test
```

4. Capture files: `captures\sessions\{task_id}\` (exe ke bagal mein)

## Note

`.exe` sirf **Windows par build** hoti hai. Mac/Linux par Python source se chalao.
