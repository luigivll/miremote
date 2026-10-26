import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../api/signaling_api.dart';
import '../pairing/qr_payload.dart';
import '../storage/identity_store.dart';
import '../storage/peer_store.dart';

/// Escanea el QR del PC, reclama la invitación y registra el PC como par.
class PairView extends StatefulWidget {
  final IdentityStore identity;
  final PeerStore peerStore;

  const PairView({super.key, required this.identity, required this.peerStore});

  @override
  State<PairView> createState() => _PairViewState();
}

class _PairViewState extends State<PairView> {
  bool _busy = false;
  String? _status;

  Future<void> _onDetect(BarcodeCapture capture) async {
    if (_busy) return;
    if (capture.barcodes.isEmpty) return;
    final raw = capture.barcodes.first.rawValue;
    if (raw == null) return;

    QrPayload payload;
    try {
      payload = QrPayload.parse(raw);
    } catch (_) {
      return; // no es un QR de MiRemote
    }

    setState(() {
      _busy = true;
      _status = 'Reclamando invitación…';
    });

    try {
      if (payload.expired) {
        throw Exception('El código QR ha caducado. Genera uno nuevo en el PC.');
      }

      final api = SignalingApi(baseUrl: payload.server, identity: widget.identity);
      final name = await widget.identity.getDeviceName();
      final result = await api.claim(
        pairingId: payload.pairingId,
        secret: payload.secret,
        name: name,
        platform: 'android',
      );

      if (result.status != 'pending_host_approval') {
        throw Exception('Respuesta inesperada: ${result.status}');
      }

      // Registrar el PC como par. El device_id del host viene en el QR; la
      // huella DTLS se anclará (TOFU) en la primera conexión exitosa.
      await widget.peerStore.upsert(Peer(
        deviceId: payload.hostDeviceId,
        name: 'PC ${payload.hostDeviceId}',
        server: payload.server,
        hostFingerprint: payload.hostFingerprint,
      ));

      setState(() {
        _status =
            'Solicitud enviada.\nAprueba este dispositivo en el PC para terminar.';
      });

      if (mounted) {
        Navigator.of(context).pop(PairResult(
          server: payload.server,
          hostFingerprint: payload.hostFingerprint,
        ));
      }
    } catch (e) {
      setState(() {
        _busy = false;
        _status = 'Error: $e';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Escanear QR del PC')),
      body: Column(
        children: [
          Expanded(
            child: MobileScanner(onDetect: _onDetect),
          ),
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(
              _status ?? 'Apunta la cámara al código QR mostrado en el PC.',
              textAlign: TextAlign.center,
            ),
          ),
          if (_busy) const Padding(
            padding: EdgeInsets.only(bottom: 16),
            child: CircularProgressIndicator(),
          ),
        ],
      ),
    );
  }
}

class PairResult {
  final String server;
  final String hostFingerprint;
  PairResult({required this.server, required this.hostFingerprint});
}
