part of 'backend_client.dart';

/// The AI calls (providers, models, keys, NeoRouter, status), on their own
/// object so tests can stand in a fake. Paths and bodies are NeoAgent's
/// where it has the route.
class AiApi {
  AiApi(this._client);

  final BackendClient _client;

  Future<Map<String, dynamic>> _get(String path) async => _client._map(await _client._request('GET', path));

  Future<Map<String, dynamic>> _send(String method, String path, [Object? body]) async {
    final result = await _client._request(method, path, json: body ?? const <String, Object?>{});
    return result is Map ? result.cast<String, dynamic>() : const <String, dynamic>{};
  }

  List<Map<String, dynamic>> _rows(Object? raw) =>
      ((raw is List) ? raw : const <Object?>[]).whereType<Map>().map((row) => row.cast<String, dynamic>()).toList();

  // The account

  /// Whether AI can run for this account, model per kind of work, budget.
  Future<AiStatus> status() async => AiStatus.fromJson(await _get('/api/ai/status'));

  /// The models this account may pick.
  Future<List<ModelMeta>> models() async =>
      _rows((await _get('/api/settings/meta/models'))['models']).map(ModelMeta.fromJson).where((model) => model.id.isNotEmpty).toList();

  Future<List<AiProviderMeta>> providers() async =>
      _rows((await _get('/api/settings/meta/ai-providers'))['providers']).map(AiProviderMeta.fromJson).where((p) => p.id.isNotEmpty).toList();

  /// Marks a folder of the account's own "Exclude from AI" (or not); it
  /// covers everything below it.
  Future<void> setFolderExcluded(String folderId, bool excluded) =>
      _send('PUT', '/api/ai/folders/${Uri.encodeComponent(folderId)}/excluded', <String, Object?>{'excluded': excluded});

  // The review queue

  /// One page of suggestions, newest first. [tier] `all` includes the
  /// hidden low-confidence ones; by default pending high and medium.
  Future<AiSuggestionPage> suggestions({String? kind, String? tier, String? status, String? nodeId, int? limit, int? offset}) async {
    final query = <String, String>{
      'kind': ?kind,
      'tier': ?tier,
      'status': ?status,
      'nodeId': ?nodeId,
      if (limit != null) 'limit': '$limit',
      if (offset != null && offset > 0) 'offset': '$offset',
    };
    return AiSuggestionPage.fromJson(_client._map(await _client._request('GET', '/api/ai/suggestions', query: query)));
  }

  Future<AiSuggestionCounts> suggestionCounts() async => AiSuggestionCounts.fromJson(await _get('/api/ai/suggestions/counts'));

  /// Applies a suggestion; [value] replaces what it proposed: a name, a
  /// folder id or a tag list.
  Future<AiSuggestion> acceptSuggestion(String id, {Object? value}) async => AiSuggestion.fromJson(_suggestion(
    await _send('POST', '/api/ai/suggestions/${Uri.encodeComponent(id)}/accept', <String, Object?>{'value': ?value}),
  ));

  Future<AiSuggestion> rejectSuggestion(String id) async =>
      AiSuggestion.fromJson(_suggestion(await _send('POST', '/api/ai/suggestions/${Uri.encodeComponent(id)}/reject')));

  /// Puts an accepted suggestion back; `409 AI_UNDO_CONFLICT` once the file
  /// changed since.
  Future<AiSuggestion> undoSuggestion(String id) async =>
      AiSuggestion.fromJson(_suggestion(await _send('POST', '/api/ai/suggestions/${Uri.encodeComponent(id)}/undo')));

  /// Accepts every pending high-confidence suggestion (of [kind], if given).
  Future<AiAcceptHighResult> acceptHigh({String? kind}) async =>
      AiAcceptHighResult.fromJson(await _send('POST', '/api/ai/suggestions/accept-high', <String, Object?>{'kind': ?kind}));

  /// Accepts or rejects many suggestions at once (`action`: `accept` or
  /// `reject`); failures come back by id.
  Future<AiBulkResult> bulkDecide(List<String> ids, String action) async =>
      AiBulkResult.fromJson(await _send('POST', '/api/ai/suggestions/bulk', <String, Object?>{'ids': ids, 'action': action}));

