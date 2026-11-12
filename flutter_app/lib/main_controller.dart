part of 'main.dart';

/// The app's single root state, like NeoAgent's controller: who is signed
/// in, which page is open, the account's settings and the upload queue.
/// Pages that own their own data (a folder listing, the admin lists) load it
/// themselves through [client].
///
/// The web app signs in with a session cookie. The Android and iOS apps sign
/// in as a device ([usesDeviceLogin]): a bearer token from QR pairing, the
/// device login or the password form. The phone is a full client, like
/// NeoAgent's: the token reaches the account, settings and admin routes as
/// a session does. What stays phone-specific is signing out (disconnect
/// forgets the token), live updates (polled, no socket) and security keys
/// (added in a browser).
class NeoCloudController extends ChangeNotifier {
  NeoCloudController({
    required this.client,
    required SharedPreferences preferences,
    DeviceSessions? deviceSessions,
    CameraUploads? cameraUploads,
    ShareIntake? shareIntake,
    AiApi? ai,
    PhotoLibrary? photoLibrary,
    Uri? launchUri,
  }) : _preferences = preferences,
       ai = ai ?? AiApi(client),
       photoLibrary = photoLibrary ?? PhotoLibrary(client),
       deviceSessions = deviceSessions ?? createDeviceSessions(),
       cameraUploads = cameraUploads ?? createCameraUploads(),
       shareIntake = shareIntake ?? createShareIntake() {
    client.onSessionExpired = _handleSessionExpired;
    uploads = UploadQueue(client: client, onFileUploaded: _handleFileUploaded);
    uploadJournal = UploadJournal(preferences, uploads);
    folderViews = FolderViews(preferences: preferences, save: (patch) => client.updateSettings(<String, Object?>{'folder_views': patch}));
    vaults = VaultSessions(client);
    aiReview = AiReviewState(this.ai)..addListener(notifyListeners);
    liveUpdates = LiveFileUpdates(
      feed: fileChanges,
      fetchChanges: client.syncChanges,
      openSocket: usesDeviceLogin ? null : liveSocketOpener(),
      socketUrl: () => socketIoUri(client.uri('/')),
      onSharesChanged: _handleSharesChanged,
      onAiSuggestions: (counts) => aiReview.applyCounts(AiSuggestionCounts.fromJson(counts)),
      onPolled: _handlePolled,
    );
    fileChanges.addListener((_) => unawaited(refreshAccount()));
    _themeMode = _themeModeFromName(_preferences.getString(_themePreferenceKey));
    pendingDeviceCode = _deviceCodeFrom(launchUri);
    pendingPairCode = _pairCodeFrom(launchUri);
  }

  static const String _themePreferenceKey = 'app.themeMode';
  /// The last page, as an [AppRoute] location (`files/<id>`,
  /// `settings/security` …); older versions stored the bare section name,
  /// which parses the same way.
  static const String _routePreferenceKey = 'app.section';

  final BackendClient client;

  /// The AI calls (Settings › AI, Admin › AI); a fake in tests.
  final AiApi ai;

  /// Whether AI runs for the account, the review counts and the pending
  /// suggestions the file list marks.
  late final AiReviewState aiReview;
  final SharedPreferences _preferences;
  late final UploadQueue uploads;

  /// Sort and view per folder and page, kept on the device and the server.
  late final FolderViews folderViews;

  /// Upload metadata kept across reloads, and the leave-page guard.
  late final UploadJournal uploadJournal;

  /// The floating "moved to trash · Undo" bar.
  final UndoCenter undo = UndoCenter();

  /// The Photos tab's timeline and thumbnails, kept across tab switches; a
  /// fake in tests.
  final PhotoLibrary photoLibrary;

