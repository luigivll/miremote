import 'dart:async';
import 'dart:convert';

import 'package:flutter_webrtc/flutter_webrtc.dart';

import '../api/signaling_api.dart';
import '../session/ws_client.dart';
import '../storage/peer_store.dart';

/// Estado observable de la sesión.
enum SessionState { idle, connecting, connected, failed, closed }

/// RemoteSession gestiona la conexión WebRTC del móvil con un PC.
///
/// Puntos clave:
///  - Declara un transceiver de vídeo RECVONLY antes de crear la oferta, para
///    que el agente pueda enviar su pista (si no, no se negocia m-line de vídeo).
///  - Crea dos DataChannels: 'control' (fiable, ordenado) y 'cursor' (no
///    fiable), coincidiendo con lo que espera el agente.
///  - Verifica la huella DTLS del SDP recibido contra la huella anclada al PC
///    emparejado. Si no coincide, aborta: cifrar el vídeo no basta si alguien
///    sustituye el extremo durante la señalización.
class RemoteSession {
  final Peer peer;
  final SignalingApi api;
  final WsClient ws;
  final PeerStore peerStore;

  RTCPeerConnection? _pc;
  RTCDataChannel? _control;
  RTCDataChannel? _cursor;
  StreamSubscription? _wsSub;

  final _stateController = StreamController<SessionState>.broadcast();
  final _streamController = StreamController<MediaStream>.broadcast();
  final _permsController = StreamController<List<String>>.broadcast();

  List<String> grantedPermissions = [];

  RemoteSession({
    required this.peer,
    required this.api,
    required this.ws,
    required this.peerStore,
  });

  Stream<SessionState> get state => _stateController.stream;
  Stream<MediaStream> get remoteStream => _streamController.stream;
  Stream<List<String>> get permissions => _permsController.stream;

  Future<void> start() async {
    _stateController.add(SessionState.connecting);

    final turn = await api.turn();
    final config = {
      'iceServers': [
        {'urls': 'stun:stun.l.google.com:19302'},
        {
          'urls': turn.urls,
          'username': turn.username,
          'credential': turn.credential,
        },
      ],
      'sdpSemantics': 'unified-plan',
    };

    _pc = await createPeerConnection(config);

    // Recibir la pista de vídeo del PC.
    await _pc!.addTransceiver(
      kind: RTCRtpMediaType.RTCRtpMediaTypeVideo,
      init: RTCRtpTransceiverInit(
          direction: TransceiverDirection.RecvOnly),
    );

    _pc!.onTrack = (RTCTrackEvent event) {
      if (event.streams.isNotEmpty) {
        _streamController.add(event.streams[0]);
      }
    };

    _pc!.onIceCandidate = (RTCIceCandidate c) {
      ws.sendTo('ice', peer.deviceId, {
        'candidate': c.candidate,
        'sdpMid': c.sdpMid,
        'sdpMLineIndex': c.sdpMLineIndex,
      });
    };

    _pc!.onConnectionState = (RTCPeerConnectionState s) {
      switch (s) {
        case RTCPeerConnectionState.RTCPeerConnectionStateConnected:
          _stateController.add(SessionState.connected);
          break;
        case RTCPeerConnectionState.RTCPeerConnectionStateFailed:
          _stateController.add(SessionState.failed);
          close();
          break;
        case RTCPeerConnectionState.RTCPeerConnectionStateClosed:
        case RTCPeerConnectionState.RTCPeerConnectionStateDisconnected:
          _stateController.add(SessionState.closed);
          close();
          break;
        default:
          break;
      }
    };

    // DataChannels. 'control' fiable y ordenado; 'cursor' no fiable.
    _control = await _pc!.createDataChannel(
      'control',
      RTCDataChannelInit()
        ..ordered = true
        ..id = 1,
    );
    _control!.onMessage = _onControlMessage;

    _cursor = await _pc!.createDataChannel(
      'cursor',
      RTCDataChannelInit()
        ..ordered = false
        ..maxRetransmits = 0
        ..id = 2,
    );

    // Escuchar answer/ice del WS.
    _wsSub = ws.events.listen(_onWsEvent);

    // Crear oferta y enviarla.
    final offer = await _pc!.createOffer();
    await _pc!.setLocalDescription(offer);
    ws.sendTo('offer', peer.deviceId, {
      'type': offer.type,
      'sdp': offer.sdp,
    });
  }

