import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';

import '../theme/app_theme.dart';
import 'dashboard_page.dart';
import 'marketplace_page.dart';
import 'peers_page.dart';
import 'revenue_page.dart';
import 'workloads_page.dart';
import 'settings_page.dart';

/// Root shell. Adapts between:
///   - Phone/portrait  → bottom [NavigationBar]
///   - TV/large-screen → left [NavigationRail] with D-pad traversal
///
/// The [isTV] flag is passed from main.dart based on Android TV detection;
/// the width breakpoint (960dp) handles tablets automatically.
class HomePage extends StatefulWidget {
  final bool isTV;
  const HomePage({super.key, this.isTV = false});

  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  int _selectedIndex = 0;

  // Focus node for the navigation rail (TV D-pad entry point).
  final _railFocus = FocusScopeNode();

  static const _pages = <Widget>[
    DashboardPage(),
    PeersPage(),
    RevenuePage(),
    WorkloadsPage(),
    MarketplacePage(),
    SettingsPage(),
  ];

  static const _railDestinations = <NavigationRailDestination>[
    NavigationRailDestination(
      icon:         Icon(Icons.dashboard_outlined),
      selectedIcon: Icon(Icons.dashboard_rounded),
      label: Text('Dashboard'),
    ),
    NavigationRailDestination(
      icon:         Icon(Icons.hub_outlined),
      selectedIcon: Icon(Icons.hub_rounded),
      label: Text('Peers'),
    ),
    NavigationRailDestination(
      icon:         Icon(Icons.bolt_outlined),
      selectedIcon: Icon(Icons.bolt_rounded),
      label: Text('Revenue'),
    ),
    NavigationRailDestination(
      icon:         Icon(Icons.memory_outlined),
      selectedIcon: Icon(Icons.memory_rounded),
      label: Text('Workloads'),
    ),
    NavigationRailDestination(
      icon:         Icon(Icons.storefront_outlined),
      selectedIcon: Icon(Icons.storefront_rounded),
      label: Text('Market'),
    ),
    NavigationRailDestination(
      icon:         Icon(Icons.settings_outlined),
      selectedIcon: Icon(Icons.settings_rounded),
      label: Text('Settings'),
    ),
  ];

  static const _destinations = <NavigationDestination>[
    NavigationDestination(
      icon:         Icon(Icons.dashboard_outlined),
      selectedIcon: Icon(Icons.dashboard_rounded),
      label: 'Dashboard',
    ),
    NavigationDestination(
      icon:         Icon(Icons.hub_outlined),
      selectedIcon: Icon(Icons.hub_rounded),
      label: 'Peers',
    ),
    NavigationDestination(
      icon:         Icon(Icons.bolt_outlined),
      selectedIcon: Icon(Icons.bolt_rounded),
      label: 'Revenue',
    ),
    NavigationDestination(
      icon:         Icon(Icons.memory_outlined),
      selectedIcon: Icon(Icons.memory_rounded),
      label: 'Workloads',
    ),
    NavigationDestination(
      icon:         Icon(Icons.storefront_outlined),
      selectedIcon: Icon(Icons.storefront_rounded),
      label: 'Market',
    ),
    NavigationDestination(
      icon:         Icon(Icons.settings_outlined),
      selectedIcon: Icon(Icons.settings_rounded),
      label: 'Settings',
    ),
  ];

  bool _isTV(BuildContext ctx) =>
      widget.isTV || MediaQuery.of(ctx).size.width >= 960;

  @override
  void dispose() {
    _railFocus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return _isTV(context) ? _buildTVLayout(context) : _buildPhoneLayout(context);
  }

  // ── Phone layout (unchanged from original) ────────────────────────────────

