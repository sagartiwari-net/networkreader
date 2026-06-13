@echo off
setlocal
title Launch Chrome for ONF
cd /d "%~dp0.."

echo ============================================================
echo   Launch Chrome for ONF (profile sync + debug port 9222)
echo ============================================================
echo.
echo Chrome 136+ real User Data folder par debug allow nahi karta.
echo ONF profile ko onf-chrome-debug folder mein copy karke khulta hai.
echo.

set "CHROME=%ProgramFiles%\Google\Chrome\Application\chrome.exe"
if not exist "%CHROME%" set "CHROME=%ProgramFiles(x86)%\Google\Chrome\Application\chrome.exe"
if not exist "%CHROME%" set "CHROME=%LocalAppData%\Google\Chrome\Application\chrome.exe"
if not exist "%CHROME%" (
  echo ERROR: Google Chrome not found.
  pause
  exit /b 1
)

set "SRC_DATA=%LOCALAPPDATA%\Google\Chrome\User Data"
set "ONF_DATA=%LOCALAPPDATA%\onf-chrome-debug"
echo Installed profiles (folder name):
echo   Default
echo   Profile 1
echo   Profile 2
echo   ... (jo Chrome mein dikhta hai)
echo.
set /p PROFILE_DIR="Profile folder enter karo [Default]: "
if "%PROFILE_DIR%"=="" set "PROFILE_DIR=Default"

if not exist "%SRC_DATA%\%PROFILE_DIR%" (
  echo ERROR: Profile folder not found: %SRC_DATA%\%PROFILE_DIR%
  pause
  exit /b 1
)

echo.
echo Closing Chrome...
taskkill /F /T /IM chrome.exe >nul 2>&1
timeout /t 2 /nobreak >nul

echo Syncing profile to ONF debug folder...
if not exist "%ONF_DATA%" mkdir "%ONF_DATA%"
robocopy "%SRC_DATA%\%PROFILE_DIR%" "%ONF_DATA%\%PROFILE_DIR%" /MIR /XD Cache "Code Cache" GPUCache "Service Worker" Crashpad GrShaderCache ShaderCache DawnCache VideoDecodeStats /NFL /NDL /NJH /NJS /nc /ns /np >nul
if errorlevel 8 (
  echo ERROR: Profile sync failed.
  pause
  exit /b 1
)
copy /Y "%SRC_DATA%\Local State" "%ONF_DATA%\Local State" >nul 2>&1

echo Starting Chrome debug: %PROFILE_DIR%
start "" "%CHROME%" --remote-debugging-port=9222 --remote-debugging-address=127.0.0.1 --remote-allow-origins=* --user-data-dir="%ONF_DATA%" --profile-directory="%PROFILE_DIR%" --no-first-run --no-default-browser-check --new-window about:blank

echo Waiting for port 9222...
set /a TRIES=0
:wait_loop
set /a TRIES+=1
powershell -NoProfile -Command "try { (Invoke-WebRequest -Uri 'http://127.0.0.1:9222/json/version' -UseBasicParsing -TimeoutSec 2).Content | Out-Null; exit 0 } catch { exit 1 }" >nul 2>&1
if not errorlevel 1 goto :ready
if %TRIES% GEQ 40 goto :fail
timeout /t 1 /nobreak >nul
goto :wait_loop

:ready
echo.
echo SUCCESS — Chrome debug ready hai. Ab Start ONF.bat chalao.
echo ONF debug folder: %ONF_DATA%
echo.
pause
exit /b 0

:fail
echo ERROR: Port 9222 ready nahi hua.
echo Test: http://127.0.0.1:9222/json/version
pause
exit /b 1
