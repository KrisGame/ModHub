@echo off
setlocal EnableExtensions
cd /d "%~dp0"

if exist "modhub.exe" (
  start "" "%~dp0modhub.exe"
  exit /b 0
)

call "%~dp0build.bat" --from-start
if errorlevel 1 (
  echo.
  echo [ERROR] Build failed. Check the messages above.
  pause
  exit /b 1
)

if exist "modhub.exe" (
  start "" "%~dp0modhub.exe"
  exit /b 0
)

echo [ERROR] modhub.exe was not found after build.
pause
exit /b 1
