part of 'main.dart';

/// The app's single root state, like NeoAgent's controller: who is signed
/// in, which page is open, the account's settings and the upload queue.
/// Pages that own their own data (a folder listing, the admin lists) load it
/// themselves through [client].
///
/// The web app signs in with a session cookie. The Android and iOS apps sign
/// in as a device ([usesDeviceLogin]): a bearer token approved in the web
/// app, which reaches files, transfers, shares and vaults but not the
/// account or settings routes, so their preferences are kept on the device.
class NeoCloudController extends ChangeNotifier {
  NeoCloudController({
    required this.client,
    required SharedPreferences preferences,
    DeviceSessions? deviceSessions,
    CameraUploads? cameraUploads,
    ShareIntake? shareIntake,
    Uri? launchUri,
  }) : _preferences = preferences,
       deviceSessions = deviceSessions ?? createDeviceSessions(),
       cameraUploads = cameraUploads ?? createCameraUploads(),
       shareIntake = shareIntake ?? createShareIntake() {
    client.onSessionExpired = _handleSessionExpired;
    uploads = UploadQueue(client: client, onFileUploaded: _handleFileUploaded);
    uploadJournal = UploadJournal(preferences, uploads);
    vaults = VaultSessions(client);
    _themeMode = _themeModeFromName(_preferences.getString(_themePreferenceKey));
    pendingDeviceCode = _deviceCodeFrom(launchUri);
    pendingPairCode = _pairCodeFrom(launchUri);
  }

  static const String _themePreferenceKey = 'app.themeMode';
  static const String _sectionPreferenceKey = 'app.section';
  static const String _deviceSettingsKey = 'device.settings';

  final BackendClient client;
  final SharedPreferences _preferences;
  late final UploadQueue uploads;

  /// Upload metadata kept across reloads, and the leave-page guard.
  late final UploadJournal uploadJournal;

  /// The floating "moved to trash · Undo" bar.
  final UndoCenter undo = UndoCenter();
  late final VaultSessions vaults;
  final DeviceSessions deviceSessions;
  final CameraUploads cameraUploads;
  final ShareIntake shareIntake;

  /// Native apps sign in as a device instead of with a password.
  bool get usesDeviceLogin => deviceSessions.supported;

  /// The server a native app is connected to (empty on the web).
  String connectedServer = '';

  /// From /app/device?code=…: a device waiting for approval. The approval
  /// page shows once the account is signed in.
  String? pendingDeviceCode;

  static String? _deviceCodeFrom(Uri? uri) {
    if (uri == null) return null;
    final segments = uri.pathSegments.where((segment) => segment.isNotEmpty).toList();
    if (segments.isEmpty || segments.last != 'device') return null;
    return (uri.queryParameters['code'] ?? '').trim().toUpperCase();
  }

  /// From /app/pair?code=…: the pairing link opened in a browser instead
  /// of the app. The page explains the app and hands the code to it.
  String? pendingPairCode;

  static String? _pairCodeFrom(Uri? uri) {
    if (uri == null) return null;
    final segments = uri.pathSegments.where((segment) => segment.isNotEmpty).toList();
    if (segments.isEmpty || segments.last != 'pair') return null;
    return (uri.queryParameters['code'] ?? '').trim();
  }

  /// Leaves the pairing page for the app (or its sign-in).
  void finishPairLanding() {
    pendingPairCode = null;
    replaceBrowserPath('/${Uri.base.pathSegments.where((s) => s.isNotEmpty && s != 'pair').map((s) => '$s/').join()}');
    notifyListeners();
  }

  /// Leaves the approval page for the app.
  void finishDeviceApproval() {
    pendingDeviceCode = null;
    final base = Uri.base;
    final segments = base.pathSegments.where((segment) => segment.isNotEmpty).toList();
    if (segments.isNotEmpty && segments.last == 'device') {
      replaceBrowserPath('/${segments.sublist(0, segments.length - 1).map((s) => '$s/').join()}');
    }
    notifyListeners();
  }

  bool isBooting = true;
  bool isAuthenticating = false;
  bool isAwaitingTwoFactor = false;
  bool hasUser = true;
  bool signupKeyRequired = false;
  AccountUser? user;
  UserSettings settings = const UserSettings();
  String? errorMessage;
  String? authErrorCode;

