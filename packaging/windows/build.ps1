param(
    [Parameter(Mandatory)][ValidatePattern('^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$')][string]$Version,
    [Parameter(Mandatory)][ValidateSet('amd64', 'arm64')][string]$Architecture,
    [string]$Output = 'dist',
    [string]$Dotnet = 'dotnet'
)

$ErrorActionPreference = 'Stop'
$name = "GoIM_${Version}_windows_$Architecture"
$outputPath = (Resolve-Path -LiteralPath $Output).Path
$payload = (Resolve-Path -LiteralPath (Join-Path $outputPath $name)).Path
$msi = Join-Path $outputPath "$name.msi"
$arch = if ($Architecture -eq 'amd64') { 'x64' } else { 'arm64' }
$wixVersion = '5.0.2'
$toolPath = Join-Path $outputPath '.wix-tools'
$wix = Join-Path $toolPath 'wix.exe'
$env:DOTNET_ROLL_FORWARD = 'Major'

function Invoke-Checked([string]$Command, [string[]]$Arguments) {
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Command failed (exit $LASTEXITCODE)" }
}

if (!(Test-Path -LiteralPath $wix)) {
    Invoke-Checked $Dotnet @('tool', 'install', 'wix', '--version', $wixVersion, '--tool-path', $toolPath)
}
Push-Location $toolPath
try {
    Invoke-Checked $wix @('extension', 'add', "WixToolset.Util.wixext/$wixVersion")
    $productVersion = ($Version -split '-', 2)[0]
    Invoke-Checked $wix @('build', '-ext', 'WixToolset.Util.wixext', '-arch', $arch, '-d', "ProductVersion=$productVersion",
        '-d', "Payload=$payload", '-bindpath', "payload=$payload", '-out', $msi,
        (Join-Path $PSScriptRoot 'product.wxs'))
} finally {
    Pop-Location
}

$hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $msi).Hash.ToLowerInvariant()
"$hash  $name.msi" | Set-Content -LiteralPath "$msi.sha256" -Encoding ascii
