@echo off
setlocal
title Chrome Debug Mode (port 9222)
cd /d "%~dp0.."

echo ============================================================
echo   Chrome Debug Mode (port 9222)
echo ============================================================
echo.
echo NOTE: Normal Chrome icon se khola browser ONF ko NAHI dikhega.
echo Ye script alag debug Chrome start karega.
echo.

echo Closing existing Chrome processes...
taskkill /F /IM chrome.exe >nul 2>&1
timeout /t 2 /nobreak >nul

set "CHROME=%ProgramFiles%\Google\Chrome\Application\chrome.exe"
if not exist "%CHROME%" set "CHROME=%ProgramFiles(x86)%\Google\Chrome\Application\chrome.exe"
if not exist "%CHROME%" set "CHROME=%LocalAppData%\Google\Chrome\Application\chrome.exe"
if not exist "%CHROME%" (
  echo ERROR: Google Chrome not found. Install Chrome first.
  pause
  exit /b 1
)

set "DEBUG_PROFILE=%LOCALAPPDATA%\onf-chrome-debug"
echo Starting Chrome: %CHROME%
echo Profile folder: %DEBUG_PROFILE%
echo.
start "" "%CHROME%" --remote-debugging-port=9222 --remote-allow-origins=* --user-data-dir="%DEBUG_PROFILE%" --no-first-run --no-default-browser-check

echo Waiting for debug port 9222 (max 60 seconds)...
set /a TRIES=0
:wait_loop
set /a TRIES+=1
powershell -NoProfile -Command "try { (Invoke-WebRequest -Uri 'http://127.0.0.1:9222/json/version' -UseBasicParsing -TimeoutSec 3).Content | Out-Null; exit 0 } catch { exit 1 }" >nul 2>&1
if not errorlevel 1 goto :ready
if %TRIES% GEQ 30 (
  echo.
  echo ERROR: Port 9222 abhi bhi ready nahi hai.
  echo - Chrome window mein profile select karo
  echo - Browser mein test karo: http://127.0.0.1:9222/json/version
  echo - Agar JSON na dikhe to Chrome reinstall ya firewall check karo
  pause
  exit /b 1
)
timeout /t 2 /nobreak >nul
goto :wait_loop

:ready
echo.
echo SUCCESS: Chrome debug port 9222 ready hai.
echo Ab Start ONF.bat chalao (Option 1 ya 2).
echo Test URL: http://127.0.0.1:9222/json/version
echo.
if /I not "%~1"=="--no-pause" pause
exit /b 0
