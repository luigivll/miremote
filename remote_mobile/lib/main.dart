import 'package:flutter/material.dart';

import 'src/api/signaling_api.dart';
import 'src/session/ws_client.dart';
import 'src/storage/identity_store.dart';
import 'src/storage/peer_store.dart';
import 'src/ui/pair_view.dart';
import 'src/ui/remote_view.dart';
import 'src/webrtc/remote_session.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final identity = IdentityStore();
  await identity.load();
  runApp(MiRemoteApp(identity: identity, peerStore: PeerStore()));
}

class MiRemoteApp extends StatelessWidget {
  final IdentityStore identity;
  final PeerStore peerStore;

  const MiRemoteApp({
    super.key,
    required this.identity,
    required this.peerStore,
  });

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'MiRemote',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        brightness: Brightness.dark,
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFF6C5CE7),
          brightness: Brightness.dark,
        ),
        scaffoldBackgroundColor: const Color(0xFF0E0F13),
        useMaterial3: true,
      ),
      home: HomeView(identity: identity, peerStore: peerStore),
    );
  }
}

class HomeView extends StatefulWidget {
  final IdentityStore identity;
  final PeerStore peerStore;

  const HomeView({
    super.key,
    required this.identity,
    required this.peerStore,
  });

  @override
  State<HomeView> createState() => _HomeViewState();
}

class _HomeViewState extends State<HomeView> {
  List<Peer> _peers = [];
  String _deviceId = '';

  @override
  void initState() {
    super.initState();
    _deviceId = widget.identity.deviceId;
    _reload();
  }

  Future<void> _reload() async {
    final peers = await widget.peerStore.list();
    if (mounted) setState(() => _peers = peers);
  }

  Future<void> _startPairing() async {
    final result = await Navigator.of(context).push<PairResult>(
      MaterialPageRoute(
        builder: (_) => PairView(
          identity: widget.identity,
          peerStore: widget.peerStore,
        ),
      ),
    );
    if (result != null) {
      // Tras aprobar en el PC, el device_id del host se obtiene al conectar.
      // Aquí mostramos un aviso para que el usuario confirme en el PC.
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
              content: Text('Aprueba el dispositivo en el PC para terminar.')),
        );
      }
    }
    await _reload();
  }

  Future<void> _connect(Peer peer) async {
    final api = SignalingApi(baseUrl: peer.server, identity: widget.identity);
    final wsUrl = peer.server
            .replaceFirst('https://', 'wss://')
            .replaceFirst('http://', 'ws://') +
        '/v1/ws';
    final ws = WsClient(wsUrl: wsUrl, identity: widget.identity, api: api);

    try {
      await ws.connect();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('No se pudo conectar: $e')),
        );
      }
      return;
    }

    final session = RemoteSession(
      peer: peer,
      api: api,
      ws: ws,
      peerStore: widget.peerStore,
    );
    await session.start();

    if (mounted) {
      await Navigator.of(context).push(
        MaterialPageRoute(
          builder: (_) => RemoteView(session: session, peerName: peer.name),
        ),
      );
    }
    await ws.close();
    session.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('MiRemote'),
        actions: [
          IconButton(
            icon: const Icon(Icons.info_outline),
            onPressed: () => showDialog(
              context: context,
              builder: (_) => AlertDialog(
                title: const Text('Este dispositivo'),
                content: SelectableText(
                  'device_id:\n$_deviceId\n\nhuella:\n${widget.identity.publicKeyFingerprint()}',
                ),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.of(context).pop(),
                    child: const Text('Cerrar'),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
      body: _peers.isEmpty
          ? _emptyState()
          : ListView.builder(
              itemCount: _peers.length,
              itemBuilder: (_, i) {
                final p = _peers[i];
                return Card(
                  margin:
                      const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                  child: ListTile(
                    leading: const Icon(Icons.computer),
                    title: Text(p.name),
                    subtitle: Text(p.deviceId,
                        maxLines: 1, overflow: TextOverflow.ellipsis),
                    trailing: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        IconButton(
                          icon: const Icon(Icons.power_settings_new),
                          tooltip: 'Encender (WoL)',
                          onPressed: () => _wake(p),
                        ),
                        IconButton(
                          icon: const Icon(Icons.play_arrow),
                          tooltip: 'Conectar',
                          onPressed: () => _connect(p),
                        ),
                      ],
                    ),
                  ),
                );
              },
            ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _startPairing,
        icon: const Icon(Icons.qr_code_scanner),
        label: const Text('Emparejar'),
      ),
    );
  }

  Future<void> _wake(Peer p) async {
    // El WoL requiere la URL del relevo; se pediría en ajustes del par.
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(
          content: Text(
              'Configura la URL del relevo WoL en los ajustes del PC para usar el encendido remoto.')),
    );
  }

  Widget _emptyState() {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.devices_other, size: 64, color: Colors.white24),
          const SizedBox(height: 16),
          const Text('No hay PCs vinculados',
              style: TextStyle(color: Colors.white54)),
          const SizedBox(height: 8),
          const Text('Pulsa "Emparejar" y escanea el QR del PC',
              style: TextStyle(color: Colors.white38, fontSize: 13)),
        ],
      ),
    );
  }
}
