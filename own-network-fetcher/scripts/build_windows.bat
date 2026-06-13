@echo off
setlocal enabledelayedexpansion

REM Build onf.exe on Windows (RDP ya local Windows machine).
cd /d "%~dp0.."

if not exist .venv (
  python -m venv .venv
)

call .venv\Scripts\activate.bat
python -m pip install --upgrade pip
pip install -r requirements-dev.txt

if exist build rmdir /s /q build
if exist dist rmdir /s /q dist

pyinstaller onf.spec --noconfirm
if errorlevel 1 (
  echo Build failed.
  pause
  exit /b 1
)

if not exist dist\onf.exe (
  echo dist\onf.exe not found after build.
  pause
  exit /b 1
)

if not exist releases mkdir releases
for /f "delims=" %%v in ('python scripts\get_version.py') do set VERSION=%%v

copy /y dist\onf.exe releases\onf-%VERSION%.exe
copy /y dist\onf.exe releases\onf.exe
copy /y dist\onf.exe ..\onf.exe
copy /y dist\onf.exe ..\onf-%VERSION%.exe

echo.
echo ========================================
echo Build complete:
echo   dist\onf.exe
echo   ..\onf.exe              ^<- run from CMD
echo   ..\Start ONF.bat        ^<- double-click this (recommended)
echo   releases\onf.exe
echo.
echo Double-click = cookie mode, port 9222
echo Captures save: captures\ (exe ke bagal)
echo ========================================
pause
