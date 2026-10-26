# MiRemote — Protocolo de mensajes (v1)

Este documento es la fuente de verdad del protocolo. Las implementaciones en
Go (`signaling`, `agent`, `wol-relay`) y Dart (`remote_mobile`) deben coincidir
con lo que aquí se describe. Todos los mensajes son JSON sobre WSS o HTTPS.

## 0. Conceptos

Se separan tres credenciales distintas, nunca se mezclan:

| Concepto            | Vida           | Dónde vive                                  |
|---------------------|----------------|---------------------------------------------|
| Identidad de dispositivo | Permanente | Clave privada Ed25519 en el keystore del SO |
| Invitación (QR)     | ~120 s, 1 uso  | Secreto de 32 bytes, solo en el QR          |
| Token de sesión     | ~5 min         | JWT firmado por el servidor de señalización |

El QR **no** contiene un token permanente. Es un secreto de arranque de un
solo uso que se canjea por HTTPS y se destruye.

## 1. Identidad

Cada dispositivo (PC y cada móvil) genera un par de claves Ed25519 la primera
vez que arranca. La clave privada nunca sale del dispositivo. La clave pública
y su huella identifican al dispositivo.

```
device_id      = base64url( SHA256( ed25519_public_key ) )[:16]   (texto)
fingerprint    = "sha256:" + hex( SHA256( ed25519_public_key ) )
```

## 2. HTTP API (señalización)

Base: `https://<signaling_host>`

### 2.1 Crear invitación — `POST /v1/pairing/invitations`
Autenticado con la identidad del PC (cabecera `X-Device-Auth`, ver §5).

Respuesta:
```json
{
  "pairing_id": "b6f1...",
  "secret": "base64url-32-bytes",        // SOLO se devuelve aquí, para el QR
  "expires_at": "2026-10-25T21:02:00Z",
  "poll_token": "opaque"                  // el PC lo usa para sondear el estado
}
```
El servidor guarda únicamente `SHA256(secret)`, nunca el secreto.

### 2.2 Canjear invitación — `POST /v1/pairing/claim`
Lo llama el **móvil** tras escanear el QR. No autenticado (el secreto es la prueba).

Petición:
```json
{
  "pairing_id": "b6f1...",
  "secret": "base64url-32-bytes",
  "device": {
    "device_id": "m-9c2...",
    "public_key": "base64-ed25519-pub",
    "name": "Pixel de Luis",
    "platform": "android"
  }
}
```
El servidor valida con comparación en tiempo constante, caducidad y consumo
atómico (una sola reclamación gana). Si todo es correcto marca la invitación
como `claimed` y guarda la identidad del móvil como **pendiente de aprobación**.

Respuesta:
```json
{ "status": "pending_host_approval", "host_fingerprint": "sha256:..." }
```

### 2.3 Sondeo del PC — `GET /v1/pairing/invitations/{pairing_id}?poll_token=...`
El PC sondea hasta ver `claimed`, muestra al usuario el nombre y la huella del
móvil, y aprueba o rechaza.

### 2.4 Aprobar / rechazar — `POST /v1/pairing/invitations/{pairing_id}/decision`
Autenticado con la identidad del PC.
```json
{ "decision": "approve", "permissions": ["screen.view", "input.control"] }
```
Al aprobar, el servidor registra el vínculo permanente (device_id del móvil ↔
device_id del PC) con los permisos concedidos.

### 2.5 Credenciales TURN — `POST /v1/turn/credentials`
Autenticado. Devuelve credenciales temporales HMAC (coturn `use-auth-secret`):
```json
{
  "username": "1761421320:m-9c2...",
  "credential": "base64-hmac-sha1",
  "ttl": 600,
  "urls": ["turn:turn.example.com:3478?transport=udp",
           "turns:turn.example.com:5349?transport=tcp"]
}
```
`username = <expiry_unix>:<device_id>`, `credential = base64(HMAC-SHA1(secret, username))`.

## 3. WebSocket de señalización — `wss://<host>/v1/ws`

