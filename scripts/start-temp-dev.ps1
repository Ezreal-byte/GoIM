param(
    [string]$Name = 'Temp Dev Client',
    [string]$Listen = '0.0.0.0:0',
    [switch]$NoBuild,
    [switch]$StopExistingGoIM
)

$ErrorActionPreference = 'Stop'
$taskProject = Split-Path -Parent $PSScriptRoot
$taskExecutable = Join-Path $taskProject 'build/GoIM.exe'

function Test-UdpBind {
    param([int]$Port)
    $taskSocket = [System.Net.Sockets.UdpClient]::new()
    try {
        $taskSocket.Client.Bind([System.Net.IPEndPoint]::new([System.Net.IPAddress]::Any, $Port))
        return $true
    } catch {
        return $false
    } finally {
        $taskSocket.Dispose()
    }
}

function Test-TcpBind {
    param([int]$Port)
    $taskListener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Any, $Port)
    try {
        $taskListener.Start()
        return $true
    } catch {
        return $false
    } finally {
        try { $taskListener.Stop() } catch {}
    }
}

function Get-PortOwner {
    param([int]$Port)
    $taskIds = @()
    $taskIds += Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue |
        Select-Object -ExpandProperty OwningProcess
    $taskIds += Get-NetUDPEndpoint -LocalPort $Port -ErrorAction SilentlyContinue |
        Select-Object -ExpandProperty OwningProcess
    $taskIds |
        Where-Object { $_ -gt 0 } |
        Sort-Object -Unique |
        ForEach-Object {
            $taskProcess = Get-Process -Id $_ -ErrorAction SilentlyContinue
            if ($taskProcess) { "$($taskProcess.ProcessName) (PID $($taskProcess.Id))" }
        }
}

if (!$NoBuild) {
    & (Join-Path $PSScriptRoot 'build.ps1')
    if ($LASTEXITCODE -ne 0) { throw 'GoIM build failed' }
}
if (!(Test-Path -LiteralPath $taskExecutable)) {
    throw "Missing $taskExecutable. Run scripts/build.ps1 first or omit -NoBuild."
}

$taskPortRequirements = @(
    @{ Port = 9011; Protocol = 'UDP'; Label = 'Neiwangtong discovery' },
    @{ Port = 9012; Protocol = 'UDP'; Label = 'Neiwangtong control' },
    @{ Port = 9013; Protocol = 'TCP'; Label = 'Neiwangtong transfer' },
    @{ Port = 2425; Protocol = 'UDP'; Label = 'IPMSG compatibility' },
    @{ Port = 2425; Protocol = 'TCP'; Label = 'IPMSG compatibility' },
    @{ Port = 47841; Protocol = 'UDP'; Label = 'GoIM discovery' }
)
$taskConflicts = @()
foreach ($taskRequirement in $taskPortRequirements) {
    $taskAvailable = if ($taskRequirement.Protocol -eq 'UDP') {
        Test-UdpBind -Port $taskRequirement.Port
    } else {
        Test-TcpBind -Port $taskRequirement.Port
    }
    if (!$taskAvailable) {
        $taskOwner = Get-PortOwner -Port $taskRequirement.Port
        if (!$taskOwner) { $taskOwner = 'unknown process' }
        $taskConflicts += "$($taskRequirement.Label) $($taskRequirement.Protocol) $($taskRequirement.Port): $taskOwner"
    }
}

if ($StopExistingGoIM) {
    Get-Process -Name 'GoIM', 'goim' -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -like (Join-Path $taskProject 'build/*') } |
        ForEach-Object {
            Stop-Process -Id $_.Id
            Write-Host "Stopped old GoIM process $($_.Id)" -ForegroundColor Yellow
        }
    Start-Sleep -Milliseconds 400
    $taskConflicts = @()
    foreach ($taskRequirement in $taskPortRequirements) {
        $taskAvailable = if ($taskRequirement.Protocol -eq 'UDP') {
            Test-UdpBind -Port $taskRequirement.Port
        } else {
            Test-TcpBind -Port $taskRequirement.Port
        }
        if (!$taskAvailable) {
            $taskOwner = Get-PortOwner -Port $taskRequirement.Port
            if (!$taskOwner) { $taskOwner = 'unknown process' }
            $taskConflicts += "$($taskRequirement.Label) $($taskRequirement.Protocol) $($taskRequirement.Port): $taskOwner"
        }
    }
}

if ($taskConflicts.Count -gt 0) {
    throw @"
Compatibility ports are in use; the temporary client was not started:
$($taskConflicts -join "`n")

Only one program can own 9011/2425 on this PC. Exit the local Neiwangtong client or old GoIM.
To stop only GoIM processes from this project's build directory and retry:
  powershell -ExecutionPolicy Bypass -File scripts/start-temp-dev.ps1 -StopExistingGoIM
"@
}

$taskStamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$taskRunID = "$taskStamp-$([guid]::NewGuid().ToString('N').Substring(0,8))"
$taskRunRoot = Join-Path $taskProject ".runtime/temp-dev/$taskRunID"
$taskDataDir = Join-Path $taskRunRoot 'data'
$taskDownloadDir = Join-Path $taskRunRoot 'downloads'
New-Item -ItemType Directory -Path $taskDataDir, $taskDownloadDir -Force | Out-Null

$taskStartInfo = [System.Diagnostics.ProcessStartInfo]::new()
$taskStartInfo.FileName = $taskExecutable
$taskStartInfo.WorkingDirectory = $taskProject
$taskStartInfo.UseShellExecute = $false
$taskArguments = @(
    '--data-dir', $taskDataDir,
    '--download-dir', $taskDownloadDir,
    '--listen', $Listen,
    '--name', $Name
)
if ($null -ne $taskStartInfo.ArgumentList) {
    foreach ($taskArgument in $taskArguments) {
        $taskStartInfo.ArgumentList.Add([string]$taskArgument)
    }
} else {
    # Windows PowerShell 5.1 uses Arguments instead of ArgumentList.
    $taskStartInfo.Arguments = ($taskArguments | ForEach-Object {
        $taskValue = [string]$_
        if ($taskValue -match '[\s"]') {
            '"' + $taskValue.Replace('"', '\"') + '"'
        } else {
            $taskValue
        }
    }) -join ' '
}
$taskProcess = [System.Diagnostics.Process]::Start($taskStartInfo)
Start-Sleep -Milliseconds 900
$taskProcess.Refresh()
if ($taskProcess.HasExited) {
    throw "Temporary client exited immediately with code $($taskProcess.ExitCode). Data: $taskDataDir"
}

$taskInfo = @(
    "PID=$($taskProcess.Id)"
    "Name=$Name"
    "Listen=$Listen"
    "DataDir=$taskDataDir"
    "DownloadDir=$taskDownloadDir"
    "NeiwangtongDiscovery=UDP 9011"
    "NeiwangtongControl=UDP 9012"
    "NeiwangtongTransfer=TCP 9013"
    "IPMSG=UDP/TCP 2425"
) -join "`r`n"
Set-Content -LiteralPath (Join-Path $taskRunRoot 'launch.txt') -Value $taskInfo -Encoding UTF8

Write-Host 'GoIM temporary development client started' -ForegroundColor Green
Write-Host "PID:          $($taskProcess.Id)"
Write-Host "Data:         $taskDataDir"
Write-Host "Downloads:    $taskDownloadDir"
Write-Host 'Protocols:    Neiwangtong 9011/9012/9013 + IPMSG 2425'
Write-Host 'Tip:          Run Neiwangtong on another PC and allow bidirectional UDP/TCP in VMware.'
