param(
    [switch]$Headless,
    [string]$OutputDir = 'build'
)
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
if (![IO.Path]::IsPathRooted($OutputDir)) {
    $OutputDir = Join-Path $root $OutputDir
}
$OutputDir = [IO.Path]::GetFullPath($OutputDir)
Push-Location $root
try {
    $source = 'local'
    $ciRef = ''
    $ciRunID = ''
    $ciURL = ''
    if ($env:GITHUB_ACTIONS -eq 'true') {
        $source = 'github-actions'
        $ciRef = $env:GITHUB_REF
        $ciRunID = $env:GITHUB_RUN_ID
        if ($env:GITHUB_SERVER_URL -and $env:GITHUB_REPOSITORY -and $ciRunID) {
            $ciURL = $env:GITHUB_SERVER_URL.TrimEnd('/') + '/' + $env:GITHUB_REPOSITORY + '/actions/runs/' + $ciRunID
        }
    }
    $buildTime = [DateTime]::UtcNow.ToString(
        'yyyy-MM-ddTHH:mm:ssZ',
        [Globalization.CultureInfo]::InvariantCulture
    )

    $linkData = @(
        '-buildid=',
        '-X', "goim/internal/buildinfo.Source=$source",
        '-X', "goim/internal/buildinfo.BuildTime=$buildTime",
        '-X', "goim/internal/buildinfo.CIRef=$ciRef",
        '-X', "goim/internal/buildinfo.CIRunID=$ciRunID",
        '-X', "goim/internal/buildinfo.CIURL=$ciURL"
    )

    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
    if ($Headless) {
        go build -tags vivahead -trimpath -ldflags ($linkData -join ' ') -o (Join-Path $OutputDir 'goim-headless.exe') ./cmd/goim
        if ($LASTEXITCODE -ne 0) { throw 'GoIM headless build failed' }
    } else {
        $releaseLink = @('-s', '-w', '-H=windowsgui') + $linkData
        $executable = Join-Path $OutputDir 'GoIM.exe'
        go build -trimpath -ldflags ($releaseLink -join ' ') -o $executable ./cmd/goim
        if ($LASTEXITCODE -ne 0) { throw 'GoIM build failed' }
        go run ./cmd/package --stamp-windows $executable
        if ($LASTEXITCODE -ne 0) { throw 'GoIM icon resource update failed' }
        go build -trimpath -ldflags '-s -w -buildid= -H=windowsgui' -o (Join-Path $OutputDir 'goim-snip.exe') ./cmd/goim-snip
        if ($LASTEXITCODE -ne 0) { throw 'GoIM companion build failed' }
    }
} finally {
    Pop-Location
}
