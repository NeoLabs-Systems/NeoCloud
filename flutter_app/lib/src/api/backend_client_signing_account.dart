part of 'backend_client.dart';

/// The account side of signing (`/api/signing/profile`, `/claim`, `/inbox`,
/// reminders) as the contract writes it. Failures surface as
/// [SigningException] with the server's code.
class BackendSigningAccountApi implements SigningAccountApi {
  BackendSigningAccountApi(this._client);

  final BackendClient _client;

  static String _doc(String id) => '/api/signing/documents/${Uri.encodeComponent(id)}';

  Future<T> _guard<T>(Future<T> Function() call) async {
    try {
      return await call();
    } on ApiException catch (error) {
      throw SigningException.fromStatus(error.statusCode, message: error.message, code: error.code, retryAfter: error.retryAfter);
    }
  }

  Future<Map<String, dynamic>> _json(String method, String path, {Map<String, String>? query, Object? body}) => _guard(() async {
    final result = await _client._request(method, path, query: query, json: body ?? (method == 'POST' || method == 'PUT' ? const <String, Object?>{} : null));
    return result is Map ? result.cast<String, dynamic>() : const <String, dynamic>{};
  });

  @override
  Future<RemindResult> remind(String documentId, String partyId, {String? message}) async {
    final body = await _json('POST', '${_doc(documentId)}/parties/${Uri.encodeComponent(partyId)}/remind', body: <String, Object?>{
      if (message != null && message.trim().isNotEmpty) 'message': message.trim(),
    });
    return RemindResult(partyId: partyId, delivery: PartyDelivery.fromJson(body['delivery']));
  }

  @override
  Future<List<RemindResult>> remindAll(String documentId) async {
    final body = await _json('POST', '${_doc(documentId)}/remind-all');
    final rows = body['results'] is List ? body['results'] as List : (body['delivery'] is List ? body['delivery'] as List : const <Object?>[]);
    return <RemindResult>[
      for (final row in rows.whereType<Map>())
        RemindResult(
          partyId: '${row['partyId']}',
          delivery: PartyDelivery.fromJson(row['delivery'] ?? row),
          errorCode: row['code']?.toString() ?? (row['status'] == 'skipped' ? 'SKIPPED' : null),
        ),
    ];
  }

  @override
  Future<String> openAsOwner(String documentId, String partyId) async =>
      '${(await _json('POST', '${_doc(documentId)}/parties/${Uri.encodeComponent(partyId)}/open-as-owner'))['url']}';

  @override
  Future<SigningProfile> profile() async => SigningProfile.fromJson(await _json('GET', '/api/signing/profile'));

  @override
  Future<SigningProfile> saveProfile(SigningProfile profile) async => SigningProfile.fromJson(await _json('PUT', '/api/signing/profile', body: profile.toJson()));

  @override
  Future<ProfileImage> addImage(Uint8List png, {required SavedKind kind, required SignatureMethod method, String? label, bool makeDefault = false}) => _guard(() async {
    final result = await _client._request(
      'POST',
      '/api/signing/profile/images',
      query: <String, String>{
        'kind': kind.name,
        'method': method.wire,
        if (method == SignatureMethod.typed && label != null && label.isNotEmpty) 'text': label,
        if (label != null && label.isNotEmpty) 'label': label,
        'default': makeDefault ? '1' : '0',
      },
      bytes: png,
      headers: const <String, String>{'Content-Type': 'image/png'},
    );
    final map = result is Map ? result.cast<String, dynamic>() : const <String, dynamic>{};
    return ProfileImage.fromJson(map['image'] is Map ? (map['image'] as Map).cast<String, dynamic>() : map);
  });

  @override
  Future<Uint8List> imageBytes(String imageId) => _guard(() => _client._bytes('/api/signing/profile/images/${Uri.encodeComponent(imageId)}'));

  @override
  Future<void> updateImage(String imageId, {String? label, bool? isDefault}) async {
    await _json('PUT', '/api/signing/profile/images/${Uri.encodeComponent(imageId)}', body: <String, Object?>{'label': ?label, 'isDefault': ?isDefault});
  }

  @override
  Future<void> deleteImage(String imageId) async {
    await _json('DELETE', '/api/signing/profile/images/${Uri.encodeComponent(imageId)}');
  }

  @override
  Future<ClaimResult> claim(String token, {bool apply = false, bool preview = false}) async => ClaimResult.fromJson(
    await _json('POST', '/api/signing/claim', body: <String, Object?>{'token': token, if (apply) 'apply': true, if (preview) 'preview': true}),
  );

  @override
  Future<Uint8List> inboxFinal(String partyId, {bool certificate = true}) => _guard(
    () => _client._bytes('/api/signing/inbox/${Uri.encodeComponent(partyId)}/final', certificate ? null : const <String, String>{'certificate': '0'}),
  );

  @override
  Future<InboxPage> inbox({required bool done}) async => InboxPage.fromJson(await _json('GET', '/api/signing/inbox', query: <String, String>{'status': done ? 'done' : 'todo'}));

  @override
  Future<String> openInbox(String partyId) async => '${(await _json('POST', '/api/signing/inbox/${Uri.encodeComponent(partyId)}/open'))['url']}';
}
