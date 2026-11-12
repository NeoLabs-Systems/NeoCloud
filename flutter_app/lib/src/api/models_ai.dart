// Models for the AI routes (settings meta, admin providers and models,
// /api/ai/status), ported from NeoAgent's ModelMeta and AiProviderMeta.
// Unknown fields are ignored and missing ones fall back, so a server that
// adds to these shapes keeps working with this app.
part of 'models.dart';

int? _optionalInt(Object? value) {
  if (value == null) return null;
  if (value is num) return value.toInt();
  return int.tryParse('$value');
}

List<Map<String, dynamic>> _maps(Object? raw) =>
    ((raw is List) ? raw : const <Object?>[]).whereType<Map>().map((item) => item.cast<String, dynamic>()).toList();

List<String> _strings(Object? raw) => ((raw is List) ? raw : const <Object?>[]).map((item) => '$item').toList();

/// One model a picker offers (`GET /api/settings/meta/models`, and the
/// admin catalog). [id] is `provider::model`.
class ModelMeta {
  const ModelMeta({
    required this.id,
    required this.modelId,
    required this.label,
    required this.provider,
    this.purpose = '',
    this.available = true,
    this.priceTier,
    this.inputCostPerM,
    this.isByok = false,
    this.adminGranted = false,
    this.supportsVision = false,
    this.supportsTools = false,
    this.kind = 'chat',
  });

  factory ModelMeta.fromJson(Map<String, dynamic> json) {
    final id = _string(json['id']);
    final tier = json['priceTier']?.toString();
    final cost = json['inputCostPerM'];
    return ModelMeta(
      id: id,
      modelId: _string(json['modelId'] ?? id),
      label: _string(json['label']),
      provider: _string(json['provider']),
      purpose: _string(json['purpose']),
      available: json['available'] != false,
      priceTier: tier == null || tier.isEmpty ? null : tier,
      inputCostPerM: cost is num ? cost.toDouble() : null,
      isByok: json['isByok'] == true,
      adminGranted: json['adminGranted'] == true,
      supportsVision: json['supportsVision'] == true,
      supportsTools: json['supportsTools'] == true,
      kind: _string(json['kind']).isEmpty ? 'chat' : _string(json['kind']),
    );
  }

  final String id;

  /// The id the provider itself uses.
  final String modelId;
  final String label;
  final String provider;
  final String purpose;
  final bool available;

  /// `free`, `cheap`, `medium`, `expensive`, or null when unknown.
  final String? priceTier;

  /// USD per million input tokens; null when unknown.
  final double? inputCostPerM;

  /// Runs on the account's own key rather than the server's.
  final bool isByok;

  /// Switched off for everyone else; an admin granted it to this account.
  final bool adminGranted;
  final bool supportsVision;
  final bool supportsTools;

  /// `chat`, `embedding`, `transcription` or `image`.
  final String kind;

  String get displayLabel => label.isEmpty ? modelId : label;
}

/// A provider as the account sees it (`GET /api/settings/meta/ai-providers`).
class AiProviderMeta {
  const AiProviderMeta({
    required this.id,
    required this.label,
    this.description = '',
    this.available = false,
    this.supportsApiKey = false,
    this.supportsBaseUrl = false,
    this.requiresBaseUrl = false,
    this.defaultBaseUrl = '',
    this.baseUrl = '',
    this.isByok = false,
    this.modelCount = 0,
  });

  factory AiProviderMeta.fromJson(Map<String, dynamic> json) => AiProviderMeta(
    id: _string(json['id']),
    label: _string(json['label'] ?? json['id']),
    description: _string(json['description']),
    available: json['available'] == true,
    supportsApiKey: json['supportsApiKey'] == true,
    supportsBaseUrl: json['supportsBaseUrl'] == true,
    requiresBaseUrl: json['requiresBaseUrl'] == true,
    defaultBaseUrl: _string(json['defaultBaseUrl']),
    baseUrl: _string(json['baseUrl']),
    isByok: json['isByok'] == true,
    modelCount: _int(json['modelCount']),
  );

  final String id;
  final String label;
  final String description;
  final bool available;
  final bool supportsApiKey;
  final bool supportsBaseUrl;
  final bool requiresBaseUrl;
  final String defaultBaseUrl;
  final String baseUrl;
  final bool isByok;
  final int modelCount;
}

