import 'dart:convert';

/// Contenido del QR de emparejamiento (shared/proto §2).
class QrPayload {
  final int version;
  final String server; // https://host
  final String hostDeviceId; // p-...
  final String pairingId;
  final String secret;
  final String hostFingerprint;
  final DateTime expiresAt;

  QrPayload({
    required this.version,
    required this.server,
    required this.hostDeviceId,
    required this.pairingId,
    required this.secret,
    required this.hostFingerprint,
    required this.expiresAt,
  });

  factory QrPayload.parse(String raw) {
    final m = jsonDecode(raw) as Map<String, dynamic>;
    return QrPayload(
      version: m['version'] as int,
      server: m['server'] as String,
      hostDeviceId: m['host_device_id'] as String? ?? '',
      pairingId: m['pairing_id'] as String,
      secret: m['secret'] as String,
      hostFingerprint: m['host_key_fingerprint'] as String? ?? '',
      expiresAt: DateTime.parse(m['expires_at'] as String),
    );
  }

  bool get expired => DateTime.now().toUtc().isAfter(expiresAt.toUtc());
}
