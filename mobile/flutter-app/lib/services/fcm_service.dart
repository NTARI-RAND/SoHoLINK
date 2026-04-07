import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import '../api/soholink_client.dart';

/// Top-level handler for FCM messages received while the app is in the
/// background or terminated. Must be a top-level function (not a closure).
@pragma('vm:entry-point')
Future<void> _firebaseBackgroundHandler(RemoteMessage message) async {
  // The WebSocketService will reconnect automatically when the app resumes.
  // Nothing else needed here for a data-only job_request message.
}

/// Singleton service that registers for FCM push notifications and posts
/// the device token to the SoHoLINK node so the server can wake this device
/// when a task is available and the WebSocket is disconnected.
class FcmService {
  FcmService._();
  static final FcmService instance = FcmService._();

  final _localNotifications = FlutterLocalNotificationsPlugin();

  Future<void> init() async {
    // Register the background handler (must be called before any other
    // Firebase Messaging method).
    FirebaseMessaging.onBackgroundMessage(_firebaseBackgroundHandler);

    // Request notification permission (required on Android 13+ and iOS).
    await FirebaseMessaging.instance.requestPermission(
      alert: true,
      badge: true,
      sound: true,
    );

    // Initialise local notifications for foreground message display.
    const androidSettings =
        AndroidInitializationSettings('@mipmap/ic_launcher');
    const iosSettings = DarwinInitializationSettings();
    await _localNotifications.initialize(
      const InitializationSettings(
          android: androidSettings, iOS: iosSettings),
    );

    // Obtain the FCM token and register it with the node.
    final token = await FirebaseMessaging.instance.getToken();
    if (token != null && SoHoLinkClient.instance.hasConfiguredUrl) {
      await _registerToken(token);
    }

    // Refresh token handler.
    FirebaseMessaging.instance.onTokenRefresh.listen(_registerToken);

    // Foreground message → show a local notification.
    FirebaseMessaging.onMessage.listen((message) {
      final notification = message.notification;
      if (notification != null) {
        _localNotifications.show(
          notification.hashCode,
          notification.title ?? 'SoHoLINK',
          notification.body ?? '',
          const NotificationDetails(
            android: AndroidNotificationDetails(
              'soholink_channel',
              'SoHoLINK Notifications',
              importance: Importance.high,
              priority: Priority.high,
            ),
          ),
        );
      }
      // Data-only job_request messages are handled by the WebSocket reconnect
      // logic in WebSocketService — no explicit action needed here.
    });

    // App opened from a notification tap → navigate to workloads (handled
    // by the caller if needed; nothing app-level to do here).
    FirebaseMessaging.onMessageOpenedApp.listen((_) {
      // Workloads page already subscribes to taskStream; new tasks will
      // appear naturally once the WebSocket reconnects.
    });
  }

  Future<void> _registerToken(String token) async {
    try {
      await SoHoLinkClient.instance.registerFcmToken(token);
    } catch (_) {
      // Non-fatal: the app still functions without FCM registration.
    }
  }
}
