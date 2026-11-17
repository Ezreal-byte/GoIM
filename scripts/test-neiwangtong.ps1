#Requires -Version 7.2
param(
    [ValidateSet('Loopback', 'Reference', 'Sandbox')][string]$Mode = 'Loopback',
    [string]$OutputDir,
    [string]$Bind,
    [string]$Target,
    [string]$Vmx,
    [string]$Vmrun = 'vmrun.exe',
    [string]$Adapter = 'VMware Network Adapter VMnet8',
    [string]$Version = '3.4.3055',
    [string]$Peer,
    [string]$SandboxOriginalDir = '/opt/goim-lab/original',
    [switch]$Avatar,
    [switch]$TextReceipt,
    [switch]$SandboxSuite,
    [switch]$LargeFile,
    [switch]$StockLargeFile,
    [switch]$OfflineReplay,
    [switch]$OfflineSend,
    [switch]$MultiGroup,
    [switch]$GroupManagement,
    [switch]$GroupFile,
    [switch]$GroupFolder,
    [switch]$Emoticons,
    [switch]$AvatarUpdate,
    [switch]$TextRecall,
    [switch]$ResumeReceive,
    [switch]$DirectoryRetry,
    [switch]$DirectoryResend
)
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
$special = @($LargeFile, $StockLargeFile, $OfflineReplay, $OfflineSend, $MultiGroup, $GroupManagement, $GroupFile, $GroupFolder, $Emoticons, $AvatarUpdate, $TextRecall,
    $ResumeReceive, $DirectoryRetry, $DirectoryResend | Where-Object { $_ })
if ($special.Count -gt 1 -or ($special.Count -gt 0 -and $Mode -ne 'Sandbox')) {
    throw 'Each special check requires a separate Sandbox run.'
}
if ($SandboxSuite -and ($Mode -ne 'Sandbox' -or $special.Count -gt 0)) {
    throw '-SandboxSuite requires Sandbox mode without individual check switches.'
}
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

