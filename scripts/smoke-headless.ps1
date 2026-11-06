param([string]$OutputDir = 'artifacts/headless')
$ErrorActionPreference='Stop'
$taskProjectRoot=Split-Path -Parent $PSScriptRoot
Push-Location $taskProjectRoot
$taskProcesses=[System.Collections.Generic.List[System.Diagnostics.Process]]::new()
function Read-Reply($TaskProcess) {
    $taskLines=[System.Collections.Generic.List[string]]::new()
    while ($true) {
        $taskLine=$TaskProcess.StandardOutput.ReadLineAsync().WaitAsync([TimeSpan]::FromSeconds(20)).GetAwaiter().GetResult()
        if ($null -eq $taskLine) { throw "Headless process exited: $($TaskProcess.StandardError.ReadToEnd())" }
        $taskLines.Add($taskLine)
        if ($taskLine -match '^error(?: |$)') { throw ($taskLines -join "`n") }
        if ($taskLine -match '^ok(?: |$)') { return ($taskLines -join "`n") }
    }
}
function Send-Command($TaskProcess,[string]$TaskCommand) {
    $TaskProcess.StandardInput.WriteLine($TaskCommand)
    $TaskProcess.StandardInput.Flush()
    return Read-Reply $TaskProcess
}
function Start-Peer([string]$TaskName,[string]$TaskConnect='') {
    $taskInfo=[System.Diagnostics.ProcessStartInfo]::new()
    $taskInfo.FileName=Join-Path $taskProjectRoot 'build/goim-headless.exe'
    $taskInfo.WorkingDirectory=$taskProjectRoot
    $taskInfo.UseShellExecute=$false;$taskInfo.CreateNoWindow=$true
    $taskInfo.RedirectStandardInput=$true;$taskInfo.RedirectStandardOutput=$true;$taskInfo.RedirectStandardError=$true
    $taskInfo.StandardOutputEncoding=[System.Text.Encoding]::UTF8;$taskInfo.StandardInputEncoding=[System.Text.UTF8Encoding]::new($false)
    $taskInfo.Environment['VIVA_HEADLESS']='1';$taskInfo.Environment['VIVA_HEADLESS_SCALE']='1'
    foreach($taskArgument in @('--data-dir',(Join-Path $script:taskRunDir $TaskName),'--listen','127.0.0.1:0','--name',$TaskName,'--no-discovery')){$taskInfo.ArgumentList.Add($taskArgument)}
    if($TaskConnect){$taskInfo.ArgumentList.Add('--connect');$taskInfo.ArgumentList.Add($TaskConnect)}
    $taskProcess=[System.Diagnostics.Process]::Start($taskInfo)
    $script:taskProcesses.Add($taskProcess)
    $null=Read-Reply $taskProcess
    return $taskProcess
}
try {
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
    $taskRunDir=Join-Path ([System.IO.Path]::GetTempPath()) ('goim-smoke-'+[guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $taskRunDir -Force | Out-Null
    $taskBob=Start-Peer '林晓'
    $taskTexts=Send-Command $taskBob 'texts'
    $taskAddress=[regex]::Match($taskTexts,'127\.0\.0\.1:\d+').Value
    if(!$taskAddress){throw 'Listening address missing from real headless window'}
    $taskAlice=Start-Peer '我' $taskAddress
    Send-Command $taskAlice 'waitfor "林晓" 10000'
    Send-Command $taskAlice 'tree' | Set-Content -LiteralPath (Join-Path $OutputDir 'initial-tree.txt')
    Send-Command $taskAlice 'expect named'
    Send-Command $taskAlice 'expect layout clean'
    $taskScreenshot=Join-Path (Join-Path $taskProjectRoot $OutputDir) 'real-connected.png'
    Send-Command $taskAlice ('shot "'+$taskScreenshot.Replace('\','/')+'"')
    Send-Command $taskAlice 'click "设置"'
    Send-Command $taskAlice 'expect text "常规"'
    Send-Command $taskAlice 'expect layout clean'
    Send-Command $taskAlice ('shot "'+(Join-Path (Join-Path $taskProjectRoot $OutputDir) 'real-settings.png').Replace('\','/')+'"')
    'PASS: two independent processes connect through real TLS; initial and settings layouts clean.'
} finally {
    foreach($taskProcess in $taskProcesses){if(!$taskProcess.HasExited){try{$null=Send-Command $taskProcess 'quit'}catch{};if(!$taskProcess.WaitForExit(3000)){$taskProcess.Kill($true)}};$taskProcess.Dispose()}
    Pop-Location
}
