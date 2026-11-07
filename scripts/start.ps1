$ErrorActionPreference = 'Stop'
$taskProject = Split-Path -Parent $PSScriptRoot
$taskExecutable = Join-Path $taskProject 'build/optimized/GoIM.exe'
if (!(Test-Path -LiteralPath $taskExecutable)) { $taskExecutable = Join-Path $taskProject 'build/GoIM.exe' }
if (!(Test-Path -LiteralPath $taskExecutable)) { & (Join-Path $PSScriptRoot 'build.ps1') }
Start-Process -FilePath $taskExecutable
