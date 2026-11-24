@echo off
setlocal EnableExtensions
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo [ERROR] Go is required for dev mode.
  pause
  exit /b 1
)
where npm >nul 2>nul
if errorlevel 1 (
  echo [ERROR] Node.js and npm are required for dev mode.
  pause
  exit /b 1
)

where wails >nul 2>nul
if not errorlevel 1 (
  echo [ModHub] Starting Wails Desktop Dev Mode (native window + Vite HMR)...
  pushd backend
  call wails dev
  popd
  exit /b %errorlevel%
)

echo [ModHub] Wails CLI not installed - falling back to Web Dev Mode (Go API :8080 + Vite :5173)...
pushd frontend
if not exist "node_modules\@tanstack\react-virtual" call npm install --no-audit --no-fund
popd

start "ModHub Go API (Web Dev)" cmd /k "cd /d %~dp0backend && set CGO_ENABLED=0 && set MODHUB_WEB=1 && set NO_BROWSER=1 && set NO_TRAY=1 && go run ."
timeout /t 2 /nobreak >nul
pushd frontend
call npm run dev
popd