  AppSection selectedSection = AppSection.files;
  SettingsPage? settingsPage;

  /// Bumped whenever files change in a way open listings should reload for.
  final ValueNotifier<int> filesRevision = ValueNotifier<int>(0);

  /// The folder the Files page shows; the root when null.
  String? openFolderId;

  /// Whether the server converts Office documents for preview (LibreOffice).
  bool officePreview = false;

  /// Whether the admin turned full-text search on (`/api/capabilities`).
  bool fullTextSearch = false;

  void _applyCapabilities(Map<String, dynamic> caps) {
    officePreview = caps['officePreview'] == true;
    fullTextSearch = caps['fullTextSearch'] == true;
  }

  /// Thumbnails by file etag, so scrolling back doesn't fetch them again.
  final Map<String, Future<Uint8List?>> _thumbnails = <String, Future<Uint8List?>>{};

  Future<Uint8List?> thumbnailFor(FileNode node) {
    if (node.inVault) return Future<Uint8List?>.value();
    if (_thumbnails.length > 400) _thumbnails.remove(_thumbnails.keys.first);
    return _thumbnails.putIfAbsent(
      node.etag,
      () => client.thumbnailBytes(node.id).catchError((Object _) => null),
    );
  }

  ThemeMode _themeMode = ThemeMode.system;
  ThemeMode get themeMode => _themeMode;

  bool get isAuthenticated => user != null;
  bool get isAdmin => user?.isAdmin ?? false;
  String get accountLabel => user?.label ?? '';
  StorageUsage? get storage => user?.storage;

  ThemeMode _themeModeFromName(String? name) {
    switch (name) {
      case 'light':
        return ThemeMode.light;
      case 'dark':
        return ThemeMode.dark;
      default:
        return ThemeMode.system;
    }
  }

  /// Readable text for any error a call can throw.
  String friendlyError(Object error) {
    if (error is ApiException) {
      if (error.code == 'NETWORK') return appStrings.connectionError;
      if (error.code == 'VAULT_BOUNDARY') return appStrings.vaultBoundaryError;
      if (error.code == 'DISK_FULL' || error.statusCode == 507) return appStrings.securityDiskFull;
      if (error.code == 'SERVER_STOPPING') return appStrings.securityServerStopping;
      if (error.code == 'IMPORT_HOST_BLOCKED') return appStrings.importHostBlocked;
      if (error.message.isNotEmpty && !error.message.startsWith('HTTP ')) return error.message;
    }
    return appStrings.genericError;
  }

  Future<void> boot() async {
    final storedSection = _preferences.getString(_sectionPreferenceKey);
    selectedSection = AppSection.values.where((s) => s.name == storedSection).firstOrNull ?? AppSection.files;
    if (usesDeviceLogin) {
      await _bootDevice();
      return;
    }
    try {
      final status = await client.authStatus();
      hasUser = status['hasUser'] == true;
      signupKeyRequired = status['signupKeyRequired'] == true;
      if (status['authenticated'] == true && status['user'] is Map) {
        await _enterSession(AccountUser.fromJson((status['user'] as Map).cast<String, dynamic>()));
      }
    } catch (error) {
      errorMessage = friendlyError(error);
    } finally {
      isBooting = false;
      notifyListeners();
    }
  }

  // Device sign-in (Android and iOS)

  Future<void> _bootDevice() async {
    try {
      final connection = await deviceSessions.read();
      connectedServer = connection?.server ?? await deviceSessions.lastServer() ?? '';
      if (connection != null) await _enterDeviceSession(connection);
    } catch (error) {
      errorMessage = friendlyError(error);
    } finally {
      isBooting = false;
      notifyListeners();
    }
  }

