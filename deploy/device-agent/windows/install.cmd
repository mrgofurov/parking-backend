@echo off
rem Right-click -> "Run as administrator"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0install.ps1"
pause
