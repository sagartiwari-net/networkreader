@echo off
setlocal
title Own Network Fetcher
cd /d "%~dp0"

echo ============================================================
echo   Own Network Fetcher (ONF)
echo ============================================================
echo.
echo STEP 1 (ZAROORI): Pehle Chrome debug mode kholo — alag CMD window:
echo   scripts\start_chrome_debug.bat
echo   ^> Profile select karo, Chrome khula rehna chahiye
echo.
echo STEP 2: Neeche mode select karo (1 ya 2)
echo   Band karne ke liye: Ctrl+C
echo ============================================================
echo.

if not exist onf.exe (
  echo ERROR: onf.exe is folder mein nahi mila.
  echo Pehle build chalao: own-network-fetcher\scripts\build_windows.bat
  goto :done
)

echo Checking Chrome debug port 9222...
powershell -NoProfile -Command "try { (Invoke-WebRequest -Uri 'http://127.0.0.1:9222/json/version' -UseBasicParsing -TimeoutSec 3).StatusCode | Out-Null; exit 0 } catch { exit 1 }" >nul 2>&1
if errorlevel 1 (
  echo.
  echo [WARNING] Port 9222 par Chrome debug nahi mila!
  echo Pehle scripts\start_chrome_debug.bat chalao, phir yahan aao.
  echo.
  set /p READY="Chrome debug start ho gaya? Enter dabao to continue: "
) else (
  echo Chrome debug port OK.
)
echo.

echo Select capture mode:
echo   1 = Full network scan
echo   2 = Cookie scan only
echo.
set /p MODE="Enter 1 or 2: "

if "%MODE%"=="1" (
  onf.exe --full-network --no-pause
  goto :after_run
)

if "%MODE%"=="2" (
  onf.exe --cookie-export --no-pause
  goto :after_run
)

echo Invalid choice. Please run again and enter 1 or 2.
goto :done

:after_run
echo.
if errorlevel 1 (
  echo ONF finished with errors.
) else (
  echo ONF band ho gaya.
)

:done
echo.
pause
