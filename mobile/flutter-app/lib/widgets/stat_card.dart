import 'package:flutter/material.dart';

import '../theme/app_theme.dart';

/// A compact metric tile used on the Overview/Dashboard pages.
///
/// ```dart
/// StatCard(
///   icon: Icons.bolt,
///   label: 'Earned Today',
///   value: '1,234 sats',
///   accent: SLColors.amber,
/// )
/// ```
class StatCard extends StatelessWidget {
  final IconData icon;
  final String   label;
  final String   value;
  final String?  subtitle;
  final Color    accent;
  final VoidCallback? onTap;

  const StatCard({
    super.key,
    required this.icon,
    required this.label,
    required this.value,
    this.subtitle,
    this.accent = SLColors.cyan,
    this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Focus(
      onFocusChange: (focused) {
        // Visual focus indication for TV D-pad navigation — no setState needed
        // because Focus rebuilds automatically.
        (context as Element).markNeedsBuild();
      },
      child: Builder(builder: (ctx) {
        final focused = Focus.of(ctx).hasFocus;
        return GestureDetector(
          onTap: onTap,
          child: Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              color: SLColors.surface,
              borderRadius: BorderRadius.circular(12),
              border: Border.all(
                color: focused ? SLColors.cyan : SLColors.border,
                width: focused ? 2.0 : 1.0,
              ),
            ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Row(
              children: [
                Container(
                  padding: const EdgeInsets.all(6),
                  decoration: BoxDecoration(
                    color: accent.withOpacity(0.12),
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: Icon(icon, color: accent, size: 16),
                ),
                const Spacer(),
                if (onTap != null)
                  Icon(Icons.chevron_right_rounded,
                      color: SLColors.textMuted, size: 16),
              ],
            ),
            const SizedBox(height: 12),
            Text(value,
                style: theme.textTheme.displayMedium?.copyWith(
                  fontSize: 22, color: SLColors.textPrimary,
                )),
            const SizedBox(height: 2),
            Text(label, style: theme.textTheme.bodyMedium),
            if (subtitle != null) ...[
              const SizedBox(height: 2),
              Text(subtitle!, style: theme.textTheme.labelSmall),
            ],
          ],
        ),
      ),
        );
      }),
    );
  }
}
