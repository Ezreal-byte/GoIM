param(
    [Parameter(Mandatory)][ValidatePattern('^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$')][string]$Version,
    [Parameter(Mandatory)][ValidateSet('amd64', 'arm64')][string]$Architecture,
    [switch]$SkipMSI,
    [string]$Output = 'dist'
)

$ErrorActionPreference = 'Stop'
$name = "GoIM_${Version}_windows_$Architecture"
$outputPath = (Resolve-Path -LiteralPath $Output).Path
$staging = (Resolve-Path -LiteralPath (Join-Path $outputPath $name)).Path
$setup = (Resolve-Path -LiteralPath (Join-Path $outputPath "$name-Setup.exe")).Path
$msi = if (!$SkipMSI) { (Resolve-Path -LiteralPath (Join-Path $outputPath "$name.msi")).Path } else { $null }
$userLink = Join-Path ([Environment]::GetFolderPath('Programs')) 'GoIM.lnk'
$machineLink = Join-Path ([Environment]::GetFolderPath('CommonPrograms')) 'GoIM.lnk'

# Exercise the actual installers without touching a preexisting installation.
$uninstallPaths = @(
    'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*',
    'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*',
    'HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*'
)
$existing = Get-ItemProperty -Path $uninstallPaths -ErrorAction SilentlyContinue |
    Where-Object { $_.DisplayName -eq 'GoIM' }
if ($existing -or (Test-Path -LiteralPath $userLink) -or (Test-Path -LiteralPath $machineLink)) {
    throw 'Installer verification needs a clean Windows user and no installed GoIM.'
}

Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

public static class InstallerIcons {
    [DllImport("shell32.dll", CharSet = CharSet.Unicode)]
    private static extern uint ExtractIconEx(string path, int index, out IntPtr large, IntPtr small, uint count);
    [DllImport("user32.dll")]
    private static extern bool DestroyIcon(IntPtr icon);

    public static bool CanLoad(string path, int index) {
        uint count = ExtractIconEx(path, index, out IntPtr icon, IntPtr.Zero, 1);
        if (icon != IntPtr.Zero) DestroyIcon(icon);
        return count == 1 && icon != IntPtr.Zero;
    }
}
'@

function Invoke-Installer([string]$File, [string]$Arguments) {
    $process = Start-Process -FilePath $File -ArgumentList $Arguments -Wait -PassThru -WindowStyle Hidden
    if ($process.ExitCode -ne 0) { throw "$File failed with exit code $($process.ExitCode)" }
}

function Confirm-Installation([string]$Directory, [string]$Link, [string]$IconName) {
    foreach ($source in Get-ChildItem -LiteralPath $staging -File -Recurse) {
        $relative = $source.FullName.Substring($staging.Length + 1)
        $installed = Join-Path $Directory $relative
        if (!(Test-Path -LiteralPath $installed -PathType Leaf) -or
            (Get-FileHash -LiteralPath $source.FullName).Hash -ne (Get-FileHash -LiteralPath $installed).Hash) {
            throw "Missing or changed installed file: $relative"
        }
    }

    if (!(Test-Path -LiteralPath $Link -PathType Leaf)) { throw "Missing shortcut: $Link" }
    $shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut($Link)
    $executable = Join-Path $Directory 'GoIM.exe'
    if ($shortcut.TargetPath -ne $executable) {
        throw "Wrong target for ${Link}: $($shortcut.TargetPath)"
    }
    $location, $index = $shortcut.IconLocation -split ',(?=[^,]*$)'
    if (!$location) { $location = $executable }
    $expectedIcon = Join-Path $Directory $IconName
    if ($location -ne $expectedIcon -or ![InstallerIcons]::CanLoad($location, [int]$index)) {
        throw "Windows could not load the expected icon for ${Link}: $location,$index"
    }
    Write-Output "Verified shortcut and icon: $Link -> $executable ($location,$index)"
}

$scratch = Join-Path ([IO.Path]::GetTempPath()) ("GoIM installer check " + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $scratch | Out-Null
$nsisRoot = Join-Path $scratch 'Setup'
$uninstaller = Join-Path $nsisRoot 'Uninstall.exe'
try {
    # NSIS requires /D= last, with the path left unquoted even if it has spaces.
    Invoke-Installer $setup "/S /D=$nsisRoot"
    Confirm-Installation $nsisRoot $userLink 'GoIM.ico'
} finally {
    if (Test-Path -LiteralPath $uninstaller) { Invoke-Installer $uninstaller '/S' }
}
if (Test-Path -LiteralPath $userLink) { throw "Setup did not remove $userLink" }

if (!$SkipMSI) {
    $msiRoot = Join-Path $scratch 'MSI'
    $log = Join-Path $scratch 'msi-install.log'
    $msiInstalled = $false
    try {
        Invoke-Installer 'msiexec.exe' ('/i "{0}" /qn /norestart INSTALLFOLDER="{1}" /l*v "{2}"' -f $msi, $msiRoot, $log)
        $msiInstalled = $true
        Confirm-Installation $msiRoot $machineLink 'GoIM.exe'
    } catch {
        if (Test-Path -LiteralPath $log) { Get-Content -LiteralPath $log -Tail 40 | Write-Output }
        throw
    } finally {
        if ($msiInstalled) { Invoke-Installer 'msiexec.exe' ('/x "{0}" /qn /norestart' -f $msi) }
    }
    if (Test-Path -LiteralPath $machineLink) { throw "MSI did not remove $machineLink" }
}
