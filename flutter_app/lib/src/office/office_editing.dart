import '../api/backend_client.dart';
import '../api/models.dart';

/// An editing session the server opened for one file: the editor's page and
/// the token to post to it (docs/office-editing.md).
class OfficeSession {
  const OfficeSession({required this.editorUrl, required this.accessToken, required this.accessTokenTtl, required this.canWrite});

  factory OfficeSession.fromJson(Map<String, dynamic> json) => OfficeSession(
    editorUrl: json['editorUrl']?.toString() ?? '',
    accessToken: json['accessToken']?.toString() ?? '',
    accessTokenTtl: (json['accessTokenTtl'] as num?)?.toInt() ?? 0,
    canWrite: json['canWrite'] == true,
  );

  final String editorUrl;
  final String accessToken;

  /// The moment the token ends, in milliseconds since 1970 (WOPI).
  final int accessTokenTtl;
  final bool canWrite;

  /// The page with the token in its address: for a browser that is opened
  /// from outside the app, where no form can be posted.
  Uri get linkWithToken {
    final page = Uri.parse(editorUrl);
    return page.replace(queryParameters: <String, String>{
      ...page.queryParameters,
      'access_token': accessToken,
      'access_token_ttl': '$accessTokenTtl',
    });
  }
}

/// Whether the server has a document editor, and for which files. Nothing
/// is offered while it is off or unreachable.
class OfficeEditing {
  OfficeEditing(this._client);

  final BackendClient _client;
  Set<String> _extensions = const <String>{};

  bool get enabled => _extensions.isNotEmpty;

  /// Whether [node] opens in the editor.
  bool canEdit(FileNode node) {
    if (node.isFolder || node.inVault || !node.name.contains('.')) return false;
    return _extensions.contains(node.name.split('.').last.toLowerCase());
  }

  Future<void> load() async {
    try {
      final answer = await _client.officeConfig();
      final list = answer['extensions'];
      _extensions = answer['enabled'] == true && list is List ? list.map((item) => '$item'.toLowerCase()).toSet() : const <String>{};
    } catch (_) {
      _extensions = const <String>{};
    }
  }

  Future<OfficeSession> startSession(FileNode node) async => OfficeSession.fromJson(await _client.officeEditSession(node.id));
}
