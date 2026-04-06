import 'dart:convert';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

import '../models/marketplace.dart';
import '../models/node_status.dart';
import '../models/peer_info.dart';
import '../models/revenue.dart';
import '../models/workload.dart';

/// ─────────────────────────────────────────────────────────────────────────────
/// CONFIGURATION
///
/// [kNodeUrl] is the *default* URL pre-filled in the setup screen.
/// Each device authenticates independently using the owner's 64-char hex
/// private key (shown once when the node first starts).
///
/// To change the default for a new build, update this constant and rebuild.
/// See README → "Mobile App: Changing the Node Address" for details.
/// ─────────────────────────────────────────────────────────────────────────────
const kNodeUrl = 'https://soholink.org';

const _kBaseUrl     = 'node_base_url';
const _kDeviceToken = 'device_token';   // key used in flutter_secure_storage (encrypted)
const _kTimeoutSecs = 10;

/// [SoHoLinkClient] is a singleton HTTP client for the SoHoLINK REST API.
///
/// Auth flow (Option C — Ed25519 owner keypair):
///   1. Setup screen: user enters node URL + 64-char hex owner private key.
///   2. Client fetches a challenge nonce from GET /api/auth/challenge.
///   3. Client signs the nonce with the Ed25519 private key.
///   4. Client POSTs the signature to /api/auth/connect → receives device token.
///   5. Device token is stored in flutter_secure_storage (iOS Keychain /
///      Android EncryptedSharedPreferences) and sent as
///      `Authorization: Bearer <token>` on every subsequent request.
///      The node base URL (not sensitive) is kept in SharedPreferences.
class SoHoLinkClient {
  SoHoLinkClient._();
  static final SoHoLinkClient instance = SoHoLinkClient._();

  late SharedPreferences _prefs;
  // flutter_secure_storage: token is encrypted at rest using the platform
  // keystore (iOS Keychain / Android Keystore via EncryptedSharedPreferences).
  final FlutterSecureStorage _secureStorage = const FlutterSecureStorage(
    aOptions: AndroidOptions(encryptedSharedPreferences: true),
  );
  // Eagerly cached after init() so _authHeaders can remain synchronous.
  String? _cachedToken;
  bool _ready = false;

  Future<void> init() async {
    if (_ready) return;
    _prefs = await SharedPreferences.getInstance();
    // Seed the default URL on first ever launch so the setup screen is
    // pre-filled, but do NOT overwrite a URL the user already chose.
    if (!_prefs.containsKey(_kBaseUrl)) {
      await _prefs.setString(_kBaseUrl, kNodeUrl);
    }
    // Load token from secure storage into the synchronous cache.
    _cachedToken = await _secureStorage.read(key: _kDeviceToken);
    _ready = true;
  }

  // ── State ────────────────────────────────────────────────────────────────

  String get baseUrl => _prefs.getString(_kBaseUrl) ?? kNodeUrl;

  /// WebSocket URL derived from the base HTTP URL.
  String get wsUrl => baseUrl.replaceFirst(RegExp(r'^http'), 'ws') + '/ws/nodes';

  /// True once the device has a persisted encrypted device token.
  bool get hasConfiguredUrl => _cachedToken != null;

  /// The node DID stored in the device token (used for WebSocket registration).
  String get nodeDid => _cachedToken ?? '';

  String? get _deviceToken => _cachedToken;

  // ── Authentication ───────────────────────────────────────────────────────

