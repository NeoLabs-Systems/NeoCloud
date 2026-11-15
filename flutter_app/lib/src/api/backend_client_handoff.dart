part of 'backend_client.dart';

/// Send to device (docs/api.md, "Send to device").
extension BackendHandoffCalls on BackendClient {
  Future<List<HandoffDevice>> handoffDevices() async => <HandoffDevice>[
    for (final device
        in (_map(await _request('GET', '/api/handoff/devices'))['devices']
                as List?) ??
            const <Object?>[])
      HandoffDevice.fromJson((device as Map).cast<String, dynamic>()),
  ];

  Future<({List<HandoffOffer> sent, List<HandoffOffer> inbox})>
  handoffOffers() async {
    final body = _map(await _request('GET', '/api/handoff/offers'));
    List<HandoffOffer> read(Object? list) => <HandoffOffer>[
      for (final offer in (list as List?) ?? const <Object?>[])
        HandoffOffer.fromJson((offer as Map).cast<String, dynamic>()),
    ];
    return (sent: read(body['sent']), inbox: read(body['inbox']));
  }

  Future<HandoffOffer> handoffOffer(String id) async => HandoffOffer.fromJson(
    _map(await _request('GET', '/api/handoff/offers/$id')),
  );

  /// Creates a send; files named in [files] are uploaded next with [uploadHandoffFile].
  Future<HandoffOffer> createHandoff({
    required String targetId,
    List<({String name, int size})> files = const <({String name, int size})>[],
    List<String> nodeIds = const <String>[],
  }) async => HandoffOffer.fromJson(
    _map(
      await _request(
        'POST',
        '/api/handoff/offers',
        json: <String, Object?>{
          'targetId': targetId,
          if (files.isNotEmpty)
            'files': <Map<String, Object?>>[
              for (final file in files)
                <String, Object?>{'name': file.name, 'size': file.size},
            ],
          if (nodeIds.isNotEmpty) 'nodeIds': nodeIds,
        },
      ),
    ),
  );

  Future<void> uploadHandoffFile(
    String offerId,
    int fileId,
    UploadSource source, {
    void Function(int sent)? onProgress,
  }) async {
    final ({int status, String body}) result;
    try {
      result = await putStreamed(
        uri('/api/handoff/offers/$offerId/files/$fileId'),
        _authHeaders,
        source,
        onProgress: onProgress,
      );
    } catch (_) {
      throw const ApiException(0, '', code: 'NETWORK');
    }
    if (result.status >= 200 && result.status < 300) return;
    Object? decoded;
    try {
      decoded = jsonDecode(result.body);
    } catch (_) {
      decoded = null;
    }
    final body = decoded is Map ? decoded : const <String, dynamic>{};
    if (result.status == 401) onSessionExpired?.call();
    throw ApiException(
      result.status,
      body['error']?.toString() ?? 'HTTP ${result.status}',
      code: body['code']?.toString(),
    );
  }

  Future<HandoffOffer> acceptHandoff(String id) async => HandoffOffer.fromJson(
    _map(await _request('POST', '/api/handoff/offers/$id/accept')),
  );

  Future<void> declineHandoff(String id) =>
      _request('POST', '/api/handoff/offers/$id/decline');

  Future<void> completeHandoff(String id) =>
      _request('POST', '/api/handoff/offers/$id/complete');

  Future<void> cancelHandoff(String id) =>
      _request('DELETE', '/api/handoff/offers/$id');

  Uri handoffFileUri(String offerId, int fileId) =>
      uri('/api/handoff/offers/$offerId/files/$fileId');

  Uri handoffZipUri(String offerId) => uri('/api/handoff/offers/$offerId/zip');

  /// The headers a download of an accepted send needs (the device token; a browser sends its cookie itself).
  Map<String, String> get handoffDownloadHeaders => _authHeaders;
}
