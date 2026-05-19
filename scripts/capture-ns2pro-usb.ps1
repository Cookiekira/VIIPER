#requires -Version 7.0
<#
.SYNOPSIS
    Capture USB traffic for a wired NS2 Pro / Switch 2 Pro controller Steam session.

.DESCRIPTION
    This helper wraps USBPcap/Wireshark so the useful capture sequence is
    repeatable: start capture before plugging the controller, enumerate the real
    USB device, open Steam, test input, rumble, and gyro/calibration, then save a
    timestamped pcapng plus a small manifest.

    USBPcapCMD.exe is preferred because modern Wireshark installs often expose
    USBPcap through extcap/USBPcapCMD rather than dumpcap -D. When capturing all
    root hubs, this script starts one USBPcapCMD process per root hub and merges
    the raw pcaps with mergecap.exe.

.EXAMPLE
    pwsh -ExecutionPolicy Bypass -File .\scripts\capture-ns2pro-usb.ps1

.EXAMPLE
    pwsh -ExecutionPolicy Bypass -File .\scripts\capture-ns2pro-usb.ps1 -AllUSBPcap -OpenSteam

.EXAMPLE
    pwsh -ExecutionPolicy Bypass -File .\scripts\capture-ns2pro-usb.ps1 -Interface USBPcap1 -DurationSec 120
#>

