@echo off
setlocal
title Launch Chrome for ONF
cd /d "%~dp0.."

echo ============================================================
echo   Launch Chrome for ONF (real profile + debug port 9222)
echo ============================================================
echo.
echo Ye script ek baar chalao — phir Start ONF.bat turant connect karega.
echo Chrome band karke dubara khulega (debug port ke liye zaroori).
echo.

set "CHROME=%ProgramFiles%\Google\Chrome\Application\chrome.exe"
if not exist "%CHROME%" set "CHROME=%ProgramFiles(x86)%\Google\Chrome\Application\chrome.exe"
if not exist "%CHROME%" set "CHROME=%LocalAppData%\Google\Chrome\Application\chrome.exe"
if not exist "%CHROME%" (
  echo ERROR: Google Chrome not found.
  pause
  exit /b 1
)

set "USER_DATA=%LOCALAPPDATA%\Google\Chrome\User Data"
echo Installed profiles (folder name):
echo   Default
echo   Profile 1
echo   Profile 2
echo   ... (jo Chrome mein dikhta hai)
echo.
set /p PROFILE_DIR="Profile folder enter karo [Default]: "
if "%PROFILE_DIR%"=="" set "PROFILE_DIR=Default"

echo.
echo Closing Chrome...
taskkill /F /IM chrome.exe >nul 2>&1
timeout /t 1 /nobreak >nul

echo Starting Chrome: %PROFILE_DIR%
start "" "%CHROME%" --remote-debugging-port=9222 --remote-allow-origins=* --user-data-dir="%USER_DATA%" --profile-directory="%PROFILE_DIR%" --no-first-run --no-default-browser-check

echo Waiting for port 9222...
set /a TRIES=0
:wait_loop
set /a TRIES+=1
powershell -NoProfile -Command "try { (Invoke-WebRequest -Uri 'http://127.0.0.1:9222/json/version' -UseBasicParsing -TimeoutSec 2).Content | Out-Null; exit 0 } catch { exit 1 }" >nul 2>&1
if not errorlevel 1 goto :ready
if %TRIES% GEQ 25 goto :fail
timeout /t 1 /nobreak >nul
goto :wait_loop

:ready
echo.
echo SUCCESS — Chrome ready hai. Ab Start ONF.bat chalao.
echo Chrome open rakho — ONF band nahi karega.
echo.
pause
exit /b 0

:fail
echo ERROR: Port 9222 ready nahi hua.
pause
exit /b 1
