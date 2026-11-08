param([string]$OutputDir = 'artifacts/headless', [switch]$BrowserScreenshots, [switch]$AvatarSample)
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
    foreach($taskArgument in @('--data-dir',(Join-Path $script:taskRunDir $TaskName),'--download-dir',(Join-Path $script:taskRunDir ($TaskName+'-downloads')),'--listen','127.0.0.1:0','--name',$TaskName,'--no-discovery','--no-ipmsg')){$taskInfo.ArgumentList.Add($taskArgument)}
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
    if (Test-Path -LiteralPath (Join-Path (Join-Path $taskRunDir '林晓-downloads') '真实文件.txt')) { throw 'An ordinary file arrived before acceptance' }
    Send-Command $taskBob 'click "接收文件"'
    Send-Command $taskBob 'waitfor "已接收" 10000'
    $taskReceived=Join-Path (Join-Path $taskRunDir '林晓-downloads') '真实文件.txt'
    if(!(Test-Path -LiteralPath $taskReceived)){throw 'Received file missing'}
    if((Get-FileHash -LiteralPath $taskReceived).Hash -ne (Get-FileHash -LiteralPath $taskSamplePath).Hash){throw 'Received SHA256 differs'}
    Send-Command $taskBob 'key esc'
    Send-Command $taskAlice 'click "插入表情"'
    Send-Command $taskAlice 'expect text "经典表情"'
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
    Wait-Control $taskBob '查看图片 真实图片.png'
    $taskImages=@(Get-ChildItem -LiteralPath (Join-Path $taskRunDir '林晓/chat-images') -Filter '*.png')
    if ($taskImages.Count -ne 1) { throw 'Expected one automatically received PNG' }
    $taskReceivedImage=$taskImages[0].FullName
    if((Get-FileHash -LiteralPath $taskReceivedImage).Hash -ne (Get-FileHash -LiteralPath $taskPicturePath).Hash){throw 'Received picture SHA256 differs'}
    Send-Command $taskBob 'expect layout clean'
    Send-Command $taskBob ('shot "'+(Join-Path (Join-Path $taskProjectRoot $OutputDir) 'real-picture-emoji.png').Replace('\','/')+'"')
    Send-Command $taskBob 'click "查看图片 真实图片.png"'
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
    Send-Command $taskAlice 'click "快捷键"'
    Send-Command $taskAlice 'expect layout clean'
    Send-Command $taskAlice ('shot "'+(Join-Path (Join-Path $taskProjectRoot $OutputDir) 'real-shortcuts.png').Replace('\','/')+'"')
    Send-Command $taskAlice 'click "锁定应用"'
    Send-Command $taskAlice 'expect text "设置锁定密码"'
    Send-Command $taskAlice 'click "textfield:新密码"'
    Send-Command $taskAlice 'type goim-smoke-only'
    Send-Command $taskAlice 'click "textfield:确认新密码"'
    Send-Command $taskAlice 'type goim-smoke-only'
    Send-Command $taskAlice 'click "设置并锁定"'
    Send-Command $taskAlice 'waitfor "GoIM 已锁定" 10000'
    Send-Command $taskAlice 'expect layout clean'
    Send-Command $taskAlice ('shot "'+(Join-Path (Join-Path $taskProjectRoot $OutputDir) 'real-locked.png').Replace('\','/')+'"')
    Send-Command $taskAlice 'type goim-smoke-only'
    Send-Command $taskAlice 'key enter'
    Send-Command $taskAlice 'waitfor "快捷键" 10000'
    Send-Command $taskAlice 'click "文件"'
    Send-Command $taskAlice 'click "textfield:本机文件夹路径"'
    Send-Command $taskAlice 'key cmd+a'
    Send-Command $taskAlice ('type '+$taskRunDir)
    Send-Command $taskAlice 'key enter'
    Send-Command $taskAlice 'waitfor "真实文件.txt" 10000'
    Send-Command $taskAlice 'click "选择文件 真实文件.txt"'
    Send-Command $taskAlice 'click "分享所选"'
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
    Send-Command $taskAlice 'expect layout clean'
    Send-Command $taskAlice ('shot "'+(Join-Path (Join-Path $taskProjectRoot $OutputDir) 'real-file-browser.png').Replace('\','/')+'"')
    Send-Command $taskAlice 'click "二维码"'
    Send-Command $taskAlice 'waitfor "分享二维码" 10000'
    Send-Command $taskAlice 'wait 300'
    Send-Command $taskAlice ('shot "'+(Join-Path (Join-Path $taskProjectRoot $OutputDir) 'real-share-qr.png').Replace('\','/')+'"')
    if($BrowserScreenshots){
        $taskChrome=(Get-Process chrome -ErrorAction SilentlyContinue | Where-Object Path | Select-Object -First 1 -ExpandProperty Path)
        if(!$taskChrome){$taskChrome=Join-Path $env:ProgramFiles 'Google/Chrome/Application/chrome.exe'}
        if(!(Test-Path -LiteralPath $taskChrome)){throw 'Browser screenshot requires installed Chrome'}
        $taskBrowserProfile=Join-Path $taskRunDir 'chrome-screenshot'
        node scripts/share-browser-qa.mjs $taskChrome $taskBrowserProfile $taskURL (Join-Path $taskProjectRoot $OutputDir)
        if($LASTEXITCODE -ne 0){throw 'Responsive browser screenshot check failed'}
    }
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
        Send-Command $taskAlice ('shot "'+(Join-Path (Join-Path $taskProjectRoot $OutputDir) 'real-avatar-preview.png').Replace('\','/')+'"')
        Send-Command $taskAlice 'click "保存"'
        Send-Command $taskAlice 'settle 500'
        $taskCanonical=Get-ChildItem -LiteralPath (Join-Path (Join-Path $taskRunDir '我') 'avatars') -Filter '*.png'
        if(!$taskCanonical -or ($taskCanonical | Measure-Object Length -Maximum).Maximum -gt 180KB){throw 'Avatar was not stored as a compact PNG'}
        ('Avatar {0:N2} MiB JPEG; process working set before {1:N1} MiB, preview {2:N1} MiB; idle CPU over 850 ms {3:N1} ms.' -f ($taskPhotoSize/1MB),($taskMemoryBefore/1MB),($taskMemoryPreview/1MB),$taskAvatarCPU)
    }
    'PASS: three independent processes, real TLS text, emoji, inline images, group delivery, SHA256, lock/unlock and a password-protected browser download.'
} catch {
    foreach($taskProcess in $taskProcesses){if(!$taskProcess.HasExited){try{Send-Command $taskProcess 'tree' | Set-Content -LiteralPath (Join-Path $OutputDir ("failure-"+$taskProcess.Id+'.txt'));$null=Send-Command $taskProcess ('shot "'+(Join-Path (Join-Path $taskProjectRoot $OutputDir) ("failure-"+$taskProcess.Id+'.png')).Replace('\','/')+'"')}catch{}}}
    throw
} finally {
    foreach($taskProcess in $taskProcesses){if(!$taskProcess.HasExited){try{$null=Send-Command $taskProcess 'quit'}catch{};if(!$taskProcess.WaitForExit(3000)){$taskProcess.Kill($true)}};$taskProcess.Dispose()}
    Pop-Location
}
