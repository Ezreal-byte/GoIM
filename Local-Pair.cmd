@echo off
setlocal
if not exist "%~dp0build\GoIM.exe" (
  echo Build or download the Windows package first. See README.md.
  pause
  exit /b 1
)
start "GoIM Loopback A" "%~dp0build\GoIM.exe" --data-dir "%~dp0.runtime\pair-A" --download-dir "%~dp0.runtime\pair-A-downloads" --listen 127.0.0.1:47911 --no-discovery --name "Loopback A"
start "GoIM Loopback B" "%~dp0build\GoIM.exe" --data-dir "%~dp0.runtime\pair-B" --download-dir "%~dp0.runtime\pair-B-downloads" --listen 127.0.0.1:47912 --no-discovery --name "Loopback B" --connect 127.0.0.1:47911
