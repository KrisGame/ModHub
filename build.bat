@echo off
setlocal EnableExtensions
cd /d "%~dp0"

set "FROM_START=0"
if /i "%~1"=="--from-start" set "FROM_START=1"

set "NODE_VER=22.12.0"
set "GO_VER=1.23.4"

set "GO_ARCH=amd64"
set "NODE_ARCH=x64"
if /i "%PROCESSOR_ARCHITECTURE%"=="ARM64" (
  set "GO_ARCH=arm64"
  set "NODE_ARCH=arm64"
)
if /i "%PROCESSOR_ARCHITEW6432%"=="ARM64" (
  set "GO_ARCH=arm64"
  set "NODE_ARCH=arm64"
)

set "TOOL_DIR=%TEMP%\modhub-portable-tools"
set "USED_PORTABLE=0"
set "USED_PORTABLE_GO=0"

taskkill /F /IM modhub.exe >nul 2>nul

set "NEED_FRONTEND=1"
if "%FROM_START%"=="1" (
  if exist "backend\webdist\index.html" set "NEED_FRONTEND=0"
)

if "%NEED_FRONTEND%"=="1" (
  call :ensure_node
  if errorlevel 1 goto :fail

  echo [1/2] Building React + TypeScript + Vite frontend into backend\webdist...
  pushd frontend
  if exist "package-lock.json" (
    call npm ci --no-audit --no-fund
  ) else (
    call npm install --no-audit --no-fund
  )
  if errorlevel 1 (
    popd
    goto :fail
  )
  call npm run build
  if errorlevel 1 (
    popd
    goto :fail
  )
  popd
) else (
  echo [1/2] Frontend already built in backend\webdist - skipping...
)

call :ensure_go
if errorlevel 1 goto :fail

echo [2/2] Building ModHub Desktop (Wails v2, native Windows window)...
set "CGO_ENABLED=0"
pushd backend
if exist "go.sum" (
  go mod download
) else (
  go mod tidy
)
if errorlevel 1 (
  popd
  goto :fail
)

set "MS_CLIENT_LDFLAGS="
if defined MODHUB_MS_CLIENT_ID (
  set "MS_CLIENT_LDFLAGS=-X modhub/internal/accounts.DefaultClientID=%MODHUB_MS_CLIENT_ID%"
)

where wails >nul 2>nul
if not errorlevel 1 (
  echo [ModHub] Using Wails CLI for build with Windows icon and DPI manifest...
  call wails build -s -clean -ldflags "%MS_CLIENT_LDFLAGS%" -o modhub.exe
  if errorlevel 1 (
    popd
    goto :fail
  )
  if exist "build\bin\modhub.exe" (
    copy /Y "build\bin\modhub.exe" "..\modhub.exe" >nul
  )
) else (
  echo [ModHub] Wails CLI not found - building directly via go build -tags desktop,production...
  go build -tags "desktop,production" -ldflags="-s -w -H=windowsgui %MS_CLIENT_LDFLAGS%" -o ..\modhub.exe .
  if errorlevel 1 (
    popd
    goto :fail
  )
)
popd

call :cleanup_tools
echo [OK] ModHub Desktop (modhub.exe) built successfully!
if "%FROM_START%"=="0" pause
exit /b 0

:fail
call :cleanup_tools
echo [ERROR] Build failed!
if "%FROM_START%"=="0" pause
exit /b 1

:ensure_node
where node >nul 2>nul
if not errorlevel 1 (
  where npm >nul 2>nul
  if not errorlevel 1 exit /b 0
)
set "NODE_HOME=%TOOL_DIR%\node\node-v%NODE_VER%-win-%NODE_ARCH%"
if exist "%NODE_HOME%\npm.cmd" (
  set "USED_PORTABLE=1"
  set "PATH=%NODE_HOME%;%PATH%"
  set "npm_config_cache=%TOOL_DIR%\npm-cache"
  exit /b 0
)
echo [ModHub] Node.js not found - downloading portable Node.js v%NODE_VER%...
set "USED_PORTABLE=1"
if not exist "%TOOL_DIR%\node" mkdir "%TOOL_DIR%\node" >nul 2>nul
set "NODE_URL=https://nodejs.org/dist/v%NODE_VER%/node-v%NODE_VER%-win-%NODE_ARCH%.zip"
set "NODE_ZIP=%TOOL_DIR%\node.zip"
call :download_file "%NODE_URL%" "%NODE_ZIP%"
if errorlevel 1 exit /b 1
call :extract_zip "%NODE_ZIP%" "%TOOL_DIR%\node"
del /f /q "%NODE_ZIP%" >nul 2>nul
if not exist "%NODE_HOME%\npm.cmd" exit /b 1
set "PATH=%NODE_HOME%;%PATH%"
set "npm_config_cache=%TOOL_DIR%\npm-cache"
exit /b 0

