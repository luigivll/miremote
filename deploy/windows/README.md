# MiRemote en Windows — servicio, agente y autologon

## Visión general

En Windows el agente se divide en dos procesos, porque un servicio corre en una
sesión aislada (sesión 0) y no puede capturar la pantalla ni inyectar entrada
en la sesión interactiva del usuario:

1. **Servicio** (`MiRemoteSvc`, sesión 0): identidad, configuración, conexión
   con la señalización, autorización. Arranca al encender el equipo.
2. **Agente de sesión** (`agent run`, sesión del usuario): captura DXGI,
   codificación, `SendInput`, indicador de bandeja. Lo lanza el servicio en la
   sesión interactiva activa.

Para la primera versión, el arranque del agente en la sesión del usuario se
resuelve con **autologon** (ver más abajo): al encender, Windows inicia sesión
automáticamente en la cuenta elegida y una tarea al iniciar sesión lanza
`agent run`. Es la opción más simple; su contrapartida es que debilita la
seguridad física del equipo (cualquiera con acceso físico encuentra la sesión
abierta). Úsala solo en un equipo en un entorno de confianza.

> Alternativas más seguras que puedes adoptar después, sin cambiar el resto:
> - El servicio lanza el agente con `CreateProcessAsUser` en `winsta0\default`
>   usando el token de la sesión activa (`WTSQueryUserToken`), sin autologon.
> - RDP solo para iniciar sesión tras el encendido.

## 1. Compilar

Requiere Go, y para la captura real, un toolchain C++ (MSVC o MinGW-w64) y
`CGO_ENABLED=1`.

```powershell
# Agente con captura real (DXGI + Media Foundation)
cd agent
$env:CGO_ENABLED=1
go build -ldflags "-H windowsgui" -o MiRemoteAgent.exe ./cmd/agent

# Sin captura (para pruebas de conexión; da error al capturar):
$env:CGO_ENABLED=0
go build -o MiRemoteAgent.exe ./cmd/agent
```

La DLL nativa `winshot` se compila desde `agent/native/winshot` (ver su
README) y debe quedar junto al ejecutable o en el PATH.

## 2. Configurar

```powershell
# Datos en %ProgramData%\MiRemote (requiere permisos de administrador para el servicio)
.\MiRemoteAgent.exe setup -signaling https://TU_SERVIDOR -ws wss://TU_SERVIDOR/v1/ws
.\MiRemoteAgent.exe id        # muestra device_id y huellas
```

## 3. Emparejar

```powershell
.\MiRemoteAgent.exe pair       # muestra un QR; escanéalo con el móvil y confirma
```

## 4. Autologon (opción elegida para el punto "encendido")

Configúralo con Sysinternals Autologon (recomendado, cifra la contraseña en el
registro con LSA) o con el script `autologon.ps1` de esta carpeta.

```powershell
# Opción A: Sysinternals (descarga Autologon.exe de Microsoft)
#   Autologon.exe   -> introduce usuario, dominio y contraseña.

# Opción B: script incluido (menos seguro: ver nota dentro del script)
.\autologon.ps1 -User "MiUsuario" -Domain $env:COMPUTERNAME
```

Después, registra la tarea que lanza el agente al iniciar sesión:

```powershell
.\install-agent-task.ps1
```

## 5. Servicio (identidad y arranque temprano)

```powershell
# Instala el servicio que mantiene la identidad y la conexión aunque no haya
# sesión todavía (p.ej. para recibir el aviso de "listo" tras WoL).
.\install-service.ps1
```

## 6. Wake-on-LAN (firmware + adaptador)

Ver `deploy/wol-setup.md`.
