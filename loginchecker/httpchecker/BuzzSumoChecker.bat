@echo off
title BuzzSumo HTTP Checker
cd /d "%~dp0"

if not exist "buzzsumo-checker.exe" (
    echo buzzsumo-checker.exe not found in this folder.
    echo Run build_windows.bat first, or git pull for the pre-built exe.
    pause
    exit /b 1
)

start "" "%~dp0buzzsumo-checker.exe"
