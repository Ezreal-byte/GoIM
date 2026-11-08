@echo off
setlocal
set "GOIM_EXE=%~dp0build\optimized\GoIM.exe"
if not exist "%GOIM_EXE%" set "GOIM_EXE=%~dp0build\GoIM.exe"
if not exist "%GOIM_EXE%" (
  echo Build or download the Windows package first. See README.md.
  pause
  exit /b 1
)
start "GoIM Loopback A" "%GOIM_EXE%" --data-dir "%~dp0.runtime\pair-A" --download-dir "%~dp0.runtime\pair-A-downloads" --listen 127.0.0.1:47911 --no-discovery --no-ipmsg --name "Loopback A"
start "GoIM Loopback B" "%GOIM_EXE%" --data-dir "%~dp0.runtime\pair-B" --download-dir "%~dp0.runtime\pair-B-downloads" --listen 127.0.0.1:47912 --no-discovery --no-ipmsg --name "Loopback B" --connect 127.0.0.1:47911
