@echo off
chcp 65001 >nul
cd /d "%~dp0"
assist.exe --uninstall
if errorlevel 1 pause
