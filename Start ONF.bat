@echo off
title Own Network Fetcher
cd /d "%~dp0"

echo ============================================================
echo   Own Network Fetcher (ONF)
echo ============================================================
echo.
echo STEP 1: Chrome debug mode mein kholo (alag CMD window):
echo   chrome.exe --remote-debugging-port=9222 --remote-allow-origins=*
echo.
echo STEP 2: Ab ONF start ho raha hai...
echo   Band karne ke liye: Ctrl+C
echo ============================================================
echo.

if not exist onf.exe (
  echo ERROR: onf.exe is folder mein nahi mila.
  echo Pehle build chalao: own-network-fetcher\scripts\build_windows.bat
  goto :done
)

onf.exe
set ERR=%ERRORLEVEL%

echo.
if %ERR% NEQ 0 (
  echo ONF error code: %ERR%
) else (
  echo ONF band ho gaya.
)

:done
echo.
pause
