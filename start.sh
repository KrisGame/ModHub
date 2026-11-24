#!/usr/bin/env bash
set -e
cd "$(dirname "$0")"
if [ ! -f "backend/webdist/index.html" ]; then
  (
    cd frontend
    if [ ! -d node_modules ]; then
      if [ -f package-lock.json ]; then
        npm ci --no-audit --no-fund
      else
        npm install --no-audit --no-fund
      fi
    fi
    npm run build
  )
fi
if [ ! -f "./modhub" ]; then
  (
    cd backend
    if [ -f go.sum ]; then
      go mod download
    else
      go mod tidy
    fi
    if command -v wails >/dev/null 2>&1; then
      wails build -s -o modhub
      cp build/bin/modhub ../modhub
    else
      go build -tags "desktop,production" -ldflags="-s -w" -o ../modhub .
    fi
  )
fi
./modhub "$@"