function Invoke-ReferenceSandbox {
    if ($Bind -or $Target -or $Vmx -or $Peer -or $Avatar -or $Version -ne '3.4.3055') {
        throw 'Sandbox mode uses the stock 3.4.3055 client, a fresh profile, and fixed isolated addresses.'
    }
    $distro = 'GoIM-Neiwangtong-Lab'
    $linux = @('-d', $distro, '-u', 'nwtlab', '--exec')
    $stage = (& wsl.exe @linux mktemp -d /tmp/goim-nwt-tools.XXXXXXXX | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $stage -cnotmatch '^/tmp/goim-nwt-tools\.[A-Za-z0-9]{8}$') {
        throw "The dedicated WSL distro $distro is unavailable. See docs/neiwangtong-testing.md."
    }
    $hostStage = "\\wsl.localhost\$distro" + $stage.Replace('/', '\')
    $binary = New-TemporaryFile
    $buildEnv = @{ GOOS = 'linux'; GOARCH = 'amd64'; CGO_ENABLED = '0' }
    $savedEnv = @{}
    try {
        foreach ($name in $buildEnv.Keys) {
            $savedEnv[$name] = [Environment]::GetEnvironmentVariable($name)
            [Environment]::SetEnvironmentVariable($name, $buildEnv[$name])
        }
        & go build -o $binary.FullName ./cmd/neiwangtong-check
        if ($LASTEXITCODE -ne 0) { throw 'Failed to build the Linux reference checker.' }
        Copy-Item -LiteralPath $binary.FullName -Destination (Join-Path $hostStage 'check')
        $driverSource = Join-Path $PSScriptRoot 'neiwangtong-lab'
        Copy-Item -LiteralPath (Join-Path $driverSource 'drive.c') -Destination $hostStage
        foreach ($script in 'run.sh', 'stock-pair.sh') {
            $source = Get-Content -LiteralPath (Join-Path $driverSource $script) -Raw
            $source.Replace("`r`n", "`n") | Set-Content -LiteralPath (Join-Path $hostStage $script) -Encoding utf8NoBOM -NoNewline
        }
        & wsl.exe @linux chmod +x "$stage/run.sh" "$stage/check"
        if ($LASTEXITCODE -ne 0) { throw 'Failed to stage the Linux reference checker.' }
        & wsl.exe @linux i686-w64-mingw32-gcc -municode -O2 -Wall -Wextra -Werror -o "$stage/drive.exe" "$stage/drive.c"
        if ($LASTEXITCODE -ne 0) { throw 'Failed to build the reference fixture. Install the dependencies listed in docs/neiwangtong-testing.md.' }
        $runs = if ($SandboxSuite) {
            @(
                @{ Name = 'standard'; Option = '' },
                @{ Name = 'offline-from-original'; Option = '--offline-replay' },
                @{ Name = 'offline-from-goim'; Option = '--offline-send' },
                @{ Name = 'three-client-group'; Option = '--multi-group' },
                @{ Name = 'resume-on-original'; Option = '--resume-receive' },
                @{ Name = 'avatar-update'; Option = '--avatar-update' },
                @{ Name = 'text-recall'; Option = '--text-recall' },
                @{ Name = 'group-recall'; Option = '--group-recall' },
                @{ Name = 'original-group-dismiss'; Option = '--group-dismiss' },
                @{ Name = 'original-group-kick'; Option = '--group-kick' },
                @{ Name = 'group-file'; Option = '--group-file' },
                @{ Name = 'group-folder'; Option = '--group-folder' },
                @{ Name = 'emoticons'; Option = '--emoticons' },
                @{ Name = 'folder-resend-from-original'; Option = '--directory-resend-from-reference' },
                @{ Name = 'folder-resend-to-original'; Option = '--directory-resend-to-reference' }
            )
        } elseif ($LargeFile) {
            @(
                @{ Name = 'from-original'; Option = '--large-from-reference' },
                @{ Name = 'to-original'; Option = '--large-to-reference' }
            )
        } elseif ($StockLargeFile) {
            @(
                @{ Name = 'stock-control'; Option = '--large-stock-control' },
                @{ Name = 'stock-4gib'; Option = '--large-stock-boundary' }
            )
        } elseif ($TextRecall) {
            @(
                @{ Name = 'text-recall'; Option = '--text-recall' },
                @{ Name = 'group-recall'; Option = '--group-recall' }
            )
        } elseif ($GroupManagement) {
            @(
                @{ Name = 'three-client-group'; Option = '--multi-group' },
                @{ Name = 'group-recall'; Option = '--group-recall' },
                @{ Name = 'original-group-dismiss'; Option = '--group-dismiss' },
                @{ Name = 'original-group-kick'; Option = '--group-kick' }
            )
        } elseif ($DirectoryRetry -or $DirectoryResend) {
            $prefix = if ($DirectoryResend) { '--directory-resend' } else { '--directory' }
            @(
                @{ Name = 'from-original'; Option = "$prefix-from-reference" },
                @{ Name = 'to-original'; Option = "$prefix-to-reference" }
            )
        } else {
            $option = if ($OfflineReplay) { '--offline-replay' }
                      elseif ($OfflineSend) { '--offline-send' }
                      elseif ($MultiGroup) { '--multi-group' }
                      elseif ($GroupFile) { '--group-file' }
                      elseif ($GroupFolder) { '--group-folder' }
                      elseif ($Emoticons) { '--emoticons' }
                      elseif ($AvatarUpdate) { '--avatar-update' }
                      elseif ($ResumeReceive) { '--resume-receive' }
                      else { '' }
            @(@{ Name = ''; Option = $option })
        }
        $failed = [Collections.Generic.List[string]]::new()
        foreach ($run in $runs) {
            $reportDir = if ($run.Name) { Join-Path $OutputDir $run.Name } else { $OutputDir }
            New-Item -ItemType Directory -Path $reportDir -Force | Out-Null
            $report = Join-Path $reportDir 'sandbox.json'
            $labOptions = @($SandboxOriginalDir)
            if ($run.Option) { $labOptions += $run.Option }
            & wsl.exe -d $distro -u root --exec "$stage/run.sh" @labOptions | Tee-Object -FilePath $report
            if ($LASTEXITCODE -ne 0) { $failed.Add($report) } else { Write-Host "Sandbox report: $report" }
        }
        if ($failed.Count) { throw "Reference sandbox checks failed. See: $($failed -join ', ')" }
    } finally {
        foreach ($name in $savedEnv.Keys) { [Environment]::SetEnvironmentVariable($name, $savedEnv[$name]) }
        Remove-Item -LiteralPath $binary.FullName -ErrorAction SilentlyContinue
        & wsl.exe @linux rm -f -- "$stage/run.sh" "$stage/stock-pair.sh" "$stage/check" "$stage/drive.c" "$stage/drive.exe"
        & wsl.exe @linux rmdir -- "$stage"
    }
}

Push-Location $root
try {
    if ($Mode -eq 'Loopback') {
        Invoke-GoTests 'wire.jsonl' @('./internal/neiwangtong') 'TestServiceLocalLoopback'
        Invoke-GoTests 'core.jsonl' @('./internal/core', '-run', '^TestNeiwangtong') 'TestNeiwangtongDirectCommunication'
        Write-Host "Loopback reports: $OutputDir"
        return
    }
    if ($Mode -eq 'Sandbox') {
        Invoke-ReferenceSandbox
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
