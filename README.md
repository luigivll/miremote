# MiRemote

Control remoto de PC multiplataforma: encender el equipo (WoL), ver la pantalla
en tiempo real y controlarlo desde el móvil, con emparejamiento por QR y de
forma **transparente** (siempre hay un indicador visible de sesión activa, con
botón de Detener).

Stack: **Flutter** (móvil) · **Go + Pion WebRTC** (PC y servidores) ·
**C++ DXGI + Media Foundation** (captura/codificación Windows) · **coturn** (TURN).

## Componentes

```
miremote/
├── signaling/      Servidor de señalización (Go): QR, WSS, TURN HMAC, JWT
├── agent/          Agente de PC (Go + Pion): captura→WebRTC, SendInput, bandeja
│   └── native/winshot/   DLL C++ de captura DXGI + codificación H.264
├── wol-relay/      Relevo Wake-on-LAN (Go) para la LAN
├── remote_mobile/  Cliente móvil (Flutter)
├── shared/proto/   Protocolo (fuente de verdad de los mensajes)
└── deploy/         coturn, systemd, Windows (autologon, servicio), WoL
```

## Arquitectura

```
  Móvil (Flutter)                    Servidores                   PC (agente Go)
  ───────────────                    ──────────                   ──────────────
  escaneo QR ──HTTPS: claim────────► Señalización ◄───WSS: hello── Servicio/Agente
  identidad Ed25519                  (QR atómico,                   identidad Ed25519
  almacén seguro                      JWT, TURN HMAC)               cert DTLS persistente
       │                                  ▲                              │
       │  WSS: offer/ice ─────────────────┘  reenvía solo entre          │
       │                                     vínculos aprobados          │
       └──────── WebRTC (DTLS-SRTP, directo o vía TURN) ─────────────────┘
                 vídeo H.264 ◄── pista        DataChannels control/cursor ──► SendInput
                 verifica huella DTLS                                         indicador bandeja

  Encendido:  Móvil ──HTTPS firmado──► Relevo WoL (LAN) ──paquete mágico──► PC
```

Puntos de diseño que resuelve esta implementación (más allá del esqueleto):

- **Huella DTLS estable**: el PC reutiliza un certificado DTLS persistente
  (`CertificateFromX509`, no `NewCertificate`), para que el móvil pueda anclar la
  huella a la identidad emparejada. Hay un test que garantiza que no cambia
  entre cargas.
- **Canje de QR atómico**: una sola reclamación gana aunque lleguen 20 a la vez
  (test de concurrencia incluido).
- **TURN sin cuentas fijas**: credenciales HMAC temporales (coturn
  `use-auth-secret`).
- **SendInput correcto**: coordenadas absolutas 0..65535 con
  `MOUSEEVENTF_VIRTUALDESK` (multi-monitor), texto Unicode con
  `KEYEVENTF_UNICODE`, y `ReleaseAll` que suelta todo lo pulsado al desconectar.
- **Unión captura→Pion**: pista `TrackLocalStaticSample` H.264, `WriteSample`
  por fotograma, RTCP PLI/FIR → `RequestKeyframe`, REMB → `SetBitrate` (test
  WebRTC de extremo a extremo incluido).
- **Transparencia**: sin indicador visible no se habilita el control de entrada.

## Puesta en marcha (orden recomendado)

1. **Servidores** (un VPS con dominio y TLS):
   - coturn con `deploy/turnserver.conf` (`static-auth-secret`).
   - Señalización con `deploy/systemd/miremote-signaling.service`
     (`MR_TURN_SECRET` = el `static-auth-secret` de coturn).
2. **PC** (Windows): ver `deploy/windows/README.md`.
   - `agent setup` → `agent pair` (escanea el QR con el móvil) → `agent run`.
   - Autologon + tarea al iniciar sesión para que el agente arranque solo.
3. **Móvil**: `remote_mobile/README.md` (`flutter create .` para los
   andamiajes, luego `flutter run`).
4. **WoL**: relevo en un equipo siempre encendido de la LAN
   (`deploy/wol-setup.md`).

## Compilar

```bash
./build.sh          # signaling, wol-relay, agent (Go) → bin/
# Agente Windows con captura real: CGO_ENABLED=1 + DLL winshot (ver su README)
# Móvil: cd remote_mobile && flutter create . && flutter build apk
```

## Tests

```bash
cd signaling && go test ./...   # flujo de emparejamiento, atomicidad, nonce
cd agent     && go test ./...   # identidad persistente, sesión WebRTC e2e
cd wol-relay && go test ./...   # paquete mágico
```

## Estado de cada pieza

| Componente              | Estado                                                        |
|-------------------------|---------------------------------------------------------------|
| Señalización            | Completo y probado (unit + e2e con binarios reales).          |
| Relevo WoL              | Completo y probado.                                           |
| Agente (lógica Go)      | Completo: identidad, WebRTC, entrada, indicador, emparejamiento. Probado en Linux (stubs) y compila cruzado a Windows. |
| Captura nativa winshot  | Contrato C + puente cgo completos; el cableado del encoder MF está señalado en el .cpp y requiere Windows+GPU para terminarse. |
| Indicador bandeja Win   | Implementado (Shell_NotifyIcon); requiere Windows para probar.|
| Cliente móvil Flutter   | Código completo; requiere `flutter create .` y un entorno con Flutter para compilar (no disponible en este entorno). |

## Seguridad (resumen)

- Identidad por dispositivo (Ed25519); la clave privada nunca se transmite.
- Toda acción autenticada firma un nonce fresco (anti-repetición).
- QR = secreto de arranque de un solo uso, ~120 s; el servidor guarda solo su hash.
- Tokens de sesión JWT cortos con audiencia, sesión y permisos.
- WebRTC: DTLS-SRTP; se verifica la huella DTLS contra la identidad emparejada.
- Copias de recuperación: AES-256-GCM con clave independiente (ver protocolo).
- WoL autenticado en el relevo; el puerto UDP 9 nunca se expone a Internet.

Ver `shared/proto/protocol.md` para el detalle del protocolo.
