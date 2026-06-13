@echo off
setlocal
title Check Chrome Debug Port
cd /d "%~dp0.."

echo Testing http://127.0.0.1:9222/json/version ...
powershell -NoProfile -Command "try { $r = Invoke-WebRequest -Uri 'http://127.0.0.1:9222/json/version' -UseBasicParsing -TimeoutSec 5; Write-Host 'OK - Chrome debug mode chal raha hai:'; Write-Host $r.Content } catch { Write-Host 'FAIL - Debug port nahi mila. scripts\start_chrome_debug.bat chalao.'; exit 1 }"
echo.
pause
