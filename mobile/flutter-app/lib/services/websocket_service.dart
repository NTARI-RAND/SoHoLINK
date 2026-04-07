import 'dart:async';
import 'dart:convert';

import 'package:web_socket_channel/web_socket_channel.dart';

import '../models/task.dart';

/// Singleton WebSocket service that connects to the SoHoLINK node's
/// /ws/nodes endpoint and exchanges task-dispatch frames.
///
/// Wire envelope (matches internal/httpapi/mobilehub.go ServeWS):
///   {"type": "register"|"heartbeat"|"task"|"result", "payload": {...}}
class WebSocketService {
  WebSocketService._();
  static final WebSocketService instance = WebSocketService._();

  WebSocketChannel? _channel;
  StreamSubscription? _sub;
  Timer? _heartbeatTimer;

  final _taskController = StreamController<MobileTaskDescriptor>.broadcast();

  /// Broadcast stream of incoming task descriptors.
  Stream<MobileTaskDescriptor> get taskStream => _taskController.stream;

  bool _disposed = false;
  int  _reconnectAttempt = 0;
  MobileNodeInfo? _nodeInfo;
  String? _wsUrl;

  /// Connect to [wsUrl] and register as [nodeInfo].
  /// Safe to call multiple times — closes any existing connection first.
  void connect(String wsUrl, MobileNodeInfo nodeInfo) {
    _wsUrl    = wsUrl;
    _nodeInfo = nodeInfo;
    _reconnectAttempt = 0;
    _connect();
  }

  void _connect() {
    if (_disposed || _wsUrl == null || _nodeInfo == null) return;

    _close(reconnect: false);

    final uri = Uri.parse(_wsUrl!);
    _channel = WebSocketChannel.connect(uri);

    _sub = _channel!.stream.listen(
      _onMessage,
      onError: (_) => _scheduleReconnect(),
      onDone:  _scheduleReconnect,
      cancelOnError: false,
    );

    // Send registration frame immediately after connect.
    _send('register', _nodeInfo!.toJson());

    // Heartbeat every 25 seconds (server pings at 30s).
    _heartbeatTimer?.cancel();
    _heartbeatTimer = Timer.periodic(const Duration(seconds: 25), (_) {
      final hb = MobileHeartbeat(
        nodeDid:    _nodeInfo!.nodeDid,
        batteryPct: _nodeInfo!.batteryPct,
        plugged:    _nodeInfo!.plugged,
        wifi:       _nodeInfo!.wifi,
      );
      _send('heartbeat', hb.toJson());
    });

    _reconnectAttempt = 0;
  }

  void _onMessage(dynamic raw) {
    try {
      final env = jsonDecode(raw as String) as Map<String, dynamic>;
      final type    = env['type']    as String? ?? '';
      final payload = env['payload'] as Map<String, dynamic>? ?? {};

      if (type == 'task') {
        final descriptor = MobileTaskDescriptor.fromJson(payload);
        _taskController.add(descriptor);
      }
      // Other types (pong, etc.) are silently ignored.
    } catch (_) {
      // Malformed frame — ignore.
    }
  }

  /// Send a result frame back to the server.
  void sendResult(MobileTaskResult result) {
    _send('result', result.toJson());
  }

  void _send(String type, Map<String, dynamic> payload) {
    try {
      _channel?.sink.add(jsonEncode({'type': type, 'payload': payload}));
    } catch (_) {
      // Channel may be closing; ignore.
    }
  }

  void _scheduleReconnect() {
    _heartbeatTimer?.cancel();
    _sub?.cancel();
    _sub = null;

    if (_disposed) return;

    final delayMs = (1000 * (1 << _reconnectAttempt.clamp(0, 6))).clamp(1000, 60000);
    _reconnectAttempt++;

    Timer(Duration(milliseconds: delayMs), _connect);
  }

  void _close({required bool reconnect}) {
    _heartbeatTimer?.cancel();
    _heartbeatTimer = null;
    _sub?.cancel();
    _sub = null;
    _channel?.sink.close();
    _channel = null;
  }

  void dispose() {
    _disposed = true;
    _close(reconnect: false);
    _taskController.close();
  }
}