  // Empty folders AI left behind

  /// Empty folders AI created, and other empty top-level folders of the last week.
  Future<AiEmptyFolders> emptyFolders() async => AiEmptyFolders.fromJson(await _get('/api/ai/cleanup/folders'));

  /// Moves the chosen empty folders to the trash (only ones still empty).
  Future<AiCleanupResult> removeEmptyFolders(List<String> ids) async =>
      AiCleanupResult.fromJson(await _send('POST', '/api/ai/cleanup/folders', <String, Object?>{'ids': ids}));

  // Duplicates (no AI needed)

  /// Groups of identical files and look-alike photos.
  Future<ReviewDuplicates> duplicates() async => ReviewDuplicates.fromJson(await _get('/api/ai/duplicates'));

  /// "Not duplicates": the group of these files isn't listed again.
  Future<void> dismissDuplicates(List<String> nodeIds) =>
      _send('POST', '/api/ai/duplicates/dismiss', <String, Object?>{'nodeIds': nodeIds});

  Map<String, dynamic> _suggestion(Map<String, dynamic> body) =>
      body['suggestion'] is Map ? (body['suggestion'] as Map).cast<String, dynamic>() : const <String, dynamic>{};

  // Jobs

  /// Asks for suggestions for [nodeIds] (files) or every file below
  /// [folderId]; [kinds] of `names`, `folders`, `tags` (default: every
  /// switched-on kind). Resolves to the batch id.
  Future<String> requestSuggestions({List<String>? nodeIds, String? folderId, List<String>? kinds}) async {
    final body = await _send('POST', '/api/ai/jobs', <String, Object?>{'nodeIds': ?nodeIds, 'folderId': ?folderId, 'kinds': ?kinds});
    return '${body['batchId'] ?? ''}';
  }

  Future<AiJobStatus> jobStatus({String? batchId}) async =>
      AiJobStatus.fromJson(_client._map(await _client._request('GET', '/api/ai/jobs', query: <String, String>{'batchId': ?batchId})));

  // Sorting rules

  Future<List<AiRule>> rules() async => _rows((await _get('/api/ai/rules'))['rules']).map(AiRule.fromJson).toList();

  Future<AiRule> createRule(AiRule rule) async => AiRule.fromJson(_rule(await _send('POST', '/api/ai/rules', rule.toJson())));

  Future<AiRule> updateRule(int id, Map<String, Object?> patch) async => AiRule.fromJson(_rule(await _send('PATCH', '/api/ai/rules/$id', patch)));

  Future<void> deleteRule(int id) => _send('DELETE', '/api/ai/rules/$id');

  Future<List<AiRuleProposal>> ruleProposals() async =>
      _rows((await _get('/api/ai/rules/proposals'))['proposals']).map(AiRuleProposal.fromJson).toList();

  /// Turns a proposal into a rule matching its type and issuer.
  Future<AiRule> acceptProposal(int id, {String? name}) async =>
      AiRule.fromJson(_rule(await _send('POST', '/api/ai/rules/proposals/$id', <String, Object?>{'name': ?name})));

  /// Not offered again.
  Future<void> dismissProposal(int id) => _send('DELETE', '/api/ai/rules/proposals/$id');

  Map<String, dynamic> _rule(Map<String, dynamic> body) => body['rule'] is Map ? (body['rule'] as Map).cast<String, dynamic>() : const <String, dynamic>{};

  // Summaries

  /// A file's summary: the kept one for its content unless [refresh].
  Future<AiSummary> summarize(String fileId, {bool refresh = false}) async => AiSummary.fromJson(
    await _send('POST', '/api/ai/files/${Uri.encodeComponent(fileId)}/summary', <String, Object?>{if (refresh) 'refresh': true}),
  );

  // Bring your own key

  /// The providers an account can add its own key or endpoint for, as
  /// NeoAgent's `GET /api/settings/byok` rows.
  Future<List<Map<String, dynamic>>> byokProviders() async => _rows((await _get('/api/settings/byok'))['providers']);