/// Whether one kind of work (text, smart, vision, embedding) has a model.
class AiRoleStatus {
  const AiRoleStatus({required this.ready, this.model, this.source, this.requestedUnavailable = false});

  factory AiRoleStatus.fromJson(Object? raw) {
    final json = raw is Map ? raw.cast<String, dynamic>() : const <String, dynamic>{};
    final model = json['model']?.toString();
    return AiRoleStatus(
      ready: json['ready'] == true,
      model: model == null || model.isEmpty ? null : model,
      source: json['source']?.toString(),
      requestedUnavailable: json['requestedUnavailable'] == true,
    );
  }

  final bool ready;

  /// The model it resolves to, `provider::model`.
  final String? model;

  /// `user`, `neorouter` or `auto`: where the model came from.
  final String? source;

  /// The account's own choice could not be used, so another model runs.
  final bool requestedUnavailable;
}

/// `GET /api/ai/status`: whether AI can run for this account.
class AiStatus {
  const AiStatus({
    required this.available,
    required this.enabled,
    required this.optedIn,
    this.roles = const <String, AiRoleStatus>{},
    this.budgetUsed = 0,
    this.budgetLimit,
  });

  factory AiStatus.fromJson(Map<String, dynamic> json) {
    final roles = json['roles'] is Map ? (json['roles'] as Map).cast<String, dynamic>() : const <String, dynamic>{};
    final budget = json['budget'] is Map ? (json['budget'] as Map).cast<String, dynamic>() : const <String, dynamic>{};
    final limit = _optionalInt(budget['limit']);
    return AiStatus(
      available: json['available'] == true,
      enabled: json['enabled'] != false,
      optedIn: json['optedIn'] == true,
      roles: <String, AiRoleStatus>{for (final entry in roles.entries) entry.key: AiRoleStatus.fromJson(entry.value)},
      budgetUsed: _int(budget['used']),
      budgetLimit: limit == null || limit <= 0 ? null : limit,
    );
  }

  /// Some model can run for this account.
  final bool available;

  /// The admin turned AI on for the server.
  final bool enabled;
  final bool optedIn;
  final Map<String, AiRoleStatus> roles;

  /// Tokens used this month.
  final int budgetUsed;

  /// The monthly token budget; null for no limit.
  final int? budgetLimit;
}

/// One server-wide provider setting (`GET /api/admin/providers`): a key or
/// an endpoint address.
class AdminAiProvider {
  const AdminAiProvider({required this.key, required this.label, required this.isUrl, required this.configured, required this.hint});

  factory AdminAiProvider.fromJson(Map<String, dynamic> json) {
    final key = _string(json['key']);
    return AdminAiProvider(
      key: key,
      label: _string(json['label'] ?? key),
      isUrl: json['type'] == 'url',
      configured: json['configured'] == true,
      hint: _string(json['hint']),
    );
  }

  final String key;
  final String label;
  final bool isUrl;
  final bool configured;

  /// A masked key, or the whole address for a URL setting.
  final String hint;
}

/// `GET /api/admin/models`: every model the providers offer, and which the
/// admin switched off.
class AdminModelCatalog {
  const AdminModelCatalog({required this.models, required this.disabledModels, required this.autoEnableNewModels});

  factory AdminModelCatalog.fromJson(Map<String, dynamic> json) => AdminModelCatalog(
    models: _maps(json['models']).map(ModelMeta.fromJson).where((model) => model.id.isNotEmpty).toList(),
    disabledModels: _strings(json['disabledModels']).toSet(),
    autoEnableNewModels: json['autoEnableNewModels'] != false,
  );

  final List<ModelMeta> models;
  final Set<String> disabledModels;
  final bool autoEnableNewModels;
}

/// `GET /api/admin/ai`: the server-wide switch and the default budget.
class AdminAiConfig {
  const AdminAiConfig({required this.enabled, this.monthlyTokens});

  factory AdminAiConfig.fromJson(Map<String, dynamic> json) {
    final budget = _optionalInt(json['monthlyTokens']);
    return AdminAiConfig(enabled: json['enabled'] == true, monthlyTokens: budget == null || budget <= 0 ? null : budget);
  }

  final bool enabled;

  /// Tokens per account and month; null for no limit.
  final int? monthlyTokens;
}

