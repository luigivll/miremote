import 'dart:async';
import 'dart:convert';

import 'package:web_socket_channel/web_socket_channel.dart';

import '../storage/identity_store.dart';
import '../api/signaling_api.dart';

/// Sobre del protocolo WS (shared/proto §3).
class Envelope {
  final String type;
  final String? to;
  final String? from;
  final Map<String, dynamic>? data;

  Envelope({required this.type, this.to, this.from, this.data});

  Map<String, dynamic> toJson() => {
        'type': type,
        if (to != null) 'to': to,
        if (data != null) 'data': data,
      };

  factory Envelope.fromJson(Map<String, dynamic> m) => Envelope(
        type: m['type'] as String,
        from: m['from'] as String?,
        data: (m['data'] as Map?)?.cast<String, dynamic>(),
      );
}

/// Cliente WebSocket de señalización del móvil.
class WsClient {
  final String wsUrl; // wss://host/v1/ws
  final IdentityStore identity;
  final SignalingApi api;

  WebSocketChannel? _ch;
  final _controller = StreamController<Envelope>.broadcast();

  WsClient({
    required this.wsUrl,
    required this.identity,
    required this.api,
  });

  Stream<Envelope> get events => _controller.stream;

  Future<void> connect() async {
    _ch = WebSocketChannel.connect(Uri.parse(wsUrl));
    await _ch!.ready;

    // Registrar el listener ANTES de enviar hello para no perder el welcome.
    _ch!.stream.listen(
      (raw) {
        try {
          final env = Envelope.fromJson(
              jsonDecode(raw as String) as Map<String, dynamic>);
          _controller.add(env);
        } catch (_) {}
      },
      onDone: () => _controller.close(),
      onError: (e) => _controller.addError(e),
    );

    // hello firmado. El nonce se obtiene de la API HTTP.
    final nonce = await api.freshNonce();
    final sig = await identity.sign(nonce);
    _send(Envelope(type: 'hello', data: {
      'device_id': identity.deviceId,
      'nonce': nonce,
      'signature': sig,
    }));
  }

  void sendTo(String type, String to, Map<String, dynamic> data) {
    _send(Envelope(type: type, to: to, data: data));
  }

  void _send(Envelope env) {
    _ch?.sink.add(jsonEncode(env.toJson()));
  }

  Future<void> close() async {
    await _ch?.sink.close();
  }
}
