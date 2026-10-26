# install-agent-task.ps1 — registra una tarea programada que lanza el agente de
# sesión (captura + entrada + indicador) al iniciar sesión el usuario.
#
# Combinado con autologon, esto arranca el agente automáticamente tras encender.

param(
    [string]$ExePath = "$PSScriptRoot\MiRemoteAgent.exe"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $ExePath)) {
    throw "No se encuentra el ejecutable: $ExePath"
}

$action = New-ScheduledTaskAction -Execute $ExePath -Argument "run"
# Al iniciar sesión el usuario actual.
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -StartWhenAvailable `
    -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)

Register-ScheduledTask -TaskName "MiRemoteAgent" `
    -Action $action -Trigger $trigger -Settings $settings `
    -Description "Agente de sesión MiRemote (captura e indicador)" `
    -Force

Write-Host "Tarea 'MiRemoteAgent' registrada. Se lanzará al iniciar sesión."
