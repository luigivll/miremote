# install-service.ps1 — instala el servicio de Windows que mantiene identidad y
# conexión de MiRemote desde el arranque (sesión 0).
#
# Nota: el binario actual implementa el agente de sesión (`run`). Para un
# servicio de sesión 0 propiamente dicho, compila el mismo binario e invócalo
# con "run" bajo SYSTEM; la captura/entrada reales ocurren en el agente de la
# sesión interactiva (ver install-agent-task.ps1). Este servicio es útil para
# arranque temprano y para recibir el aviso de "listo" tras WoL.

param(
    [string]$ExePath = "$PSScriptRoot\MiRemoteAgent.exe",
    [string]$ServiceName = "MiRemoteSvc"
)

$ErrorActionPreference = "Stop"

if (-not ([Security.Principal.WindowsPrincipal] `
        [Security.Principal.WindowsIdentity]::GetCurrent()
    ).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Ejecuta como Administrador."
}

# ProgramData para datos compartidos del servicio.
$dataDir = "$env:ProgramData\MiRemote"
New-Item -ItemType Directory -Force -Path $dataDir | Out-Null

# sc.exe crea el servicio. binPath debe tener comillas internas escapadas.
$bin = "`"$ExePath`" run"
sc.exe create $ServiceName binPath= $bin start= auto obj= LocalSystem DisplayName= "MiRemote Service" | Out-Null
sc.exe description $ServiceName "Mantiene la identidad y la conexion de MiRemote" | Out-Null

Write-Host "Servicio '$ServiceName' instalado. Inícialo con: sc.exe start $ServiceName"
Write-Host "Datos en: $dataDir (protegidos con DPAPI de maquina)"