/// One line of the usage summary: an account, a feature or a model.
class AiUsageLine {
  const AiUsageLine({required this.label, required this.tokens, required this.calls, this.detail = '', this.errors = 0, this.userId, this.limit});

  factory AiUsageLine.fromJson(Map<String, dynamic> json) {
    final limit = _optionalInt(json['limit']);
    return AiUsageLine(
      label: _string(json['username'] ?? json['feature'] ?? json['model']),
      detail: _string(json['provider']),
      tokens: _int(json['tokens']),
      calls: _int(json['calls']),
      errors: _int(json['errors']),
      userId: _optionalInt(json['userId']),
      limit: limit == null || limit <= 0 ? null : limit,
    );
  }

  /// The username, feature or model id.
  final String label;

  /// A model's provider.
  final String detail;
  final int tokens;
  final int calls;
  final int errors;

  /// An account line's id and its monthly limit (null: none).
  final int? userId;
  final int? limit;
}

/// `GET /api/admin/ai/usage`: this month per account, feature and model;
/// metadata only.
class AiUsageSummary {
  const AiUsageSummary({required this.users, required this.features, required this.models});

  factory AiUsageSummary.fromJson(Map<String, dynamic> json) => AiUsageSummary(
    users: _maps(json['users']).map(AiUsageLine.fromJson).toList(),
    features: _maps(json['features']).map(AiUsageLine.fromJson).toList(),
    models: _maps(json['models']).map(AiUsageLine.fromJson).toList(),
  );

  final List<AiUsageLine> users;
  final List<AiUsageLine> features;
  final List<AiUsageLine> models;

  int get calls => users.fold(0, (sum, line) => sum + line.calls);

  int get tokens => users.fold(0, (sum, line) => sum + line.tokens);

  int get errors => users.fold(0, (sum, line) => sum + line.errors);
}

/// Reads the optional booleans the AI settings keep under `ai_features`.
Map<String, bool> _aiFeatures(Object? raw) {
  if (raw is! Map) return const <String, bool>{};
  return <String, bool>{
    for (final entry in raw.entries)
      if (entry.value is bool) '${entry.key}': entry.value as bool,
  };
}

/// The `ai_features` switches with the server's defaults: what runs on
/// request is on once the person opted in; what acts unasked is off.
const Map<String, bool> aiFeatureDefaults = <String, bool>{
  'names': true,
  'folders': true,
  'tags': true,
  'summaries': true,
  'onUpload': false,
  'autoApplyHighConfidence': false,
  'newTags': true,
  'newFolders': false,
};

/// `GET /api/ai/suggestions/counts` (also the `ai:suggestions` socket
/// event): pending high and medium suggestions per kind, the hidden low
/// ones and the open rule proposals.
class AiSuggestionCounts {
  const AiSuggestionCounts({this.rename = 0, this.move = 0, this.tags = 0, this.total = 0, this.high = 0, this.hiddenLow = 0, this.proposals = 0});

  factory AiSuggestionCounts.fromJson(Object? raw) {
    final json = raw is Map ? raw.cast<String, dynamic>() : const <String, dynamic>{};
    final pending = json['pending'] is Map ? (json['pending'] as Map).cast<String, dynamic>() : const <String, dynamic>{};
    return AiSuggestionCounts(
      rename: _int(pending['rename']),
      move: _int(pending['move']),
      tags: _int(pending['tags']),
      total: _int(pending['total']),
      high: _int(json['high']),
      hiddenLow: _int(json['hiddenLow']),
      proposals: _int(json['proposals']),
    );
  }

  final int rename;
  final int move;
  final int tags;
  final int total;
  final int high;
  final int hiddenLow;
  final int proposals;

  int ofKind(String kind) => switch (kind) {
    'rename' => rename,
    'move' => move,
    'tags' => tags,
    _ => 0,
  };

  @override
  bool operator ==(Object other) =>
      other is AiSuggestionCounts &&
      other.rename == rename &&
      other.move == move &&
      other.tags == tags &&
      other.total == total &&
      other.high == high &&
      other.hiddenLow == hiddenLow &&
      other.proposals == proposals;

  @override
  int get hashCode => Object.hash(rename, move, tags, total, high, hiddenLow, proposals);
}

/// A folder a move suggestion names: an existing one, or (with no [id]) one
/// accepting creates as [name] in [parentId].
class AiFolderRef {
  const AiFolderRef({required this.id, required this.path, this.parentId, this.name});

