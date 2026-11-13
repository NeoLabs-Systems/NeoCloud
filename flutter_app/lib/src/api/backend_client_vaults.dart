part of 'backend_client.dart';

/// The calls for turning a folder into a vault (docs/clients.md §4), kept
/// beside [BackendClient] so that file stays readable.
extension BackendVaultConversionCalls on BackendClient {
  Future<VaultConversionCheck> vaultConversionCheck(String folderId) async =>
      VaultConversionCheck.fromJson(_map(await _request('GET', '/api/vaults/conversions/$folderId')));

  /// Trashes [folderId] and gives vault [vaultId]'s folder its name and
  /// place. `409 VAULT_SOURCE_CHANGED` when the folder changed after [cursor].
  Future<FileNode> replaceFolderWithVault(String vaultId, {required String folderId, required int cursor}) async =>
      FileNode.fromJson(_map(_map(await _request('POST', '/api/vaults/$vaultId/replace', json: {'folderId': folderId, 'cursor': cursor}))['folder']));

  /// Bytes [start] up to (not including) [end] of a file.
  Future<Uint8List> fileRange(String fileId, int start, int end) =>
      _bytes('/api/files/$fileId/content', null, {'Range': 'bytes=$start-${end - 1}'});

  /// One part of a resumable upload; answers the SHA-256 (hex) the server
  /// stored for it.
  Future<String> uploadPartChecked(String uploadId, int index, Uint8List bytes) async =>
      '${_map(await _request('PUT', '/api/uploads/$uploadId/parts/$index', bytes: bytes))['sha256'] ?? ''}';
}
