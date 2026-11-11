import 'device_session.dart';

DeviceSessions createDeviceSessions() => _NoDeviceSessions();

/// The web app has no device login; it signs in with the session cookie.
class _NoDeviceSessions implements DeviceSessions {
  @override
  bool get supported => false;

  @override
  String get deviceName => '';

  @override
  String get platformName => 'web';

  @override
  Future<DeviceConnection?> read() async => null;

  @override
  Future<void> write(DeviceConnection connection) async {}

  @override
  Future<void> clear() async {}

  @override
  Future<String?> lastServer() async => null;

  @override
  Future<PendingDeviceLogin> start(Uri server) => throw UnsupportedError('Device login needs a native app.');

  @override
  Future<DeviceConnection> pair(Uri server, String code) => throw UnsupportedError('Pairing needs a native app.');
}