  /// Signs in with a stored or just-approved device token. Offline, the app
  /// still opens with what it knows.
  Future<void> _enterDeviceSession(DeviceConnection connection) async {
    client
      ..baseUrl = connection.server
      ..bearerToken = connection.token;
    connectedServer = connection.server;
    settings = UserSettings.fromJson(_storedDeviceSettings());
    uploads.defaultConflict = settings.uploadConflict;
    var known = connection;
    try {
      user = _deviceUser(await client.deviceMe());
      final accountId = user!.id;
      if (accountId > 0 && connection.accountId != accountId) {
        known = connection.withAccountId(accountId);
        await deviceSessions.write(known);
      }
    } on ApiException catch (error) {
      if (error.statusCode == 401) {
        await deviceSessions.clear();
        await cameraUploads.signOut().catchError((Object _) {});
        client.bearerToken = null;
        errorMessage = appStrings.deviceRevokedNotice;
        return;
      }
      user = AccountUser(
        id: 0,
        username: connection.username,
        displayName: null,
        email: null,
        isAdmin: false,
        storage: const StorageUsage(usedBytes: 0, quotaBytes: -1, trashBytes: 0, versionBytes: 0),
      );
      errorMessage = friendlyError(error);
    }
    if (selectedSection == AppSection.admin) {
      selectedSection = AppSection.files;
    }
    unawaited(client.capabilities().then(_applyCapabilities).catchError((Object _) {}));
    unawaited(_startNativeServices(known));
  }

