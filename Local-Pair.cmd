@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\start-dual.ps1" %*
exit /b %ERRORLEVEL%