  factory AiFolderRef.fromJson(Object? raw) {
    final json = raw is Map ? raw.cast<String, dynamic>() : const <String, dynamic>{};
    final id = json['id'];
    return AiFolderRef(
      id: id == null ? null : '$id',
      path: _string(json['path']),
      parentId: json['parentId']?.toString(),
      name: json['name']?.toString(),
    );
  }

  final String? id;
  final String path;
  final String? parentId;
  final String? name;

  bool get isNew => id == null;
}

/// One suggestion of the review queue. [kind] is `rename` (from and to are
/// names), `move` (folders) or `tags` (tag lists).
class AiSuggestion {
  const AiSuggestion({
    required this.id,
    required this.nodeId,
    required this.kind,
    required this.from,
    required this.to,
    this.reason = '',
    this.confidence = 0,
    this.tier = 'medium',
    this.status = 'pending',
    this.source = 'ai',
    this.ruleId,
    this.model,
    this.createdAt,
    this.decidedAt,
    this.node,
  });

  factory AiSuggestion.fromJson(Map<String, dynamic> json) {
    final kind = _string(json['kind']);
    Object? side(Object? raw) => switch (kind) {
      'move' => AiFolderRef.fromJson(raw),
      'tags' => _strings(raw),
      _ => _string(raw),
    };
    final confidence = json['confidence'];
    final model = json['model']?.toString();
    return AiSuggestion(
      id: _string(json['id']),
      nodeId: _string(json['nodeId']),
      kind: kind,
      from: side(json['from']),
      to: side(json['to']),
      reason: _string(json['reason']),
      confidence: confidence is num ? confidence.toDouble() : 0,
      tier: _string(json['tier'] ?? 'medium'),
      status: _string(json['status'] ?? 'pending'),
      source: _string(json['source'] ?? 'ai'),
      ruleId: _optionalInt(json['ruleId']),
      model: model == null || model.isEmpty ? null : model,
      createdAt: _date(json['createdAt']),
      decidedAt: _date(json['decidedAt']),
      node: json['node'] is Map ? FileNode.fromJson((json['node'] as Map).cast<String, dynamic>()) : null,
    );
  }

  final String id;
  final String nodeId;
  final String kind;

  /// A name (rename), an [AiFolderRef] (move) or a tag list (tags).
  final Object? from;
  final Object? to;
  final String reason;

  /// 0–1.
  final double confidence;

  /// `high`, `medium` or `low`.
  final String tier;

  /// `pending`, `accepted`, `rejected`, `applied`, `undone` or `stale`.
  final String status;

  /// `ai`, or `rule` when one of the account's sorting rules decided.
  final String source;
  final int? ruleId;

  /// The model that suggested it, `provider::model`.
  final String? model;
  final DateTime? createdAt;
  final DateTime? decidedAt;

  /// The file now, with its folder `path`; null once deleted.
  final FileNode? node;

  bool get fromRule => source == 'rule';

  String get fromName => from is String ? from! as String : '';
  String get toName => to is String ? to! as String : '';
  AiFolderRef? get fromFolder => from is AiFolderRef ? from! as AiFolderRef : null;
  AiFolderRef? get toFolder => to is AiFolderRef ? to! as AiFolderRef : null;
  List<String> get fromTags => from is List<String> ? from! as List<String> : const <String>[];
  List<String> get toTags => to is List<String> ? to! as List<String> : const <String>[];
}

/// One page of `GET /api/ai/suggestions`.
class AiSuggestionPage {
  const AiSuggestionPage({required this.items, required this.total, required this.counts, this.nextOffset});

  factory AiSuggestionPage.fromJson(Map<String, dynamic> json) => AiSuggestionPage(
    items: _maps(json['items']).map(AiSuggestion.fromJson).where((item) => item.id.isNotEmpty).toList(),
    total: _int(json['total']),
    nextOffset: _optionalInt(json['nextOffset']),
    counts: AiSuggestionCounts.fromJson(json['counts']),
  );

  final List<AiSuggestion> items;
  final int total;
  final int? nextOffset;
  final AiSuggestionCounts counts;
}

/// `POST /api/ai/suggestions/accept-high`.
class AiAcceptHighResult {
  const AiAcceptHighResult({required this.accepted, required this.failed});

