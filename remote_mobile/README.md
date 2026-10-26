# remote_mobile — Cliente móvil MiRemote (Flutter)

Cliente que empareja con el PC por QR, se conecta por WebRTC y controla el
escritorio remoto.

## Generar los andamiajes de plataforma

Este repo incluye `lib/`, `pubspec.yaml` y el `AndroidManifest.xml` con los
permisos. Los directorios de plataforma completos (gradle, runner iOS, etc.) se
generan con:

```bash
cd remote_mobile
flutter create .        # conserva lib/ y pubspec.yaml, crea android/ ios/ etc.
flutter pub get
```

Tras `flutter create .`, vuelve a aplicar el `AndroidManifest.xml` de este repo
(añade los permisos de cámara e internet) y, en iOS, añade a
`ios/Runner/Info.plist`:

```xml
<key>NSCameraUsageDescription</key>
<string>Para escanear el código QR de emparejamiento con el PC.</string>
<key>NSLocalNetworkUsageDescription</key>
<string>Para conectarse al PC en la red local por WebRTC.</string>
<key>NSBonjourServices</key>
<array>
  <string>_miremote._tcp</string>
</array>
```

Para flutter_webrtc en iOS, añade también en `Info.plist`
`NSMicrophoneUsageDescription` aunque no se use micrófono (el plugin lo exige):

```xml
<key>NSMicrophoneUsageDescription</key>
<string>No se utiliza; requerido por el componente WebRTC.</string>
```

Y sube la versión mínima de iOS a 13 en `ios/Podfile` (`platform :ios, '13.0'`).

## Ejecutar

```bash
flutter run            # dispositivo o emulador conectado
```

## Arquitectura (lib/src)

- `storage/identity_store.dart` — identidad Ed25519 en Keystore/Keychain.
- `storage/peer_store.dart` — PCs vinculados y su huella DTLS anclada.
- `api/signaling_api.dart` — HTTP: claim, token de sesión, TURN.
- `api/wol_client.dart` — relevo Wake-on-LAN.
- `session/ws_client.dart` — WebSocket de señalización (hello firmado).
- `webrtc/remote_session.dart` — PeerConnection, verificación de huella DTLS,
  DataChannels de control/cursor, envío de entrada.
- `ui/pair_view.dart` — escaneo del QR y canje.
- `ui/remote_view.dart` — vídeo + gestos/teclado → eventos de entrada.

## Seguridad

- La clave privada nunca sale del dispositivo (almacén seguro del SO).
- Cada acción autenticada firma un nonce fresco (desafío-respuesta).
- La huella DTLS del PC se ancla en el primer emparejamiento (TOFU) y se
  verifica en cada conexión: si no coincide, la sesión se aborta.
