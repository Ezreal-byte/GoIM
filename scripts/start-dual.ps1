$ErrorActionPreference = 'Stop'
$taskProject = Split-Path -Parent $PSScriptRoot
$taskExecutable = Join-Path $taskProject 'build/goim.exe'

if (!(Test-Path -LiteralPath $taskExecutable)) {
    & (Join-Path $PSScriptRoot 'build.ps1')
}

$runtimeBase = Join-Path $taskProject '.runtime'
$user1Dir = Join-Path $runtimeBase 'client-alice'
$user2Dir = Join-Path $runtimeBase 'client-bob'
$user1Dl = Join-Path $runtimeBase 'client-alice-dl'
$user2Dl = Join-Path $runtimeBase 'client-bob-dl'

New-Item -ItemType Directory -Path $user1Dir, $user2Dir, $user1Dl, $user2Dl -Force | Out-Null

Write-Host "正在启动客户端 1 (林晓)..." -ForegroundColor Cyan
Start-Process -FilePath $taskExecutable -ArgumentList @(
    '--data-dir', $user1Dir,
    '--download-dir', $user1Dl,
    '--listen', '127.0.0.1:47842',
    '--name', '林晓'
)

Start-Sleep -Milliseconds 800

Write-Host "正在启动客户端 2 (陈序)..." -ForegroundColor Green
Start-Process -FilePath $taskExecutable -ArgumentList @(
    '--data-dir', $user2Dir,
    '--download-dir', $user2Dl,
    '--listen', '127.0.0.1:47843',
    '--name', '陈序',
    '--connect', '127.0.0.1:47842'
)

Write-Host "双客户端已成功启动，已自动建立连接。" -ForegroundColor Yellow