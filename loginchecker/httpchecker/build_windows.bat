@echo off
title Build BuzzSumo HTTP Checker
cd /d "%~dp0"

echo Building buzzsumo-checker.exe ...
go build -ldflags="-s -w" -o buzzsumo-checker.exe .
if errorlevel 1 (
    echo Build failed. Install Go from https://go.dev/dl/
    pause
    exit /b 1
)

echo.
echo Done: %~dp0buzzsumo-checker.exe
echo Double-click buzzsumo-checker.exe to run interactive mode.
echo Keep configs\ folder in the same directory.
echo.
pause
