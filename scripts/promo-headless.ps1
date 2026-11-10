param([string]$OutputDir = 'artifacts/promo/screenshots')
$ErrorActionPreference = 'Stop'
Push-Location (Split-Path -Parent $PSScriptRoot)
$previousPromoDir = $env:GOIM_PROMO_DIR
try {
    $promoDir = $OutputDir
    if (![IO.Path]::IsPathRooted($promoDir)) {
        $promoDir = Join-Path (Get-Location).Path $promoDir
    }
    $env:GOIM_PROMO_DIR = [IO.Path]::GetFullPath($promoDir)
    New-Item -ItemType Directory -Path $env:GOIM_PROMO_DIR -Force | Out-Null
    go test -tags vivahead ./internal/desktop -run '^TestPromoScreenshots$' -count=1 -v
    if ($LASTEXITCODE -ne 0) { throw 'GoIM promotional screenshot render failed.' }
} finally {
    $env:GOIM_PROMO_DIR = $previousPromoDir
    Pop-Location
}
