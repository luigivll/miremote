import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';

import '../webrtc/remote_session.dart';

/// Pantalla de control remoto: muestra el vídeo del PC y traduce gestos y
/// teclado en eventos de entrada normalizados (0..1) que envía por la sesión.
class RemoteView extends StatefulWidget {
  final RemoteSession session;
  final String peerName;

  const RemoteView({super.key, required this.session, required this.peerName});

  @override
  State<RemoteView> createState() => _RemoteViewState();
}

class _RemoteViewState extends State<RemoteView> {
  final RTCVideoRenderer _renderer = RTCVideoRenderer();
  final FocusNode _keyboardFocus = FocusNode();
  SessionState _state = SessionState.connecting;
  List<String> _perms = [];
  final GlobalKey _videoKey = GlobalKey();

  @override
  void initState() {
    super.initState();
    _init();
  }

  Future<void> _init() async {
    await _renderer.initialize();
    widget.session.remoteStream.listen((stream) {
      setState(() => _renderer.srcObject = stream);
    });
    widget.session.state.listen((s) {
      if (mounted) setState(() => _state = s);
    });
    widget.session.permissions.listen((p) {
      if (mounted) setState(() => _perms = p);
    });
  }

  @override
  void dispose() {
    _renderer.dispose();
    _keyboardFocus.dispose();
    super.dispose();
  }

  bool get _canControl => _perms.contains('input.control');

  /// Convierte una posición local sobre el vídeo a coordenadas 0..1.
  Offset? _normalize(Offset localPos) {
    final box = _videoKey.currentContext?.findRenderObject() as RenderBox?;
    if (box == null) return null;
    final size = box.size;
    if (size.width == 0 || size.height == 0) return null;
    final x = (localPos.dx / size.width).clamp(0.0, 1.0);
    final y = (localPos.dy / size.height).clamp(0.0, 1.0);
    return Offset(x, y);
  }

  void _onTapDown(TapDownDetails d) {
    final n = _normalize(d.localPosition);
    if (n == null) return;
    widget.session.sendMouseMove(n.dx, n.dy);
    widget.session.sendMouseButton('left', true);
    widget.session.sendMouseButton('left', false);
  }

  void _onPanUpdate(DragUpdateDetails d) {
    final n = _normalize(d.localPosition);
    if (n == null) return;
    widget.session.sendMouseMove(n.dx, n.dy);
  }

  void _onSecondaryTap(TapDownDetails d) {
    final n = _normalize(d.localPosition);
    if (n == null) return;
    widget.session.sendMouseMove(n.dx, n.dy);
    widget.session.sendMouseButton('right', true);
    widget.session.sendMouseButton('right', false);
  }

  KeyEventResult _onKey(FocusNode node, KeyEvent event) {
    final domCode = _flutterKeyToDomCode(event.logicalKey, event.physicalKey);
    if (domCode == null) return KeyEventResult.ignored;
    if (event is KeyDownEvent) {
      widget.session.sendKey(domCode, true);
    } else if (event is KeyUpEvent) {
      widget.session.sendKey(domCode, false);
    }
    return KeyEventResult.handled;
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        backgroundColor: const Color(0xFF14151A),
        title: Text(widget.peerName),
        actions: [
          IconButton(
            tooltip: 'Teclado',
            icon: const Icon(Icons.keyboard),
            onPressed: _canControl
                ? () => _keyboardFocus.requestFocus()
                : null,
          ),
          IconButton(
            tooltip: 'Pedir fotograma clave',
            icon: const Icon(Icons.refresh),
            onPressed: () => widget.session.requestKeyframe(),
          ),
          IconButton(
            tooltip: 'Desconectar',
            icon: const Icon(Icons.call_end, color: Colors.redAccent),
            onPressed: () async {
              await widget.session.close();
              if (mounted) Navigator.of(context).pop();
            },
          ),
        ],
      ),
      body: Stack(
        children: [
          Center(
            child: _state == SessionState.connected
                ? Focus(
                    focusNode: _keyboardFocus,
                    onKeyEvent: _onKey,
                    child: GestureDetector(
                      onTapDown: _canControl ? _onTapDown : null,
                      onPanUpdate: _canControl ? _onPanUpdate : null,
                      onSecondaryTapDown:
                          _canControl ? _onSecondaryTap : null,
                      child: Container(
                        key: _videoKey,
                        child: RTCVideoView(
                          _renderer,
                          objectFit: RTCVideoViewObjectFit
                              .RTCVideoViewObjectFitContain,
                        ),
                      ),
                    ),
                  )
                : _statusWidget(),
          ),
          if (!_canControl && _state == SessionState.connected)
            const Positioned(
              bottom: 16,
              left: 16,
              right: 16,
              child: _InfoBanner(
                text: 'Solo visualización — el control no está concedido',
              ),
            ),
        ],
      ),
    );
  }

  Widget _statusWidget() {
    switch (_state) {
      case SessionState.connecting:
        return const Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            CircularProgressIndicator(),
            SizedBox(height: 16),
            Text('Conectando…', style: TextStyle(color: Colors.white70)),
          ],
        );
      case SessionState.failed:
        return const Text(
          'Conexión fallida.\nLa huella del PC no coincide o la red falló.',
          textAlign: TextAlign.center,
          style: TextStyle(color: Colors.redAccent),
        );
      default:
        return const Text('Sesión cerrada',
            style: TextStyle(color: Colors.white70));
    }
  }
}

class _InfoBanner extends StatelessWidget {
  final String text;
  const _InfoBanner({required this.text});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Colors.black.withOpacity(0.7),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Text(text,
          textAlign: TextAlign.center,
          style: const TextStyle(color: Colors.amberAccent)),
    );
  }
}

/// Mapea teclas de Flutter a DOM code (lo que espera el agente).
String? _flutterKeyToDomCode(LogicalKeyboardKey logical, PhysicalKeyboardKey physical) {
  // Mapa mínimo de teclas de control; las imprimibles se envían mejor como
  // texto, pero el teclado físico del móvil también funciona aquí.
  const map = {
    'Enter': 'Enter',
    'Backspace': 'Backspace',
    'Escape': 'Escape',
    'Tab': 'Tab',
    ' ': 'Space',
    'Arrow Left': 'ArrowLeft',
    'Arrow Right': 'ArrowRight',
    'Arrow Up': 'ArrowUp',
    'Arrow Down': 'ArrowDown',
  };
  final label = logical.keyLabel;
  if (map.containsKey(label)) return map[label];
  // Letras y dígitos.
  if (label.length == 1) {
    final c = label.toUpperCase();
    if (RegExp(r'[A-Z]').hasMatch(c)) return 'Key$c';
    if (RegExp(r'[0-9]').hasMatch(c)) return 'Digit$c';
  }
  return null;
}
