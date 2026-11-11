import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/device/device_session.dart';
import 'package:neocloud/src/files/file_format.dart';
import 'package:neocloud/src/mobile/camera_backup.dart' show CameraLedger;
import 'package:neocloud/src/mobile/camera_upload.dart';

FileNode _file(String name, {String? mime, int size = 10, String? vaultId}) => FileNode.fromJson(<String, dynamic>{
  'id': 'n1',
  'parentId': 'p1',
  'type': 'file',
  'name': name,
  'size': size,
  'mime': mime,
  'etag': 'e1',
  'version': 1,
  'vaultId': vaultId,
});

void main() {
  test('share links parse the server shape', () {
    final link = ShareLink.fromJson(<String, dynamic>{
      'id': 7,
      'url': 'https://cloud.example/s/abc',
      'nodeId': 'n1',
      'nodeName': 'Photos',
      'nodeType': 'folder',
      'expiresAt': '2099-01-01 10:00:00',
      'expired': false,
      'hasPassword': true,
      'allowUpload': true,
      'createdAt': '2026-11-10 10:00:00',
    });
    expect(link.id, '7');
    expect(link.nodeName, 'Photos');
    expect(link.hasPassword, isTrue);
    expect(link.allowUpload, isTrue);
    expect(link.expired, isFalse);
    expect(ShareLink.fromJson(<String, dynamic>{'id': 1, 'expired': true}).expired, isTrue);
  });

  test('vault nodes keep the stored name after decryption', () {
    final stored = _file('Q2lwaGVy', mime: 'application/octet-stream', vaultId: 'v1');
    final plain = stored.withName('notes.md', mime: guessMimeType('notes.md'));
    expect(plain.inVault, isTrue);
    expect(plain.name, 'notes.md');
    expect(plain.cipherName, 'Q2lwaGVy');
    expect(isEditableText(plain), isTrue);
    final saved = plain.updatedFrom(_file('Q2lwaGVy', vaultId: 'v1'));
    expect(saved.name, 'notes.md');
  });

  test('editable text covers text, code and known extensions only', () {
    expect(isEditableText(_file('a.txt', mime: 'text/plain')), isTrue);
    expect(isEditableText(_file('main.go', mime: 'application/octet-stream')), isTrue);
    expect(isEditableText(_file('photo.jpg', mime: 'image/jpeg')), isFalse);
    expect(isEditableText(_file('big.txt', mime: 'text/plain', size: editableTextLimitBytes + 1)), isFalse);
  });

  test('camera upload settings survive a round trip', () {
    final settings = CameraUploadSettings(
      enabled: true,
      account: 'https://cloud.example.com#7',
      folderId: 'f1',
      folderName: 'Camera',
      wifiOnly: false,
      includeVideos: true,
      enabledAt: DateTime.utc(2026, 11, 10, 8),
    );
    final decoded = CameraUploadSettings.decode(settings.encode());
    expect(decoded.enabled, isTrue);
    expect(decoded.account, 'https://cloud.example.com#7');
    expect(decoded.folderId, 'f1');
    expect(decoded.wifiOnly, isFalse);
    expect(decoded.includeVideos, isTrue);
    expect(decoded.enabledAt, DateTime.utc(2026, 11, 10, 8));
    expect(CameraUploadSettings.decode('not json').enabled, isFalse);
  });

  group('sharing with accounts', () {
    test('members carry the typed name, a display name only when set', () {
      final named = ShareMember.fromJson(<String, dynamic>{
        'id': 4,
        'nodeId': 'n',
        'recipient': 'ADA@example.com',
        'displayName': 'Ada',
        'permission': 'write',
        'expiresAt': null,
        'expired': false,
      });
      expect(named.label, 'Ada');
      expect(named.permission.canWrite, isTrue);
      final unnamed = ShareMember.fromJson(<String, dynamic>{'id': 5, 'recipient': 'grace', 'displayName': '  ', 'permission': 'read'});
      expect(unnamed.label, 'grace');
      expect(unnamed.permission, MemberPermission.read);
      expect(MemberPermission.fromApi('anything'), MemberPermission.read);
    });

    test('shared-with-me entries and the owner\'s shared flag parse', () {
      final share = SharedWithMe.fromJson(<String, dynamic>{
        'id': 9,
        'permission': 'read',
        'ownerName': 'Grace',
        'sharedAt': '2026-11-11 10:00:00',
        'node': <String, dynamic>{'id': 'f', 'type': 'folder', 'name': 'Team', 'parentId': null},
      });
      expect(share.ownerName, 'Grace');
      expect(share.node.isFolder, isTrue);
      expect(share.permission.canWrite, isFalse);
      expect(FileNode.fromJson(<String, dynamic>{'id': 'x', 'type': 'file', 'name': 'a', 'shared': true}).shared, isTrue);
      expect(FileNode.fromJson(<String, dynamic>{'id': 'x', 'type': 'file', 'name': 'a'}).shared, isFalse);
      expect(FileNode.fromJson(<String, dynamic>{'id': 'x', 'type': 'file', 'name': 'a', 'shared': true}).copyWith(name: 'b').shared, isTrue);
    });
  });

  group('camera upload is bound to one account on one server', () {
    final mine = cameraAccountKey('https://cloud.example.com', 7);
    final enabled = CameraUploadSettings(enabled: true, account: mine, folderId: 'f1', folderName: 'Camera', includeVideos: true);

    test('it runs only for the account it was turned on for', () {
      expect(enabled.activeFor(mine), isTrue);
      expect(enabled.activeFor(cameraAccountKey('https://cloud.example.com', 8)), isFalse);
      expect(enabled.activeFor(cameraAccountKey('https://other.example.com', 7)), isFalse);
      expect(enabled.activeFor(null), isFalse);
      expect(enabled.copyWith(enabled: false).activeFor(mine), isFalse);
    });

    test('settings from before the binding are not active for anyone', () {
      final legacy = CameraUploadSettings.decode('{"enabled":true,"folderId":"root","scope":"everything"}');
      expect(legacy.enabled, isTrue);
      expect(legacy.account, isNull);
      expect(legacy.activeFor(mine), isFalse);
    });

    test('another account starts off, without the folder', () {
      final other = enabled.rebound(cameraAccountKey('https://other.example.com', 1));
      expect(other.enabled, isFalse);
      expect(other.folderId, isNull);
      expect(other.folderName, isEmpty);
      expect(other.account, 'https://other.example.com#1');
      expect(other.includeVideos, isTrue);
    });

    test('the ledger keeps its account', () {
      final ledger = CameraLedger.decode((CameraLedger(account: mine)..addLog('x', DateTime(2026))).encode());
      expect(ledger.account, mine);
      expect(ledger.log, hasLength(1));
    });

    test('a device connection knows its account once the id is known', () {
      const legacy = DeviceConnection(server: 'https://cloud.example.com', token: 't', username: 'neo');
      expect(legacy.accountKey, isNull);
      expect(legacy.withAccountId(7).accountKey, mine);
    });
  });
}
