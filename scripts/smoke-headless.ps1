#Requires -Version 7.2
param([string]$OutputDir = 'artifacts/headless', [switch]$BrowserScreenshots, [switch]$AvatarSample)
$ErrorActionPreference='Stop'
$taskProjectRoot=Split-Path -Parent $PSScriptRoot
if (![IO.Path]::IsPathRooted($OutputDir)) { $OutputDir=Join-Path $taskProjectRoot $OutputDir }
$OutputDir=[IO.Path]::GetFullPath($OutputDir)
$taskTempRoot=[IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$taskRunDir=Join-Path $taskTempRoot ('goim-smoke-'+[guid]::NewGuid().ToString('N'))
$taskBinaryDir=Join-Path $taskRunDir 'bin'
$taskProcesses=[System.Collections.Generic.List[System.Diagnostics.Process]]::new()
$taskStderr=@{}

function Read-Reply($TaskProcess) {
    $taskLines=[System.Collections.Generic.List[string]]::new()
    $taskDeadline=[Diagnostics.Stopwatch]::StartNew()
    while ($true) {
        $remaining=[TimeSpan]::FromSeconds(25)-$taskDeadline.Elapsed
        if ($remaining -le [TimeSpan]::Zero) { throw 'Timed out waiting for Viva reply' }
        $taskLine=$TaskProcess.StandardOutput.ReadLineAsync().WaitAsync($remaining).GetAwaiter().GetResult()
        if ($null -eq $taskLine) { throw 'Headless process closed its output' }
        $taskLines.Add($taskLine)
        if ($taskLine -match '^error(?: |$)') { throw ($taskLines -join "`n") }
        if ($taskLine -match '^ok(?: |$)') { return ($taskLines -join "`n") }
    }
}
function Send-Command($TaskProcess,[string]$TaskCommand) {
    try {
        $TaskProcess.StandardInput.WriteLine($TaskCommand)
        $TaskProcess.StandardInput.Flush()
        return Read-Reply $TaskProcess
    } catch {
        throw "Headless peer $($TaskProcess.Id), command '$TaskCommand': $($_.Exception.Message)"
    }
}
function Enter-PasswordKeys($TaskProcess) {
    foreach ($letter in 'goim-smoke-only'.ToCharArray()) {
        $null=Send-Command $TaskProcess ('keydown '+$letter)
        $null=Send-Command $TaskProcess ('keyup '+$letter)
    }
}
function Wait-Control($TaskProcess,[string]$TaskLabel) {
    $null=Send-Command $TaskProcess ('expect exists "button:'+$TaskLabel+'" within 15000')
}
function Save-Screenshot($TaskProcess,[string]$FileName) {
    $path=(Join-Path $OutputDir $FileName).Replace('\','/')
    $null=Send-Command $TaskProcess ('shot "'+$path+'"')
}
function Start-Peer([string]$TaskName,[string]$TaskConnect='') {
    $taskInfo=[System.Diagnostics.ProcessStartInfo]::new()
    $taskInfo.FileName=Join-Path $taskBinaryDir 'goim-headless.exe'
    $taskInfo.WorkingDirectory=$taskProjectRoot
    $taskInfo.UseShellExecute=$false;$taskInfo.CreateNoWindow=$true
    $taskInfo.RedirectStandardInput=$true;$taskInfo.RedirectStandardOutput=$true;$taskInfo.RedirectStandardError=$true
    $taskInfo.StandardOutputEncoding=[System.Text.Encoding]::UTF8;$taskInfo.StandardInputEncoding=[System.Text.UTF8Encoding]::new($false)
    $taskInfo.StandardErrorEncoding=[System.Text.Encoding]::UTF8
    $taskInfo.Environment['VIVA_HEADLESS']='1';$taskInfo.Environment['VIVA_HEADLESS_SCALE']='1'
    foreach($taskArgument in @('--data-dir',(Join-Path $script:taskRunDir $TaskName),'--download-dir',(Join-Path $script:taskRunDir ($TaskName+'-downloads')),'--listen','127.0.0.1:0','--name',$TaskName,'--no-discovery','--no-compatibility')){$taskInfo.ArgumentList.Add($taskArgument)}
    if($TaskConnect){$taskInfo.ArgumentList.Add('--connect');$taskInfo.ArgumentList.Add($TaskConnect)}
    $taskProcess=[System.Diagnostics.Process]::Start($taskInfo)
    $script:taskProcesses.Add($taskProcess)
    $script:taskStderr[$taskProcess.Id]=$taskProcess.StandardError.ReadToEndAsync()
    $null=Read-Reply $taskProcess
    return $taskProcess
}
function Request-PeerReopen([string]$TaskName) {
    $info=[System.Diagnostics.ProcessStartInfo]::new()
    $info.FileName=Join-Path $taskBinaryDir 'goim-headless.exe'
    $info.UseShellExecute=$false
    $info.CreateNoWindow=$true
    $info.RedirectStandardError=$true
    $info.Environment['VIVA_HEADLESS']='1'
    foreach($argument in @('--data-dir',(Join-Path $taskRunDir $TaskName),'--listen','127.0.0.1:0','--no-discovery','--no-compatibility')) {
        $info.ArgumentList.Add($argument)
    }
    $launcher=[System.Diagnostics.Process]::Start($info)
    try {
        if (!$launcher.WaitForExit(6000)) {
            $launcher.Kill()
            throw 'Second GoIM launch did not exit'
        }
        if ($launcher.ExitCode -ne 0) { throw $launcher.StandardError.ReadToEnd() }
    } finally {
        $launcher.Dispose()
    }
}
function Get-PeerAddress($TaskProcess, [string]$TaskName) {
    $endpoint=Get-Content -Raw -LiteralPath (Join-Path $taskRunDir $TaskName '.goim-activation.json') | ConvertFrom-Json
    $listeners=@(Get-NetTCPConnection -State Listen -OwningProcess $TaskProcess.Id -LocalAddress '127.0.0.1' |
        Where-Object LocalPort -ne $endpoint.Port)
    if ($listeners.Count -ne 1) { throw "Expected one loopback listener for peer $($TaskProcess.Id)" }
    return "127.0.0.1:$($listeners[0].LocalPort)"
}
Push-Location $taskProjectRoot
try {
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
    New-Item -ItemType Directory -Path $taskRunDir -Force | Out-Null
    & (Join-Path $PSScriptRoot 'build.ps1') -Headless -OutputDir $taskBinaryDir
    $taskBob=Start-Peer '林晓'
    $taskAlice=Start-Peer '我' (Get-PeerAddress $taskBob '林晓')
    Send-Command $taskAlice 'click id:goim/navigation/contacts'
    Send-Command $taskAlice 'waitfor "林晓" 10000'
    Send-Command $taskAlice 'tree' | Set-Content -LiteralPath (Join-Path $OutputDir 'initial-tree.txt')
    Send-Command $taskAlice 'expect named'
    Send-Command $taskAlice 'expect layout clean'
    Save-Screenshot $taskAlice 'real-connected.png'
    Send-Command $taskAlice 'click id:goim/sidebar/connect'
    Send-Command $taskAlice 'click "combobox:连接方式"'
    Send-Command $taskAlice 'expect exists "option:GoIM"'
    Send-Command $taskAlice 'key esc'
    Send-Command $taskAlice 'expect absent "option:GoIM"'
    Send-Command $taskAlice 'expect exists "textfield:对方 IP 地址"'
    Send-Command $taskAlice 'click "combobox:连接方式"'
    Send-Command $taskAlice 'click "option:GoIM"'
    Send-Command $taskAlice 'click "combobox:连接方式"'
    Send-Command $taskAlice 'click "option:自动"'
    Send-Command $taskAlice 'expect exists "textfield:对方 IP 地址"'
    Send-Command $taskAlice 'key esc'
    Send-Command $taskAlice 'expect absent "textfield:对方 IP 地址"'
    Send-Command $taskAlice 'click "林晓"'
    Send-Command $taskAlice 'expect text "设备信息"'
    Send-Command $taskAlice 'click "button:发消息"'
    Send-Command $taskAlice 'waitfor "发送" 10000'
    Send-Command $taskAlice 'click "消息输入框"'
    Send-Command $taskBob 'requestclose'
    if ((Send-Command $taskBob 'windows') -notmatch 'hidden=true') { throw 'Receiver did not minimize to the tray' }
    Request-PeerReopen '林晓'
    $reopenDeadline=[Diagnostics.Stopwatch]::StartNew()
    do {
        $windowState=Send-Command $taskBob 'windows'
        if ($windowState -match 'hidden=false') { break }
        $null=Send-Command $taskBob 'wait 50'
        Start-Sleep -Milliseconds 20
    } while ($reopenDeadline.Elapsed.TotalSeconds -lt 5)
    if ($windowState -notmatch 'hidden=false') { throw 'Second launch did not restore the same main window' }
    Send-Command $taskBob 'requestclose'
    if ((Send-Command $taskBob 'windows') -notmatch 'hidden=true') { throw 'Reopened window did not return to the tray' }
    Send-Command $taskAlice 'type 双端真实消息：你好，局域网！'
    Send-Command $taskAlice 'key enter'
    $taskTrayDeadline=[Diagnostics.Stopwatch]::StartNew()
    do {
        Send-Command $taskBob 'wait 100'
        $taskTrayMenu=Send-Command $taskBob 'statusmenu 1'
    } until ($taskTrayMenu -match 'path=2 title=.*1 条未读' -or $taskTrayDeadline.Elapsed.TotalSeconds -ge 10)
    if ($taskTrayMenu -notmatch 'path=2 title="([^"]+1 条未读[^"]+)"') { throw 'Unread chat did not appear in the hidden receiver tray' }
    $taskTrayRow=$Matches[1]
    Send-Command $taskBob 'statusclick 1'
    $taskTrayWindows=Send-Command $taskBob 'windows'
    if ($taskTrayWindows -notmatch '(?m)^id=(\d+) .*panel=true hidden=false title="未读消息 · GoIM"') {
        throw 'Activating the tray did not open the unread popup'
    }
    Send-Command $taskBob ('window '+$Matches[1])
    Send-Command $taskBob ('click "button:'+$taskTrayRow+'"')
    if ((Send-Command $taskBob 'windows') -notmatch '(?m)^id=1 .*hidden=false') { throw 'Tray chat did not restore the receiver' }
    Send-Command $taskBob 'window 1'
    Send-Command $taskBob 'waitfor "双端真实消息：你好，局域网！" 10000'
    Send-Command $taskBob 'expect text "双端真实消息：你好，局域网！"'
    $taskSamplePath=Join-Path $taskRunDir '真实文件.txt'
    [System.IO.File]::WriteAllText($taskSamplePath,'GoIM independent-process file transfer: 中文 '+[guid]::NewGuid().ToString(),[System.Text.UTF8Encoding]::new($false))
    Send-Command $taskAlice ('openfiles "'+$taskSamplePath.Replace('\','/')+'"')
    Send-Command $taskAlice 'click "发送文件"'
    Send-Command $taskAlice 'expect exists "button:草稿附件 · 真实文件.txt" within 10000'
    Send-Command $taskAlice 'key enter'
    Send-Command $taskBob 'waitfor "真实文件.txt" 10000'
    Send-Command $taskBob 'waitfor "查看详情" 10000'
    Send-Command $taskBob 'click "查看 真实文件.txt 详情"'
    if (Test-Path -LiteralPath (Join-Path (Join-Path $taskRunDir '林晓-downloads') '真实文件.txt')) { throw 'An ordinary file arrived before acceptance' }
    Send-Command $taskBob 'key esc'
    Send-Command $taskBob 'click id:goim/navigation/transfers'
    Send-Command $taskBob 'waitfor "待接收 1" 10000'
    Send-Command $taskBob 'expect layout clean'
    Save-Screenshot $taskBob 'real-transfer-incoming.png'
    Send-Command $taskBob 'click "button:接收"'
    Send-Command $taskBob 'waitfor "已结束 1" 10000'
    Send-Command $taskBob 'click "tab:已结束 1"'
    Send-Command $taskBob 'expect text "真实文件.txt"'
    Send-Command $taskBob 'expect text "已完成"'
    $taskReceived=Join-Path (Join-Path $taskRunDir '林晓-downloads') '真实文件.txt'
    if(!(Test-Path -LiteralPath $taskReceived)){throw 'Received file missing'}
    if((Get-FileHash -LiteralPath $taskReceived).Hash -ne (Get-FileHash -LiteralPath $taskSamplePath).Hash){throw 'Received SHA256 differs'}
    Send-Command $taskBob 'click id:goim/navigation/messages'
    Send-Command $taskAlice 'click "插入表情"'
    Send-Command $taskAlice 'expect text "所有表情"'
    Send-Command $taskAlice 'click id:goim/chat/emoji:grin'
    Send-Command $taskAlice 'click "发送"'
    Send-Command $taskBob 'waitfor "😀" 10000'
    $taskPicturePath=Join-Path $taskRunDir '真实图片.png'
    Add-Type -AssemblyName System.Drawing
    $taskPicture=[Drawing.Bitmap]::new(360,180)
    $taskGraphics=[Drawing.Graphics]::FromImage($taskPicture)
    try {
        $taskGraphics.Clear([Drawing.Color]::FromArgb(29,87,212))
        $taskGraphics.FillEllipse([Drawing.Brushes]::Gold,130,35,100,100)
        $taskPicture.Save($taskPicturePath,[Drawing.Imaging.ImageFormat]::Png)
    } finally { $taskGraphics.Dispose(); $taskPicture.Dispose() }
    Send-Command $taskAlice ('openfiles "'+$taskPicturePath.Replace('\','/')+'"')
    Send-Command $taskAlice 'click "发送图片"'
    Send-Command $taskAlice 'expect exists "button:草稿附件 · 真实图片.png" within 10000'
    Send-Command $taskAlice 'click "button:发送"'
    Wait-Control $taskBob '查看图片 真实图片.png'
    $taskImages=@(Get-ChildItem -LiteralPath (Join-Path $taskRunDir '林晓/chat-images') -Filter '*.png')
    if ($taskImages.Count -ne 1) { throw 'Expected one automatically received PNG' }
    $taskReceivedImage=$taskImages[0].FullName
    if((Get-FileHash -LiteralPath $taskReceivedImage).Hash -ne (Get-FileHash -LiteralPath $taskPicturePath).Hash){throw 'Received picture SHA256 differs'}
    Send-Command $taskBob 'expect layout clean'
    Save-Screenshot $taskBob 'real-picture-emoji.png'
    Send-Command $taskBob 'click "查看图片 真实图片.png"'
    $taskViewerDeadline=[DateTime]::UtcNow.AddSeconds(8)
    do {
        $null=Send-Command $taskBob 'wait 50'
        $taskWindows=Send-Command $taskBob 'windows'
        $taskViewer=[regex]::Match($taskWindows,'(?m)^id=(\d+) [^\r\n]+title="GoIM · 媒体查看"')
    } until($taskViewer.Success -or [DateTime]::UtcNow -ge $taskViewerDeadline)
    if (!$taskViewer.Success) { throw "Separate image window did not open: $taskWindows" }
    Send-Command $taskBob 'window 1'
    Wait-Control $taskBob "发送图片"
    Send-Command $taskBob ('window '+$taskViewer.Groups[1].Value)
    Wait-Control $taskBob "保存原图"
    Send-Command $taskBob 'click "实际大小 1:1"'
    Send-Command $taskBob 'click "适应窗口"'
    Send-Command $taskBob 'click "媒体查看选项"'
    Send-Command $taskBob 'expect exists "menuitem:复制"'
    Save-Screenshot $taskBob 'image-viewer-copy-menu.png'
    Send-Command $taskBob 'expect text "逆时针旋转图片"'
    Send-Command $taskBob 'click "逆时针旋转图片"'
    Send-Command $taskBob 'click "顺时针旋转图片"'
    Send-Command $taskBob 'expect layout clean'
    Save-Screenshot $taskBob 'image-viewer-window.png'
    Send-Command $taskBob 'window 1'
    Send-Command $taskBob 'click "切换到暗色外观"'
    Wait-Control $taskBob '切换到明亮外观'
    Send-Command $taskBob ('window '+$taskViewer.Groups[1].Value)
    Send-Command $taskBob 'expect layout clean'
    Save-Screenshot $taskBob 'image-viewer-window-dark.png'
    $taskSavedImage=Join-Path $taskRunDir 'saved-original.png'
    Send-Command $taskBob ('savefile "'+$taskSavedImage.Replace('\','/')+'"')
    Send-Command $taskBob 'click "保存原图"'
    $taskSaveDeadline=[DateTime]::UtcNow.AddSeconds(8)
    do {
        $null=Send-Command $taskBob 'wait 50'
    } until((Test-Path -LiteralPath $taskSavedImage) -or [DateTime]::UtcNow -ge $taskSaveDeadline)
    if (!(Test-Path -LiteralPath $taskSavedImage) -or (Get-FileHash -LiteralPath $taskSavedImage).Hash -ne (Get-FileHash -LiteralPath $taskPicturePath).Hash) {
        throw 'Image viewer did not save the original bytes'
    }
    Send-Command $taskBob ('closewindow '+$taskViewer.Groups[1].Value)
    Send-Command $taskBob 'window 1'
    $taskCharlie=Start-Peer '陈序' (Get-PeerAddress $taskAlice '我')

    Send-Command $taskAlice 'click "语音通话"'
    foreach($taskPeer in @($taskAlice,$taskBob)) {
        Send-Command $taskPeer 'settle 150'
        $taskVoiceWindows=Send-Command $taskPeer 'windows'
        if($taskPeer -eq $taskBob) {
            $taskVoice=[regex]::Match($taskVoiceWindows,'id=(\d+) active=false .*title="语音来电 · GoIM"')
            if($taskVoiceWindows -match 'title="语音通话 · GoIM"'){throw "Call window opened before answering: $taskVoiceWindows"}
        } else {
            $taskVoice=[regex]::Match($taskVoiceWindows,'id=(\d+).*title="语音通话 · GoIM"')
        }
        if(!$taskVoice.Success){throw "Expected voice surface did not open: $taskVoiceWindows"}
        Send-Command $taskPeer 'window 1'
        Send-Command $taskPeer 'click "消息输入框"'
        Send-Command $taskPeer 'type 通话期间输入不受阻塞'
        Send-Command $taskPeer 'expect exists "消息输入框"'
        Send-Command $taskPeer ('window '+$taskVoice.Groups[1].Value)
        Send-Command $taskPeer 'expect layout clean'
        Save-Screenshot $taskPeer ('voice-window-'+$taskPeer.Id+'.png')
        if($taskPeer -eq $taskBob){
            Send-Command $taskPeer 'expect exists "button:接听"'
            Send-Command $taskPeer 'click "拒绝"'
        }
    }
    foreach($taskPeer in @($taskAlice,$taskBob)) {
        Send-Command $taskPeer 'window 1'
        Send-Command $taskPeer 'waitfor "语音通话 · 通话已拒绝" 10000'
        Send-Command $taskPeer 'click "消息输入框"'
        Send-Command $taskPeer 'select 0 11'
        Send-Command $taskPeer 'key backspace'
    }
    Send-Command $taskAlice 'click id:goim/navigation/contacts'
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
    foreach($taskMemberPass in 1..2){
        Send-Command $taskAlice 'window 1'
        Send-Command $taskAlice 'click "群聊成员"'
        Send-Command $taskAlice 'settle 100'
        Send-Command $taskAlice 'expect exists "搜索群成员"'
        if($taskMemberPass -eq 1) {
            Send-Command $taskAlice 'click "搜索群成员"'
            Send-Command $taskAlice 'type 不存在的成员'
            Send-Command $taskAlice 'expect text "没有匹配的成员"'
            Send-Command $taskAlice 'expect layout clean'
            Send-Command $taskAlice 'expect state "button:关闭详情" reachable within 6000'
            Send-Command $taskAlice 'click "关闭详情"'
        } else {
            Send-Command $taskAlice 'expect exists "搜索群成员"'
            Send-Command $taskAlice 'expect text "林晓"'
            Send-Command $taskAlice 'click "button:林晓"'
            Send-Command $taskAlice 'click "查看资料"'
            Send-Command $taskAlice 'expect text "成员资料"'
            Send-Command $taskAlice 'key esc'
            Send-Command $taskAlice 'expect state "button:关闭详情" reachable within 6000'
            Send-Command $taskAlice 'click "关闭详情"'
        }
    }
    Send-Command $taskAlice 'window 1'
    Send-Command $taskAlice 'click "消息输入框"'
    Send-Command $taskAlice 'type 三个独立进程通过真实群聊通信。'
    Send-Command $taskAlice 'key enter'
    foreach($taskGuest in @($taskBob,$taskCharlie)){
        Send-Command $taskGuest 'click id:goim/navigation/messages'
        Wait-Control $taskGuest '三端验证群'
        Send-Command $taskGuest 'click "三端验证群"'
        Send-Command $taskGuest 'waitfor "三个独立进程通过真实群聊通信。" 10000'
    }
    Save-Screenshot $taskAlice 'real-group-chat.png'
    Send-Command $taskAlice 'click "设置"'
    Send-Command $taskAlice 'expect text "常规"'
    Send-Command $taskAlice 'expect layout clean'
    Save-Screenshot $taskAlice 'real-settings.png'
    Send-Command $taskAlice 'click "English"'
    Send-Command $taskAlice 'waitfor "Language" 10000'
    Send-Command $taskAlice 'expect text "Change the GoIM interface language immediately"'
    Send-Command $taskAlice 'expect layout clean'
    Save-Screenshot $taskAlice 'settings-english.png'
    Send-Command $taskAlice 'click "简体中文"'
    Send-Command $taskAlice 'waitfor "界面语言" 10000'
    Send-Command $taskAlice 'click "快捷键"'
    Send-Command $taskAlice 'expect layout clean'
    Save-Screenshot $taskAlice 'real-shortcuts.png'
    Send-Command $taskAlice 'click "锁定应用"'
    Send-Command $taskAlice 'expect text "设置锁定密码"'
    Send-Command $taskAlice 'click "textfield:新密码"'
    Enter-PasswordKeys $taskAlice
    Send-Command $taskAlice 'click "textfield:确认新密码"'
    Enter-PasswordKeys $taskAlice
    Send-Command $taskAlice 'click "设置并锁定"'
    Send-Command $taskAlice 'waitfor "GoIM 已锁定" 10000'
    $taskLockedWindows=Send-Command $taskAlice 'windows'
    if($taskLockedWindows -match '群聊成员 · 三端验证群'){throw "Member pane leaked into a separate window: $taskLockedWindows"}
    Send-Command $taskAlice 'expect layout clean'
    Save-Screenshot $taskAlice 'real-locked.png'
    Enter-PasswordKeys $taskAlice
    Send-Command $taskAlice 'key enter'
    Send-Command $taskAlice 'window 1'
    Send-Command $taskAlice 'waitfor "快捷键" 10000'
    $taskFolderShare=Join-Path $taskRunDir '交付资料'
    $taskFolderChild=Join-Path $taskFolderShare '界面设计'
    New-Item -ItemType Directory -Path $taskFolderChild -Force | Out-Null
    Copy-Item -LiteralPath $taskSamplePath -Destination (Join-Path $taskFolderChild '浏览器下载.txt')
    Copy-Item -LiteralPath $taskSamplePath -Destination (Join-Path $taskFolderShare '清单.txt')
    Send-Command $taskAlice 'click "button:文件共享"'
    Send-Command $taskAlice ('openfiles "'+$taskSamplePath.Replace('\','/')+'"')
    Send-Command $taskAlice 'click "新增分享"'
    Send-Command $taskAlice 'click "选择文件…"'
    Send-Command $taskAlice 'click "textfield:访问密码（可选）"'
    Send-Command $taskAlice 'type goim-share-smoke'
    Send-Command $taskAlice 'click "创建分享"'
    Wait-Control $taskAlice '复制链接'
    Send-Command $taskAlice 'click "复制链接"'
    $taskClip=Send-Command $taskAlice 'clip'
    $taskURL=[regex]::Match($taskClip,'http://127\.0\.0\.1:\d+/s/[a-zA-Z0-9_-]+').Value
    if(!$taskURL){throw 'No usable browser share URL was copied'}
    $taskLoginPage=Invoke-WebRequest -UseBasicParsing -Uri $taskURL -TimeoutSec 8
    if($taskLoginPage.Content -notmatch '分享密码' -or $taskLoginPage.Content -match '真实文件.txt'){throw 'The password page exposes the wrong content'}
    $taskSharePage=Invoke-WebRequest -UseBasicParsing -Uri $taskURL -Method Post -Body @{password='goim-share-smoke'} -SessionVariable taskBrowser -TimeoutSec 8
    if($taskSharePage.Content -notmatch '真实文件.txt' -or $taskSharePage.Content -notmatch '下载文件'){throw 'The unlocked browser page lacks the shared file'}
    $taskHTTPDownload=Join-Path $taskRunDir 'browser-download.txt'
    Invoke-WebRequest -UseBasicParsing -Uri ($taskURL+'/download') -WebSession $taskBrowser -OutFile $taskHTTPDownload -TimeoutSec 8
    if((Get-FileHash -LiteralPath $taskHTTPDownload).Hash -ne (Get-FileHash -LiteralPath $taskSamplePath).Hash){throw 'Browser download SHA256 differs'}
    Send-Command $taskAlice 'wait 300'
    Send-Command $taskAlice 'expect layout clean'
    Save-Screenshot $taskAlice 'real-share-list.png'
    Send-Command $taskAlice 'rightclick "真实文件.txt"'
    Send-Command $taskAlice 'click "查看二维码"'
    Send-Command $taskAlice 'waitfor "分享二维码" 10000'
    Send-Command $taskAlice 'wait 300'
    Save-Screenshot $taskAlice 'real-share-qr.png'
    if($BrowserScreenshots){
        $taskChrome=(Get-Process chrome -ErrorAction SilentlyContinue | Where-Object Path | Select-Object -First 1 -ExpandProperty Path)
        if(!$taskChrome){$taskChrome=Join-Path $env:ProgramFiles 'Google/Chrome/Application/chrome.exe'}
        if(!(Test-Path -LiteralPath $taskChrome)){throw 'Browser screenshot requires installed Chrome'}
        $taskBrowserProfile=Join-Path $taskRunDir 'chrome-screenshot'

    }
    Send-Command $taskAlice 'key esc'
    Send-Command $taskAlice 'wait 300'
    Send-Command $taskAlice ('opendirectory "'+$taskFolderShare.Replace('\','/')+'"')
    Send-Command $taskAlice 'click "新增分享"'
    Send-Command $taskAlice 'click "选择文件夹…"'
    Send-Command $taskAlice 'wait 200'
    Send-Command $taskAlice 'expect layout clean'
    Send-Command $taskAlice 'click "创建分享"'
    Wait-Control $taskAlice '交付资料'
    Send-Command $taskBob 'click "button:文件共享"'
    Send-Command $taskBob 'expect exists "checkbox:选择 交付资料" within 15000'
    Send-Command $taskBob 'click "checkbox:选择 交付资料"'
    Send-Command $taskBob 'click "checkbox:选择 真实文件.txt"'
    Send-Command $taskBob 'click "下载所选（2）"'
    Send-Command $taskBob 'expect exists "textfield:分享密码" within 15000'
    Send-Command $taskBob 'expect text "真实文件.txt"'
    Save-Screenshot $taskBob 'real-share-password.png'
    Send-Command $taskBob 'type goim-share-smoke'
    Send-Command $taskBob 'key enter'
    Send-Command $taskBob 'waitfor "文件传输" 15000'
    Send-Command $taskBob 'expect text "来源"'
    Send-Command $taskBob 'expect text "文件分享" within 15000'
    Send-Command $taskBob 'expect text "聊天"'
    $taskClientFolder=Join-Path $taskRunDir '林晓-downloads/交付资料/界面设计/浏览器下载.txt'
    $taskClientFile=Join-Path $taskRunDir '林晓-downloads/真实文件 (1).txt'
    $taskDeadline=[Diagnostics.Stopwatch]::StartNew()
    while(!(Test-Path -LiteralPath $taskClientFolder) -or !(Test-Path -LiteralPath $taskClientFile)) {
        if($taskDeadline.Elapsed.TotalSeconds -gt 15){throw 'Incoming batch did not finish'}
        Start-Sleep -Milliseconds 80
    }
    foreach($received in @($taskClientFolder,$taskClientFile)){
        if((Get-FileHash -LiteralPath $received).Hash -ne (Get-FileHash -LiteralPath $taskSamplePath).Hash){throw 'Member batch download differs'}
    }
    Send-Command $taskBob 'expect layout clean'
    Save-Screenshot $taskBob 'real-incoming-shares.png'
    Send-Command $taskBob 'click "button:文件共享"'
    Send-Command $taskBob 'click "交付资料"'
    Send-Command $taskBob 'expect exists "checkbox:选择 界面设计" within 15000'
    Send-Command $taskBob 'click "button:打开"'
    Send-Command $taskBob 'expect text "交付资料/界面设计" within 15000'
    Send-Command $taskBob 'expect exists "checkbox:选择 浏览器下载.txt"'
    Send-Command $taskBob 'expect text "成员共享 2"'
    Send-Command $taskBob 'expect layout clean'
    Save-Screenshot $taskBob 'member-shared-folder.png'
    Send-Command $taskBob 'click "button:返回"'
    Send-Command $taskBob 'expect exists "checkbox:选择 界面设计" within 15000'
    Send-Command $taskBob 'click "button:返回"'
    Send-Command $taskBob 'expect exists "checkbox:选择 交付资料"'
    Send-Command $taskBob 'click "交付资料"'
    Send-Command $taskBob 'expect exists "checkbox:选择 界面设计" within 15000'
    Send-Command $taskBob 'click "全选"'
    Send-Command $taskBob 'expect exists "button:取消全选"'
    Send-Command $taskBob 'click "下载所选（2）"'
    Send-Command $taskBob 'waitfor "文件传输" 15000'
    $taskNestedDownloads=@('林晓-downloads/清单.txt','林晓-downloads/界面设计/浏览器下载.txt')
    foreach($relative in $taskNestedDownloads) {
        $taskNestedFile=Join-Path $taskRunDir $relative
        $taskDeadline.Restart()
        while(!(Test-Path -LiteralPath $taskNestedFile)){
            if($taskDeadline.Elapsed.TotalSeconds -gt 15){throw "Selected item did not arrive: $relative"}
            Start-Sleep -Milliseconds 80
        }
        if((Get-FileHash -LiteralPath $taskNestedFile).Hash -ne (Get-FileHash -LiteralPath $taskSamplePath).Hash){throw 'Selected folder entry differs'}
    }
    Send-Command $taskAlice 'rightclick "交付资料"'
    Send-Command $taskAlice 'click "复制项目链接"'
    $taskFolderURL=[regex]::Match((Send-Command $taskAlice 'clip'),'http://127\.0\.0\.1:\d+/s/[a-zA-Z0-9_-]+').Value
    if(!$taskFolderURL -or $taskFolderURL -eq $taskURL){throw 'No distinct browser folder share URL was copied'}
    $taskRootPage=Invoke-WebRequest -UseBasicParsing -Uri $taskFolderURL -TimeoutSec 8
    $taskNestedHref=[regex]::Match($taskRootPage.Content,'href="([^"]+\?dir=\d+)" aria-label="打开文件夹：界面设计"').Groups[1].Value
    if(!$taskNestedHref){throw 'Shared folder is missing its subfolder link'}
    $taskNestedURL=[uri]::new([uri]$taskFolderURL, $taskNestedHref)
    $taskNestedPage=Invoke-WebRequest -UseBasicParsing -Uri $taskNestedURL -TimeoutSec 8
    $taskDownloadHref=[regex]::Match($taskNestedPage.Content,'href="([^"]+/download\?file=\d+)" aria-label="下载文件：浏览器下载.txt"').Groups[1].Value
    if(!$taskDownloadHref -or $taskNestedPage.Content -notmatch '返回上一级'){throw 'Nested folder has no download or parent navigation'}
    $taskFolderDownload=Join-Path $taskRunDir 'browser-folder-download.txt'
    Invoke-WebRequest -UseBasicParsing -Uri ([uri]::new([uri]$taskFolderURL,$taskDownloadHref)) -OutFile $taskFolderDownload -TimeoutSec 8
    if((Get-FileHash -LiteralPath $taskFolderDownload).Hash -ne (Get-FileHash -LiteralPath $taskSamplePath).Hash){throw 'Folder share download SHA256 differs'}
    Send-Command $taskAlice 'click "设备分享主页"'
    Send-Command $taskAlice 'click "启用设备主页"'
    Send-Command $taskAlice 'waitfor "设备主页已启用" 10000'
    Send-Command $taskAlice 'click "设备分享主页"'
    Send-Command $taskAlice 'click "复制设备主页"'
    $taskDeviceURL=[regex]::Match((Send-Command $taskAlice 'clip'),'http://127\.0\.0\.1:\d+/d/[a-zA-Z0-9_-]+').Value
    if(!$taskDeviceURL){throw 'No browser device homepage was copied'}
    $taskDeviceHome=Invoke-WebRequest -UseBasicParsing -Uri $taskDeviceURL -TimeoutSec 8
    if($taskDeviceHome.Content -notmatch '交付资料' -or $taskDeviceHome.Content -notmatch '真实文件.txt' -or $taskDeviceHome.Content -notmatch '需密码'){throw 'Device page did not list both active shares'}
    $taskUploadExpectedHash=(Get-FileHash -LiteralPath $taskSamplePath).Hash
    $taskFileInfo=Get-Item -LiteralPath $taskSamplePath
    $taskOffer=Invoke-RestMethod -Uri ($taskDeviceURL+'/upload/request') -Method Post -ContentType 'application/json; charset=utf-8' -Body (@{name=$taskFileInfo.Name; size=$taskFileInfo.Length}|ConvertTo-Json)
    if($taskOffer.status -ne 'offered'){throw 'Web upload bypassed confirmation'}
    Send-Command $taskAlice 'click "button:传输"'
    Send-Command $taskAlice 'waitfor "待接收 1" 10000'
    Send-Command $taskAlice 'click "button:接收"'
    $taskUploadURL=$taskDeviceURL+'/upload/'+$taskOffer.id
    if((Invoke-RestMethod -Uri $taskUploadURL).status -ne 'waiting'){throw 'Client did not approve web upload'}
    $taskResult=Invoke-WebRequest -Uri $taskUploadURL -Method Put -InFile $taskSamplePath -TimeoutSec 8
    if($taskResult.StatusCode -ne 204){throw 'Upload failed'}
    Send-Command $taskAlice 'waitfor "网页接收" 10000'
    Send-Command $taskAlice 'click "button:文件共享"'
    $taskInboxFile=Join-Path (Join-Path $taskRunDir '我-downloads/网页接收') '真实文件.txt'
    if((Get-FileHash -LiteralPath $taskInboxFile).Hash -ne $taskUploadExpectedHash){throw 'Browser upload SHA256 differs'}
    if($BrowserScreenshots){
        Send-Command $taskAlice 'click "设备分享主页"'
        Send-Command $taskAlice 'click "临时免确认（10 分钟）"'
    }
    if($BrowserScreenshots){
        Copy-Item -LiteralPath 'website/assets/goim.png' -Destination (Join-Path $taskFolderShare '项目标识.png')
        Set-Content -LiteralPath (Join-Path $taskFolderShare '验收计划.csv') -Value @('阶段,交付物', '设计,界面设计', '验收,交付清单') -Encoding utf8
        Set-Content -LiteralPath (Join-Path $taskFolderShare '交付说明.md') -Value '# 交付说明' -Encoding utf8
        $nodeOptions=@()
        if([int](node -p "process.versions.node.split('.')[0]") -eq 20){$nodeOptions+='--experimental-websocket'}
        node @nodeOptions scripts/share-browser-qa.mjs $taskChrome $taskBrowserProfile $taskURL $OutputDir $taskFolderURL $taskFolderShare $taskDeviceURL
        if($LASTEXITCODE -ne 0){throw 'Responsive browser screenshot check failed'}
        foreach($taskWebName in @('browser-first.txt','browser-second.txt')){
            $taskWebPath=Join-Path (Split-Path $taskInboxFile) $taskWebName
            if([IO.File]::ReadAllText($taskWebPath) -cne $taskWebName){throw 'Dropped file contents differ'}
        }
    }
    if($BrowserScreenshots){
        Send-Command $taskAlice 'click "设备分享主页"'
        Send-Command $taskAlice 'click "恢复网页接收确认"'
    }
    Send-Command $taskAlice 'wait 300'
    Send-Command $taskAlice 'expect layout clean'
    Save-Screenshot $taskAlice 'device-share-toolbar.png'
    Send-Command $taskAlice 'click "设备分享主页"'
    Send-Command $taskAlice 'click "关闭主页"'
    Send-Command $taskAlice 'waitfor "设备主页已关闭" 10000'
    $taskClosedHome=Invoke-WebRequest -UseBasicParsing -Uri $taskDeviceURL -SkipHttpErrorCheck -TimeoutSec 8
    if($taskClosedHome.StatusCode -ne 404){throw 'Disabled device page did not return 404'}
    $taskProject=Invoke-WebRequest -UseBasicParsing -Uri $taskFolderURL -TimeoutSec 8
    if($taskProject.Content -notmatch '界面设计'){throw 'Closing the device homepage revoked a direct project link'}
    Send-Command $taskBob 'click "button:文件共享"'
    Send-Command $taskBob 'click "交付资料"'
    Send-Command $taskBob 'expect exists "checkbox:选择 清单.txt" within 15000'
    $taskLiveFile=Join-Path $taskFolderShare '实时新增.txt'
    Copy-Item -LiteralPath $taskSamplePath -Destination $taskLiveFile
    Send-Command $taskBob 'expect exists "checkbox:选择 实时新增.txt" within 15000'
    Remove-Item -LiteralPath $taskLiveFile
    Send-Command $taskBob 'expect absent "checkbox:选择 实时新增.txt" within 15000'
    $taskMovedShare=Join-Path $taskRunDir '迁移后的交付资料'
    foreach($testPath in @($taskMovedShare,$taskFolderShare)){
        if([IO.Path]::GetDirectoryName([IO.Path]::GetFullPath($testPath)) -ne [IO.Path]::GetFullPath($taskRunDir)){throw 'Unexpected test folder location'}
    }
    Rename-Item -LiteralPath $taskFolderShare -NewName '迁移后的交付资料'
    Send-Command $taskAlice 'waitfor "分享不可用" 15000'
    Send-Command $taskBob 'expect absent "button:返回" within 15000'
    Send-Command $taskBob 'expect absent "checkbox:选择 交付资料"'
    $taskMissingShare=Invoke-WebRequest -UseBasicParsing -Uri $taskFolderURL -SkipHttpErrorCheck -TimeoutSec 8
    if($taskMissingShare.StatusCode -ne 410){throw 'Missing folder still accessible'}
    Send-Command $taskAlice ('opendirectory "'+$taskMovedShare.Replace('\','/')+'"')
    Send-Command $taskAlice 'click "修改共享源"'
    Send-Command $taskAlice 'click "重新选择文件夹…"'
    Send-Command $taskAlice 'waitfor "共享源已更新" 15000'
    Send-Command $taskBob 'expect exists "checkbox:选择 迁移后的交付资料" within 15000'
    $taskRepairedShare=Invoke-WebRequest -UseBasicParsing -Uri $taskFolderURL -TimeoutSec 8
    if($taskRepairedShare.Content -notmatch '迁移后的交付资料' -or $taskRepairedShare.Content -notmatch '界面设计'){throw 'Repaired share failed to preserve its link'}
    Send-Command $taskAlice 'rightclick "迁移后的交付资料"'
    Send-Command $taskAlice 'click "取消分享"'
    Send-Command $taskAlice 'waitfor "已取消分享" 10000'
    $taskRevokedProject=Invoke-WebRequest -UseBasicParsing -Uri $taskFolderURL -SkipHttpErrorCheck -TimeoutSec 8
    if($taskRevokedProject.StatusCode -ne 404){throw 'Removed share did not revoke its project link'}
    if($AvatarSample){
        Send-Command $taskAlice 'key esc'
        Send-Command $taskAlice 'click "消息"'
        $taskLargeAvatar=Join-Path $taskRunDir 'camera-avatar.jpg'
        $taskPhoto=[Drawing.Bitmap]::new(3000,2000,[Drawing.Imaging.PixelFormat]::Format24bppRgb)
        try {
            $taskBits=$taskPhoto.LockBits([Drawing.Rectangle]::new(0,0,3000,2000),[Drawing.Imaging.ImageLockMode]::WriteOnly,[Drawing.Imaging.PixelFormat]::Format24bppRgb)
            try {
                $taskPixels=[byte[]]::new([Math]::Abs($taskBits.Stride)*2000)
                [Random]::new(137).NextBytes($taskPixels)
                [Runtime.InteropServices.Marshal]::Copy($taskPixels,0,$taskBits.Scan0,$taskPixels.Length)
            } finally { $taskPhoto.UnlockBits($taskBits) }
            $taskPhoto.Save($taskLargeAvatar,[Drawing.Imaging.ImageFormat]::Jpeg)
        } finally { $taskPhoto.Dispose() }
        $taskPhotoSize=(Get-Item -LiteralPath $taskLargeAvatar).Length
        if($taskPhotoSize -lt 3MB -or $taskPhotoSize -gt 8MB){throw "Expected 3 to 8 MiB JPEG fixture; got $taskPhotoSize"}
        Send-Command $taskAlice 'click "编辑个人资料"'
        Send-Command $taskAlice ('openfiles "'+$taskLargeAvatar.Replace('\','/')+'"')
        $taskAlice.Refresh()
        $taskMemoryBefore=$taskAlice.WorkingSet64
        Send-Command $taskAlice 'click "选择图片"'
        Send-Command $taskAlice 'settle 500'
        $taskAlice.Refresh()
        $taskMemoryPreview=$taskAlice.WorkingSet64
        $taskIdleCPU=$taskAlice.TotalProcessorTime
        Start-Sleep -Milliseconds 850
        $taskAlice.Refresh()
        $taskAvatarCPU=($taskAlice.TotalProcessorTime-$taskIdleCPU).TotalMilliseconds
        Save-Screenshot $taskAlice 'real-avatar-preview.png'
        Send-Command $taskAlice 'click "保存"'
        Send-Command $taskAlice 'settle 500'
        $taskCanonical=Get-ChildItem -LiteralPath (Join-Path (Join-Path $taskRunDir '我') 'avatars') -Filter '*.png'
        if(!$taskCanonical -or ($taskCanonical | Measure-Object Length -Maximum).Maximum -gt 180KB){throw 'Avatar was not stored as a compact PNG'}
        ('Avatar {0:N2} MiB JPEG; process working set before {1:N1} MiB, preview {2:N1} MiB; idle CPU over 850 ms {3:N1} ms.' -f ($taskPhotoSize/1MB),($taskMemoryBefore/1MB),($taskMemoryPreview/1MB),$taskAvatarCPU)
    }
    'PASS: three independent processes, second launch restores the hidden main window, real TLS text, emoji, inline images, independent viewer and original save, live language switch, group delivery, SHA256, lock/unlock, browser project shares, incoming batch downloads, device catalog and upload.'
} catch {
    foreach ($taskProcess in $taskProcesses) {
        if (!$taskProcess.HasExited) {
            try {
                Send-Command $taskProcess 'tree' | Set-Content -LiteralPath (Join-Path $OutputDir "failure-$($taskProcess.Id).txt")
                Save-Screenshot $taskProcess "failure-$($taskProcess.Id).png"
            } catch {
                Write-Warning "Could not capture diagnostics for headless peer $($taskProcess.Id)"
            }
        }
    }
    throw
} finally {
    foreach ($taskProcess in $taskProcesses) {
        try {
            if (!$taskProcess.HasExited) {
                $taskProcess.StandardInput.Close()
                if (!$taskProcess.WaitForExit(3000)) {
                    $taskProcess.Kill($true)
                    $null=$taskProcess.WaitForExit(3000)
                }
            }
            $stderr=$taskStderr[$taskProcess.Id]
            if ($stderr.IsCompletedSuccessfully -and $stderr.Result) {
                Set-Content -LiteralPath (Join-Path $OutputDir "peer-$($taskProcess.Id).log") -Value $stderr.Result
            }
        } finally {
            $taskProcess.Dispose()
        }
    }
    if (Test-Path -LiteralPath $taskRunDir) {
        $resolved=(Resolve-Path -LiteralPath $taskRunDir).Path
        if ((Split-Path -Parent $resolved) -eq [IO.Path]::TrimEndingDirectorySeparator($taskTempRoot) -and
            (Split-Path -Leaf $resolved) -match '^goim-smoke-[a-f0-9]{32}$') {
            try {
                Remove-Item -LiteralPath $resolved -Recurse -Force
            } catch {
                Write-Warning "Could not remove isolated smoke files at ${resolved}: $($_.Exception.Message)"
            }
        }
    }
    Pop-Location
}
