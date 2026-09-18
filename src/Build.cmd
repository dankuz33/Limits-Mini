@echo off
setlocal
cd /d "%~dp0"
where go >nul 2>nul
if errorlevel 1 (
  echo Go SDK is required only to rebuild from source.
  pause
  exit /b 1
)
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go test ./...
if errorlevel 1 (pause & exit /b 1)
go build -trimpath -ldflags="-s -w -H=windowsgui" -o "..\LimitsMini.exe" .
if errorlevel 1 (pause & exit /b 1)
echo Build completed.
pause