  Future<void> _onWsEvent(Envelope env) async {
    switch (env.type) {
      case 'answer':
        final sdp = env.data!['sdp'] as String;

        // Verificar la huella DTLS antes de aceptar la answer.
        final fp = _extractDtlsFingerprint(sdp);
        if (!_verifyFingerprint(fp)) {
          _stateController.add(SessionState.failed);
          await close();
          return;
        }

        await _pc!.setRemoteDescription(
          RTCSessionDescription(sdp, 'answer'),
        );
        break;

      case 'ice':
        final d = env.data!;
        await _pc!.addCandidate(RTCIceCandidate(
          d['candidate'] as String?,
          d['sdpMid'] as String?,
          d['sdpMLineIndex'] as int?,
        ));
        break;
    }
  }

  /// Extrae la huella DTLS (a=fingerprint:sha-256 ...) del SDP.
  String? _extractDtlsFingerprint(String sdp) {
    for (final line in sdp.split(RegExp(r'\r?\n'))) {
      final t = line.trim();
      if (t.toLowerCase().startsWith('a=fingerprint:sha-256')) {
        final parts = t.split(' ');
        if (parts.length >= 2) return parts[1].toUpperCase();
      }
    }
    return null;
  }

  /// Compara la huella con la anclada al PC. Si el PC aún no tiene huella
  /// guardada (primer emparejamiento), la fija (TOFU) y continúa.
  bool _verifyFingerprint(String? fp) {
    if (fp == null) return false;
    final expected = peer.dtlsFingerprint?.toUpperCase();
    if (expected == null || expected.isEmpty) {
      // Primera vez: anclar.
      peer.dtlsFingerprint = fp;
      peerStore.upsert(peer);
      return true;
    }
    return expected == fp;
  }

  void _onControlMessage(RTCDataChannelMessage msg) {
    try {
      final m = jsonDecode(msg.text) as Map<String, dynamic>;
      switch (m['t']) {
        case 'session.permissions':
          grantedPermissions = (m['granted'] as List).cast<String>();
          _permsController.add(grantedPermissions);
          break;
        case 'session.stop':
          close();
          break;
      }
    } catch (_) {}
  }

  // --- Envío de entrada ---

  bool get canControl => grantedPermissions.contains('input.control');

  void sendMouseMove(double x, double y) {
    _sendCursor({'t': 'mouse.move', 'x': x, 'y': y});
  }

  void sendMouseButton(String button, bool down) {
    _sendControl({'t': down ? 'mouse.down' : 'mouse.up', 'button': button});
  }

  void sendWheel(double dx, double dy) {
    _sendControl({'t': 'mouse.wheel', 'dx': dx, 'dy': dy});
  }

  void sendKey(String domCode, bool down) {
    _sendControl({'t': down ? 'key.down' : 'key.up', 'code': domCode});
  }

  void sendText(String text) {
    _sendControl({'t': 'text', 'value': text});
  }

  void requestKeyframe() {
    _sendControl({'t': 'keyframe.request'});
  }

  void _sendControl(Map<String, dynamic> m) {
    if (!canControl && m['t'] != 'keyframe.request') return;
    _control?.send(RTCDataChannelMessage(jsonEncode(m)));
  }

  void _sendCursor(Map<String, dynamic> m) {
    if (!canControl) return;
    _cursor?.send(RTCDataChannelMessage(jsonEncode(m)));
  }

  Future<void> close() async {
    await _wsSub?.cancel();
    try {
      await _control?.close();
      await _cursor?.close();
      await _pc?.close();
    } catch (_) {}
    if (!_stateController.isClosed) {
      _stateController.add(SessionState.closed);
    }
  }

  void dispose() {
    _stateController.close();
    _streamController.close();
    _permsController.close();
  }
}
