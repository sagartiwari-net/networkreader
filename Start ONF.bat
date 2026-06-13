@echo off
setlocal
title Own Network Fetcher
cd /d "%~dp0"

echo ============================================================
echo   Own Network Fetcher (ONF) v0.3
echo ============================================================
echo.
echo FAST WORKFLOW (Friend jaisa):
echo   1) Pehle: scripts\launch_chrome_profile.bat  ^(ek baar^)
echo   2) Phir:  Start ONF.bat  ^(turant connect^)
echo.
echo ONF aapka Chrome automatically BAND nahi karega.
echo ============================================================
echo.

if not exist onf.exe (
  echo ERROR: onf.exe nahi mila. Build: own-network-fetcher\scripts\build_windows.bat
  goto :done
)

powershell -NoProfile -Command "try { (Invoke-WebRequest -Uri 'http://127.0.0.1:9222/json/version' -UseBasicParsing -TimeoutSec 2).StatusCode | Out-Null; exit 0 } catch { exit 1 }" >nul 2>&1
if errorlevel 1 (
  echo [INFO] Debug port 9222 nahi mila.
  echo Pehle scripts\launch_chrome_profile.bat chalao ^(recommended^).
  echo.
  set /p READY="Launch script chala liya? Enter dabao: "
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

echo Invalid choice.
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
