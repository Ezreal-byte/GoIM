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
$setup = Join-Path $outputPath "$name-Setup.exe"
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
    Invoke-Checked $wix @('extension', 'add', "WixToolset.Bal.wixext/$wixVersion")
    $productVersion = ($Version -split '-', 2)[0]
    Invoke-Checked $wix @('build', '-arch', $arch, '-d', "ProductVersion=$productVersion",
        '-d', "Payload=$payload", '-bindpath', "payload=$payload", '-out', $msi,
        (Join-Path $PSScriptRoot 'product.wxs'))
    Invoke-Checked $wix @('build', '-arch', $arch, '-d', "Version=$Version", '-d', "Payload=$payload",
        '-d', "MSI=$msi", '-ext', 'WixToolset.Bal.wixext', '-out', $setup,
        (Join-Path $PSScriptRoot 'setup.wxs'))
} finally {
    Pop-Location
}

# Check the MSI platform and its contents without installing on the runner.
$installer = New-Object -ComObject WindowsInstaller.Installer
$database = $installer.GetType().InvokeMember('OpenDatabase', 'InvokeMethod', $null, $installer, @($msi, 0))
$summary = $database.GetType().InvokeMember('SummaryInformation', 'GetProperty', $null, $database, @(0))
$template = $summary.GetType().InvokeMember('Property', 'GetProperty', $null, $summary, @(7))
if (($template -split ';')[0] -ine $arch) { throw "Unexpected MSI architecture: $template" }
$view = $database.GetType().InvokeMember('OpenView', 'InvokeMethod', $null, $database, @('SELECT `FileName` FROM `File`'))
$null = $view.GetType().InvokeMember('Execute', 'InvokeMethod', $null, $view, $null)
$files = while ($record = $view.GetType().InvokeMember('Fetch', 'InvokeMethod', $null, $view, $null)) {
    ($record.GetType().InvokeMember('StringData', 'GetProperty', $null, $record, @(1)) -split '\|')[-1]
}
$null = $view.GetType().InvokeMember('Close', 'InvokeMethod', $null, $view, $null)
foreach ($file in 'GoIM.exe', 'goim-snip.exe', 'GoIM.ico', 'build-info.json', 'LICENSE') {
    if ($files -notcontains $file) { throw "Missing MSI payload: $file" }
}

$setupBytes = [IO.File]::ReadAllBytes($setup)
$header = [BitConverter]::ToInt32($setupBytes, 0x3c)
$expectedMachine = if ($arch -eq 'x64') { 0x8664 } else { 0xaa64 }
if ([BitConverter]::ToUInt32($setupBytes, $header) -ne 0x4550 -or
    [BitConverter]::ToUInt16($setupBytes, $header + 4) -ne $expectedMachine) {
    throw 'Unexpected Setup.exe architecture'
}

foreach ($file in $msi, $setup) {
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $file).Hash.ToLowerInvariant()
    "$hash  $([IO.Path]::GetFileName($file))" | Set-Content -LiteralPath "$file.sha256" -Encoding ascii
}
