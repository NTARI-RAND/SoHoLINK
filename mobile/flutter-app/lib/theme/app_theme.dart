import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';

/// SoHoLINK brand colours
class SLColors {
  SLColors._();

  // backgrounds
  static const canvas     = Color(0xFF0D1117); // deepest bg
  static const surface    = Color(0xFF161B22); // cards
  static const surfaceAlt = Color(0xFF1C2333); // elevated cards / dialogs

  // brand accent
  static const cyan       = Color(0xFF00E5FF);
  static const cyanDim    = Color(0xFF0097A7);
  static const purple     = Color(0xFF7C3AED);
  static const purpleDim  = Color(0xFF4C1D95);

  // status
  static const green      = Color(0xFF22C55E);
  static const amber      = Color(0xFFF59E0B);
  static const red        = Color(0xFFEF4444);

  // text
  static const textPrimary   = Color(0xFFE6EDF3);
  static const textSecondary = Color(0xFF8B949E);
  static const textMuted     = Color(0xFF484F58);

  // borders
  static const border     = Color(0xFF30363D);
}

class AppTheme {
  AppTheme._();

  static ThemeData get dark {
    final base = ThemeData.dark(useMaterial3: true);

    return base.copyWith(
      scaffoldBackgroundColor: SLColors.canvas,
      colorScheme: const ColorScheme.dark(
        primary:   SLColors.cyan,
        secondary: SLColors.purple,
        surface:   SLColors.surface,
        error:     SLColors.red,
        onPrimary: SLColors.canvas,
        onSurface: SLColors.textPrimary,
      ),

      textTheme: GoogleFonts.interTextTheme(base.textTheme).copyWith(
        displayLarge: GoogleFonts.rajdhani(
          fontSize: 32, fontWeight: FontWeight.w700,
          color: SLColors.textPrimary, letterSpacing: 1.2,
        ),
        displayMedium: GoogleFonts.rajdhani(
          fontSize: 24, fontWeight: FontWeight.w600,
          color: SLColors.textPrimary, letterSpacing: 0.8,
        ),
        titleLarge: GoogleFonts.rajdhani(
          fontSize: 20, fontWeight: FontWeight.w600,
          color: SLColors.textPrimary,
        ),
        titleMedium: GoogleFonts.inter(
          fontSize: 16, fontWeight: FontWeight.w500,
          color: SLColors.textPrimary,
        ),
        bodyLarge: GoogleFonts.inter(
          fontSize: 14, color: SLColors.textPrimary,
        ),
        bodyMedium: GoogleFonts.inter(
          fontSize: 13, color: SLColors.textSecondary,
        ),
        labelLarge: GoogleFonts.inter(
          fontSize: 13, fontWeight: FontWeight.w600,
          color: SLColors.textPrimary, letterSpacing: 0.5,
        ),
        labelSmall: GoogleFonts.inter(
          fontSize: 11, color: SLColors.textMuted, letterSpacing: 0.8,
        ),
      ),

      appBarTheme: AppBarTheme(
        backgroundColor: SLColors.surface,
        foregroundColor: SLColors.textPrimary,
        elevation: 0,
        centerTitle: false,
        titleTextStyle: GoogleFonts.rajdhani(
          fontSize: 20, fontWeight: FontWeight.w700,
          color: SLColors.textPrimary, letterSpacing: 1.0,
        ),
        iconTheme: const IconThemeData(color: SLColors.cyan),
      ),

      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: SLColors.surface,
        indicatorColor: SLColors.cyan.withOpacity(0.18),
        labelTextStyle: WidgetStatePropertyAll(
          GoogleFonts.inter(fontSize: 11, color: SLColors.textSecondary),
        ),
        iconTheme: const WidgetStatePropertyAll(
          IconThemeData(color: SLColors.textSecondary, size: 22),
        ),
      ),

      cardTheme: CardTheme(
        color: SLColors.surface,
        elevation: 0,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(12),
          side: const BorderSide(color: SLColors.border, width: 1),
        ),
        margin: EdgeInsets.zero,
      ),

      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: SLColors.surfaceAlt,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: const BorderSide(color: SLColors.border),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: const BorderSide(color: SLColors.border),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: const BorderSide(color: SLColors.cyan, width: 1.5),
        ),
        labelStyle: GoogleFonts.inter(color: SLColors.textSecondary, fontSize: 13),
        hintStyle: GoogleFonts.inter(color: SLColors.textMuted, fontSize: 13),
      ),

      elevatedButtonTheme: ElevatedButtonThemeData(
        style: ElevatedButton.styleFrom(
          backgroundColor: SLColors.cyan,
          foregroundColor: SLColors.canvas,
          textStyle: GoogleFonts.inter(fontWeight: FontWeight.w700, fontSize: 14),
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 14),
          elevation: 0,
        ),
      ),

      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: SLColors.cyan,
          side: const BorderSide(color: SLColors.cyan),
          textStyle: GoogleFonts.inter(fontWeight: FontWeight.w600, fontSize: 14),
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 14),
        ),
      ),

      dividerTheme: const DividerThemeData(
        color: SLColors.border,
        thickness: 1,
        space: 1,
      ),

      snackBarTheme: SnackBarThemeData(
        backgroundColor: SLColors.surfaceAlt,
        contentTextStyle: GoogleFonts.inter(color: SLColors.textPrimary, fontSize: 13),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
        behavior: SnackBarBehavior.floating,
      ),
    );
  }

  /// TV theme — identical colour scheme to [dark] but with all text sizes
  /// scaled 1.5× and icons enlarged for comfortable viewing at 3 metres.
  static ThemeData get tv {
    final base = dark;
    return base.copyWith(
      textTheme: base.textTheme.copyWith(
        displayLarge: base.textTheme.displayLarge?.copyWith(fontSize: 48),
        displayMedium: base.textTheme.displayMedium?.copyWith(fontSize: 36),
        titleLarge: base.textTheme.titleLarge?.copyWith(fontSize: 30),
        titleMedium: base.textTheme.titleMedium?.copyWith(fontSize: 24),
        bodyLarge: base.textTheme.bodyLarge?.copyWith(fontSize: 21),
        bodyMedium: base.textTheme.bodyMedium?.copyWith(fontSize: 19),
        labelLarge: base.textTheme.labelLarge?.copyWith(fontSize: 19),
        labelSmall: base.textTheme.labelSmall?.copyWith(fontSize: 16),
      ),
      iconTheme: const IconThemeData(color: SLColors.cyan, size: 36),
      cardTheme: base.cardTheme.copyWith(
        margin: const EdgeInsets.all(12),
      ),
    );
  }
}
