# Installs the Smart Parking device-agent on Windows.
#
# Usually started by parking-agent-setup.exe (downloaded from the admin panel,
# settings already inside: double-tap it and answer "Yes"). It can also be run
# from the unzipped folder: right-click install.cmd -> "Run as administrator".
#
# It copies the agent to C:\Program Files\ParkingAgent, opens TCP 8090 for
# cameras in Windows Firewall and registers a task that starts the agent at
# boot (as SYSTEM, restarted if it stops).
#
#   -Gui   show the result in a message box (touch screens: no console to read)

param([switch]$Gui)

$ErrorActionPreference = 'Stop'
$TaskName = 'ParkingAgent'
$RuleName = 'Parking Agent (cameras, TCP 8090)'
$Src = $PSScriptRoot
$Dst = Join-Path $env:ProgramFiles 'ParkingAgent'
$Exe = Join-Path $Dst 'parking-agent.exe'
$Env = Join-Path $Dst 'agent.env'
$Log = Join-Path $env:ProgramData 'parking-agent\agent.log'

function Show-Result([string]$Text, [string]$Kind) {
    $color = @{ Info = 'Green'; Warning = 'Yellow'; Error = 'Red' }[$Kind]
    Write-Host ""
    Write-Host $Text -ForegroundColor $color
    if ($Gui) {
        Add-Type -AssemblyName System.Windows.Forms
        $icon = [System.Windows.Forms.MessageBoxIcon]::$Kind
        [System.Windows.Forms.MessageBox]::Show($Text, 'Parking Agent', 'OK', $icon) | Out-Null
    }
}

try {
    $admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if (-not $admin) {
        Show-Result "Administrator huquqi kerak: install.cmd ni o'ng tugma -> 'Run as administrator' bilan oching." 'Error'
        exit 1
    }

    # Stop a running copy before replacing the exe
    if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
        Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 2
    }
    Get-Process parking-agent -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 1

    New-Item -ItemType Directory -Force -Path $Dst | Out-Null
    Copy-Item (Join-Path $Src 'parking-agent.exe') $Exe -Force
    foreach ($f in 'agent.env.example', 'uninstall.ps1', 'uninstall.cmd') {
        if (Test-Path (Join-Path $Src $f)) { Copy-Item (Join-Path $Src $f) (Join-Path $Dst $f) -Force }
    }

    # agent.env from the package (the setup .exe always brings one: a new
    # token) replaces the installed one; without it the installed one stays.
    if (Test-Path (Join-Path $Src 'agent.env')) {
        if (Test-Path $Env) { Remove-Item $Env -Force }
        Copy-Item (Join-Path $Src 'agent.env') $Env
    } elseif (-not (Test-Path $Env)) {
        Copy-Item (Join-Path $Src 'agent.env.example') $Env
    }

    # agent.env holds the token: only Administrators and SYSTEM may read it
    icacls $Env /inheritance:r /grant:r '*S-1-5-32-544:F' '*S-1-5-18:F' | Out-Null

    if (Select-String -Path $Env -Pattern '^AGENT_TOKEN=pa_xxxx' -Quiet) {
        Start-Process notepad.exe $Env
        Show-Result ("agent.env hali to'ldirilmagan. Notepad'da AGENT_TOKEN ni (admin paneldagi tokenni) yozing, " +
            "saqlang va install.cmd ni qayta ishga tushiring.`n`n$Env`n`n" +
            "Osonroq yo'l: admin panel -> Kamera & Shlagbaum -> agent -> 'Windows o'rnatuvchi'.") 'Warning'
        exit 1
    }

    # Cameras / capture-service on the LAN send events to this PC on TCP 8090
    if (-not (Get-NetFirewallRule -DisplayName $RuleName -ErrorAction SilentlyContinue)) {
        New-NetFirewallRule -DisplayName $RuleName -Direction Inbound -Protocol TCP -LocalPort 8090 -Action Allow -Profile Any | Out-Null
    }

    $action = New-ScheduledTaskAction -Execute $Exe -Argument "-config `"$Env`"" -WorkingDirectory $Dst
    $trigger = New-ScheduledTaskTrigger -AtStartup
    $principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable `
        -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) `
        -MultipleInstances IgnoreNew
    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings `
        -Description 'Smart Parking device-agent: barrier commands and camera events' -Force | Out-Null
    Start-ScheduledTask -TaskName $TaskName

    Write-Host ""
    Write-Host "Agent o'rnatildi va ishga tushdi. Backendga ulanish tekshirilmoqda..."
    $ok = $false
    for ($i = 0; $i -lt 15; $i++) {
        Start-Sleep -Seconds 2
        try {
            $h = Invoke-RestMethod -Uri 'http://127.0.0.1:8090/health' -TimeoutSec 3
            if ($h.backend.connected) { $ok = $true; break }
        } catch { }
    }
    if ($ok) {
        Show-Result ("Tayyor! Agent o'rnatildi va backendga ulandi.`n`n" +
            "Kompyuter qayta yoqilganda agent o'zi ishga tushadi. Admin panelda agent 'ONLAYN' bo'lib ko'rinadi.") 'Info'
    } else {
        Show-Result ("Agent o'rnatildi va ishlayapti, lekin backendga hali ulanmadi.`n`n" +
            "Internetni tekshiring. Log: $Log") 'Warning'
    }
} catch {
    Show-Result ("O'rnatishda xatolik: $($_.Exception.Message)`n`nLog: $Log") 'Error'
    exit 1
}