:ensure_go
where go >nul 2>nul
if not errorlevel 1 (
  set "CGO_ENABLED=0"
  exit /b 0
)
set "GO_HOME=%TOOL_DIR%\go"
if exist "%GO_HOME%\bin\go.exe" (
  set "USED_PORTABLE=1"
  set "USED_PORTABLE_GO=1"
  set "GOROOT=%GO_HOME%"
  set "GOPATH=%TOOL_DIR%\gopath"
  set "GOCACHE=%TOOL_DIR%\gocache"
  set "GOTOOLCHAIN=local"
  set "CGO_ENABLED=0"
  set "PATH=%GO_HOME%\bin;%PATH%"
  exit /b 0
)
echo [ModHub] Go not found - downloading portable Go v%GO_VER%...
set "USED_PORTABLE=1"
set "USED_PORTABLE_GO=1"
if not exist "%TOOL_DIR%" mkdir "%TOOL_DIR%" >nul 2>nul
set "GO_URL=https://go.dev/dl/go%GO_VER%.windows-%GO_ARCH%.zip"
set "GO_ZIP=%TOOL_DIR%\go.zip"
call :download_file "%GO_URL%" "%GO_ZIP%"
if errorlevel 1 exit /b 1
call :extract_zip "%GO_ZIP%" "%TOOL_DIR%"
del /f /q "%GO_ZIP%" >nul 2>nul
if not exist "%GO_HOME%\bin\go.exe" exit /b 1
set "GOROOT=%GO_HOME%"
set "GOPATH=%TOOL_DIR%\gopath"
set "GOCACHE=%TOOL_DIR%\gocache"
set "GOTOOLCHAIN=local"
set "CGO_ENABLED=0"
set "PATH=%GO_HOME%\bin;%PATH%"
exit /b 0

:download_file
set "DL_URL=%~1"
set "DL_OUT=%~2"
where curl.exe >nul 2>nul
if not errorlevel 1 (
  curl.exe -fSL --retry 3 --connect-timeout 20 -# -o "%DL_OUT%" "%DL_URL%"
  if not errorlevel 1 exit /b 0
)
powershell -NoProfile -ExecutionPolicy Bypass -Command "$ProgressPreference='SilentlyContinue'; [Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12; Invoke-WebRequest -UseBasicParsing -Uri $env:DL_URL -OutFile $env:DL_OUT"
exit /b %errorlevel%

:extract_zip
set "EX_ZIP=%~1"
set "EX_DEST=%~2"
if not exist "%EX_DEST%" mkdir "%EX_DEST%" >nul 2>nul
where tar.exe >nul 2>nul
if not errorlevel 1 (
  tar.exe -xf "%EX_ZIP%" -C "%EX_DEST%"
  if not errorlevel 1 exit /b 0
)
powershell -NoProfile -ExecutionPolicy Bypass -Command "$ProgressPreference='SilentlyContinue'; Expand-Archive -LiteralPath $env:EX_ZIP -DestinationPath $env:EX_DEST -Force"
exit /b %errorlevel%

:cleanup_tools
if "%USED_PORTABLE%"=="0" exit /b 0
if "%USED_PORTABLE_GO%"=="1" (
  if exist "%TOOL_DIR%\go\bin\go.exe" "%TOOL_DIR%\go\bin\go.exe" clean -modcache >nul 2>nul
)
if exist "%TOOL_DIR%" (
  attrib -r "%TOOL_DIR%\*.*" /s /d >nul 2>nul
  rmdir /s /q "%TOOL_DIR%" >nul 2>nul
)
exit /b 0
