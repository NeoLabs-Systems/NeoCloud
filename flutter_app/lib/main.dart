import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;

import 'package:desktop_drop/desktop_drop.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_web_plugins/url_strategy.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:intl/intl.dart';
import 'package:neocloud_background/neocloud_background.dart';
import 'package:pdfrx/pdfrx.dart';
import 'package:qr_flutter/qr_flutter.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:url_launcher/url_launcher.dart';
import 'package:video_player/video_player.dart';

import 'src/api/backend_client.dart';
import 'src/api/models.dart';
import 'src/backend_url.dart';
import 'src/device/device_session.dart';
import 'src/files/batch.dart';
import 'src/files/file_format.dart';
import 'src/files/file_saver.dart';
import 'src/files/selection_model.dart';
import 'src/files/undo.dart';
import 'src/files/upload_journal.dart';
import 'src/files/upload_queue.dart';
import 'src/imports/import_progress.dart';
import 'src/l10n/app_language.dart';
import 'src/l10n/widget_localizations.dart';
import 'src/live/file_change_feed.dart';
import 'src/live/live_connection.dart';
import 'src/live/live_socket.dart';
import 'src/live/socket_io_frame.dart';
import 'src/mobile/background_reliability.dart';
import 'src/mobile/camera_upload.dart';
import 'src/mobile/qr_scanner.dart';
import 'src/mobile/share_intake.dart';
import 'src/navigation/app_route.dart';
import 'src/pairing/pairing_link.dart';
import 'src/search/snippet.dart';
import 'src/security/password_strength.dart';
import 'src/security/bip39_words.dart';
import 'src/security/passphrase_strength.dart';
import 'src/security/recovery_words.dart';
import 'src/security/webauthn_client.dart';
import 'src/sharing/share_draft.dart';
import 'src/sharing/share_text.dart';
import 'src/theme/palette.dart';
import 'src/vault/vault_sessions.dart';
import 'src/vault/vault_keys.dart';
import 'src/web/browser_location.dart';

part 'main_admin.dart';
part 'main_admin_backups.dart';
part 'main_admin_safety.dart';
part 'main_admin_search.dart';
part 'main_auth.dart';
part 'main_background_permissions.dart';
part 'main_camera_upload.dart';
part 'main_camera_status.dart';
part 'main_command_palette.dart';
part 'main_connect.dart';
part 'main_connect_qr.dart';
part 'main_controller.dart';
part 'main_device_approval.dart';
part 'main_editor.dart';
part 'main_files.dart';
part 'main_files_actions.dart';
part 'main_files_dialogs.dart';
part 'main_files_items.dart';
part 'main_files_keyboard.dart';
part 'main_files_pages.dart';
part 'main_files_placeholders.dart';
part 'main_files_search.dart';
part 'main_files_search_hits.dart';
part 'main_files_view.dart';
part 'main_files_widgets.dart';
part 'main_native_runtime.dart';
part 'main_import.dart';
part 'main_import_jobs.dart';
part 'main_navigation.dart';
part 'main_pair_landing.dart';
part 'main_pairing_dialog.dart';
part 'main_router.dart';
part 'main_password_strength.dart';
part 'main_settings.dart';
part 'main_settings_devices.dart';
part 'main_settings_pages.dart';
part 'main_settings_passkeys.dart';
part 'main_settings_webdav.dart';
part 'main_share_links.dart';
part 'main_share_form.dart';
part 'main_share_people.dart';
part 'main_shared.dart';
part 'main_shared_links_page.dart';
part 'main_shared_with_me.dart';
part 'main_mobile_shell.dart';
part 'main_shell.dart';
part 'main_spacing.dart';
part 'main_storage.dart';
part 'main_theme.dart';
part 'main_undo.dart';
part 'main_upload_panel.dart';
part 'main_vault.dart';
part 'main_vault_recovery.dart';
part 'main_vault_unlock.dart';
part 'main_vault_widgets.dart';
part 'main_viewer.dart';

/// Where the API lives. Empty: the same origin that served the web app
/// (production, under /app). Set for development, e.g.
/// --dart-define=NEOCLOUD_BACKEND_URL=http://localhost:3344
const String _configuredBackendUrl = String.fromEnvironment('NEOCLOUD_BACKEND_URL');

Future<void> main() async {
  // Real paths (/app/files/…) instead of #/ addresses; a no-op off the web.
  usePathUrlStrategy();
  WidgetsFlutterBinding.ensureInitialized();
  await ensureAppLanguageLoaded();
  final preferences = await SharedPreferences.getInstance();
  final controller = NeoCloudController(
    client: BackendClient(baseUrl: _configuredBackendUrl),
    preferences: preferences,
    launchUri: kIsWeb ? Uri.base : null,
  );
  runApp(NeoCloudApp(controller: controller));
  unawaited(controller.boot());
}

/// Lets code outside the widget tree (files shared from other apps) show
/// dialogs.
final GlobalKey<NavigatorState> _navigatorKey = GlobalKey<NavigatorState>();

class NeoCloudApp extends StatefulWidget {
  const NeoCloudApp({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<NeoCloudApp> createState() => _NeoCloudAppState();
}

class _NeoCloudAppState extends State<NeoCloudApp> {
  late final _AppRouterDelegate _routerDelegate = _AppRouterDelegate(widget.controller);
  late final _AppRouteProvider _routeProvider = _AppRouteProvider(widget.controller);

  @override
  void dispose() {
    _routerDelegate.dispose();
    _routeProvider.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    return AnimatedBuilder(
      animation: controller,
      builder: (context, _) {
        _appThemeMode = controller.themeMode;
        return MaterialApp.router(
          key: ValueKey<String>('${controller.themeMode.name}-${currentAppLanguage.code}'),
          title: 'NeoCloud',
          debugShowCheckedModeBanner: false,
          themeMode: controller.themeMode,
          theme: _buildNeoCloudTheme(lightPalette, Brightness.light),
          darkTheme: _buildNeoCloudTheme(darkPalette, Brightness.dark),
          locale: currentAppLanguage.locale,
          supportedLocales: AppLanguage.values.map((language) => language.locale),
          localizationsDelegates: appLocalizationsDelegates,
          routerDelegate: _routerDelegate,
          routeInformationParser: const _AppRouteParser(),
          routeInformationProvider: _routeProvider,
        );
      },
    );
  }
}

/// Picks the screen from the controller's state, like NeoAgent's root.
class NeoCloudRoot extends StatelessWidget {
  const NeoCloudRoot({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    if (controller.isBooting) return const SplashView();
    if (controller.pendingPairCode != null && !controller.usesDeviceLogin) return PairLandingView(controller: controller);
    if (!controller.isAuthenticated) {
      return controller.usesDeviceLogin ? ConnectView(controller: controller) : AuthView(controller: controller);
    }
    if (controller.pendingDeviceCode != null) return DeviceApprovalView(controller: controller);
    return HomeView(controller: controller);
  }
}