  factory AiAcceptHighResult.fromJson(Map<String, dynamic> json) => AiAcceptHighResult(
    accepted: _int(json['accepted']),
    failed: <String, String>{
      for (final row in _maps(json['failed'])) _string(row['id']): _string(row['code']),
    },
  );

  final int accepted;

  /// Suggestion id → error code.
  final Map<String, String> failed;
}

/// One request for suggestions (`POST /api/ai/jobs`) and how far it got.
class AiJobBatch {
  const AiJobBatch({required this.id, this.createdAt, this.total = 0, this.pending = 0, this.done = 0, this.skipped = 0, this.failed = 0});

  factory AiJobBatch.fromJson(Map<String, dynamic> json) => AiJobBatch(
    id: _string(json['id']),
    createdAt: _date(json['createdAt']),
    total: _int(json['total']),
    pending: _int(json['pending']),
    done: _int(json['done']),
    skipped: _int(json['skipped']),
    failed: _int(json['failed']),
  );

  final String id;
  final DateTime? createdAt;
  final int total;
  final int pending;
  final int done;
  final int skipped;
  final int failed;

  bool get finished => pending == 0;
}

/// `GET /api/ai/jobs`: the account's job counts, why pending jobs wait
/// ([paused]: `AI_DISABLED`, `AI_NOT_OPTED_IN`, `AI_BUDGET_EXCEEDED`) and
/// the recent requests.
class AiJobStatus {
  const AiJobStatus({this.pending = 0, this.running = 0, this.done = 0, this.skipped = 0, this.failed = 0, this.paused, this.batches = const <AiJobBatch>[]});

  factory AiJobStatus.fromJson(Map<String, dynamic> json) {
    final paused = json['paused']?.toString();
    return AiJobStatus(
      pending: _int(json['pending']),
      running: _int(json['running']),
      done: _int(json['done']),
      skipped: _int(json['skipped']),
      failed: _int(json['failed']),
      paused: paused == null || paused.isEmpty ? null : paused,
      batches: _maps(json['batches']).map(AiJobBatch.fromJson).toList(),
    );
  }

  final int pending;
  final int running;
  final int done;
  final int skipped;
  final int failed;
  final String? paused;
  final List<AiJobBatch> batches;
}

/// A sorting rule: files matching every set condition go to the target
/// folder, decided without a model.
class AiRule {
  const AiRule({
    required this.id,
    required this.name,
    this.type,
    this.issuer,
    this.extension,
    this.namePattern,
    required this.targetFolderId,
    this.targetPath = '',
    this.enabled = true,
  });

  factory AiRule.fromJson(Map<String, dynamic> json) {
    final match = json['match'] is Map ? (json['match'] as Map).cast<String, dynamic>() : const <String, dynamic>{};
    String? text(Object? value) => value == null || '$value'.isEmpty ? null : '$value';
    return AiRule(
      id: _int(json['id']),
      name: _string(json['name']),
      type: text(match['type']),
      issuer: text(match['issuer']),
      extension: text(match['extension']),
      namePattern: text(match['namePattern']),
      targetFolderId: _string(json['targetFolderId']),
      targetPath: _string(json['targetPath']),
      enabled: json['enabled'] != false,
    );
  }

  final int id;
  final String name;

  /// A document type (`invoice` …), compared case-insensitively.
  final String? type;
  final String? issuer;

  /// Without the dot.
  final String? extension;

  /// A glob (`*`, `?`) over the whole name.
  final String? namePattern;
  final String targetFolderId;

  /// "/Finance/Telekom".
  final String targetPath;
  final bool enabled;

  /// The body of `POST /rules` and `PATCH /rules/:id`.
  Map<String, Object?> toJson() => <String, Object?>{
    'name': name,
    'match': <String, Object?>{'type': type, 'issuer': issuer, 'extension': extension, 'namePattern': namePattern},
    'targetFolderId': targetFolderId,
    'enabled': enabled,
  };
}

/// "Always move these here?": offered after three accepted moves of the
/// same type and issuer into one folder.
class AiRuleProposal {
  const AiRuleProposal({required this.id, required this.type, required this.issuer, required this.targetFolderId, this.targetPath = '', this.accepts = 0, this.name = ''});

