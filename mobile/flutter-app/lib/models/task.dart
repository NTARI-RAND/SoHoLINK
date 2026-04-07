// Dart wire-protocol types mirroring internal/orchestration/mobile.go.
// All fields use snake_case JSON keys to match the Go server's output.

/// Registration payload sent on WebSocket connect.
class MobileNodeInfo {
  final String nodeDid;
  final String nodeClass; // "mobile-android" | "android-tv" | "mobile-ios"
  final String arch;      // "arm64"
  final int    memoryMb;
  final int    cpuCores;
  final int    batteryPct; // -1 when unknown / always-on
  final bool   plugged;
  final bool   wifi;
  final String appVersion;

  const MobileNodeInfo({
    required this.nodeDid,
    required this.nodeClass,
    this.arch       = 'arm64',
    this.memoryMb   = 4096,
    this.cpuCores   = 4,
    this.batteryPct = -1,
    this.plugged    = true,
    this.wifi       = true,
    this.appVersion = '0.1.0',
  });

  Map<String, dynamic> toJson() => {
    'node_did':    nodeDid,
    'node_class':  nodeClass,
    'arch':        arch,
    'memory_mb':   memoryMb,
    'cpu_cores':   cpuCores,
    'battery_pct': batteryPct,
    'plugged':     plugged,
    'wifi':        wifi,
    'app_version': appVersion,
  };
}

/// Task descriptor pushed from the server to this node.
class MobileTaskDescriptor {
  final String taskId;
  final String workloadId;
  final String wasmCid;
  final String inputCid;
  final int    maxDurationS;
  final int    segmentIndex;
  final int    segmentCount;
  final String paymentHashHex;

  const MobileTaskDescriptor({
    required this.taskId,
    required this.workloadId,
    this.wasmCid       = '',
    this.inputCid      = '',
    this.maxDurationS  = 0,
    this.segmentIndex  = 0,
    this.segmentCount  = 1,
    this.paymentHashHex = '',
  });

  factory MobileTaskDescriptor.fromJson(Map<String, dynamic> j) =>
      MobileTaskDescriptor(
        taskId:         j['task_id']          as String? ?? '',
        workloadId:     j['workload_id']       as String? ?? '',
        wasmCid:        j['wasm_cid']          as String? ?? '',
        inputCid:       j['input_cid']         as String? ?? '',
        maxDurationS:   j['max_duration_s']    as int?    ?? 0,
        segmentIndex:   j['segment_index']     as int?    ?? 0,
        segmentCount:   j['segment_count']     as int?    ?? 1,
        paymentHashHex: j['payment_hash_hex']  as String? ?? '',
      );
}

/// Result sent back to the server after task execution (or immediate error).
class MobileTaskResult {
  final String taskId;
  final String workloadId;
  final String resultCid;
  final String resultHash;
  final int    durationMs;
  final String error;

  const MobileTaskResult({
    required this.taskId,
    required this.workloadId,
    this.resultCid  = '',
    this.resultHash = '',
    this.durationMs = 0,
    this.error      = '',
  });

  Map<String, dynamic> toJson() => {
    'task_id':     taskId,
    'workload_id': workloadId,
    'result_cid':  resultCid,
    'result_hash': resultHash,
    'duration_ms': durationMs,
    'error':       error,
  };
}

/// Periodic liveness frame sent every 25 seconds.
class MobileHeartbeat {
  final String nodeDid;
  final int    batteryPct;
  final bool   plugged;
  final bool   wifi;

  const MobileHeartbeat({
    required this.nodeDid,
    this.batteryPct = -1,
    this.plugged    = true,
    this.wifi       = true,
  });

  Map<String, dynamic> toJson() => {
    'node_did':    nodeDid,
    'battery_pct': batteryPct,
    'plugged':     plugged,
    'wifi':        wifi,
  };
}
