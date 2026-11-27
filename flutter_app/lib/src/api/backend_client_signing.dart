part of 'backend_client.dart';

/// The creator's signing calls (`/api/signing`), on their own object so the
/// editor and status pages can run against a fake. Failures surface as
/// [SigningException]. The paths and bodies are the signing contract's.
class BackendSigningApi implements SigningApi {
  BackendSigningApi(this._client);

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

  Map<String, dynamic> _inner(Map<String, dynamic> body, String key) =>
      body[key] is Map ? (body[key] as Map).cast<String, dynamic>() : const <String, dynamic>{};

  Iterable<Map<String, dynamic>> _rows(Object? raw) =>
      raw is List ? raw.whereType<Map>().map((row) => row.cast<String, dynamic>()) : const <Map<String, dynamic>>[];

  @override
  Future<SigningCapabilities> capabilities() async => SigningCapabilities.fromJson(await _json('GET', '/api/signing/capabilities'));

  @override
  Future<SigningDetail> createDocument({required String nodeId, String? title, String? locale}) async => SigningDetail.fromJson(
    await _json('POST', '/api/signing/documents', body: <String, Object?>{'nodeId': nodeId, 'title': ?title, 'locale': ?locale}),
  );

  @override
  Future<SigningListPage> list({SigningStatus? status, int? limit, String? cursor}) async => SigningListPage.fromJson(
    await _json('GET', '/api/signing/documents', query: <String, String>{
      if (status != null) 'status': status.wire,
      if (limit != null) 'limit': '$limit',
      'cursor': ?cursor,
    }),
  );

  @override
  Future<SigningDetail> get(String id) async => SigningDetail.fromJson(await _json('GET', _doc(id)));

  @override
  Future<Uint8List> pdf(String id) => _guard(() => _client._bytes('${_doc(id)}/pdf'));

  @override
  Future<List<SigningValue>> previewValues(String id) async {
    final body = await _json('GET', '${_doc(id)}/preview-values');
    final values = body['values'] is Map ? (body['values'] as Map) : const <Object?, Object?>{};
    return <SigningValue>[for (final entry in values.entries) SigningValue.fromStored('${entry.key}', entry.value)];
  }

  @override
  Future<SigningDocument> update(String id, SigningSettingsPatch patch) async =>
      SigningDocument.fromJson(_inner(await _json('PUT', _doc(id), body: patch.toJson()), 'document'));

  @override
  Future<List<SigningField>> saveFields(String id, List<SigningField> fields) async {
    final body = await _json('PUT', '${_doc(id)}/fields', body: <String, Object?>{'fields': fields.map((field) => field.toJson()).toList()});
    return _rows(body['fields']).map(SigningField.fromJson).toList(growable: false);
  }

  @override
  Future<List<SigningParty>> saveParties(String id, List<PartyDraft> parties) async {
    final body = await _json('PUT', '${_doc(id)}/parties', body: <String, Object?>{'parties': parties.map((party) => party.toJson()).toList()});
    return _rows(body['parties']).map(SigningParty.fromJson).toList(growable: false);
  }

  @override
  Future<int> importFields(String id) async => int.tryParse('${(await _json('POST', '${_doc(id)}/import-fields'))['imported']}') ?? 0;

  @override
  Future<SigningSendResult> send(String id, {SendOptions? options}) async =>
      SigningSendResult.fromJson(await _json('POST', '${_doc(id)}/send', body: options?.toJson()));

  @override
  Future<SigningLink> regenerateLink(String id, String partyId) async {
    final body = await _json('POST', '${_doc(id)}/parties/${Uri.encodeComponent(partyId)}/link');
    return SigningLink.fromJson(body['link'] is Map ? _inner(body, 'link') : body);
  }

  @override
  Future<SigningDocument> cancel(String id) async => SigningDocument.fromJson(_inner(await _json('POST', '${_doc(id)}/cancel'), 'document'));

  @override
  Future<void> revokeParty(String id, String partyId) async {
    await _json('POST', '${_doc(id)}/parties/${Uri.encodeComponent(partyId)}/revoke');
  }

  @override
  Future<void> delete(String id) async {
    await _json('DELETE', _doc(id));
  }

  @override
  Future<SigningDetail> duplicate(String id) async => SigningDetail.fromJson(await _json('POST', '${_doc(id)}/duplicate'));

  @override
  Future<SigningDetail> quickSign(String id, QuickSignRequest request) async =>
      SigningDetail.fromJson(await _json('POST', '${_doc(id)}/quick-sign', body: request.toJson()));

  @override
  Future<Uint8List> valueImage(String id, String fieldId) => _guard(() => _client._bytes('${_doc(id)}/values/${Uri.encodeComponent(fieldId)}/image'));

  @override
  Future<SigningDetail> retryFinalization(String id) async => SigningDetail.fromJson(await _json('POST', '${_doc(id)}/finalize'));

  @override
  Future<SigningEventLog> events(String id) async => SigningEventLog.fromJson(await _json('GET', '${_doc(id)}/events'));

  @override
  Future<Map<String, dynamic>> auditJson(String id) => _json('GET', '${_doc(id)}/audit.json');

  @override
  Future<Uint8List> finalPdf(String id, {bool certificate = true}) =>
      _guard(() => _client._bytes('${_doc(id)}/final', certificate ? null : const <String, String>{'certificate': '0'}));

  @override
  Future<Uint8List> cleanPdfForNode(String nodeId) => _guard(() => _client._bytes('/api/signing/nodes/${Uri.encodeComponent(nodeId)}/clean'));
}