  Widget _buildPhoneLayout(BuildContext context) {
    return Scaffold(
      backgroundColor: SLColors.canvas,
      appBar: AppBar(
        backgroundColor: SLColors.surface,
        titleSpacing: 16,
        title: Row(
          children: [
            Container(
              width: 30, height: 30,
              decoration: BoxDecoration(
                color: SLColors.cyan.withOpacity(0.12),
                borderRadius: BorderRadius.circular(6),
              ),
              child: const Icon(Icons.router_rounded,
                  color: SLColors.cyan, size: 16),
            ),
            const SizedBox(width: 10),
            Text('SoHoLINK',
                style: GoogleFonts.rajdhani(
                  fontSize: 18, fontWeight: FontWeight.w700,
                  color: SLColors.textPrimary, letterSpacing: 1.2,
                )),
          ],
        ),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 12),
            child: IconButton(
              icon: const Icon(Icons.refresh_rounded),
              tooltip: 'Refresh',
              onPressed: _triggerRefresh,
            ),
          ),
        ],
      ),
      body: IndexedStack(index: _selectedIndex, children: _pages),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _selectedIndex,
        onDestinationSelected: (i) => setState(() => _selectedIndex = i),
        destinations: _destinations,
        labelBehavior: NavigationDestinationLabelBehavior.onlyShowSelected,
        animationDuration: const Duration(milliseconds: 300),
      ),
    );
  }

  // ── TV layout ──────────────────────────────────────────────────────────────

  Widget _buildTVLayout(BuildContext context) {
    return Scaffold(
      backgroundColor: SLColors.canvas,
      body: Row(
        children: [
          // Left navigation rail — D-pad navigable via FocusScope.
          _TVRail(
            selectedIndex: _selectedIndex,
            destinations: _railDestinations,
            focusScope: _railFocus,
            onDestinationSelected: (i) => setState(() => _selectedIndex = i),
            onRightArrow: () {
              // Move focus into the content panel.
              _railFocus.unfocus();
              FocusScope.of(context).nextFocus();
            },
            onRefresh: _triggerRefresh,
          ),
          // Content area — full traversal within each page.
          Expanded(
            child: FocusTraversalGroup(
              policy: ReadingOrderTraversalPolicy(),
              child: IndexedStack(index: _selectedIndex, children: _pages),
            ),
          ),
        ],
      ),
    );
  }

  void _triggerRefresh() => refreshNotifier.value = !refreshNotifier.value;
}

// ── TV navigation rail ────────────────────────────────────────────────────────

class _TVRail extends StatelessWidget {
  final int selectedIndex;
  final List<NavigationRailDestination> destinations;
  final FocusScopeNode focusScope;
  final ValueChanged<int> onDestinationSelected;
  final VoidCallback onRightArrow;
  final VoidCallback onRefresh;

  const _TVRail({
    required this.selectedIndex,
    required this.destinations,
    required this.focusScope,
    required this.onDestinationSelected,
    required this.onRightArrow,
    required this.onRefresh,
  });

  @override
  Widget build(BuildContext context) {
    return FocusScope(
      node: focusScope,
      child: Shortcuts(
        shortcuts: {
          LogicalKeySet(LogicalKeyboardKey.arrowRight): const _MoveRightIntent(),
        },
        child: Actions(
          actions: {
            _MoveRightIntent: CallbackAction<_MoveRightIntent>(
              onInvoke: (_) { onRightArrow(); return null; },
            ),
          },
          child: Container(
            color: SLColors.surface,
            child: Column(
              children: [
                // Wordmark
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 24, 16, 8),
                  child: Row(
                    children: [
                      Container(
                        width: 32, height: 32,
                        decoration: BoxDecoration(
                          color: SLColors.cyan.withOpacity(0.12),
                          borderRadius: BorderRadius.circular(8),
                        ),
                        child: const Icon(Icons.router_rounded,
                            color: SLColors.cyan, size: 18),
                      ),
                      const SizedBox(width: 10),
                      Text('SoHoLINK',
                          style: GoogleFonts.rajdhani(
                            fontSize: 20, fontWeight: FontWeight.w700,
                            color: SLColors.textPrimary, letterSpacing: 1.2,
                          )),
                    ],
                  ),
                ),
                const Divider(height: 1),
                const SizedBox(height: 8),

                Expanded(
                  child: NavigationRail(
                    selectedIndex: selectedIndex,
                    onDestinationSelected: onDestinationSelected,
                    extended: true,
                    minExtendedWidth: 200,
                    backgroundColor: Colors.transparent,
                    indicatorColor: SLColors.cyan.withOpacity(0.18),
                    selectedIconTheme: const IconThemeData(
                        color: SLColors.cyan, size: 28),
                    unselectedIconTheme: const IconThemeData(
                        color: SLColors.textSecondary, size: 28),
                    selectedLabelTextStyle: GoogleFonts.rajdhani(
                      fontSize: 16, fontWeight: FontWeight.w600,
                      color: SLColors.cyan,
                    ),
                    unselectedLabelTextStyle: GoogleFonts.rajdhani(
                      fontSize: 16, color: SLColors.textSecondary,
                    ),
                    destinations: destinations,
                    labelType: NavigationRailLabelType.none,
                  ),
                ),

                // Refresh button at the bottom of the rail.
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 0, 16, 24),
                  child: Focus(
                    child: IconButton(
                      icon: const Icon(Icons.refresh_rounded,
                          color: SLColors.textSecondary, size: 28),
                      tooltip: 'Refresh',
                      onPressed: onRefresh,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _MoveRightIntent extends Intent {
  const _MoveRightIntent();
}

/// Simple global refresh signal. Pages listen to this to re-fetch data.
final refreshNotifier = ValueNotifier<bool>(false);
