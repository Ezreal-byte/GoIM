param(
    [ValidateSet('Loopback','Sandbox')][string]$Mode = 'Loopback',
    [string]$SandboxDistro = 'GoIM-Neiwangtong-Lab',
    [string]$SandboxOriginalDir = '/opt/goim-lab/ipmsg5.9.1',
    [string]$OutputDir = 'artifacts/ipmsg'
)
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
$env:GOWORK = 'off'
if ($Mode -eq 'Loopback') {
    & go test ./internal/ipmsg ./internal/core -count=1
    exit $LASTEXITCODE
}
$linux = @('-d', $SandboxDistro, '-u', 'nwtlab', '--exec')
$stage = (& wsl.exe @linux mktemp -d /tmp/goim-ipmsg-tools.XXXXXXXX | Out-String).Trim()
if ($LASTEXITCODE -ne 0 -or $stage -cnotmatch '^/tmp/goim-ipmsg-tools\.[A-Za-z0-9]{8}$') {
    throw 'The dedicated lab is unavailable. See docs/ipmsg-compatibility-progress.md.'
}
$hostStage = "\\wsl.localhost\$SandboxDistro" + $stage.Replace('/', '\')
$binary = New-TemporaryFile
$saved = @{}
foreach ($name in @('GOOS','GOARCH','CGO_ENABLED')) { $saved[$name] = [Environment]::GetEnvironmentVariable($name) }
try {
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    & go build -o $binary.FullName ./cmd/ipmsg-check
    if ($LASTEXITCODE -ne 0) { throw 'Failed to build the real GoIM checker.' }
    Copy-Item -LiteralPath $binary.FullName -Destination (Join-Path $hostStage 'check')
    $run = (Get-Content -LiteralPath (Join-Path $PSScriptRoot 'ipmsg-lab/run.sh') -Raw).Replace("`r`n", "`n")
    [IO.File]::WriteAllText((Join-Path $hostStage 'run.sh'), $run, [Text.UTF8Encoding]::new($false))
    & wsl.exe @linux chmod +x "$stage/run.sh" "$stage/check"
    if ($LASTEXITCODE -ne 0) { throw 'Failed to stage the isolated runner.' }
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
    $result = & wsl.exe -d $SandboxDistro -u root --exec "$stage/run.sh" $SandboxOriginalDir
    $status = $LASTEXITCODE
    $result | Set-Content -LiteralPath (Join-Path $OutputDir 'sandbox.json') -Encoding utf8NoBOM
    $result | Write-Output
    if ($status -ne 0) { throw 'Original IP Messenger interoperability failed.' }
} finally {
    foreach ($name in $saved.Keys) { [Environment]::SetEnvironmentVariable($name, $saved[$name]) }
    Remove-Item -LiteralPath $binary.FullName -ErrorAction SilentlyContinue
    & wsl.exe @linux rm -f -- "$stage/check" "$stage/run.sh"
    & wsl.exe @linux rmdir -- "$stage"
}
