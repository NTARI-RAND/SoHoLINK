@echo off
set SOHOLINK_INSECURE_NO_TLS=1
cd /d "%~dp0"
start "" "%~dp0soholink-launcher.exe"
