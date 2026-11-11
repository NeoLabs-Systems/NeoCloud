import 'dart:async';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:neocloud_sync/neocloud_sync.dart';

import 'device_session.dart';

DeviceSessions createDeviceSessions() => IoDeviceSessions();

const String _serverKey = 'neocloud.server';
const String _tokenKey = 'neocloud.token';
const String _usernameKey = 'neocloud.username';
const String _accountIdKey = 'neocloud.accountId';

/// The OS keychain (iOS Keychain, Android Keystore).
const FlutterSecureStorage _storage = FlutterSecureStorage();

class IoDeviceSessions implements DeviceSessions {
  @override
  bool get supported => true;

  @override
  String get deviceName {
    final host = Platform.localHostname.replaceAll(RegExp(r'\.local$'), '');
    return host.isEmpty ? 'NeoCloud ${Platform.operatingSystem}' : host;
  }

  @override
  String get platformName => Platform.operatingSystem;

  @override
  Future<DeviceConnection?> read() async {
    final values = await _storage.readAll();
    final server = values[_serverKey];
    final token = values[_tokenKey];
    if (server == null || token == null || token.isEmpty) return null;
    return DeviceConnection(
      server: server,
      token: token,
      username: values[_usernameKey] ?? '',
      accountId: int.tryParse(values[_accountIdKey] ?? ''),
    );
  }

  @override
  Future<void> write(DeviceConnection connection) async {
    await _storage.write(key: _serverKey, value: connection.server);
    await _storage.write(key: _tokenKey, value: connection.token);
    await _storage.write(key: _usernameKey, value: connection.username);
    final accountId = connection.accountId;
    if (accountId == null) {
      await _storage.delete(key: _accountIdKey);
    } else {
      await _storage.write(key: _accountIdKey, value: '$accountId');
    }
  }

  @override
  Future<void> clear() async {
    await _storage.delete(key: _tokenKey);
    await _storage.delete(key: _usernameKey);
    await _storage.delete(key: _accountIdKey);
  }

  @override
  Future<String?> lastServer() => _storage.read(key: _serverKey);

  @override
  Future<PendingDeviceLogin> start(Uri server) async {
    try {
      final authorization = await DeviceLogin.start(server, name: deviceName, platform: platformName);
      return _PendingLogin(server, authorization);
    } on NeoCloudApiException catch (error) {
      throw DeviceLoginFailure(error.code, error.message);
    } on NeoCloudOfflineException {
      throw const DeviceLoginFailure('NETWORK');
    }
  }

  @override
  Future<DeviceConnection> pair(Uri server, String code) async {
    try {
      final credentials = await DeviceLogin.pair(server, code: code, name: deviceName, platform: platformName);
      return DeviceConnection(
        server: server.toString(),
        token: credentials.token,
        username: credentials.username,
        accountId: credentials.userId,
      );
    } on DeviceLoginException catch (error) {
      throw DeviceLoginFailure(error.code, error.message);
    } on NeoCloudApiException catch (error) {
      throw DeviceLoginFailure(error.code, error.message);
    } on NeoCloudOfflineException {
      throw const DeviceLoginFailure('NETWORK');
    }
  }
}

class _PendingLogin implements PendingDeviceLogin {
  _PendingLogin(this._server, this._authorization);

  final Uri _server;
  final DeviceAuthorization _authorization;

  @override
  String get userCode => _authorization.userCode;

  @override
  Uri get verificationUrl => _authorization.verificationUrl;

  @override
  late final Future<DeviceConnection> approved = _wait();

  Future<DeviceConnection> _wait() async {
    try {
      final credentials = await _authorization.waitForApproval();
      return DeviceConnection(
        server: _server.toString(),
        token: credentials.token,
        username: credentials.username,
        accountId: credentials.userId,
      );
    } on DeviceLoginException catch (error) {
      throw DeviceLoginFailure(error.code, error.message);
    }
  }

  @override
  void cancel() => _authorization.cancel();
}