[CmdletBinding()]
param(
    [string[]]$Interface,
    [string]$OutDir,
    [int]$DurationSec = 0,
    [switch]$AllUSBPcap,
    [switch]$OpenSteam,
    [string]$SteamPath,
    [string]$DumpcapPath,
    [string]$USBPcapCMDPath,
    [string]$MergecapPath,
    [switch]$UseUSBPcapCMD,
    [switch]$PreferDumpcap,
    [switch]$DryRun,
    [switch]$NoAdminCheck,
    [switch]$NoSteamLogCopy
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Test-IsAdministrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Find-Executable {
    param(
        [string]$ProvidedPath,
        [string]$Name,
        [string[]]$CandidatePaths
    )

    if ($ProvidedPath) {
        if (Test-Path -LiteralPath $ProvidedPath) {
            return (Resolve-Path -LiteralPath $ProvidedPath).Path
        }
        throw "Provided path for $Name does not exist: $ProvidedPath"
    }

    $cmd = Get-Command $Name -ErrorAction SilentlyContinue
    if ($cmd) {
        return $cmd.Source
    }

    foreach ($candidate in $CandidatePaths) {
        if ($candidate -and (Test-Path -LiteralPath $candidate)) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }

    return $null
}

function Write-Log {
    param(
        [string]$Message,
        [ConsoleColor]$Color = [ConsoleColor]::Gray
    )

    Write-Host $Message -ForegroundColor $Color
    if ($script:LogPath) {
        Add-Content -LiteralPath $script:LogPath -Value ("[{0}] {1}" -f (Get-Date -Format "yyyy-MM-dd HH:mm:ss"), $Message)
    }
}

function Quote-Argument {
    param([string]$Value)

    if ($Value -eq "") {
        return '""'
    }
    if ($Value -notmatch '[\s"]') {
        return $Value
    }
    return '"' + ($Value -replace '"', '\"') + '"'
}

function Join-Arguments {
    param([string[]]$ArgumentItems)
    return (($ArgumentItems | ForEach-Object { Quote-Argument $_ }) -join " ")
}

function Get-DumpcapUSBPcapInterfaces {
    param([string]$Dumpcap)

    $lines = & $Dumpcap -D 2>&1
    $items = @()

    foreach ($line in $lines) {
        $text = [string]$line
        if ($text -notmatch "USBPcap") {
            continue
        }

        $index = $null
        $name = $null

        if ($text -match '^\s*(\d+)\.\s+(.+?)(?:\s+\(|$)') {
            $index = $matches[1]
            $name = $matches[2].Trim()
        }

        if (-not $name) {
            $name = $text.Trim()
        }

        $items += [pscustomobject]@{
            Index = $index
            Name  = $name
            Line  = $text.Trim()
        }
    }

    return $items
}

function Resolve-DumpcapCaptureInterfaces {
    param(
        [string]$Dumpcap,
        [string[]]$RequestedInterfaces,
        [bool]$CaptureAll
    )

    if ($RequestedInterfaces -and $RequestedInterfaces.Count -gt 0) {
        return $RequestedInterfaces
    }

    $usbIfs = @(Get-DumpcapUSBPcapInterfaces -Dumpcap $Dumpcap)
    if ($usbIfs.Count -eq 0) {
        return @()
    }

    Write-Log "USBPcap interfaces found:" Cyan
    foreach ($item in $usbIfs) {
        Write-Log ("  {0}" -f $item.Line) Gray
    }

    if ($CaptureAll -or $usbIfs.Count -eq 1) {
        return @($usbIfs | ForEach-Object {
            if ($_.Index) { $_.Index } else { $_.Name }
        })
    }

    Write-Log "" Gray
    Write-Log "Type A to capture all USBPcap interfaces, or enter one interface index/name." Yellow
    $choice = Read-Host "Interface"

    if ($choice -match '^(?i:a|all)$') {
        return @($usbIfs | ForEach-Object {
            if ($_.Index) { $_.Index } else { $_.Name }
        })
    }

    foreach ($item in $usbIfs) {
        if ($choice -eq $item.Index -or $choice -eq $item.Name) {
            if ($item.Index) { return @($item.Index) }
            return @($item.Name)
        }
    }

    return @($choice)
}

function ConvertTo-USBPcapDeviceName {
    param([string]$Value)

    $trimmed = $Value.Trim()
    if ($trimmed -match '^\\\\\.\\USBPcap\d+$') {
        return $trimmed
    }
    if ($trimmed -match '^USBPcap\d+$') {
        return "\\.\$trimmed"
    }
    if ($trimmed -match '^\d+$') {
        return "\\.\USBPcap$trimmed"
    }
    return $trimmed
}

function Get-USBPcapRootHubCandidates {
    $rootHubs = @(
        Get-CimInstance Win32_PnPEntity -ErrorAction SilentlyContinue |
            Where-Object {
                $_.PNPDeviceID -like "USB\ROOT_HUB*" -or
                $_.Name -like "USB Root Hub*"
            }
    )

    $count = $rootHubs.Count
    if ($count -le 0) {
        Write-Log "Could not count USB Root Hubs through CIM; falling back to USBPcap1..USBPcap8." Yellow
        $count = 8
    }

    return 1..$count | ForEach-Object { "\\.\USBPcap$_" }
}

function Resolve-USBPcapCMDInterfaces {
    param(
        [string[]]$RequestedInterfaces,
        [bool]$CaptureAll
    )

    if ($RequestedInterfaces -and $RequestedInterfaces.Count -gt 0) {
        return @($RequestedInterfaces | ForEach-Object { ConvertTo-USBPcapDeviceName $_ })
    }

    $candidates = @(Get-USBPcapRootHubCandidates)
    Write-Log "USBPcapCMD root-hub candidates:" Cyan
    foreach ($candidate in $candidates) {
        Write-Log ("  {0}" -f $candidate) Gray
    }

    if ($CaptureAll -or $candidates.Count -eq 1) {
        return $candidates
    }

    Write-Log "" Gray
    Write-Log "Type A to capture all candidates, or enter one number/name such as 1, USBPcap1, or \\.\USBPcap1." Yellow
    $choice = Read-Host "USBPcap interface"

    if ($choice -match '^(?i:a|all)$') {
        return $candidates
    }

    return @(ConvertTo-USBPcapDeviceName $choice)
}

function Find-SteamExecutable {
    param([string]$ProvidedPath)

    if ($ProvidedPath) {
        if (Test-Path -LiteralPath $ProvidedPath) {
            return (Resolve-Path -LiteralPath $ProvidedPath).Path
        }
        throw "Steam path does not exist: $ProvidedPath"
    }

    $regPaths = @(
        "HKCU:\Software\Valve\Steam",
        "HKLM:\Software\Valve\Steam",
        "HKLM:\Software\WOW6432Node\Valve\Steam"
    )

    foreach ($regPath in $regPaths) {
        $props = Get-ItemProperty -LiteralPath $regPath -ErrorAction SilentlyContinue
        if (-not $props) {
            continue
        }

        foreach ($propName in @("SteamExe", "InstallPath", "SteamPath")) {
            $value = $props.$propName
            if (-not $value) {
                continue
            }

            $candidate = $value
            if ((Test-Path -LiteralPath $candidate -PathType Container)) {
                $candidate = Join-Path $candidate "steam.exe"
            }
            if (Test-Path -LiteralPath $candidate) {
                return (Resolve-Path -LiteralPath $candidate).Path
            }
        }
    }

    $fallback = Join-Path ${env:ProgramFiles(x86)} "Steam\steam.exe"
    if (Test-Path -LiteralPath $fallback) {
        return (Resolve-Path -LiteralPath $fallback).Path
    }

    return $null
}

function Start-CaptureProcess {
    param(
        [string]$FilePath,
        [string[]]$Arguments,
        [string]$SessionDir,
        [string]$Name = "capture-tool"
    )

    $safeName = $Name -replace '[^\w.-]', '_'
    $stdoutPath = Join-Path $SessionDir "$safeName.stdout.log"
    $stderrPath = Join-Path $SessionDir "$safeName.stderr.log"
    $argumentLine = Join-Arguments $Arguments

    Write-Log ("Capture command: {0} {1}" -f $FilePath, $argumentLine) DarkGray

    return Start-Process `
        -FilePath $FilePath `
        -ArgumentList $argumentLine `
        -WorkingDirectory $SessionDir `
        -RedirectStandardOutput $stdoutPath `
        -RedirectStandardError $stderrPath `
        -WindowStyle Hidden `
        -PassThru
}

function Stop-CaptureProcess {
    param([System.Diagnostics.Process]$Process)

    if (-not $Process -or $Process.HasExited) {
        return
    }

    Write-Log "Stopping capture..." Cyan
    try {
        $null = $Process.CloseMainWindow()
        Start-Sleep -Milliseconds 1200
    }
    catch {
    }

    if (-not $Process.HasExited) {
        Stop-Process -Id $Process.Id -Force -ErrorAction SilentlyContinue
    }
    try {
        $Process.WaitForExit(5000) | Out-Null
    }
    catch {
    }
}

function Wait-CaptureStep {
    param([string]$Message)

    Write-Log "" Gray
    Write-Log $Message Cyan
    [void](Read-Host "Press Enter when done")
}

function Copy-SteamLogs {
    param(
        [string]$SteamExe,
        [string]$SessionDir
    )

    if (-not $SteamExe) {
        return
    }

    $steamDir = Split-Path -Parent $SteamExe
    $logDir = Join-Path $steamDir "logs"
    if (-not (Test-Path -LiteralPath $logDir)) {
        return
    }

    foreach ($name in @("controller.txt", "controller_ui.txt", "configstore_log.txt")) {
        $src = Join-Path $logDir $name
        if (Test-Path -LiteralPath $src) {
            Copy-Item -LiteralPath $src -Destination (Join-Path $SessionDir ("steam-" + $name)) -Force
            Write-Log ("Copied Steam log: {0}" -f $name) DarkGray
        }
    }
}

function Write-Readme {
    param(
        [string]$Path,
        [string]$PcapPath
    )

    $content = @"
NS2 Pro / Switch 2 Pro USB capture

Main capture:
  $PcapPath

Useful Wireshark display filters:
  usb.idVendor == 0x057e || usb.idProduct == 0x2069
  usb.device_address == <addr> && usb.setup.bRequest == 6
  usb.device_address == <addr> && (usb.endpoint_address == 0x02 || usb.endpoint_address == 0x82)
  usb.device_address == <addr> && (usb.endpoint_address == 0x81 || usb.endpoint_address == 0x01)

What VIIPER needs from this capture:
  - EP0 descriptors from the real wired controller
  - HID report descriptor
  - Steam init/config traffic on bulk OUT 0x02 and bulk IN 0x82
  - Idle input report 0x09 on interrupt IN 0x81
  - Rumble report 0x02 on interrupt OUT 0x01
  - Gyro/IMU feature and calibration commands on bulk endpoints
"@

    Set-Content -LiteralPath $Path -Value $content -Encoding UTF8
}

$script:LogPath = $null

if (-not $OutDir) {
    $repoRoot = Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")
    $OutDir = Join-Path $repoRoot "captures\ns2pro"
}

$sessionName = "ns2pro-usb-{0}" -f (Get-Date -Format "yyyyMMdd-HHmmss")
$sessionDir = Join-Path $OutDir $sessionName
New-Item -ItemType Directory -Force -Path $sessionDir | Out-Null

$script:LogPath = Join-Path $sessionDir "capture-session.log"
$pcapPath = Join-Path $sessionDir "$sessionName.pcapng"
$readmePath = Join-Path $sessionDir "README.txt"
$manifestPath = Join-Path $sessionDir "manifest.json"

Write-Log "NS2 Pro / Switch 2 Pro USB capture helper" Green
Write-Log ("Output directory: {0}" -f $sessionDir) Green

if (-not $NoAdminCheck -and -not (Test-IsAdministrator)) {
    Write-Log "Warning: USBPcap usually needs an elevated PowerShell session." Yellow
    Write-Log "If capture fails or no USBPcap interfaces appear, rerun PowerShell as Administrator." Yellow
}

$programFiles = ${env:ProgramFiles}
$programFilesX86 = ${env:ProgramFiles(x86)}
$dumpcap = Find-Executable `
    -ProvidedPath $DumpcapPath `
    -Name "dumpcap.exe" `
    -CandidatePaths @(
        (Join-Path $programFiles "Wireshark\dumpcap.exe"),
        (Join-Path $programFilesX86 "Wireshark\dumpcap.exe")
    )

$usbpcapcmd = Find-Executable `
    -ProvidedPath $USBPcapCMDPath `
    -Name "USBPcapCMD.exe" `
    -CandidatePaths @(
        (Join-Path $programFiles "USBPcap\USBPcapCMD.exe"),
        (Join-Path $programFilesX86 "USBPcap\USBPcapCMD.exe")
    )

$mergecap = Find-Executable `
    -ProvidedPath $MergecapPath `
    -Name "mergecap.exe" `
    -CandidatePaths @(
        (Join-Path $programFiles "Wireshark\mergecap.exe"),
        (Join-Path $programFilesX86 "Wireshark\mergecap.exe")
    )

$toolKind = $null
$toolPath = $null
$captureInterfaces = @()

if ($UseUSBPcapCMD) {
    if (-not $usbpcapcmd) {
        throw "USBPcapCMD.exe was not found."
    }
    $toolKind = "USBPcapCMD"
    $toolPath = $usbpcapcmd
    $captureInterfaces = @(Resolve-USBPcapCMDInterfaces -RequestedInterfaces $Interface -CaptureAll ([bool]$AllUSBPcap))
}
elseif ($PreferDumpcap -and $dumpcap) {
    $toolKind = "dumpcap"
    $toolPath = $dumpcap
    $captureInterfaces = @(Resolve-DumpcapCaptureInterfaces -Dumpcap $dumpcap -RequestedInterfaces $Interface -CaptureAll ([bool]$AllUSBPcap))
    if ($captureInterfaces.Count -eq 0) {
        Write-Log "dumpcap did not expose USBPcap interfaces; falling back to USBPcapCMD." Yellow
        if (-not $usbpcapcmd) {
            throw "dumpcap did not expose USBPcap interfaces and USBPcapCMD.exe was not found."
        }
        $toolKind = "USBPcapCMD"
        $toolPath = $usbpcapcmd
        $captureInterfaces = @(Resolve-USBPcapCMDInterfaces -RequestedInterfaces $Interface -CaptureAll ([bool]$AllUSBPcap))
    }
}
elseif ($usbpcapcmd) {
    $toolKind = "USBPcapCMD"
    $toolPath = $usbpcapcmd
    $captureInterfaces = @(Resolve-USBPcapCMDInterfaces -RequestedInterfaces $Interface -CaptureAll ([bool]$AllUSBPcap))
}
elseif ($dumpcap) {
    $toolKind = "dumpcap"
    $toolPath = $dumpcap
    $captureInterfaces = @(Resolve-DumpcapCaptureInterfaces -Dumpcap $dumpcap -RequestedInterfaces $Interface -CaptureAll ([bool]$AllUSBPcap))
    if ($captureInterfaces.Count -eq 0) {
        throw "dumpcap did not expose USBPcap interfaces and USBPcapCMD.exe was not found."
    }
}
else {
    throw "Neither dumpcap.exe nor USBPcapCMD.exe was found. Install Wireshark with USBPcap enabled."
}

$steamExe = Find-SteamExecutable -ProvidedPath $SteamPath
if ($steamExe) {
    Write-Log ("Steam executable: {0}" -f $steamExe) DarkGray
}
else {
    Write-Log "Steam executable was not found automatically. You can still open Steam manually." Yellow
}

$captureSpecs = @()
$rawCaptureFiles = @()
if ($toolKind -eq "dumpcap") {
    $captureArgs = @()
    foreach ($iface in $captureInterfaces) {
        $captureArgs += @("-i", $iface)
    }
    $captureArgs += @("-w", $pcapPath, "-q")
    if ($DurationSec -gt 0) {
        $captureArgs += @("-a", "duration:$DurationSec")
    }
    $captureSpecs += [pscustomobject]@{
        Name       = "dumpcap"
        FilePath   = $toolPath
        Arguments  = $captureArgs
        OutputPath = $pcapPath
    }
}
else {
    $rawDir = Join-Path $sessionDir "raw"
    New-Item -ItemType Directory -Force -Path $rawDir | Out-Null

    foreach ($iface in $captureInterfaces) {
        $shortName = ($iface -replace '^\\\\\.\\', '') -replace '[^\w.-]', '_'
        $rawPath = Join-Path $rawDir "$shortName.pcap"
        $rawCaptureFiles += $rawPath
        $captureArgs = @(
            "-d", $iface,
            "-A",
            "--inject-descriptors",
            "-s", "262144",
            "-o", $rawPath
        )

        $captureSpecs += [pscustomobject]@{
            Name       = $shortName
            FilePath   = $toolPath
            Arguments  = $captureArgs
            OutputPath = $rawPath
        }
    }
}

Write-Readme -Path $readmePath -PcapPath $pcapPath

$manifest = [ordered]@{
    startedAt     = (Get-Date).ToString("o")
    toolKind      = $toolKind
    toolPath      = $toolPath
    mergecapPath  = $mergecap
    interfaces    = $captureInterfaces
    outputPcapng  = $pcapPath
    rawCaptures   = $rawCaptureFiles
    durationSec   = $DurationSec
    openSteam     = [bool]$OpenSteam
    dryRun        = [bool]$DryRun
    notes         = "Start capture before plugging the wired controller. Use README.txt for Wireshark filters."
}
$manifest | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $manifestPath -Encoding UTF8

if ($DryRun) {
    Write-Log "" Gray
    Write-Log "Dry run only. Capture processes were not started." Yellow
    foreach ($spec in $captureSpecs) {
        Write-Log ("Capture command: {0} {1}" -f $spec.FilePath, (Join-Arguments -ArgumentItems $spec.Arguments)) Cyan
    }
    Write-Log ("Output directory: {0}" -f $sessionDir) Green
    exit 0
}

Write-Log "" Gray
Write-Log "Recommended pre-flight:" Yellow
Write-Log "  1. Unplug the controller." Gray
Write-Log "  2. Fully exit Steam from the tray if you want a clean init sequence." Gray
Write-Log "  3. Keep this PowerShell window open until the capture is stopped." Gray

if ($DurationSec -eq 0) {
    [void](Read-Host "Press Enter to start capture")
}
else {
    Write-Log ("Timed mode: capture will run for about {0} seconds." -f $DurationSec) Yellow
}

$captureProcesses = @()
try {
    foreach ($spec in $captureSpecs) {
        $process = Start-CaptureProcess -FilePath $spec.FilePath -Arguments $spec.Arguments -SessionDir $sessionDir -Name $spec.Name
        $captureProcesses += [pscustomobject]@{
            Name       = $spec.Name
            OutputPath = $spec.OutputPath
            Process    = $process
        }
    }

    Start-Sleep -Seconds 2

    $running = @($captureProcesses | Where-Object { $_.Process -and -not $_.Process.HasExited })
    if ($running.Count -eq 0) {
        throw "Capture tool exited immediately. Check *.stderr.log in $sessionDir."
    }

    foreach ($item in $captureProcesses) {
        if ($item.Process.HasExited) {
            Write-Log ("Capture process {0} exited early. Check {0}.stderr.log if its raw file is empty." -f $item.Name) Yellow
        }
        else {
            Write-Log ("Capture started: {0}, PID {1}" -f $item.Name, $item.Process.Id) Green
        }
    }

    if ($DurationSec -gt 0) {
        Write-Log "Now plug the wired controller, open Steam, test input, rumble, and gyro before the timer ends." Cyan
        if ($OpenSteam -and $steamExe) {
            Start-Process -FilePath $steamExe | Out-Null
            Write-Log "Steam launched." Green
        }
        Start-Sleep -Seconds $DurationSec
    }
    else {
        Wait-CaptureStep "Plug the controller in with USB. Wait until Windows finishes enumeration."

        if ($OpenSteam -and $steamExe) {
            Start-Process -FilePath $steamExe | Out-Null
            Write-Log "Steam launched." Green
        }
        else {
            Wait-CaptureStep "Open Steam and go to the controller settings page."
        }

        Wait-CaptureStep "Confirm Steam sees the controller, then press buttons and move both sticks."
        Wait-CaptureStep "Run Steam's rumble test once."
        Wait-CaptureStep "Open gyro / calibration settings and toggle or calibrate gyro once if available."
        Wait-CaptureStep "Press Enter one more time to stop the capture."
    }
}
finally {
    foreach ($item in $captureProcesses) {
        Stop-CaptureProcess -Process $item.Process
    }
}

if ($toolKind -eq "USBPcapCMD") {
    $existingRawCaptures = @(
        $rawCaptureFiles | Where-Object {
            (Test-Path -LiteralPath $_) -and ((Get-Item -LiteralPath $_).Length -gt 0)
        }
    )
    if ($existingRawCaptures.Count -eq 0) {
        Write-Log "No raw USBPcap files were created. Check *.stderr.log in the session directory." Yellow
    }
    elseif ($mergecap) {
        Write-Log "Merging raw USBPcap captures into pcapng..." Cyan
        $mergeArgs = @("-F", "pcapng", "-w", $pcapPath) + $existingRawCaptures
        $mergeOutput = & $mergecap @mergeArgs 2>&1
        if ($LASTEXITCODE -ne 0) {
            Write-Log "mergecap failed; raw capture files are still available under the raw directory." Yellow
            foreach ($line in $mergeOutput) {
                Write-Log ([string]$line) Yellow
            }
        }
    }
    elseif ($existingRawCaptures.Count -eq 1) {
        Copy-Item -LiteralPath $existingRawCaptures[0] -Destination $pcapPath -Force
        Write-Log "mergecap.exe was not found; copied the single raw pcap to the final capture path." Yellow
    }
    else {
        Write-Log "mergecap.exe was not found; multiple raw pcap files were left unmerged under the raw directory." Yellow
    }
}

if (-not $NoSteamLogCopy) {
    Copy-SteamLogs -SteamExe $steamExe -SessionDir $sessionDir
}

$finalManifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
$finalManifest | Add-Member -NotePropertyName finishedAt -NotePropertyValue (Get-Date).ToString("o") -Force
$finalManifest | Add-Member -NotePropertyName pcapExists -NotePropertyValue (Test-Path -LiteralPath $pcapPath) -Force
if (Test-Path -LiteralPath $pcapPath) {
    $finalManifest | Add-Member -NotePropertyName pcapBytes -NotePropertyValue ((Get-Item -LiteralPath $pcapPath).Length) -Force
}
$finalManifest | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $manifestPath -Encoding UTF8

Write-Log "" Gray
Write-Log "Capture complete." Green
Write-Log ("PCAPNG: {0}" -f $pcapPath) Green
Write-Log ("Session log: {0}" -f $script:LogPath) Green
Write-Log ("README: {0}" -f $readmePath) Green
