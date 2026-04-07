import 'dart:async';

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../api/soholink_client.dart';
import '../models/task.dart';
import '../models/workload.dart';
import '../services/websocket_service.dart';
import '../theme/app_theme.dart';
import '../widgets/section_header.dart';
import 'home_page.dart';

/// Lists all active and recent workloads running on this node.
class WorkloadsPage extends StatefulWidget {
  const WorkloadsPage({super.key});

  @override
  State<WorkloadsPage> createState() => _WorkloadsPageState();
}

class _WorkloadsPageState extends State<WorkloadsPage> {
  WorkloadsResponse? _resp;
  bool               _loading = true;
  String?            _error;

  // Incoming tasks pushed via WebSocket.
  final List<MobileTaskDescriptor> _pushedTasks = [];
  StreamSubscription<MobileTaskDescriptor>? _taskSub;

  @override
  void initState() {
    super.initState();
    _fetch();
    refreshNotifier.addListener(_onRefresh);
    _taskSub = WebSocketService.instance.taskStream.listen(_onIncomingTask);
  }

  @override
  void dispose() {
    refreshNotifier.removeListener(_onRefresh);
    _taskSub?.cancel();
    super.dispose();
  }

  void _onRefresh() => _fetch();

  void _onIncomingTask(MobileTaskDescriptor task) {
    if (!mounted) return;
    setState(() => _pushedTasks.insert(0, task));
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(
      content: Text('Task received: ${task.taskId.length > 12 ? task.taskId.substring(0, 12) : task.taskId}…'),
      duration: const Duration(seconds: 3),
    ));
  }

  Future<void> _fetch() async {
    if (!mounted) return;
    setState(() { _loading = true; _error = null; });
    try {
      final r = await SoHoLinkClient.instance.getWorkloads();
      if (mounted) setState(() { _resp = r; _loading = false; });
    } catch (e) {
      if (mounted) setState(() { _error = e.toString(); _loading = false; });
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) return const Center(
        child: CircularProgressIndicator(color: SLColors.cyan));
    if (_error != null) {
      return _WorkloadsError(error: _error!, onRetry: _fetch);
    }

    final workloads = _resp!.workloads;

    return RefreshIndicator(
      color: SLColors.cyan,
      backgroundColor: SLColors.surface,
      onRefresh: _fetch,
      child: CustomScrollView(
        slivers: [
          SliverToBoxAdapter(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 0),
              child: SectionHeader(
                title: 'Active Workloads',
                trailing: _CountBadge(count: _resp!.count),
              ),
            ),
          ),

          if (workloads.isEmpty)
            const SliverFillRemaining(child: _EmptyWorkloads())
          else
            SliverList(
              delegate: SliverChildBuilderDelegate(
                (ctx, i) => Padding(
                  padding: const EdgeInsets.fromLTRB(16, 0, 16, 10),
                  child: _WorkloadCard(w: workloads[i], btcUsdRate: _resp!.btcUsdRate),
                ),
                childCount: workloads.length,
              ),
            ),

          const SliverPadding(padding: EdgeInsets.only(bottom: 32)),
        ],
      ),
    );
  }
}

// ── Workload card ─────────────────────────────────────────────────────────────

class _WorkloadCard extends StatelessWidget {
  final Workload w;
  final double   btcUsdRate;
  const _WorkloadCard({required this.w, required this.btcUsdRate});

  Color _statusColor() {
    switch (w.status) {
      case 'running': return SLColors.green;
      case 'pending': return SLColors.amber;
      default:        return SLColors.red;
    }
  }

  IconData _statusIcon() {
    switch (w.status) {
      case 'running': return Icons.play_circle_outline_rounded;
      case 'pending': return Icons.hourglass_top_rounded;
      default:        return Icons.stop_circle_outlined;
    }
  }

  String _uptime() {
    if (w.startedUnix == 0) return '—';
    final elapsed = DateTime.now().millisecondsSinceEpoch ~/ 1000 - w.startedUnix;
    final d = Duration(seconds: elapsed < 0 ? 0 : elapsed);
    if (d.inHours > 0) return '${d.inHours}h ${d.inMinutes.remainder(60)}m';
    return '${d.inMinutes}m';
  }

