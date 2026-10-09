@echo off
setlocal
cd /d "%~dp0"
if not exist dist mkdir dist
set CGO_ENABLED=0
set GOPROXY=off
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o dist\assist.exe .\cmd\assist
if errorlevel 1 exit /b 1
go build -trimpath -ldflags="-s -w" -o dist\assist-server.exe .\cmd\assist-server
if errorlevel 1 exit /b 1
go build -trimpath -ldflags="-s -w" -o dist\assistctl.exe .\cmd\assistctl
if errorlevel 1 exit /b 1
set GOOS=darwin
set GOARCH=arm64
go build -trimpath -ldflags="-s -w" -o dist\assist-darwin-arm64 .\cmd\assist
if errorlevel 1 exit /b 1
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o dist\assist-darwin-amd64 .\cmd\assist
if errorlevel 1 exit /b 1
echo Build complete.
