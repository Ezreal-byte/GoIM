param([switch]$Headless)
$ErrorActionPreference = 'Stop'
Push-Location (Split-Path -Parent $PSScriptRoot)
try {
    New-Item -ItemType Directory -Path build -Force | Out-Null
    if ($Headless) {
        go build -tags vivahead -trimpath -o build/goim-headless.exe ./cmd/goim
    } else {
        go build -trimpath -ldflags '-s -w -H=windowsgui' -o build/GoIM.exe ./cmd/goim
    }
    if ($LASTEXITCODE -ne 0) { throw 'GoIM build failed' }
} finally { Pop-Location }