  AccountUser _deviceUser(Map<String, dynamic> me) {
    final account = (me['user'] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{};
    final quota = (me['quota'] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{};
    return AccountUser.fromJson(<String, dynamic>{
      ...account,
      'isAdmin': false,
      'storage': <String, dynamic>{'usedBytes': quota['used'], 'quotaBytes': quota['limit'] ?? -1},
    });
  }

  Map<String, dynamic> _storedDeviceSettings() {
    try {
      return (jsonDecode(_preferences.getString(_deviceSettingsKey) ?? '{}') as Map).cast<String, dynamic>();
    } catch (_) {
      return const <String, dynamic>{};
    }
  }

  Future<void> _startNativeServices(DeviceConnection connection) async {
    if (cameraUploads.supported) {
      // Camera upload runs only for the account it was turned on for.
      final account = connection.accountKey;
      if (account == null) {
        await cameraUploads.load();
      } else {
        await cameraUploads.signIn(account);
        unawaited(cameraUploads.runNow(client).catchError((Object _) {}));
      }
    }
    await shareIntake.start();
  }

  // The connect screen of native apps.
  PendingDeviceLogin? pendingLogin;
  bool isConnecting = false;

  /// Starts a device login against [serverInput]; the user then approves it
  /// in the browser that [openVerification] opens.
  Future<void> connect(String serverInput, {required Future<void> Function(Uri url) openVerification}) async {
    final server = normalizeBackendUrl(serverInput);
    final uri = Uri.tryParse(server);
    if (server.isEmpty || uri == null || !uri.hasScheme) {
      showInlineError(appStrings.connectInvalidServer);
      return;
    }
    await _signInDevice(server, () async {
      final login = await deviceSessions.start(uri);
      pendingLogin = login;
      notifyListeners();
      unawaited(openVerification(login.verificationUrl).catchError((Object _) {}));
      return login.approved;
    });
  }

  /// Signs in with a QR pairing code from the web app (docs/clients.md §5).
  Future<void> pair(PairingLinkData link) async {
    final server = normalizeBackendUrl(link.server.toString());
    await _signInDevice(server, () => deviceSessions.pair(Uri.parse(server), link.code));
  }

  /// Runs a device login and keeps its token the same way for both flows.
  Future<void> _signInDevice(String server, Future<DeviceConnection> Function() login) async {
    if (isConnecting) return;
    isConnecting = true;
    errorMessage = null;
    connectedServer = server;
    notifyListeners();
    try {
      final connection = await login();
      await deviceSessions.write(connection);
      await _rememberServer(this, connection.server);
      await _enterDeviceSession(connection);
    } on DeviceLoginFailure catch (failure) {
      errorMessage = _loginFailureText(failure);
    } catch (error) {
      errorMessage = friendlyError(error);
    } finally {
      pendingLogin = null;
      isConnecting = false;
      notifyListeners();
    }
  }

  String? _loginFailureText(DeviceLoginFailure failure) {
    switch (failure.code) {
      case 'CANCELLED':
        return null;
      case 'ACCESS_DENIED':
        return appStrings.connectDenied;
      case 'EXPIRED_TOKEN':
      case 'TIMEOUT':
        return appStrings.connectExpired;
      case 'NETWORK':
        return appStrings.connectionError;
      default:
        return failure.message.isEmpty ? appStrings.genericError : failure.message;
    }
  }

  void cancelConnect() => pendingLogin?.cancel();

  Future<void> _enterSession(AccountUser account) async {
    user = account;
    isAwaitingTwoFactor = false;
    errorMessage = null;
    authErrorCode = null;
    if (selectedSection == AppSection.admin && !account.isAdmin) {
      selectedSection = AppSection.files;
    }
    unawaited(client.capabilities().then(_applyCapabilities).catchError((Object _) {}));
    try {
      settings = await client.settings();
      uploads.defaultConflict = settings.uploadConflict;
      _themeMode = _themeModeFromName(settings.theme);
      await _preferences.setString(_themePreferenceKey, settings.theme);
      final language = AppLanguage.fromCode(settings.language);
      if (language != null && language != currentAppLanguage) await storeAppLanguage(language);
    } catch (_) {
      // Defaults are fine until the next load.
    }
  }

  void _handleSessionExpired() {
    if (user == null) return;
    user = null;
    openFolderId = null;
    vaults.lockAll();
    if (usesDeviceLogin) {
      // The token was revoked: forget it and ask to connect again.
      unawaited(deviceSessions.clear());
      unawaited(cameraUploads.signOut().catchError((Object _) {}));
      client.bearerToken = null;
      errorMessage = appStrings.deviceRevokedNotice;
    }
    notifyListeners();
  }

  void _handleFileUploaded(String parentId) {
    filesRevision.value += 1;
    unawaited(refreshAccount());
  }

  void notifyFilesChanged() {
    filesRevision.value += 1;
    unawaited(refreshAccount());
  }

  Future<void> refreshAccount() async {
    if (user == null) return;
    try {
      user = usesDeviceLogin ? _deviceUser(await client.deviceMe()) : await client.me();
      notifyListeners();
    } catch (_) {
      // Shown again on the next successful load.
    }
  }

  // Sign-in

  void clearInlineError() {
    if (errorMessage == null && authErrorCode == null) return;
    errorMessage = null;
    authErrorCode = null;
    notifyListeners();
  }

  void showInlineError(String message) {
    errorMessage = message;
    notifyListeners();
  }

  Future<void> _authenticate(Future<Map<String, dynamic>> Function() call) async {
    isAuthenticating = true;
    errorMessage = null;
    authErrorCode = null;
    notifyListeners();
    try {
      final result = await call();
      if (result['requiresTwoFactor'] == true) {
        isAwaitingTwoFactor = true;
      } else if (result['user'] is Map) {
        hasUser = true;
        await _enterSession(AccountUser.fromJson((result['user'] as Map).cast<String, dynamic>()));
      }
    } catch (error) {
      errorMessage = friendlyError(error);
      authErrorCode = error is ApiException ? error.code : null;
    } finally {
      isAuthenticating = false;
      notifyListeners();
    }
  }

  Future<void> login({required String username, required String password}) =>
      _authenticate(() => client.login(username, password));

  Future<void> completeTwoFactorLogin({required String code}) => _authenticate(() => client.loginTwoFactor(code));

  void cancelTwoFactorLogin() {
    isAwaitingTwoFactor = false;
    errorMessage = null;
    notifyListeners();
  }

  Future<void> register({
    required String username,
    required String email,
    required String password,
    String? signupKey,
  }) => _authenticate(() => client.register(username: username, email: email, password: password, signupKey: signupKey));

  Future<bool> checkSignupKey(String key) async {
    isAuthenticating = true;
    errorMessage = null;
    authErrorCode = null;
    notifyListeners();
    try {
      await client.checkSignupKey(key);
      return true;
    } catch (error) {
      errorMessage = friendlyError(error);
      authErrorCode = error is ApiException ? error.code : null;
      return false;
    } finally {
      isAuthenticating = false;
      notifyListeners();
    }
  }

  Future<void> logout() async {
    if (usesDeviceLogin) {
      // Disconnect: the token stays listed under Devices in the web app
      // until it is revoked there. Camera upload goes off with it.
      await cameraUploads.signOut().catchError((Object _) {});
      await deviceSessions.clear();
      client.bearerToken = null;
    } else {
      try {
        await client.logout();
      } catch (_) {
        // Signed out locally either way.
      }
    }
    user = null;
    openFolderId = null;
    settingsPage = null;
    vaults.lockAll();
    notifyListeners();
  }

  // Navigation

  void setSelectedSection(AppSection section) {
    if (section == AppSection.admin && !isAdmin) return;
    selectedSection = section;
    unawaited(_preferences.setString(_sectionPreferenceKey, section.name));
    notifyListeners();
  }

  void openFolder(String? folderId) {
    openFolderId = folderId;
    if (selectedSection != AppSection.files) {
      selectedSection = AppSection.files;
      unawaited(_preferences.setString(_sectionPreferenceKey, AppSection.files.name));
    }
    notifyListeners();
  }

  void setSettingsPage(SettingsPage? page) {
    settingsPage = page;
    notifyListeners();
  }

  // Settings

  bool isSavingSettings = false;
  String? settingsSaveError;

  Future<void> updateSettings(Map<String, Object?> patch) async {
    isSavingSettings = true;
    settingsSaveError = null;
    notifyListeners();
    try {
      if (usesDeviceLogin) {
        final merged = <String, dynamic>{..._storedDeviceSettings(), ...patch};
        await _preferences.setString(_deviceSettingsKey, jsonEncode(merged));
        settings = UserSettings.fromJson(merged);
      } else {
        settings = await client.updateSettings(patch);
      }
      uploads.defaultConflict = settings.uploadConflict;
    } catch (error) {
      settingsSaveError = friendlyError(error);
    } finally {
      isSavingSettings = false;
      notifyListeners();
    }
  }

  Future<void> setThemeMode(ThemeMode mode) async {
    _themeMode = mode;
    final name = mode == ThemeMode.system ? 'system' : mode.name;
    await _preferences.setString(_themePreferenceKey, name);
    notifyListeners();
    await updateSettings(<String, Object?>{'theme': name});
  }

  Future<void> setLanguage(AppLanguage language) async {
    await storeAppLanguage(language);
    notifyListeners();
    await updateSettings(<String, Object?>{'language': language.code});
  }

  // Content

  /// A file's content as the user sees it: vault files are decrypted.
  Future<Uint8List> fileContent(FileNode node) async {
    final data = await client.fileBytes(node.id);
    final vaultId = node.vaultId;
    return vaultId == null ? data : vaults.decrypt(vaultId, data);
  }

  /// Saves [node] where the user picks (native) or as a browser download.
  /// Vault files pass through the app to be decrypted.
  Future<void> downloadFile(FileNode node) async {
    markOpened(node);
    if (node.inVault) {
      await FilePicker.platform.saveFile(fileName: node.name, bytes: await fileContent(node));
      return;
    }
    await startDownload(client.downloadUri(node.id), fileName: node.name, headers: client.mediaHeaders);
  }

  /// Tells the server [node] was opened, for Quick Access. Best effort.
  void markOpened(FileNode node) {
    if (node.isFolder) return;
    unawaited(client.markOpened(node.id).catchError((Object _) {}));
  }

  Future<void> downloadZip(List<FileNode> nodes) =>
      startDownload(client.zipUri(nodes.map((n) => n.id).toList()), fileName: '${appStrings.appName}.zip', headers: client.mediaHeaders);

  /// Replaces [node]'s content with [bytes] as a new version. [baseEtag]
  /// makes the server refuse (412 ETAG_MISMATCH) if someone changed the file
  /// meanwhile. Resolves to the updated node.
  Future<FileNode> saveFileContent(FileNode node, Uint8List bytes, {String? baseEtag}) async {
    final parentId = node.parentId ?? 'root';
    final vaultId = node.vaultId;
    final FileNode stored;
    if (vaultId != null) {
      final data = await vaults.encrypt(vaultId, bytes);
      stored = await client.uploadSmall(parentId, node.cipherName ?? node.name, data, conflict: 'replace', ifMatch: baseEtag);
    } else {
      stored = await client.uploadSmall(parentId, node.name, bytes, conflict: 'replace', ifMatch: baseEtag);
    }
    notifyFilesChanged();
    return node.updatedFrom(stored);
  }
}
