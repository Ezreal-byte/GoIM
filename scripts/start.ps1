$ErrorActionPreference = 'Stop'
$taskExecutable = Join-Path (Split-Path -Parent $PSScriptRoot) 'build/GoIM.exe'
if (!(Test-Path -LiteralPath $taskExecutable)) { & (Join-Path $PSScriptRoot 'build.ps1') }
Start-Process -FilePath $taskExecutable
