# Removes the Smart Parking device-agent from Windows (run as Administrator).
# The queue and log in C:\ProgramData\parking-agent are kept; delete that
# folder by hand if they are no longer needed.

$ErrorActionPreference = 'Stop'
$TaskName = 'ParkingAgent'
$RuleName = 'Parking Agent (cameras, TCP 8090)'
$Dst = Join-Path $env:ProgramFiles 'ParkingAgent'

$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) {
    Write-Host "Administrator huquqi kerak." -ForegroundColor Red
    exit 1
}

if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}
Get-Process parking-agent -ErrorAction SilentlyContinue | Stop-Process -Force
Get-NetFirewallRule -DisplayName $RuleName -ErrorAction SilentlyContinue | Remove-NetFirewallRule
if (Test-Path $Dst) { Remove-Item $Dst -Recurse -Force }

Write-Host "Agent o'chirildi."