  String _earnedLabel(int sats, double rate) {
    if (rate <= 0) return '${NumberFormat('#,###').format(sats)} sats';
    final usd = sats * rate / 100000000.0;
    return NumberFormat.currency(symbol: '\$', decimalDigits: 2).format(usd);
  }

  @override
  Widget build(BuildContext context) {
    final theme  = Theme.of(context);
    final sc     = _statusColor();
    final rate   = btcUsdRate;

    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: SLColors.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: SLColors.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // header row
          Row(
            children: [
              Icon(_statusIcon(), color: sc, size: 18),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  w.name.isNotEmpty ? w.name : w.id,
                  style: theme.textTheme.titleMedium,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                decoration: BoxDecoration(
                  color: sc.withOpacity(0.12),
                  borderRadius: BorderRadius.circular(20),
                ),
                child: Text(w.status,
                    style: TextStyle(
                        color: sc, fontSize: 11, fontWeight: FontWeight.w600)),
              ),
            ],
          ),

          const SizedBox(height: 10),

          // resource summary
          Text(w.resourceSummary,
              style: theme.textTheme.bodyMedium
                  ?.copyWith(color: SLColors.textSecondary)),

          const SizedBox(height: 10),
          const Divider(height: 1),
          const SizedBox(height: 10),

          // bottom row: earnings + uptime
          Row(
            children: [
              const Icon(Icons.attach_money_rounded, color: SLColors.amber, size: 14),
              const SizedBox(width: 4),
              Text(_earnedLabel(w.earnedSats, rate),
                  style: theme.textTheme.bodyMedium
                      ?.copyWith(color: SLColors.amber)),
              const Spacer(),
              const Icon(Icons.timer_outlined,
                  color: SLColors.textMuted, size: 14),
              const SizedBox(width: 4),
              Text('Uptime: ${_uptime()}',
                  style: theme.textTheme.labelSmall),
            ],
          ),

          if (w.tenantDid.isNotEmpty) ...[
            const SizedBox(height: 6),
            Row(
              children: [
                const Icon(Icons.person_outline_rounded,
                    color: SLColors.textMuted, size: 14),
                const SizedBox(width: 4),
                Expanded(
                  child: Text(
                    _shortDid(w.tenantDid),
                    style: const TextStyle(
                        fontSize: 11, color: SLColors.textMuted),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }

  static String _shortDid(String did) => did.length > 24
      ? '${did.substring(0, 12)}…${did.substring(did.length - 8)}'
      : did;
}

// ── Helpers ────────────────────────────────────────────────────────────────────

class _CountBadge extends StatelessWidget {
  final int count;
  const _CountBadge({required this.count});

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
    decoration: BoxDecoration(
      color: SLColors.cyan.withOpacity(0.12),
      borderRadius: BorderRadius.circular(20),
      border: Border.all(color: SLColors.cyan.withOpacity(0.3)),
    ),
    child: Text('$count running',
        style: const TextStyle(
            color: SLColors.cyan, fontSize: 12, fontWeight: FontWeight.w600)),
  );
}

class _EmptyWorkloads extends StatelessWidget {
  const _EmptyWorkloads();

  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.all(40),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.memory_outlined, color: SLColors.textMuted, size: 48),
          const SizedBox(height: 16),
          Text('No workloads running',
              style: Theme.of(context)
                  .textTheme.titleMedium
                  ?.copyWith(color: SLColors.textSecondary)),
          const SizedBox(height: 8),
          Text(
            'Workloads appear here when tenants\nschedule jobs on your node.',
            textAlign: TextAlign.center,
            style: Theme.of(context).textTheme.bodyMedium,
          ),
        ],
      ),
    ),
  );
}

class _WorkloadsError extends StatelessWidget {
  final String       error;
  final VoidCallback onRetry;
  const _WorkloadsError({required this.error, required this.onRetry});

  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.all(32),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.wifi_off_rounded, color: SLColors.red, size: 48),
          const SizedBox(height: 16),
          Text(error, textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyMedium),
          const SizedBox(height: 24),
          OutlinedButton.icon(
            onPressed: onRetry,
            icon: const Icon(Icons.refresh_rounded),
            label: const Text('Retry'),
          ),
        ],
      ),
    ),
  );
}