  Future<void> saveByok(String providerId, {required String apiKey, String? baseUrl, String? label}) => _send(
    'PUT',
    '/api/settings/byok/${Uri.encodeComponent(providerId)}',
    <String, Object?>{'apiKey': apiKey, 'baseUrl': ?baseUrl, 'label': ?label},
  );

  Future<void> clearByok(String providerId) => _send('DELETE', '/api/settings/byok/${Uri.encodeComponent(providerId)}');

  /// Tries a key or endpoint without saving it: `{ok, message | error}`.
  Future<Map<String, dynamic>> testByok(String providerId, {String? apiKey, String? baseUrl}) => _send(
    'POST',
    '/api/settings/byok/${Uri.encodeComponent(providerId)}/test',
    <String, Object?>{'apiKey': ?apiKey, 'baseUrl': ?baseUrl},
  );

  // NeoRouter: scope is 'server' (admins) or 'user'.

  /// `{roles, isAdmin, server: {connected, address, roles, offer}, user: …}`.
  Future<Map<String, dynamic>> neoRouter() => _get('/api/neorouter');

  /// Starts NeoRouter's device sign-in: `{id, url, userCode}`.
  Future<Map<String, dynamic>> beginNeoRouterConnect(String scope, String address) =>
      _send('POST', '/api/neorouter/connect', <String, Object?>{'scope': scope, 'address': address});

  /// `{status: pending | completed | failed, error?}`.
  Future<Map<String, dynamic>> neoRouterConnectStatus(String id) => _get('/api/neorouter/connect/${Uri.encodeComponent(id)}');

  Future<void> saveNeoRouterRoles(String scope, List<String> roles) =>
      _send('PUT', '/api/neorouter/${Uri.encodeComponent(scope)}/roles', <String, Object?>{'roles': roles});

  Future<void> disconnectNeoRouter(String scope) => _send('DELETE', '/api/neorouter/${Uri.encodeComponent(scope)}');

  // Admin

  Future<AdminAiConfig> adminConfig() async => AdminAiConfig.fromJson(await _get('/api/admin/ai'));

  /// [monthlyTokens] 0 means no limit.
  Future<AdminAiConfig> adminSaveConfig({bool? enabled, int? monthlyTokens}) async => AdminAiConfig.fromJson(
    await _send('PUT', '/api/admin/ai', <String, Object?>{'enabled': ?enabled, 'monthlyTokens': ?monthlyTokens}),
  );

  Future<AiUsageSummary> adminUsage() async => AiUsageSummary.fromJson(await _get('/api/admin/ai/usage'));

  Future<List<AdminAiProvider>> adminProviders() async => _rows(
    (await _get('/api/admin/providers'))['providers'],
  ).map(AdminAiProvider.fromJson).where((provider) => provider.key.isNotEmpty).toList();

  /// Sets one provider key or address for the server; empty clears it.
  Future<void> adminSaveProvider(String key, String value) =>
      _send('PUT', '/api/admin/providers', <String, Object?>{'key': key, 'value': value});

  Future<AdminModelCatalog> adminModels() async => AdminModelCatalog.fromJson(await _get('/api/admin/models'));

  Future<AdminModelCatalog> adminSaveModels(List<String> disabledModels, {required bool autoEnableNewModels}) async =>
      AdminModelCatalog.fromJson(
        await _send('PUT', '/api/admin/models/config', <String, Object?>{
          'disabledModels': disabledModels,
          'autoEnableNewModels': autoEnableNewModels,
        }),
      );

  /// The models granted to one account although they are off for everyone,
  /// and its own monthly budget.
  Future<AdminUserAi> adminUserAi(int userId) async => AdminUserAi.fromJson(await _get('/api/admin/users/$userId/allowed-models'));

  Future<void> adminSaveUserModels(int userId, List<String> modelIds) =>
      _send('PUT', '/api/admin/users/$userId/allowed-models', <String, Object?>{'modelIds': modelIds});

  /// One account's monthly token budget: null follows the server default,
  /// 0 is no limit.
  Future<void> adminSaveUserBudget(int userId, int? monthlyTokens) =>
      _send('PUT', '/api/admin/users/$userId/ai-budget', <String, Object?>{'monthlyTokens': monthlyTokens});
}
