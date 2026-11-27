# Installs the Smart Parking device-agent on Windows.
# Run from the unzipped folder, as Administrator:
#   right-click install.cmd -> "Run as administrator"
# It copies the agent to C:\Program Files\ParkingAgent, opens TCP 8090 for
# cameras in Windows Firewall and registers a task that starts the agent at
# boot (as SYSTEM, restarted if it stops).

$ErrorActionPreference = 'Stop'
$TaskName = 'ParkingAgent'
$RuleName = 'Parking Agent (cameras, TCP 8090)'
$Src = $PSScriptRoot
$Dst = Join-Path $env:ProgramFiles 'ParkingAgent'
$Exe = Join-Path $Dst 'parking-agent.exe'
$Env = Join-Path $Dst 'agent.env'

$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) {
    Write-Host "Administrator huquqi kerak: install.cmd ni o'ng tugma -> 'Run as administrator' bilan oching." -ForegroundColor Red
    exit 1
}

# Stop a running copy before replacing the exe
if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2
}
Get-Process parking-agent -ErrorAction SilentlyContinue | Stop-Process -Force

New-Item -ItemType Directory -Force -Path $Dst | Out-Null
Copy-Item (Join-Path $Src 'parking-agent.exe') $Exe -Force
Copy-Item (Join-Path $Src 'agent.env.example') (Join-Path $Dst 'agent.env.example') -Force

if (-not (Test-Path $Env)) {
    if (Test-Path (Join-Path $Src 'agent.env')) {
        Copy-Item (Join-Path $Src 'agent.env') $Env
    } else {
        Copy-Item (Join-Path $Src 'agent.env.example') $Env
    }
}

# agent.env holds the token: only Administrators and SYSTEM may read it
icacls $Env /inheritance:r /grant:r '*S-1-5-32-544:F' '*S-1-5-18:F' | Out-Null

if (Select-String -Path $Env -Pattern '^AGENT_TOKEN=pa_xxxx' -Quiet) {
    Write-Host ""
    Write-Host "agent.env hali to'ldirilmagan. Notepad'da AGENT_TOKEN ni (admin paneldagi tokenni) yozing," -ForegroundColor Yellow
    Write-Host "saqlang va install.cmd ni qayta ishga tushiring:" -ForegroundColor Yellow
    Write-Host "  $Env"
    Start-Process notepad.exe $Env
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
Write-Host "Agent o'rnatildi va ishga tushdi. Tekshirilmoqda..."
$ok = $false
for ($i = 0; $i -lt 15; $i++) {
    Start-Sleep -Seconds 2
    try {
        $h = Invoke-RestMethod -Uri 'http://127.0.0.1:8090/health' -TimeoutSec 3
        if ($h.backend.connected) { $ok = $true; break }
    } catch { }
}
$log = Join-Path $env:ProgramData 'parking-agent\agent.log'
if ($ok) {
    Write-Host "Backendga ulandi. Holat: http://127.0.0.1:8090/health" -ForegroundColor Green
} else {
    Write-Host "Agent ishlayapti, lekin backendga hali ulanmadi. Logni ko'ring: $log" -ForegroundColor Yellow
}
Write-Host "Log: $log"
