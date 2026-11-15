part of 'main.dart';

// Signing out and erasing what this device keeps of the account. Logout and
// the owner's remote wipe (Settings > Devices on another device) share it.

extension _LocalWipe on NeoCloudController {
  /// Forgets the account on this device: the stored token, camera backup,
  /// cached thumbnails and the open session. With [erasePreferences] (remote
  /// wipe) the app's saved settings, upload queue and signatures go too,
  /// except the chosen language.
  Future<void> wipeLocalData({bool erasePreferences = false}) async {
    folderViews.close();
    if (usesDeviceLogin) {
      // Camera upload goes off with the token.
      await cameraUploads.signOut().catchError((Object _) {});
      await deviceSessions.clear();
      client.bearerToken = null;
    }
    await eraseLocalFiles().catchError((Object _) {});
    if (erasePreferences) await _erasePreferences();
    user = null;
    openFolderId = null;
    openSharedFolderId = null;
    openSigningId = null;
    sharedWithMe = const <SharedWithMe>[];
    settingsPage = null;
    vaults.lockAll();
    aiReview.clear();
    _stopLiveUpdates();
    announceChange();
  }

  Future<void> _erasePreferences() async {
    for (final key in _preferences.getKeys().where((key) => key != appLanguagePreferenceKey).toList()) {
      await _preferences.remove(key);
    }
  }

  /// The server said the owner signed this device out and asked it to erase
  /// itself (401 DEVICE_WIPE_REQUESTED, once).
  void _handleDeviceWiped() {
    unawaited(
      wipeLocalData(erasePreferences: true).then((_) {
        errorMessage = appStrings.deviceWipedNotice;
        announceChange();
      }),
    );
  }
}