  factory AiRuleProposal.fromJson(Map<String, dynamic> json) => AiRuleProposal(
    id: _int(json['id']),
    type: _string(json['type']),
    issuer: _string(json['issuer']),
    targetFolderId: _string(json['targetFolderId']),
    targetPath: _string(json['targetPath']),
    accepts: _int(json['accepts']),
    name: _string(json['name']),
  );

  final int id;
  final String type;
  final String issuer;
  final String targetFolderId;
  final String targetPath;
  final int accepts;
  final String name;
}

/// `POST /api/ai/files/:id/summary`: kept per content ([etag]).
class AiSummary {
  const AiSummary({required this.summary, this.model, this.etag = '', this.createdAt, this.cached = false});

  factory AiSummary.fromJson(Map<String, dynamic> json) {
    final model = json['model']?.toString();
    return AiSummary(
      summary: _string(json['summary']),
      model: model == null || model.isEmpty ? null : model,
      etag: _string(json['etag']),
      createdAt: _date(json['createdAt']),
      cached: json['cached'] == true,
    );
  }

  final String summary;
  final String? model;
  final String etag;
  final DateTime? createdAt;
  final bool cached;
}

/// `GET /api/admin/users/:id/allowed-models`: the models granted to one
/// account and its own monthly budget (null: the server default, 0: no
/// limit).
class AdminUserAi {
  const AdminUserAi({required this.modelIds, this.monthlyTokens});

  factory AdminUserAi.fromJson(Map<String, dynamic> json) => AdminUserAi(
    modelIds: _strings(json['modelIds']).toSet(),
    monthlyTokens: _optionalInt(json['monthlyTokens']),
  );

  final Set<String> modelIds;
  final int? monthlyTokens;
}

/// What `POST /api/ai/suggestions/bulk` did: how many were decided, and
/// suggestion id → error code for the rest.
class AiBulkResult {
  const AiBulkResult({required this.done, required this.failed});

  factory AiBulkResult.fromJson(Map<String, dynamic> json) => AiBulkResult(
    done: _int(json['done']),
    failed: <String, String>{
      for (final row in _maps(json['failed'])) _string(row['id']): _string(row['code']),
    },
  );

  final int done;
  final Map<String, String> failed;
}

/// One file of a duplicate group; [keep] marks the copy to keep.
class ReviewDuplicateItem {
  const ReviewDuplicateItem({required this.node, required this.size, this.takenAt, this.width, this.height, this.keep = false});

  factory ReviewDuplicateItem.fromJson(Map<String, dynamic> json) => ReviewDuplicateItem(
    node: FileNode.fromJson(json['node'] is Map ? (json['node'] as Map).cast<String, dynamic>() : const <String, dynamic>{}),
    size: _int(json['size']),
    takenAt: _date(json['takenAt']),
    width: _optionalInt(json['width']),
    height: _optionalInt(json['height']),
    keep: json['keep'] == true,
  );

  final FileNode node;
  final int size;
  final DateTime? takenAt;
  final int? width;
  final int? height;
  final bool keep;
}

/// Files with the same content (`exact`) or photos that look alike
/// (`similar`, [similarity] in percent).
class ReviewDuplicateGroup {
  const ReviewDuplicateGroup({required this.kind, required this.similarity, required this.items});

  factory ReviewDuplicateGroup.fromJson(Map<String, dynamic> json) => ReviewDuplicateGroup(
    kind: _string(json['kind']),
    similarity: _int(json['similarity']),
    items: _maps(json['items']).map(ReviewDuplicateItem.fromJson).where((item) => item.node.id.isNotEmpty).toList(),
  );

  final String kind;
  final int similarity;
  final List<ReviewDuplicateItem> items;

  bool get exact => kind == 'exact';

  /// A stable key for the group: its file ids.
  String get key => (items.map((item) => item.node.id).toList()..sort()).join(',');
}

/// `GET /api/ai/duplicates`: the groups, and how many photos are still
/// being compared.
class ReviewDuplicates {
  const ReviewDuplicates({this.groups = const <ReviewDuplicateGroup>[], this.pending = 0});

  factory ReviewDuplicates.fromJson(Map<String, dynamic> json) => ReviewDuplicates(
    groups: _maps(json['groups']).map(ReviewDuplicateGroup.fromJson).where((group) => group.items.length > 1).toList(),
    pending: _int(json['pending']),
  );

  final List<ReviewDuplicateGroup> groups;
  final int pending;
}
