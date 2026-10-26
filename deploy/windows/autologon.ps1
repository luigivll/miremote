# autologon.ps1 — configura el inicio de sesión automático en Windows.
#
# ADVERTENCIA DE SEGURIDAD:
#   El autologon debilita la seguridad física del equipo: tras encender, la
#   sesión queda abierta sin pedir contraseña. Úsalo solo en un equipo en un
#   entorno de confianza. Este script guarda la contraseña en el registro.
#   Windows la deja en texto recuperable salvo que uses Sysinternals Autologon,
#   que la cifra con el secreto LSA; se recomienda esa herramienta en su lugar.
#
# Uso:
#   .\autologon.ps1 -User "MiUsuario" [-Domain "EQUIPO"]
#
# Pide la contraseña de forma interactiva (no se pasa por línea de comandos).

param(
    [Parameter(Mandatory = $true)][string]$User,
    [string]$Domain = $env:COMPUTERNAME
)

$ErrorActionPreference = "Stop"

if (-not ([Security.Principal.WindowsPrincipal] `
        [Security.Principal.WindowsIdentity]::GetCurrent()
    ).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Ejecuta este script como Administrador."
}

$secure = Read-Host -AsSecureString "Contraseña de $Domain\$User"
$bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
$plain = [Runtime.InteropServices.Marshal]::PtrToStringAuto($bstr)
[Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)

$key = "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon"
Set-ItemProperty $key -Name "AutoAdminLogon" -Value "1"
Set-ItemProperty $key -Name "DefaultUserName" -Value $User
Set-ItemProperty $key -Name "DefaultDomainName" -Value $Domain
Set-ItemProperty $key -Name "DefaultPassword" -Value $plain

# Limpiar la variable en memoria.
Remove-Variable plain

Write-Host "Autologon configurado para $Domain\$User."
Write-Host "RECOMENDACIÓN: sustituye esto por Sysinternals Autologon para que la"
Write-Host "contraseña quede cifrada con LSA en vez de en texto en el registro."