Tanto el PC como el móvil mantienen una conexión WSS saliente. El primer
mensaje tras conectar es `hello`, firmado (ver §5). El servidor relaciona ambos
extremos por sus vínculos aprobados y reenvía SDP/ICE sin inspeccionar el medio.

Sobre común:
```json
{ "type": "<tipo>", "to": "<device_id destino>", "id": "<uuid msg>", "data": { ... } }
```

Tipos:
| type           | dirección        | data                                           |
|----------------|------------------|------------------------------------------------|
| `hello`        | cliente→servidor | identidad + firma del desafío                  |
| `welcome`      | servidor→cliente | lista de pares vinculados y en línea           |
| `peer-online`  | servidor→cliente | `{ "device_id": ... }`                         |
| `peer-offline` | servidor→cliente | `{ "device_id": ... }`                         |
| `offer`        | móvil→PC         | `{ "sdp": ... }` (reenviado)                   |
| `answer`       | PC→móvil         | `{ "sdp": ... }` (reenviado)                   |
| `ice`          | ambos            | `{ "candidate": ... }` (reenviado)             |
| `session-start`| móvil→PC         | `{ "session_token": "jwt" }`                   |
| `error`        | servidor→cliente | `{ "code": ..., "message": ... }`              |

El servidor solo reenvía `offer`/`answer`/`ice` entre dos dispositivos con un
vínculo aprobado y ambos en línea. Cualquier otro destino se rechaza.

## 4. DataChannels WebRTC (PC ↔ móvil, extremo a extremo)

Dos canales:

- `control` — fiable y ordenado. Teclado, botones de ratón, comandos, control de sesión.
- `cursor`  — no fiable, no ordenado (`maxRetransmits: 0`). Solo movimiento de puntero.

Eventos de entrada (canal según el tipo):

```json
{ "t": "mouse.move",  "x": 0.4213, "y": 0.5120 }                 // cursor
{ "t": "mouse.down",  "button": "left" }                         // control
{ "t": "mouse.up",    "button": "left" }                         // control
{ "t": "mouse.wheel", "dx": 0, "dy": -120 }                      // control
{ "t": "key.down",    "code": "KeyA" }                           // control (tecla física)
{ "t": "key.up",      "code": "KeyA" }                           // control
{ "t": "text",        "value": "hola €" }                        // control (Unicode)
{ "t": "cmd",         "action": "application.open",
                      "arguments": { "application_id": "browser" } } // control
```

`x`,`y` son coordenadas normalizadas 0..1 sobre el escritorio virtual completo.

Control de sesión sobre `control`:
```json
{ "t": "session.permissions", "granted": ["screen.view","input.control"] }  // PC→móvil
{ "t": "session.stop", "reason": "user_local" }                             // PC→móvil
{ "t": "keyframe.request" }                                                 // móvil→PC
```

## 5. Autenticación por desafío firmado

Para cualquier acción autenticada (HTTP `X-Device-Auth` o `hello` en WS):

1. El cliente obtiene/recibe un `nonce` del servidor (`GET /v1/auth/nonce`).
2. Firma `nonce` con su clave Ed25519.
3. Envía `device_id`, `nonce`, `signature` (base64).
4. El servidor verifica la firma contra la clave pública registrada del dispositivo
   y que el `nonce` no se haya usado y no haya caducado (~30 s).

Cabecera HTTP:
```
X-Device-Auth: v1 <device_id> <nonce> <signature-base64>
```

## 6. Token de sesión (JWT)

Emitido por `POST /v1/session/token` (autenticado). HS256 con secreto del
servidor. Claims:
```json
{
  "sub": "<device_id móvil>",
  "aud": "<device_id PC>",
  "sid": "<uuid sesión>",
  "perms": ["screen.view","input.control"],
  "exp": 1761421620,
  "iat": 1761421320
}
```
El PC valida el JWT en `session-start` antes de responder a la oferta WebRTC:
firma, `aud` == su propio device_id, `sub` vinculado y aprobado, no caducado.
