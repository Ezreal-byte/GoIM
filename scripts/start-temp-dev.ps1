param(
    [string]$Name = 'Temp Dev Client',
    [string]$Listen,
    [switch]$Loopback,
    [switch]$Pair,
    [switch]$NoBuild,
    [switch]$StopExistingGoIM
)

$ErrorActionPreference = 'Stop'
$project = Split-Path -Parent $PSScriptRoot
$runtime = Join-Path $project '.runtime/temp-dev'
if ($Pair) { $Loopback = $true }
if (!$Listen) {
    $Listen = if ($Loopback) { '127.0.0.1:0' } else { '0.0.0.0:0' }
}
if ($Loopback -and $Listen -notmatch '^127\.0\.0\.1:\d+$') {
    throw '-Loopback requires a 127.0.0.1 listen address.'
}

function Assert-LanPortsAvailable {
    $ports = @(
        @{ Port = 47841; Protocol = 'Udp'; Service = 'GoIM discovery' },
        @{ Port = 9011; Protocol = 'Udp'; Service = 'Neiwangtong discovery' },
        @{ Port = 9012; Protocol = 'Udp'; Service = 'Neiwangtong control' },
        @{ Port = 9013; Protocol = 'Tcp'; Service = 'Neiwangtong transfer' },
        @{ Port = 2425; Protocol = 'Udp'; Service = 'IP Messenger' },
        @{ Port = 2425; Protocol = 'Tcp'; Service = 'IP Messenger' }
    )
    $conflicts = foreach ($entry in $ports) {
        $protocol = [Net.Sockets.ProtocolType]$entry.Protocol
        $kind = if ($protocol -eq 'Udp') { 'Dgram' } else { 'Stream' }
        $socket = [Net.Sockets.Socket]::new('InterNetwork', $kind, $protocol)
        try {
            $socket.ExclusiveAddressUse = $true
            $socket.Bind([Net.IPEndPoint]::new([Net.IPAddress]::Any, $entry.Port))
        } catch [Net.Sockets.SocketException] {
            "$($entry.Service) $protocol/$($entry.Port)"
        } finally {
            $socket.Dispose()
        }
    }
    if ($conflicts) {
        throw "LAN ports are in use: $($conflicts -join ', '). Close the owner or start with -Loopback."
    }
}

function Quote-WindowsArgument([string]$Value) {
    # Backslashes before a quote (including the closing quote) must be doubled.
    $escaped = [regex]::Replace($Value, '(\\*)"', '$1$1\"')
    return '"' + [regex]::Replace($escaped, '(\\+)$', '$1$1') + '"'
}

if ($StopExistingGoIM) {
    $buildPattern = Join-Path $project 'build/*'
    $runtimePattern = Join-Path $runtime '*/bin/GoIM.exe'
    Get-Process -Name GoIM -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -like $buildPattern -or $_.Path -like $runtimePattern } |
        Stop-Process
}
if (!$Loopback) { Assert-LanPortsAvailable }

$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$run = Join-Path $runtime "$stamp-$([guid]::NewGuid().ToString('N').Substring(0,8))"
$bin = if ($NoBuild) { Join-Path $project 'build' } else { Join-Path $run 'bin' }
if (!$NoBuild) {
    & (Join-Path $PSScriptRoot 'build.ps1') -OutputDir $bin
}
$exe = Join-Path $bin 'GoIM.exe'
$shotlet = Join-Path $bin 'shotlet/shotlet-capture.exe'
$builtin = Join-Path $bin 'goim-snip.exe'
if (!(Test-Path -LiteralPath $exe -PathType Leaf) -or
    (!(Test-Path -LiteralPath $shotlet -PathType Leaf) -and !(Test-Path -LiteralPath $builtin -PathType Leaf))) {
    throw "GoIM and the screenshot tool are required in $bin. Run scripts/build.ps1 or omit -NoBuild."
}

$started = [System.Collections.Generic.List[System.Diagnostics.Process]]::new()
function Start-Client([string]$Folder, [string]$ClientName, [string]$Address, [string]$Connect = '') {
    $client = Join-Path $run $Folder
    $data = Join-Path $client 'data'
    $downloads = Join-Path $client 'downloads'
    $stderr = Join-Path $client 'stderr.log'
    New-Item -ItemType Directory -Path $data, $downloads -Force | Out-Null
    $arguments = @('--data-dir', $data, '--download-dir', $downloads, '--listen', $Address, '--name', $ClientName)
    if ($Loopback) { $arguments += @('--no-discovery', '--no-compatibility') }
    if ($Connect) { $arguments += @('--connect', $Connect) }
    $commandLine = ($arguments | ForEach-Object { Quote-WindowsArgument $_ }) -join ' '
    $process = Start-Process -FilePath $exe -WorkingDirectory $project -ArgumentList $commandLine -PassThru -RedirectStandardError $stderr
    $started.Add($process)
    $deadline = [Diagnostics.Stopwatch]::StartNew()
    do {
        $process.Refresh()
        if ($process.HasExited) {
            throw "GoIM exited with code $($process.ExitCode). Details: $stderr"
        }
        if ($process.MainWindowHandle -ne [IntPtr]::Zero) { break }
        Start-Sleep -Milliseconds 100
    } while ($deadline.Elapsed.TotalSeconds -lt 15)
    if ($process.MainWindowHandle -eq [IntPtr]::Zero) {
        throw "GoIM did not open its window. Details: $stderr"
    }

    $bindHost, $bindPort = $Address -split ':(?=\d+$)', 2
    $bindHost = $bindHost.Trim([char[]]'[]')
    $listener = Get-NetTCPConnection -State Listen -OwningProcess $process.Id -ErrorAction SilentlyContinue |
        Where-Object {
            ($_.LocalAddress -eq $bindHost -or ($bindHost -eq '0.0.0.0' -and $_.LocalAddress -eq '::')) -and
            ($_.LocalPort -eq $bindPort -or ($bindPort -eq '0' -and $_.LocalPort -notin 2425, 9013))
        } | Select-Object -First 1
    $endpoint = if ($listener) { "${bindHost}:$($listener.LocalPort)" } else { $Address }
    @(
        "PID=$($process.Id)"
        "Name=$ClientName"
        "Listen=$endpoint"
        "DataDir=$data"
        "DownloadDir=$downloads"
        "Executable=$exe"
    ) | Set-Content -LiteralPath (Join-Path $client 'launch.txt') -Encoding UTF8
    Write-Host "GoIM started: $ClientName (PID $($process.Id), $endpoint)"
    Write-Host "Data: $data"
    return $endpoint
}

try {
    if ($Pair) {
        $first = Start-Client 'client-a' 'Loopback A' $Listen
        if ($first -notmatch '^127\.0\.0\.1:[1-9]\d*$') {
            throw 'The first client did not report a loopback listening port.'
        }
        $null = Start-Client 'client-b' 'Loopback B' '127.0.0.1:0' $first
    } else {
        $null = Start-Client 'client' $Name $Listen
    }
} catch {
    foreach ($process in $started) {
        if (!$process.HasExited) { Stop-Process -Id $process.Id -ErrorAction SilentlyContinue }
    }
    throw
} finally {
    foreach ($process in $started) { $process.Dispose() }
}
