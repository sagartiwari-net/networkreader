@echo off
setlocal enabledelayedexpansion
title Chrome Debug Mode (port 9222)
cd /d "%~dp0.."

echo ============================================================
echo   Chrome Debug Mode - REAL Chrome profile
echo ============================================================
echo.
echo Ye script aapka installed Chrome profile use karega.
echo Pehle saara Chrome band hoga, phir same profile debug mode mein khulega.
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

set "USER_DATA=%LOCALAPPDATA%\Google\Chrome\User Data"
echo.
echo Common profiles:
echo   Default
echo   Profile 1
echo   Profile 2
echo.
set /p PROFILE_DIR="Chrome profile folder name [Default]: "
if "%PROFILE_DIR%"=="" set "PROFILE_DIR=Default"

echo.
echo Starting Chrome: %CHROME%
echo User data dir : %USER_DATA%
echo Profile folder: %PROFILE_DIR%
echo.
start "" "%CHROME%" --remote-debugging-port=9222 --remote-allow-origins=* --user-data-dir="%USER_DATA%" --profile-directory="%PROFILE_DIR%" --no-first-run --no-default-browser-check --new-window about:blank

echo Waiting for debug port 9222 (max 60 seconds)...
set /a TRIES=0
:wait_loop
set /a TRIES+=1
powershell -NoProfile -Command "try { (Invoke-WebRequest -Uri 'http://127.0.0.1:9222/json/version' -UseBasicParsing -TimeoutSec 3).Content | Out-Null; exit 0 } catch { exit 1 }" >nul 2>&1
if not errorlevel 1 goto :ready
if %TRIES% GEQ 30 (
  echo.
  echo ERROR: Port 9222 abhi bhi ready nahi hai.
  echo Browser mein test karo: http://127.0.0.1:9222/json/version
  pause
  exit /b 1
)
timeout /t 2 /nobreak >nul
goto :wait_loop

:ready
echo.
echo SUCCESS: Chrome debug port 9222 ready hai ^(%PROFILE_DIR%^).
echo Ab Start ONF.bat chalao.
echo.
if /I not "%~1"=="--no-pause" pause
exit /b 0
