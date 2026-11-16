param([switch]$Headless)
$ErrorActionPreference = 'Stop'

function Invoke-Git {
    param([string[]]$Arguments)
    $git = Get-Command git -ErrorAction SilentlyContinue
    if (-not $git) { return $null }
    $output = & $git.Source @Arguments 2>$null
    if ($LASTEXITCODE -ne 0) { return $null }
    return (($output | Out-String).Trim())
}

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    $version = '0.1.0-dev'
    $commit = Invoke-Git @('rev-parse', 'HEAD')
    if (-not $commit) {
        $commit = 'working-tree'
    } elseif ((Invoke-Git @('status', '--porcelain'))) {
        $commit = "$commit-dirty"
    }

    $vivaPath = Join-Path (Split-Path -Parent $root) 'viva'
    $framework = ''
    $frameworkDirty = 'false'
    if (Test-Path -LiteralPath $vivaPath) {
        $framework = Invoke-Git @('-C', $vivaPath, 'rev-parse', 'HEAD')
        if ((Invoke-Git @('-C', $vivaPath, 'status', '--porcelain'))) {
            $frameworkDirty = 'true'
        }
    }
    if (-not $framework) {
        $pin = (Get-Content -Raw (Join-Path $root 'framework/viva-commit.txt')).Trim()
        $framework = "$pin+patch"
    }

    $source = 'local'
    if ($env:GITHUB_ACTIONS -eq 'true') {
        $source = 'github-actions'
    }
    $ciRef = $env:GITHUB_REF
    if (-not $ciRef) { $ciRef = $env:GITHUB_REF_NAME }
    $ciRunID = $env:GITHUB_RUN_ID
    $ciURL = ''
    if ($env:GITHUB_REPOSITORY -and $ciRunID) {
        $server = $env:GITHUB_SERVER_URL
        if (-not $server) { $server = 'https://github.com' }
        $ciURL = $server.TrimEnd('/') + '/' + $env:GITHUB_REPOSITORY + '/actions/runs/' + $ciRunID
    }
    $buildTime = [DateTime]::UtcNow.ToString(
        'yyyy-MM-ddTHH:mm:ssZ',
        [Globalization.CultureInfo]::InvariantCulture
    )
    $commitRevision = $commit -replace '-dirty$', ''
    $commitTime = Invoke-Git @('show', '-s', '--format=%cI', $commitRevision)

    $linkData = @(
        '-buildid=',
        '-X', "main.version=$version",
        '-X', "main.commit=$commit",
        '-X', "goim/internal/buildinfo.Version=$version",
        '-X', "goim/internal/buildinfo.Commit=$commit",
        '-X', "goim/internal/buildinfo.CommitTime=$commitTime",
        '-X', "goim/internal/buildinfo.Framework=$framework",
        '-X', "goim/internal/buildinfo.FrameworkDirty=$frameworkDirty",
        '-X', "goim/internal/buildinfo.Source=$source",
        '-X', "goim/internal/buildinfo.BuildTime=$buildTime",
        '-X', "goim/internal/buildinfo.CIRef=$ciRef",
        '-X', "goim/internal/buildinfo.CIRunID=$ciRunID",
        '-X', "goim/internal/buildinfo.CIURL=$ciURL"
    )

    New-Item -ItemType Directory -Path build -Force | Out-Null
    if ($Headless) {
        go build -tags vivahead -trimpath -ldflags ($linkData -join ' ') -o build/goim-headless.exe ./cmd/goim
        if ($LASTEXITCODE -ne 0) { throw 'GoIM headless build failed' }
    } else {
        $releaseLink = @('-s', '-w', '-H=windowsgui') + $linkData
        go build -trimpath -ldflags ($releaseLink -join ' ') -o build/GoIM.exe ./cmd/goim
        if ($LASTEXITCODE -ne 0) { throw 'GoIM build failed' }
        go run ./cmd/package --stamp-windows build/GoIM.exe
        if ($LASTEXITCODE -ne 0) { throw 'GoIM icon resource update failed' }
        go build -trimpath -ldflags '-s -w -buildid= -H=windowsgui' -o build/goim-snip.exe ./cmd/goim-snip
        if ($LASTEXITCODE -ne 0) { throw 'GoIM companion build failed' }
    }
} finally {
    Pop-Location
}
