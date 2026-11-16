#Requires -Version 7.2
param(
    [ValidateSet('Loopback', 'Reference')][string]$Mode = 'Loopback',
    [string]$OutputDir,
    [string]$Bind,
    [string]$Target,
    [string]$Vmx,
    [string]$Vmrun = 'vmrun.exe',
    [string]$Adapter = 'VMware Network Adapter VMnet8',
    [string]$Version = '3.4.3055',
    [string]$Peer,
    [switch]$Avatar,
    [switch]$TextReceipt
)
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
$root = Split-Path -Parent $PSScriptRoot
if (!$OutputDir) {
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    $OutputDir = "artifacts/neiwangtong/$stamp-$([guid]::NewGuid().ToString('N').Substring(0,8))"
}
$OutputDir = [IO.Path]::GetFullPath($OutputDir, $root)
New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null

function Invoke-GoTests([string]$FileName, [string[]]$TestArguments, [string]$RequiredTest) {
    $path = Join-Path $OutputDir $FileName
    & go test -json -count=1 -timeout=90s @TestArguments | Set-Content -LiteralPath $path -Encoding utf8
    if ($LASTEXITCODE -ne 0) {
        Get-Content -LiteralPath $path | ForEach-Object {
            $event = ConvertFrom-Json -InputObject $_
            if ($event.Action -eq 'output') { Write-Host $event.Output -NoNewline }
        }
        throw "Neiwangtong test failed. See $path"
    }
    $requiredPassed = Get-Content -LiteralPath $path | ForEach-Object {
        ConvertFrom-Json -InputObject $_
    } | Where-Object { $_.Test -eq $RequiredTest -and $_.Action -eq 'pass' } | Select-Object -First 1
    if (!$requiredPassed) { throw "$RequiredTest did not run successfully on this host. See $path" }
    Write-Host "PASS: $FileName"
}

Push-Location $root
try {
    if ($Mode -eq 'Loopback') {
        Invoke-GoTests 'wire.jsonl' @('./internal/neiwangtong') 'TestServiceLocalLoopback'
        Invoke-GoTests 'core.jsonl' @('./internal/core', '-run', '^TestNeiwangtong') 'TestNeiwangtongDirectCommunication'
        Write-Host "Loopback reports: $OutputDir"
        return
    }

    if ($Vmx) {
        if ($Target) { throw 'Specify either -Vmx or -Target.' }
        $resolvedVM = (Resolve-Path -LiteralPath $Vmx).Path
        $guest = (& $Vmrun -T ws getGuestIPAddress $resolvedVM | Out-String).Trim()
        if ($LASTEXITCODE -ne 0 -or $guest -notmatch '^\d{1,3}(?:\.\d{1,3}){3}$') {
            throw 'VMware Tools did not report an IPv4 address for the running test VM.'
        }
        $Target = "${guest}:9011"
        if (!$Bind) {
            $addresses = @(Get-NetIPAddress -InterfaceAlias $Adapter -AddressFamily IPv4 | Where-Object AddressState -eq 'Preferred')
            if ($addresses.Count -ne 1) {
                throw "Expected one preferred IPv4 address on $Adapter. Supply -Bind to select it explicitly."
            }
            $Bind = $addresses[0].IPAddress
        }
    }
    if (!$Bind -or !$Target) { throw 'Reference mode requires -Bind and -Target (IPv4:port), or an explicit -Vmx.' }
    if ($TextReceipt -and !$Peer) { throw '-TextReceipt requires -Peer with the exact lab nickname.' }

    $checkArguments = @('-bind', $Bind, '-target', $Target, '-version', $Version)
    if ($Peer) { $checkArguments += @('-peer', $Peer) }
    if ($Avatar) { $checkArguments += '-avatar' }
    if ($TextReceipt) { $checkArguments += '-text-receipt' }
    $report = Join-Path $OutputDir 'reference.json'
    & go run ./cmd/neiwangtong-check @checkArguments | Tee-Object -FilePath $report
    if ($LASTEXITCODE -ne 0) { throw "Reference check failed. See $report" }
    Write-Host "Reference report: $report"
} finally {
    Pop-Location
}