  /// Authenticate with the node using the owner's Ed25519 private key seed.
  ///
  /// [url]           — node base URL, e.g. http://192.168.1.220:8080
  /// [privateKeyHex] — 64-char hex string (32-byte seed printed at node startup)
  /// [deviceName]    — human-readable label shown in the node's device list
  ///
  /// Throws [ApiException] or [FormatException] on failure.
  Future<void> connectWithKey(
      String url, String privateKeyHex, String deviceName) async {
    // Normalise URL.
    final base = url.trimRight().replaceAll(RegExp(r'/$'), '');

    // 1. Decode the 32-byte seed from hex.
    final seedBytes = _hexToBytes(privateKeyHex.trim());
    if (seedBytes.length != 32) {
      throw FormatException(
          'Private key must be exactly 64 hex characters (32 bytes).');
    }

    // 2. Derive Ed25519 key pair from seed.
    final algorithm  = Ed25519();
    final keyPair    = await algorithm.newKeyPairFromSeed(seedBytes);
    final publicKey  = await keyPair.extractPublicKey();
    final pubB64     = base64.encode(publicKey.bytes);

    // 3. Fetch a challenge nonce.
    final challengeResp = await http
        .get(Uri.parse('$base/api/auth/challenge'))
        .timeout(const Duration(seconds: _kTimeoutSecs));
    if (challengeResp.statusCode != 200) {
      throw ApiException(challengeResp.statusCode,
          'Challenge failed: ${challengeResp.body}');
    }
    final nonce =
        (json.decode(challengeResp.body) as Map<String, dynamic>)['nonce']
            as String;

    // 4. Sign the nonce with Ed25519.
    final sig = await algorithm.sign(
      utf8.encode(nonce),
      keyPair: keyPair,
    );
    final sigB64 = base64.encode(sig.bytes);

    // 5. POST to /api/auth/connect.
    final connectResp = await http
        .post(
          Uri.parse('$base/api/auth/connect'),
          headers: {'Content-Type': 'application/json'},
          body: json.encode({
            'nonce':       nonce,
            'public_key':  pubB64,
            'signature':   sigB64,
            'device_name': deviceName,
          }),
        )
        .timeout(const Duration(seconds: _kTimeoutSecs));
    if (connectResp.statusCode != 200) {
      throw ApiException(connectResp.statusCode,
          'Auth failed: ${connectResp.body}');
    }
    final deviceToken =
        (json.decode(connectResp.body) as Map<String, dynamic>)['device_token']
            as String;

    // 6. Persist the URL (non-sensitive → SharedPreferences) and token
    //    (sensitive → encrypted secure storage).
    await _prefs.setString(_kBaseUrl, base);
    await _secureStorage.write(key: _kDeviceToken, value: deviceToken);
    _cachedToken = deviceToken; // update synchronous cache
  }

  /// Clear the stored device token (logout).
  Future<void> logout() async {
    await _secureStorage.delete(key: _kDeviceToken);
    _cachedToken = null;
  }

  Future<void> setBaseUrl(String url) =>
      _prefs.setString(_kBaseUrl, url.trimRight().replaceAll(RegExp(r'/$'), ''));

  // ── Generic request helpers ──────────────────────────────────────────────

  Uri _uri(String path, [Map<String, String>? params]) {
    final u = Uri.parse('$baseUrl$path');
    return params != null ? u.replace(queryParameters: params) : u;
  }

  Map<String, String> get _authHeaders {
    final t = _deviceToken;
    return t != null
        ? {'Authorization': 'Bearer $t'}
        : {};
  }

  Future<Map<String, dynamic>> _get(String path,
      [Map<String, String>? params]) async {
    final resp = await http
        .get(_uri(path, params), headers: _authHeaders)
        .timeout(const Duration(seconds: _kTimeoutSecs));
    _checkStatus(resp);
    return json.decode(resp.body) as Map<String, dynamic>;
  }

  void _checkStatus(http.Response r) {
    if (r.statusCode < 200 || r.statusCode >= 300) {
      throw ApiException(r.statusCode, r.body);
    }
  }

  // ── Health check (public — no auth header needed) ────────────────────────

  Future<bool> checkHealth(String url) async {
    try {
      final base = url.trimRight().replaceAll(RegExp(r'/$'), '');
      final resp = await http
          .get(Uri.parse('$base/api/health'))
          .timeout(const Duration(seconds: _kTimeoutSecs));
      return resp.statusCode == 200;
    } catch (_) {
      return false;
    }
  }

  // ── Status ───────────────────────────────────────────────────────────────

  Future<NodeStatus> getStatus() async {
    final j = await _get('/api/status');
    return NodeStatus.fromJson(j);
  }

  // ── Peers ────────────────────────────────────────────────────────────────

  Future<PeersResponse> getPeers() async {
    final j = await _get('/api/peers');
    return PeersResponse.fromJson(j);
  }

  // ── Revenue ──────────────────────────────────────────────────────────────

  Future<RevenueSummary> getRevenue() async {
    final j = await _get('/api/revenue');
    return RevenueSummary.fromJson(j);
  }

  // ── Workloads ────────────────────────────────────────────────────────────

  Future<WorkloadsResponse> getWorkloads() async {
    final j = await _get('/api/workloads');
    return WorkloadsResponse.fromJson(j);
  }

  // ── Marketplace ──────────────────────────────────────────────────────────

