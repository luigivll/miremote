import 'dart:convert';

import 'package:http/http.dart' as http;

import '../storage/identity_store.dart';

/// Cliente del relevo Wake-on-LAN. Firma un desafío con la identidad del móvil.
class WolClient {
  final String relayUrl; // http(s)://relay-lan:9797
  final IdentityStore identity;

  WolClient({required this.relayUrl, required this.identity});

  Future<void> wake(String targetId) async {
    final nonceResp = await http.get(Uri.parse('$relayUrl/v1/nonce'));
    if (nonceResp.statusCode != 200) {
      throw Exception('relevo no disponible (${nonceResp.statusCode})');
    }
    final nonce = (jsonDecode(nonceResp.body) as Map)['nonce'] as String;
    final sig = await identity.sign(nonce);

    final resp = await http.post(
      Uri.parse('$relayUrl/v1/wake'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'device_id': identity.deviceId,
        'target': targetId,
        'nonce': nonce,
        'signature': sig,
      }),
    );
    if (resp.statusCode != 200) {
      final body = jsonDecode(resp.body) as Map;
      throw Exception('WoL: ${resp.statusCode} ${body['error']}');
    }
  }
}
