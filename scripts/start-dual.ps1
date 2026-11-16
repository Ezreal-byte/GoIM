param([switch]$NoBuild)

$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'start-temp-dev.ps1') -Pair -NoBuild:$NoBuild
