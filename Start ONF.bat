@echo off
setlocal
title Own Network Fetcher
cd /d "%~dp0"

echo ============================================================
echo   Own Network Fetcher (ONF)
echo ============================================================
echo.
echo IMPORTANT: Normal Chrome icon se khola browser kaam NAHI karega.
echo ONF ko Chrome DEBUG mode chahiye (port 9222).
echo.
echo STEP 1: Chrome debug start hoga automatically agar port ready na ho.
echo STEP 2: Neeche mode select karo (1 ya 2). Band karne ke liye: Ctrl+C
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
  echo Port 9222 par debug Chrome nahi mila.
  echo Debug Chrome start ho raha hai ^(saara normal Chrome band hoga^)...
  call scripts\start_chrome_debug.bat --no-pause
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
