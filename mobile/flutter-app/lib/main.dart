import 'package:device_info_plus/device_info_plus.dart';
import 'package:firebase_core/firebase_core.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'api/soholink_client.dart';
import 'pages/home_page.dart';
import 'pages/setup_page.dart';
import 'services/fcm_service.dart';
import 'theme/app_theme.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // Detect Android TV before locking orientation — TVs must not be portrait-locked.
  final isTV = await _isAndroidTV();

  // Lock to portrait on phones only; TVs and tablets use all orientations.
  if (defaultTargetPlatform == TargetPlatform.android && !isTV) {
    await SystemChrome.setPreferredOrientations([
      DeviceOrientation.portraitUp,
      DeviceOrientation.portraitDown,
    ]);
  }

  // Status bar appearance — light icons on transparent bar.
  SystemChrome.setSystemUIOverlayStyle(const SystemUiOverlayStyle(
    statusBarColor: Colors.transparent,
    statusBarIconBrightness: Brightness.light,
    systemNavigationBarColor: SLColors.surface,
    systemNavigationBarIconBrightness: Brightness.light,
  ));

  // Firebase must be initialised before any Firebase service is used.
  await Firebase.initializeApp();

  // Initialise shared preferences once; all pages use the singleton.
  await SoHoLinkClient.instance.init();

  // Register FCM token with the node (non-fatal if Firebase unavailable).
  await FcmService.instance.init();

  runApp(SoHoLinkApp(isTV: isTV));
}

/// Returns true when running on an Android TV or Fire TV device.
/// Uses device_info_plus to read Build.CHARACTERISTICS on Android.
Future<bool> _isAndroidTV() async {
  if (defaultTargetPlatform != TargetPlatform.android) return false;
  try {
    final info = await DeviceInfoPlugin().androidInfo;
    final characteristics = info.systemFeatures;
    // Android TV sets 'android.software.leanback' in system features.
    if (characteristics.contains('android.software.leanback')) return true;
    // Fallback: very wide screen (960dp+) treated as TV/large-screen.
    return false;
  } catch (_) {
    return false;
  }
}

class SoHoLinkApp extends StatelessWidget {
  final bool isTV;
  const SoHoLinkApp({super.key, this.isTV = false});

  @override
  Widget build(BuildContext context) {
    // Decide landing page: if the user has already saved a node URL, go
    // straight to the dashboard; otherwise show the setup wizard.
    final configured = SoHoLinkClient.instance.hasConfiguredUrl;

    return MaterialApp(
      title: 'SoHoLINK',
      debugShowCheckedModeBanner: false,
      theme: isTV ? AppTheme.tv : AppTheme.dark,
      home: configured ? HomePage(isTV: isTV) : const SetupPage(),
    );
  }
}
