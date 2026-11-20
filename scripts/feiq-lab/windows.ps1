# Runs only on a disposable GitHub-hosted runner. No desktop from a development machine is used.
param([Parameter(Mandatory)][string]$Lab)
$ErrorActionPreference = 'Stop'
if ($env:RUNNER_ENVIRONMENT -ne 'github-hosted' -or $env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_OS -ne 'Windows') { throw 'Requires a disposable GitHub-hosted Windows runner' }
$root = [IO.Path]::GetFullPath($Lab)
if ([IO.Path]::GetDirectoryName($root) -ne [IO.Path]::GetFullPath($env:RUNNER_TEMP).TrimEnd('\') -or
    [IO.Path]::GetFileName($root) -ne 'goim-feiq-lab') { throw 'Invalid temporary lab directory' }
$original = Join-Path $root 'original'
$exe = Join-Path $original 'FeiQ.exe'
$checker = Join-Path $root 'check.exe'
$group = 'GoIM FeiQ isolated test'
$checks = [ordered]@{package_hash=$false; firewall=$false; original_listening=$false; unicast_discovery=$false; outgoing_text_receipt=$false; outgoing_text_visible=$false; incoming_text_exact=$false; outgoing_file_served=$false; outgoing_file_sha256=$false; incoming_file_sha256=$false}
$problem = ''
$exitDescription = ''
$env:GOIM_FEIQ_LAB='1'
try {
    New-Item -ItemType Directory -Force -Path $original,(Join-Path $root 'stock-received'),(Join-Path $root 'received') | Out-Null
    if (!(Test-Path -LiteralPath $checker)) { throw 'Build the production checker before starting the lab' }
    $zip = Join-Path $root 'original.zip'
    # The vendor currently serves this 2013 build over HTTP. Authenticate bytes before any extraction or execution.
    & curl.exe --fail --silent --show-error --max-time 45 --retry 2 --output $zip http://www.feiq18.com/down/feiq.zip
    if ($LASTEXITCODE -ne 0) {throw 'Official FeiQ download failed'}
    if ((Get-FileHash -LiteralPath $zip).Hash -ne '0611D7963D25E47EE87D8859C225D1D9B087B2F5FE27152906F04E94FE9E5C72') {throw 'Unexpected FeiQ archive SHA-256'}
    $archive=[IO.Compression.ZipFile]::OpenRead($zip)
    try {
        if ($archive.Entries.Count -ne 1 -or $archive.Entries[0].FullName -cne 'FeiQ.exe') {throw 'Unexpected archive member'}
        [IO.Compression.ZipFileExtensions]::ExtractToFile($archive.Entries[0],$exe)
    } finally { $archive.Dispose() }
    if ((Get-FileHash -LiteralPath $exe).Hash -ne '480FF41B04BD8E93CE027B33A2D8DAE9531FDF9E4651C0D728E9F490C93F1AE6') {throw 'Unexpected FeiQ binary SHA-256'}
    $checks.package_hash=$true
    if (@(Get-NetUDPEndpoint -LocalPort 2425,2426 -ErrorAction SilentlyContinue).Count) {throw 'Reference or checker port is already occupied'}
    Set-NetFirewallProfile -All -Enabled True
    # Cover all actual adapters (including virtual/VPN), leaving only the OS loopback route.
    $loopback = @(Get-NetIPAddress -IPAddress 127.0.0.1 | Select-Object -ExpandProperty InterfaceIndex -Unique)
    if ($loopback.Count -ne 1) {throw 'Cannot identify loopback interface'}
    $adapters = @(Get-NetIPInterface | Where-Object InterfaceIndex -ne $loopback[0] | Select-Object -ExpandProperty InterfaceAlias -Unique)
    if ($adapters.Count -eq 0) {throw 'Cannot enumerate non-loopback adapters'}
    foreach ($p in @($exe,$checker)) {
        foreach ($direction in @('Inbound','Outbound')) {
            $rule = New-NetFirewallRule -Group $group -DisplayName "GoIM test $([IO.Path]::GetFileName($p)) $direction" -Direction $direction -Action Block -Enabled True -Profile Any -Program $p -InterfaceAlias $adapters
            $active = Get-NetFirewallRule -PolicyStore ActiveStore -Name $rule.Name
            if ($active.Enabled -ne 'True' -or $active.Action -ne 'Block' -or $active.Direction -ne $direction) {throw 'Required firewall isolation is not active'}
        }
    }
    if (@(Get-NetFirewallProfile -PolicyStore ActiveStore | Where-Object Enabled -ne True).Count) {throw 'Windows Firewall is disabled'}
    $checks.firewall=$true
    $stock = Start-Process -FilePath $exe -WorkingDirectory $original -PassThru -WindowStyle Hidden
    # The UI driver will bring only FeiQ windows to the front on this temporary machine.
    Start-Sleep -Seconds 2
    $backend = Start-Process -FilePath $checker -ArgumentList @('-feiq-windows','-lab-dir',('"' + $root + '"')) -WorkingDirectory $root -PassThru -WindowStyle Hidden -RedirectStandardError (Join-Path $root 'checker-errors.txt')
    $python=(Get-Command python.exe).Source
    $driver = Join-Path $PSScriptRoot 'drive.py'
    $automation=Start-Process -FilePath $python -ArgumentList @(('"' + $driver + '"'),$stock.Id,('"' + $root + '"')) -WorkingDirectory $root -PassThru -WindowStyle Hidden -RedirectStandardError (Join-Path $root 'ui-errors.txt')
    $limit=[datetime]::UtcNow.AddSeconds(130)
    while ([datetime]::UtcNow -lt $limit -and !($backend.HasExited -and $automation.HasExited)) {
        if (!$checks.original_listening -and !$stock.HasExited) {
            $checks.original_listening = @(Get-NetUDPEndpoint -LocalPort 2425 -ErrorAction SilentlyContinue | Where-Object OwningProcess -eq $stock.Id).Count -gt 0
        }
        Start-Sleep -Milliseconds 800
    }
    $exitDescription="stockExited=$($stock.HasExited) backendExited=$($backend.HasExited) driverExited=$($automation.HasExited) session=$($stock.SessionId)"
    foreach ($entry in @(@('backend.json',$false),@('ui.json',$true))) {
        $file = Join-Path $root $entry[0]
        if (!(Test-Path -LiteralPath $file)) {continue}
        $data = Get-Content -LiteralPath $file -Encoding utf8 -Raw | ConvertFrom-Json -AsHashtable
        if ($entry[1]) {$data=$data.checks}
        foreach ($name in $data.Keys) {if ($checks.Contains($name) -and $data[$name] -is [bool]) {$checks[$name]=$data[$name]}}
    }
} catch {
    $problem=$_.Exception.Message
} finally {
    foreach ($process in @($automation,$backend,$stock)) {
        if ($null -ne $process -and !$process.HasExited) {
            # Kill descendants before restoring network access.
            $process.Kill($true); $process.WaitForExit()
        }
    }
    # The VM will be discarded, leave its program restrictions in force.
    $report=[ordered]@{reference='FeiQ 2013 3.0.0.2'; scope='single disposable Windows host, explicit loopback discovery'; checks=$checks; error=$problem; processes=$exitDescription}
    $report | ConvertTo-Json -Depth 4 | Set-Content (Join-Path $root 'report.json') -Encoding utf8NoBOM
    Get-Content (Join-Path $root 'report.json') -Raw
}
if ($checks.Values -contains $false -or $problem) {exit 1}