  /// Browse available provider nodes, optionally filtered by resource needs.
  Future<List<MarketplaceNode>> getMarketplaceNodes({
    double? minCpu,
    int?    maxPriceSats,
    String? region,
    bool?   gpu,
    int?    minReputation,
  }) async {
    final params = <String, String>{};
    if (minCpu      != null) params['min_cpu']          = minCpu.toString();
    if (maxPriceSats != null) params['max_price_sats']  = maxPriceSats.toString();
    if (region      != null && region.isNotEmpty) params['region'] = region;
    if (gpu         == true) params['gpu']              = 'true';
    if (minReputation != null) params['min_reputation'] = minReputation.toString();

    final j = await _get('/api/marketplace/nodes', params.isEmpty ? null : params);
    final list = (j['nodes'] as List?) ?? [];
    return list.map((e) => MarketplaceNode.fromJson(e as Map<String, dynamic>)).toList();
  }

  /// Estimate cost for a workload configuration.
  Future<CostEstimate> estimateCost({
    required double cpuCores,
    required int    memoryMb,
    required int    diskGb,
    required int    durationHours,
  }) async {
    final j = await _post('/api/marketplace/estimate', {
      'cpu_cores':      cpuCores,
      'memory_mb':      memoryMb,
      'disk_gb':        diskGb,
      'duration_hours': durationHours,
    });
    return CostEstimate.fromJson(j);
  }

  /// Purchase a compute workload — debits wallet and submits to scheduler.
  Future<Map<String, dynamic>> purchaseWorkload({
    required double cpuCores,
    required int    memoryMb,
    required int    diskGb,
    required int    durationHours,
    int    replicas    = 1,
    String description = '',
    String image       = '',
  }) async {
    return _post('/api/marketplace/purchase', {
      'cpu_cores':      cpuCores,
      'memory_mb':      memoryMb,
      'disk_gb':        diskGb,
      'duration_hours': durationHours,
      'replicas':       replicas,
      'description':    description,
      'image':          image,
    });
  }

  // ── Wallet ───────────────────────────────────────────────────────────────

  /// Fetch the current prepaid sats balance.
  Future<WalletBalance> getWalletBalance() async {
    final j = await _get('/api/wallet/balance');
    return WalletBalance.fromJson(j);
  }

  /// Create a Lightning invoice or Stripe payment intent for a top-up.
  Future<Map<String, dynamic>> topupWallet({
    required int    amountSats,
    String processor = 'lightning',
  }) async {
    return _post('/api/wallet/topup', {
      'amount_sats': amountSats,
      'processor':   processor,
    });
  }

  /// Manually confirm a pending topup (dev / test helper).
  Future<Map<String, dynamic>> confirmTopup(String topupId) async {
    return _post('/api/wallet/confirm-topup', {'topup_id': topupId});
  }

  // ── Orders ───────────────────────────────────────────────────────────────

  /// Fetch recent orders placed by this node's owner.
  Future<List<Order>> getOrders({int limit = 20}) async {
    final j = await _get('/api/orders', {'limit': limit.toString()});
    final list = (j['orders'] as List?) ?? [];
    return list.map((e) => Order.fromJson(e as Map<String, dynamic>)).toList();
  }

  /// Cancel an active order and receive a proportional refund.
  Future<Map<String, dynamic>> cancelOrder(String orderId) async {
    return _post('/api/orders/$orderId/cancel', {});
  }

  // ── FCM token registration ────────────────────────────────────────────────

  /// Post the FCM device token to the node so it can send background wakeups.
  Future<void> registerFcmToken(String fcmToken) async {
    await _post('/api/v1/nodes/mobile/fcm-token', {
      'node_did':  nodeDid,
      'fcm_token': fcmToken,
    });
  }

  // ── Helpers ──────────────────────────────────────────────────────────────

  Future<Map<String, dynamic>> _post(String path, Map<String, dynamic> body) async {
    final resp = await http
        .post(
          _uri(path),
          headers: {
            ...?(_deviceToken != null
                ? {'Authorization': 'Bearer $_deviceToken'}
                : null),
            'Content-Type': 'application/json',
          },
          body: json.encode(body),
        )
        .timeout(const Duration(seconds: _kTimeoutSecs));
    _checkStatus(resp);
    return json.decode(resp.body) as Map<String, dynamic>;
  }

  static Uint8List _hexToBytes(String hex) {
    if (hex.length % 2 != 0) throw FormatException('Odd-length hex string');
    final result = Uint8List(hex.length ~/ 2);
    for (var i = 0; i < result.length; i++) {
      result[i] = int.parse(hex.substring(i * 2, i * 2 + 2), radix: 16);
    }
    return result;
  }
}

// ── Exceptions ──────────────────────────────────────────────────────────────

class ApiException implements Exception {
  final int    statusCode;
  final String body;

  const ApiException(this.statusCode, this.body);

  @override
  String toString() => 'ApiException($statusCode): $body';
}
