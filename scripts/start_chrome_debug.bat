@echo off
title Chrome Debug Mode (port 9222)
echo Closing existing Chrome processes...
taskkill /F /IM chrome.exe >nul 2>&1
timeout /t 2 /nobreak >nul

set "CHROME=%ProgramFiles%\Google\Chrome\Application\chrome.exe"
if not exist "%CHROME%" set "CHROME=%ProgramFiles(x86)%\Google\Chrome\Application\chrome.exe"
if not exist "%CHROME%" (
  echo Chrome not found. Install Google Chrome first.
  pause
  exit /b 1
)

set "DEBUG_PROFILE=%LOCALAPPDATA%\onf-chrome-debug"
echo Starting Chrome with remote debugging on port 9222...
echo Profile folder: %DEBUG_PROFILE%
echo.
start "" "%CHROME%" --remote-debugging-port=9222 --remote-allow-origins=* --user-data-dir="%DEBUG_PROFILE%"

echo.
echo Chrome khul gaya. Profile select karo.
echo Test URL browser mein kholo: http://127.0.0.1:9222/json/version
echo Agar JSON dikhe to debug mode sahi hai — ab Start ONF.bat chalao.
echo.
pause
