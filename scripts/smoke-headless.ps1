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
function Wait-Control($TaskProcess,[string]$TaskLabel) {
    $taskDeadline=[DateTime]::UtcNow.AddSeconds(15)
    do {
        $null=Send-Command $TaskProcess 'wait 100'
        $taskTree=Send-Command $TaskProcess 'tree'
        if($taskTree -match ('(?m)^role=button[^\r\n]* label="'+[regex]::Escape($TaskLabel)+'"')){return}
        Start-Sleep -Milliseconds 100
    } while([DateTime]::UtcNow -lt $taskDeadline)
    throw "Button '$TaskLabel' did not appear after the asynchronous state change"
}
function Start-Peer([string]$TaskName,[string]$TaskConnect='') {
    $taskInfo=[System.Diagnostics.ProcessStartInfo]::new()
    $taskInfo.FileName=Join-Path $taskProjectRoot 'build/goim-headless.exe'
    $taskInfo.WorkingDirectory=$taskProjectRoot
    $taskInfo.UseShellExecute=$false;$taskInfo.CreateNoWindow=$true
    $taskInfo.RedirectStandardInput=$true;$taskInfo.RedirectStandardOutput=$true;$taskInfo.RedirectStandardError=$true
    $taskInfo.StandardOutputEncoding=[System.Text.Encoding]::UTF8;$taskInfo.StandardInputEncoding=[System.Text.UTF8Encoding]::new($false)
    $taskInfo.Environment['VIVA_HEADLESS']='1';$taskInfo.Environment['VIVA_HEADLESS_SCALE']='1'
    foreach($taskArgument in @('--data-dir',(Join-Path $script:taskRunDir $TaskName),'--download-dir',(Join-Path $script:taskRunDir ($TaskName+'-downloads')),'--listen','127.0.0.1:0','--name',$TaskName,'--no-discovery')){$taskInfo.ArgumentList.Add($taskArgument)}
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
    Send-Command $taskAlice 'click "林晓"'
    Send-Command $taskAlice 'waitfor "发送" 10000'
    Send-Command $taskAlice 'click id:goim/chat/editor'
    Send-Command $taskAlice 'type 双端真实消息：你好，局域网！'
    Send-Command $taskAlice 'key enter'
    Send-Command $taskBob 'waitfor "双端真实消息：你好，局域网！" 10000'
    Send-Command $taskBob 'click "我"'
    Send-Command $taskBob 'expect text "双端真实消息：你好，局域网！"'
    $taskSamplePath=Join-Path $taskRunDir '真实文件.txt'
    [System.IO.File]::WriteAllText($taskSamplePath,'GoIM independent-process file transfer: 中文 '+[guid]::NewGuid().ToString(),[System.Text.UTF8Encoding]::new($false))
    Send-Command $taskAlice ('openfiles "'+$taskSamplePath.Replace('\','/')+'"')
    Send-Command $taskAlice 'click "发送文件"'
    Send-Command $taskBob 'waitfor "真实文件.txt" 10000'
    Send-Command $taskBob 'waitfor "查看详情" 10000'
    Send-Command $taskBob 'click "查看 真实文件.txt 详情"'
    Send-Command $taskBob 'click "接收"'
    Send-Command $taskBob 'waitfor "已接收" 10000'
    $taskReceived=Join-Path (Join-Path $taskRunDir '林晓-downloads') '真实文件.txt'
    if(!(Test-Path -LiteralPath $taskReceived)){throw 'Received file missing'}
    if((Get-FileHash -LiteralPath $taskReceived).Hash -ne (Get-FileHash -LiteralPath $taskSamplePath).Hash){throw 'Received SHA256 differs'}
    Send-Command $taskBob 'key esc'
    $taskAliceTexts=Send-Command $taskAlice 'texts'
    $taskAliceAddress=[regex]::Match($taskAliceTexts,'127\.0\.0\.1:\d+').Value
    $taskCharlie=Start-Peer '陈序' $taskAliceAddress
    Send-Command $taskAlice 'waitfor "陈序" 10000'
    Send-Command $taskAlice 'click id:goim/sidebar/create'
    Send-Command $taskAlice 'click "textfield:群名称"'
    Send-Command $taskAlice 'type 三端验证群'
    Send-Command $taskAlice 'click "checkbox:选择 林晓"'
    Send-Command $taskAlice 'click "checkbox:选择 陈序"'
    Send-Command $taskAlice 'click "创建并邀请（2）"'
    foreach($taskGuest in @($taskBob,$taskCharlie)){Send-Command $taskGuest 'waitfor "加入" 10000';Send-Command $taskGuest 'click "加入"'}
    Send-Command $taskAlice 'waitfor "三端验证群" 10000'
    Send-Command $taskAlice 'waitfor "3 人在线" 15000'
    Send-Command $taskAlice 'click id:goim/chat/editor'
    Send-Command $taskAlice 'type 三个独立进程通过真实群聊通信。'
    Send-Command $taskAlice 'key enter'
    foreach($taskGuest in @($taskBob,$taskCharlie)){Wait-Control $taskGuest '三端验证群';Send-Command $taskGuest 'click "三端验证群"';Send-Command $taskGuest 'waitfor "三个独立进程通过真实群聊通信。" 10000'}
    Send-Command $taskAlice ('shot "'+(Join-Path (Join-Path $taskProjectRoot $OutputDir) 'real-group-chat.png').Replace('\','/')+'"')
    Send-Command $taskAlice 'click "设置"'
    Send-Command $taskAlice 'expect text "常规"'
    Send-Command $taskAlice 'expect layout clean'
    Send-Command $taskAlice ('shot "'+(Join-Path (Join-Path $taskProjectRoot $OutputDir) 'real-settings.png').Replace('\','/')+'"')
    'PASS: three independent processes, real TLS text/group delivery, confirmed file transfer SHA256, clean initial/settings layouts.'
} catch {
    foreach($taskProcess in $taskProcesses){if(!$taskProcess.HasExited){try{Send-Command $taskProcess 'tree' | Set-Content -LiteralPath (Join-Path $OutputDir ("failure-"+$taskProcess.Id+'.txt'));$null=Send-Command $taskProcess ('shot "'+(Join-Path (Join-Path $taskProjectRoot $OutputDir) ("failure-"+$taskProcess.Id+'.png')).Replace('\','/')+'"')}catch{}}}
    throw
} finally {
    foreach($taskProcess in $taskProcesses){if(!$taskProcess.HasExited){try{$null=Send-Command $taskProcess 'quit'}catch{};if(!$taskProcess.WaitForExit(3000)){$taskProcess.Kill($true)}};$taskProcess.Dispose()}
    Pop-Location
}
