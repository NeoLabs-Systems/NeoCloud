import 'device_session_stub.dart' if (dart.library.io) 'device_session_io.dart' as platform;

/// The server and device token a native app is connected with.
class DeviceConnection {
  const DeviceConnection({required this.server, required this.token, required this.username, this.accountId});

  final String server;
  final String token;
  final String username;

  /// The account's id on [server]; null for connections stored before it
  /// was kept, until the server is reached once.
  final int? accountId;

  /// Server and account together: what camera upload is bound to.
  String? get accountKey => accountId == null ? null : cameraAccountKey(server, accountId!);

  DeviceConnection withAccountId(int id) => DeviceConnection(server: server, token: token, username: username, accountId: id);
}

/// `<server>#<account id>`: one account on one server.
String cameraAccountKey(String server, int accountId) => '$server#$accountId';

/// Why a device login ended without a token: `ACCESS_DENIED`,
/// `EXPIRED_TOKEN`, `TIMEOUT`, `CANCELLED`, `NETWORK` or a server code.
class DeviceLoginFailure implements Exception {
  const DeviceLoginFailure(this.code, [this.message = '']);

  final String code;
  final String message;

  @override
  String toString() => 'DeviceLoginFailure($code)';
}

/// A sign-in waiting for approval in the web app (docs/clients.md §1).
abstract class PendingDeviceLogin {
  /// `ABCD-EFGH`, shown so the user can compare it in the browser.
  String get userCode;
  Uri get verificationUrl;

  /// Completes once the account approves; throws [DeviceLoginFailure].
  Future<DeviceConnection> get approved;

  void cancel();
}

/// Device login and the keychain entry that keeps its token. Only native
/// apps use it; the web app signs in with the session cookie.
abstract class DeviceSessions {
  /// Whether this platform signs in as a device at all.
  bool get supported;

  /// "Neo's Pixel" and "android", sent with the login request.
  String get deviceName;
  String get platformName;

  Future<DeviceConnection?> read();

  Future<void> write(DeviceConnection connection);

  Future<void> clear();

  /// The server the app was last connected to, kept after a disconnect.
  Future<String?> lastServer();

  Future<PendingDeviceLogin> start(Uri server);

  /// Redeems a QR pairing code shown by the web app (docs/clients.md §5);
  /// throws [DeviceLoginFailure] (`EXPIRED_TOKEN` for used or old codes).
  Future<DeviceConnection> pair(Uri server, String code);
}

DeviceSessions createDeviceSessions() => platform.createDeviceSessions();
