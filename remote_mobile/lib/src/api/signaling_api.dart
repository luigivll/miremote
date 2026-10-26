import 'dart:convert';

import 'package:http/http.dart' as http;

import '../storage/identity_store.dart';

/// Cliente HTTPS de la señalización, del lado del móvil.
class SignalingApi {
  final String baseUrl; // https://host
  final IdentityStore identity;

  SignalingApi({required this.baseUrl, required this.identity});

  Uri _u(String path) => Uri.parse('$baseUrl$path');

  Future<String> _getNonce() async {
    final resp = await http.get(_u('/v1/auth/nonce'));
    if (resp.statusCode != 200) {
      throw Exception('nonce: ${resp.statusCode}');
    }
    return (jsonDecode(resp.body) as Map)['nonce'] as String;
  }

  /// Nonce fresco, para el hello del WebSocket.
  Future<String> freshNonce() => _getNonce();

  /// Construye la cabecera X-Device-Auth firmando un nonce fresco.
  Future<String> _authHeader() async {
    final nonce = await _getNonce();
    final sig = await identity.sign(nonce);
    return 'v1 ${identity.deviceId} $nonce $sig';
  }

  /// Reclama una invitación con el secreto del QR. No requiere autenticación
  /// (el secreto es la prueba), pero envía la identidad del móvil.
  Future<ClaimResult> claim({
    required String pairingId,
    required String secret,
    required String name,
    required String platform,
  }) async {
    final resp = await http.post(
      _u('/v1/pairing/claim'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'pairing_id': pairingId,
        'secret': secret,
        'device': {
          'device_id': identity.deviceId,
          'public_key': identity.publicKeyB64,
          'name': name,
          'platform': platform,
        },
      }),
    );
    final body = jsonDecode(resp.body) as Map<String, dynamic>;
    if (resp.statusCode != 200) {
      throw Exception('claim: ${resp.statusCode} ${body['error']}');
    }
    return ClaimResult(
      status: body['status'] as String,
      hostFingerprint: body['host_fingerprint'] as String? ?? '',
    );
  }

  /// Pide un token de sesión para conectar con un PC concreto.
  Future<SessionToken> sessionToken(String targetDeviceId) async {
    final resp = await http.post(
      _u('/v1/session/token'),
      headers: {
        'Content-Type': 'application/json',
        'X-Device-Auth': await _authHeader(),
      },
      body: jsonEncode({'target_device_id': targetDeviceId}),
    );
    final body = jsonDecode(resp.body) as Map<String, dynamic>;
    if (resp.statusCode != 200) {
      throw Exception('session token: ${resp.statusCode} ${body['error']}');
    }
    return SessionToken(
      token: body['session_token'] as String,
      sessionId: body['session_id'] as String,
      permissions: (body['permissions'] as List).cast<String>(),
    );
  }

  /// Obtiene credenciales TURN temporales.
  Future<TurnCredentials> turn() async {
    final resp = await http.post(
      _u('/v1/turn/credentials'),
      headers: {
        'Content-Type': 'application/json',
        'X-Device-Auth': await _authHeader(),
      },
      body: '{}',
    );
    final body = jsonDecode(resp.body) as Map<String, dynamic>;
    if (resp.statusCode != 200) {
      throw Exception('turn: ${resp.statusCode}');
    }
    return TurnCredentials(
      username: body['username'] as String,
      credential: body['credential'] as String,
      urls: (body['urls'] as List).cast<String>(),
    );
  }
}

class ClaimResult {
  final String status;
  final String hostFingerprint;
  ClaimResult({required this.status, required this.hostFingerprint});
}

class SessionToken {
  final String token;
  final String sessionId;
  final List<String> permissions;
  SessionToken({
    required this.token,
    required this.sessionId,
    required this.permissions,
  });
}

class TurnCredentials {
  final String username;
  final String credential;
  final List<String> urls;
  TurnCredentials({
    required this.username,
    required this.credential,
    required this.urls,
  });
}
