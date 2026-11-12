import '../api/backend_client.dart';
import 'device_session.dart';

/// Where a password sign-in stands: done with a device token, or waiting
/// for the second factor.
class PasswordLoginResult {
  const PasswordLoginResult.connected(DeviceConnection this.connection) : requiresTwoFactor = false;

  const PasswordLoginResult.twoFactor()
    : connection = null,
      requiresTwoFactor = true;

  final DeviceConnection? connection;
  final bool requiresTwoFactor;
}

/// The phone app's sign-in with the web's login form: username or email and
/// password (then the 2FA code), or registration, against the session
/// routes. The session cookie lives only in [client]'s jar while signing in;
/// it is swapped for a device token (POST /api/account/devices), then the
/// session is ended and the cookie dropped. From then on the app uses the
/// token like after the device-code or QR sign-in.
class PasswordDeviceLogin {
  PasswordDeviceLogin({required this.server, required this.client, required this.deviceName, required this.platform});

  /// `https://cloud.example.com`.
  final String server;

  /// A client of its own for [server], without a bearer token: the cookie
  /// jar of the sign-in.
  final BackendClient client;
  final String deviceName;

  /// `android` or `ios`.
  final String platform;

  /// Whether the server has an account yet and takes sign-ups
  /// (GET /api/auth/status); also proves it is a NeoCloud server.
  Future<({bool hasUser, bool registrationOpen})> status() async {
    final status = await client.authStatus();
    return (hasUser: status['hasUser'] == true, registrationOpen: status['registrationOpen'] != false);
  }

  Future<PasswordLoginResult> signIn(String username, String password) async => _finish(await client.login(username, password));

  Future<PasswordLoginResult> verifyCode(String code) async => _finish(await client.loginTwoFactor(code));

  Future<PasswordLoginResult> register({required String username, required String email, required String password}) async =>
      _finish(await client.register(username: username, email: email, password: password));

  Future<PasswordLoginResult> _finish(Map<String, dynamic> result) async {
    if (result['requiresTwoFactor'] == true) return const PasswordLoginResult.twoFactor();
    final user = (result['user'] as Map?)?.cast<String, dynamic>();
    if (user == null) throw const ApiException(500, '', code: 'UNEXPECTED_RESPONSE');
    return PasswordLoginResult.connected(await _exchange(user));
  }

  /// Session → device token; the session ends whatever happens.
  Future<DeviceConnection> _exchange(Map<String, dynamic> user) async {
    try {
      final token = await client.createDevice(deviceName, platform: platform);
      final id = user['id'];
      return DeviceConnection(
        server: server,
        token: token,
        username: '${user['username'] ?? ''}',
        accountId: id is num ? id.toInt() : int.tryParse('$id'),
      );
    } finally {
      await end();
    }
  }

  /// Signs the session out (if there is one) and forgets the cookie.
  Future<void> end() async {
    try {
      await client.logout();
    } catch (_) {
      // The session expires on its own; the cookie is gone either way.
    }
    client.forgetSession();
  }
}