  /// Changes made elsewhere (another tab, a device, WebDAV, the sync
  /// client), batched; pages refresh what they show from it.
  final FileChangeFeed fileChanges = FileChangeFeed();
  late final LiveFileUpdates liveUpdates;
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
    _replaceRoute = true;
    replaceBrowserPath('/${Uri.base.pathSegments.where((s) => s.isNotEmpty && s != 'pair').map((s) => '$s/').join()}');
    notifyListeners();
  }

  /// Leaves the approval page for the app.
  void finishDeviceApproval() {
    pendingDeviceCode = null;
    _replaceRoute = true;
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
  /// False once the server has an account and sign-up is turned off.
  bool registrationOpen = true;

  /// The QR code on the web sign-in card, which a signed-in phone approves.
  late final QrLoginFlow qrLogin = QrLoginFlow(
    client: client,
    onChanged: notifyListeners,
    onSignedIn: _completeQrLogin,
    describeError: friendlyError,
  );

  Future<void> _completeQrLogin(Map<String, dynamic> result) async {
    final account = result['user'];
    if (account is! Map) return;
    hasUser = true;
    await _enterSession(AccountUser.fromJson(account.cast<String, dynamic>()));
    notifyListeners();
  }

  /// The browser's security key API; replaced in tests.
  WebAuthnClient webAuthn = createWebAuthnClient();

  /// Only the web app can sign in with a security key or passkey; the
  /// phone apps would need the native credential APIs.
  bool get supportsSecurityKeys => !usesDeviceLogin && webAuthn.isSupported;
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

  /// The shared folder open under Shared with me; the list of shares when null.
  String? openSharedFolderId;

  /// What other accounts share with this one (`GET /api/shared-with-me`).
  List<SharedWithMe> sharedWithMe = const <SharedWithMe>[];

  /// The share a listing belongs to, by its first breadcrumb (the shared
  /// folder itself); null for the account's own files.
  SharedWithMe? shareFor(String? rootId) => sharedWithMe.where((share) => share.node.id == rootId).firstOrNull;

  Future<void> refreshSharedWithMe() async {
    if (user == null) return;
    try {
      sharedWithMe = await client.sharedWithMe();
      notifyListeners();
    } catch (_) {
      // Kept as it was until the next load.
    }
  }

  /// Someone added, changed or removed a share with this account, or a
  /// member left one of its own: the list and the indicators change.
  void _handleSharesChanged() {
    unawaited(refreshSharedWithMe());
    filesRevision.value += 1;
  }

  /// Without the socket, nobody says when a shared folder changes (it is in
  /// the owner's journal, not this account's): refresh it on every poll.
  void _handlePolled() {
    unawaited(aiReview.poll());
    if (selectedSection != AppSection.shared) return;
    unawaited(refreshSharedWithMe());
    final open = openSharedFolderId;
    if (open != null) fileChanges.addFolders(<String>[open]);
  }

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
    if (error is WebAuthnException) return error.message;
    if (error is ApiException) {
      if (error.code == 'NETWORK') return appStrings.connectionError;
      final signIn = _signInError(error);
      if (signIn != null) return signIn;
      if (error.code == 'APPROVAL_NEEDS_SESSION') return appStrings.approvalNeedsServerUpdate;
      if (error.code == 'SERVER_UPDATE_NEEDED') return appStrings.serverUpdateNeeded;
      if (error.code == 'PHONE_REQUIRED') return appStrings.approvalNeedsPhone;
      if (error.code == 'VAULT_BOUNDARY') return appStrings.vaultBoundaryError;
      final sharing = _sharingError(error.code);
      if (sharing != null) return sharing;
      if (error.code == 'DISK_FULL' || error.statusCode == 507) return appStrings.securityDiskFull;
      if (error.code == 'SERVER_STOPPING') return appStrings.securityServerStopping;
      if (error.code == 'IMPORT_HOST_BLOCKED') return appStrings.importHostBlocked;
      if (error.message.isNotEmpty && !error.message.startsWith('HTTP ')) return error.message;
    }
    return appStrings.genericError;
  }

  /// Sign-in refusals in the app's language: wrong password or code, and
  /// the throttles (per account with a wait, per address without).
  String? _signInError(ApiException error) {
    switch (error.code) {
      case 'INVALID_CREDENTIALS':
        return appStrings.signInInvalidCredentials;
      case 'INVALID_CODE':
        return appStrings.signInInvalidCode;
      case 'LOGIN_LOCKED':
        final wait = error.retryAfter;
        if (wait == null) return appStrings.signInRateLimited;
        return wait.inSeconds >= 90
            ? appStrings.signInLockedMinutes((wait.inSeconds / 60).ceil())
            : appStrings.signInLockedSeconds(math.max(1, wait.inSeconds));
      case 'RATE_LIMITED':
        return appStrings.signInRateLimited;
      default:
        return null;
    }
  }

  /// The sharing errors in the app's language (the server's text is English).
  String? _sharingError(String? code) {
    switch (code) {
      case 'RECIPIENT_NOT_FOUND':
        return appStrings.memberNotFound;
      case 'ALREADY_MEMBER':
        return appStrings.memberAlreadyAdded;
      case 'SHARE_READ_ONLY':
        return appStrings.sharedReadOnlyError;
      case 'SHARE_BOUNDARY':
        return appStrings.sharedBoundaryError;
      case 'SHARE_ROOT_IMMUTABLE':
        return appStrings.sharedRootError;
      case 'ZIP_MIXED_OWNERS':
        return appStrings.sharedZipMixedError;
      default:
        return null;
    }
  }

  Future<void> boot() async {
    if (usesDeviceLogin) {
      await _bootDevice();
      return;
    }
    try {
      final status = await client.authStatus();
      hasUser = status['hasUser'] == true;
      registrationOpen = status['registrationOpen'] != false;
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
  Future<void> _enterDeviceSession(DeviceConnection connection, {bool firstSignIn = false}) async {
    client
      ..baseUrl = connection.server
      ..bearerToken = connection.token;
    connectedServer = connection.server;
    var known = connection;
    try {
      user = await _loadDeviceUser();
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
    _enterRoute();
    liveUpdates.start();
    unawaited(refreshSharedWithMe());
    unawaited(client.capabilities().then(_applyCapabilities).catchError((Object _) {}));
    unawaited(_loadSettings());
    unawaited(aiReview.refresh());
    unawaited(_startNativeServices(known).then((_) {
      if (!firstSignIn || !cameraUploads.supported || user == null) return;
      offerCameraUpload = true;
      notifyListeners();
    }).catchError((Object _) {}));
  }

  /// Set once camera upload is ready after a fresh connect: the app then
  /// offers it once, like NeoAgent's first-run permission prompt.
  bool offerCameraUpload = false;

  /// The phone's account, from `/api/account` like the web's. A server from
  /// before full phone access refuses that (`SERVER_UPDATE_NEEDED`); its
  /// `/api/devices/me` still names the account, admin flag included.
  Future<AccountUser> _loadDeviceUser() async {
    try {
      return await client.accountUser();
    } on ApiException catch (error) {
      if (error.code != 'SERVER_UPDATE_NEEDED') rethrow;
      return _deviceUser(await client.deviceMe());
    }
  }

  AccountUser _deviceUser(Map<String, dynamic> me) {
    final account = (me['user'] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{};
    final quota = (me['quota'] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{};
    return AccountUser.fromJson(<String, dynamic>{
      ...account,
      'storage': <String, dynamic>{'usedBytes': quota['used'], 'quotaBytes': quota['limit'] ?? -1},
    });
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
      await _enterDeviceSession(connection, firstSignIn: true);
      // Signed in another way while the login card was up.
      final abandoned = passwordLogin;
      passwordLogin = null;
      isAwaitingTwoFactor = false;
      if (abandoned != null) unawaited(abandoned.end());
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
    qrLogin.clear();
    user = account;
    folderViews.open('${account.id}');
    isAwaitingTwoFactor = false;
    errorMessage = null;
    authErrorCode = null;
    _enterRoute();
    liveUpdates.start();
    unawaited(refreshSharedWithMe());
    unawaited(client.capabilities().then(_applyCapabilities).catchError((Object _) {}));
    await _loadSettings();
    unawaited(aiReview.refresh());
  }

  /// The account's settings, on the web and the phone alike.
  Future<void> _loadSettings() async {
    try {
      settings = await client.settings();
      folderViews.reconcile(settings.folderViews);
      uploads.defaultConflict = settings.uploadConflict;
      _themeMode = _themeModeFromName(settings.theme);
      await _preferences.setString(_themePreferenceKey, settings.theme);
      final language = AppLanguage.fromCode(settings.language);
      if (language != null && language != currentAppLanguage) await storeAppLanguage(language);
    } catch (_) {
      // Defaults are fine until the next load.
    }
  }

  void _stopLiveUpdates() {
    liveUpdates.stop();
    fileChanges.clear();
  }

  void _handleSessionExpired() {
    if (user == null) return;
    user = null;
    folderViews.close();
    openFolderId = null;
    openSharedFolderId = null;
    sharedWithMe = const <SharedWithMe>[];
    vaults.lockAll();
    aiReview.clear();
    _stopLiveUpdates();
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
      user = usesDeviceLogin ? await _loadDeviceUser() : await client.me();
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

  Future<void> login({required String username, required String password}) => usesDeviceLogin
      ? _authenticateDevice((login) => login.signIn(username, password))
      : _authenticate(() => client.login(username, password));

  Future<void> completeTwoFactorLogin({required String code}) => usesDeviceLogin
      ? _authenticateDevice((login) => login.verifyCode(code))
      : _authenticate(() => client.loginTwoFactor(code));

  void cancelTwoFactorLogin() {
    isAwaitingTwoFactor = false;
    errorMessage = null;
    // The half-finished session of a phone's sign-in goes with it.
    unawaited(passwordLogin?.end());
    notifyListeners();
  }

  Future<void> register({
    required String username,
    required String email,
    required String password,
  }) => usesDeviceLogin
      ? _authenticateDevice((login) => login.register(username: username, email: email, password: password))
      : _authenticate(() => client.register(username: username, email: email, password: password));

  // The phone's sign-in with the login form (src/device/password_login.dart).

  /// Set once the connect screen has a server: the app then shows the web's
  /// login card for it.
  PasswordDeviceLogin? passwordLogin;

  /// Checks [serverInput] is a NeoCloud server and opens its login card.
  Future<void> chooseServer(String serverInput) async {
    final server = normalizeBackendUrl(serverInput);
    final uri = Uri.tryParse(server);
    if (server.isEmpty || uri == null || !uri.hasScheme) {
      showInlineError(appStrings.connectInvalidServer);
      return;
    }
    if (isConnecting) return;
    isConnecting = true;
    errorMessage = null;
    authErrorCode = null;
    connectedServer = server;
    notifyListeners();
    final login = PasswordDeviceLogin(
      server: server,
      client: BackendClient(baseUrl: server),
      deviceName: deviceSessions.deviceName,
      platform: deviceSessions.platformName,
    );
    try {
      final status = await login.status();
      hasUser = status.hasUser;
      registrationOpen = status.registrationOpen;
      isAwaitingTwoFactor = false;
      passwordLogin = login;
    } catch (error) {
      errorMessage = friendlyError(error);
    } finally {
      isConnecting = false;
      notifyListeners();
    }
  }

  /// Back from the login card to the server field.
  void leavePasswordLogin() {
    final login = passwordLogin;
    passwordLogin = null;
    isAwaitingTwoFactor = false;
    errorMessage = null;
    authErrorCode = null;
    if (login != null) unawaited(login.end());
    notifyListeners();
  }

  Future<void> _authenticateDevice(Future<PasswordLoginResult> Function(PasswordDeviceLogin login) step) async {
    final login = passwordLogin;
    if (login == null || isAuthenticating) return;
    isAuthenticating = true;
    errorMessage = null;
    authErrorCode = null;
    notifyListeners();
    try {
      final result = await step(login);
      final connection = result.connection;
      if (connection == null) {
        isAwaitingTwoFactor = result.requiresTwoFactor;
        return;
      }
      passwordLogin = null;
      isAwaitingTwoFactor = false;
      await deviceSessions.write(connection);
      await _rememberServer(this, connection.server);
      await _enterDeviceSession(connection, firstSignIn: true);
    } catch (error) {
      errorMessage = friendlyError(error);
      authErrorCode = error is ApiException ? error.code : null;
    } finally {
      isAuthenticating = false;
      notifyListeners();
    }
  }

  /// Signs in with a security key or passkey, like NeoAgent: the server may
  /// still ask for the 2FA code when the key only confirmed presence.
  /// Dismissing the browser's prompt is not an error.
  Future<void> signInWithSecurityKey({String? username}) => _authenticate(() async {
    final begin = await client.beginSecurityKeyLogin(username: username?.trim());
    final options = begin['options'];
    if (options is! Map) throw WebAuthnException(appStrings.passkeySignInFailed);
    final Map<String, dynamic> assertion;
    try {
      assertion = await webAuthn.getAssertion(options.cast<String, dynamic>());
    } on WebAuthnException catch (error) {
      if (error.cancelled) return const <String, dynamic>{};
      rethrow;
    }
    return client.completeSecurityKeyLogin(assertion);
  });

  Future<void> logout() async {
    await folderViews.flush();
    folderViews.close();
    if (usesDeviceLogin) {
      // Disconnect: the phone forgets its token, which stays listed under
      // Devices until it is revoked there. Camera upload goes off with it.
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
    openSharedFolderId = null;
    sharedWithMe = const <SharedWithMe>[];
    settingsPage = null;
    vaults.lockAll();
    aiReview.clear();
    _stopLiveUpdates();
    notifyListeners();
  }

  // Navigation

  /// The page the address bar and the preferences show; null while nobody
  /// is signed in or another screen (device approval, pairing) is up, so a
  /// deep link survives the sign-in.
  AppRoute? get currentRoute {
    if (isBooting || !isAuthenticated || pendingDeviceCode != null || pendingPairCode != null) return null;
    return _routeOfPage();
  }

  AppRoute _routeOfPage() {
    switch (selectedSection) {
      case AppSection.files:
        return AppRoute(AppSection.files.name, openFolderId);
      case AppSection.shared:
        return AppRoute(AppSection.shared.name, openSharedFolderId);
      case AppSection.settings:
        final page = settingsPage;
        return AppRoute(AppSection.settings.name, page == null ? null : AppRoute.slug(page.name));
      case AppSection.admin:
        return AppRoute(AppSection.admin.name, _adminTab == _AdminTab.users ? null : _adminTab.name);
      case AppSection.photos:
      case AppSection.favorites:
      case AppSection.recent:
      case AppSection.trash:
      case AppSection.review:
        return AppRoute(selectedSection.name);
    }
  }

  /// A page asked for before signing in (a deep link), opened after it.
  AppRoute? _requestedRoute;

  /// Whether the next address replaces the current history entry instead
  /// of adding one.
  bool _replaceRoute = false;

  bool _takeRouteReplacement() {
    final replace = _replaceRoute;
    _replaceRoute = false;
    return replace;
  }

  /// Opens [route] from the address bar: at start ([initial]) or from the
  /// browser's back and forward buttons ([fromBrowser]).
  void openRoute(AppRoute route, {bool initial = false, bool fromBrowser = false}) {
    if (!isAuthenticated) {
      if (!route.isNone) _requestedRoute = route;
      return;
    }
    if (initial && route.isNone) return;
    if (fromBrowser) _replaceRoute = true;
    _applyRoute(route.isNone ? AppRoute(AppSection.files.name) : route);
    _storeRoute();
    notifyListeners();
  }

  /// After signing in: the deep link, else the page last open on this device.
  void _enterRoute() {
    final requested = _requestedRoute;
    _requestedRoute = null;
    _applyRoute(requested ?? AppRoute.parse(_preferences.getString(_routePreferenceKey) ?? ''));
    _replaceRoute = true;
  }

  void _applyRoute(AppRoute route) {
    var section = AppSection.values.where((candidate) => candidate.name == route.section).firstOrNull ?? AppSection.files;
    if (section == AppSection.admin && !isAdmin) section = AppSection.files;
    final detail = route.section == section.name ? route.detail : null;
    switch (section) {
      case AppSection.files:
        openFolderId = detail;
      case AppSection.shared:
        openSharedFolderId = detail;
      case AppSection.settings:
        settingsPage = SettingsPage.values.where((page) => AppRoute.slug(page.name) == detail).firstOrNull;
      case AppSection.admin:
        _adminTab = _AdminTab.values.where((tab) => tab.name == detail).firstOrNull ?? _AdminTab.users;
      case AppSection.photos:
      case AppSection.favorites:
      case AppSection.recent:
      case AppSection.trash:
      case AppSection.review:
        break;
    }
    selectedSection = section;
  }

  /// Native apps have no address bar; they reopen the page kept here.
  void _storeRoute() {
    final route = _routeOfPage();
    unawaited(_preferences.setString(_routePreferenceKey, route.location.substring(1)));
  }

  void setSelectedSection(AppSection section) {
    if (section == AppSection.admin && !isAdmin) return;
    selectedSection = section;
    _storeRoute();
    notifyListeners();
  }

  /// Opens [folderId] in Files (the root when null). [replace] swaps the
  /// history entry, for a folder that turned out to be gone.
  void openFolder(String? folderId, {bool replace = false}) {
    openFolderId = folderId;
    selectedSection = AppSection.files;
    if (replace) _replaceRoute = true;
    _storeRoute();
    notifyListeners();
  }

  /// Opens [folderId] under Shared with me (the list of shares when null).
  /// [replace] swaps the history entry, for a share that is gone.
  void openSharedFolder(String? folderId, {bool replace = false}) {
    openSharedFolderId = folderId;
    selectedSection = AppSection.shared;
    if (replace) _replaceRoute = true;
    if (folderId == null) unawaited(refreshSharedWithMe());
    _storeRoute();
    notifyListeners();
  }

  void setSettingsPage(SettingsPage? page) {
    settingsPage = page;
    _storeRoute();
    notifyListeners();
  }

  /// The open Admin tab; part of the address (/app/admin/backups).
  _AdminTab _adminTab = _AdminTab.users;

  void _openAdminTab(_AdminTab tab) {
    _adminTab = tab;
    _storeRoute();
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
      settings = await client.updateSettings(patch);
      uploads.defaultConflict = settings.uploadConflict;
      // Opting in or out, or a feature switch, changes what AI shows.
      if (patch.keys.any((key) => key.startsWith('ai_'))) unawaited(aiReview.refresh());
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
