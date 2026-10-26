import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Un PC vinculado, con la huella DTLS esperada para anclar la verificación.
class Peer {
  final String deviceId; // p-...
  final String name;
  final String server; // https://host
  final String hostFingerprint; // sha256:... de la clave pública del host

  /// Huella DTLS esperada del PC. Se fija tras la primera conexión exitosa
  /// (TOFU) o puede venir del QR; en adelante toda sesión debe presentar esta.
  String? dtlsFingerprint;

  Peer({
    required this.deviceId,
    required this.name,
    required this.server,
    required this.hostFingerprint,
    this.dtlsFingerprint,
  });

  Map<String, dynamic> toJson() => {
        'device_id': deviceId,
        'name': name,
        'server': server,
        'host_fingerprint': hostFingerprint,
        'dtls_fingerprint': dtlsFingerprint,
      };

  factory Peer.fromJson(Map<String, dynamic> m) => Peer(
        deviceId: m['device_id'] as String,
        name: m['name'] as String,
        server: m['server'] as String,
        hostFingerprint: m['host_fingerprint'] as String? ?? '',
        dtlsFingerprint: m['dtls_fingerprint'] as String?,
      );
}

class PeerStore {
  static const _kPeers = 'miremote_peers';
  final FlutterSecureStorage _storage;

  PeerStore([FlutterSecureStorage? storage])
      : _storage = storage ??
            const FlutterSecureStorage(
              aOptions: AndroidOptions(encryptedSharedPreferences: true),
            );

  Future<List<Peer>> list() async {
    final raw = await _storage.read(key: _kPeers);
    if (raw == null) return [];
    final arr = jsonDecode(raw) as List;
    return arr
        .map((e) => Peer.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<void> upsert(Peer peer) async {
    final peers = await list();
    final idx = peers.indexWhere((p) => p.deviceId == peer.deviceId);
    if (idx >= 0) {
      peers[idx] = peer;
    } else {
      peers.add(peer);
    }
    await _save(peers);
  }

  Future<void> remove(String deviceId) async {
    final peers = await list();
    peers.removeWhere((p) => p.deviceId == deviceId);
    await _save(peers);
  }

  Future<void> _save(List<Peer> peers) async {
    await _storage.write(
      key: _kPeers,
      value: jsonEncode(peers.map((p) => p.toJson()).toList()),
    );
  }
}
