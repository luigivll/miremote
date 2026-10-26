import 'dart:convert';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';
import 'package:crypto/crypto.dart' as crypto;
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Gestiona la identidad Ed25519 persistente del móvil.
///
/// La clave privada se guarda en el almacén seguro del SO (Android Keystore /
/// iOS Keychain) a través de flutter_secure_storage. El device_id se deriva de
/// la clave pública, igual que en el agente y el servidor, de modo que las tres
/// partes calculan el mismo identificador.
class IdentityStore {
  static const _kSeed = 'miremote_ed25519_seed';
  static const _kDeviceName = 'miremote_device_name';

  final FlutterSecureStorage _storage;
  final Ed25519 _algo = Ed25519();

  SimpleKeyPair? _keyPair;
  String? _deviceId;
  List<int>? _publicKeyBytes;

  IdentityStore([FlutterSecureStorage? storage])
      : _storage = storage ??
            const FlutterSecureStorage(
              aOptions: AndroidOptions(encryptedSharedPreferences: true),
            );

  /// Carga la identidad existente o la crea la primera vez.
  Future<void> load() async {
    final seedB64 = await _storage.read(key: _kSeed);
    if (seedB64 != null) {
      final seed = base64Decode(seedB64);
      _keyPair = await _algo.newKeyPairFromSeed(seed);
    } else {
      _keyPair = await _algo.newKeyPair();
      final seed = await _keyPair!.extractPrivateKeyBytes();
      await _storage.write(key: _kSeed, value: base64Encode(seed));
    }
    final pub = await _keyPair!.extractPublicKey();
    _publicKeyBytes = pub.bytes;
    _deviceId = _deriveDeviceId(pub.bytes);
  }

  String get deviceId => _deviceId!;

  /// Clave pública en base64 estándar (como la espera el servidor).
  String get publicKeyB64 => base64Encode(_publicKeyBytes!);

  /// Firma un mensaje (p.ej. un nonce) y devuelve la firma en base64 estándar.
  Future<String> sign(String message) async {
    final signature = await _algo.sign(
      utf8.encode(message),
      keyPair: _keyPair!,
    );
    return base64Encode(signature.bytes);
  }

  Future<String> getDeviceName() async {
    return (await _storage.read(key: _kDeviceName)) ?? 'Móvil';
  }

  Future<void> setDeviceName(String name) async {
    await _storage.write(key: _kDeviceName, value: name);
  }

  /// device_id = "m-" + base64url(sha256(pub))[2:16]. Debe coincidir con la
  /// derivación del servidor (deviceIDFromPub con role "mobile").
  static String _deriveDeviceId(List<int> publicKey) {
    final digest = crypto.sha256.convert(publicKey).bytes;
    final b64url = base64Url.encode(digest).replaceAll('=', '');
    return 'm-${b64url.substring(2, 16)}';
  }

  /// Huella sha256:hex de la clave pública.
  String publicKeyFingerprint() {
    final digest = crypto.sha256.convert(_publicKeyBytes!).bytes;
    final hex = digest
        .map((b) => b.toRadixString(16).padLeft(2, '0'))
        .join();
    return 'sha256:$hex';
  }
}
