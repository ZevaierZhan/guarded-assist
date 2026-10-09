@echo off
chcp 65001 >nul
cd /d "%~dp0"
assist-server.exe
if errorlevel 1 pause
