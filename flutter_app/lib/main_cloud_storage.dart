part of 'main.dart';

// Cloud storage the account connected itself (S3, WebDAV, SFTP): the list of
// connections every screen shares, and how errors read. The connections are
// shown in Settings › Cloud storage (main_cloud_storage_settings.dart),
// browsed in main_cloud_storage_browser.dart and offered on Home through
// main_cloud_storage_strip.dart. Files stay at the provider; nothing here is
// part of the file tree.

class CloudStorage extends ChangeNotifier {
  CloudStorage(this._controller);

  final NeoCloudController _controller;
  List<ExternalMount>? _mounts;
  Object? _loadError;
  int? _loadedFor;
  Future<void>? _loading;

  /// The connections, or null until they were read for the signed-in account.
  List<ExternalMount>? get mounts => _loadedFor == _controller.user?.id ? _mounts : null;
  Object? get loadError => _loadError;

  /// Reads the connections again; calls made while a read runs share it.
  Future<void> load() => _loading ??= _read().whenComplete(() => _loading = null);

  Future<void> _read() async {
    final userId = _controller.user?.id;
    try {
      final mounts = await _controller.client.externalMounts();
      _mounts = mounts;
      _loadedFor = userId;
      _loadError = null;
    } catch (error) {
      _loadError = error;
    }
    notifyListeners();
  }

  /// Connections are read once per account; call again after a change.
  void ensureLoaded() {
    if (_loadedFor != _controller.user?.id) unawaited(load());
  }
}

final Expando<CloudStorage> _cloudStorages = Expando<CloudStorage>('cloud storage');

extension CloudStorageAccess on NeoCloudController {
  CloudStorage get cloudStorage => _cloudStorages[this] ??= CloudStorage(this);
}

/// The server's answer in the app's language where it is a known refusal.
String _cloudError(NeoCloudController controller, Object error) {
  if (error is ApiException) {
    switch (error.code) {
      case 'EXTERNAL_HOST_BLOCKED':
        return appStrings.cloudHostBlocked;
      case 'EXTERNAL_READ_ONLY':
        return appStrings.cloudReadOnlyError;
      case 'EXTERNAL_MOUNT_LIMIT':
        return appStrings.cloudLimitError;
      case 'NAME_EXISTS':
        return appStrings.cloudNameExists;
      case 'EXTERNAL_NOT_FOUND':
        return appStrings.cloudNotFound;
    }
  }
  return controller.friendlyError(error);
}

extension ExternalProviderInfo on ExternalProvider {
  String get label {
    switch (this) {
      case ExternalProvider.s3:
        return appStrings.cloudProviderS3;
      case ExternalProvider.webdav:
        return appStrings.cloudProviderWebdav;
      case ExternalProvider.sftp:
        return appStrings.cloudProviderSftp;
    }
  }

  IconData get icon {
    switch (this) {
      case ExternalProvider.s3:
        return Icons.cloud_queue_rounded;
      case ExternalProvider.webdav:
        return Icons.dns_outlined;
      case ExternalProvider.sftp:
        return Icons.terminal_rounded;
    }
  }
}

extension ExternalMountInfo on ExternalMount {
  /// Where it points, for the list: bucket, server or host.
  String get summary {
    switch (provider) {
      case ExternalProvider.s3:
        return '${config['bucket'] ?? ''}';
      case ExternalProvider.webdav:
        return Uri.tryParse('${config['url'] ?? ''}')?.host ?? '${config['url'] ?? ''}';
      case ExternalProvider.sftp:
        return '${config['username'] ?? ''}@${config['host'] ?? ''}';
    }
  }
}
