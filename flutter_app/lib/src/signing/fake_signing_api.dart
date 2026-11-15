import 'dart:math' as math;
import 'dart:convert';
import 'dart:typed_data';

import 'field_types.dart';
import 'models.dart';
import 'signing_api.dart';

/// In-memory stand-ins for the two signing APIs, so the editor, the status
/// page and the public flow can be built and tested without a server. They
/// follow the contract closely enough for the happy paths (create, edit,
/// send, sign, finish) and can be made to fail on demand.

/// A valid PDF of [pages] A4 pages ("Page n" at the top), for pdfrx.
Uint8List buildSamplePdf({int pages = 2, double width = 595, double height = 842}) {
  final objects = <String>[];
  final kids = <String>[];
  // 1: catalog, 2: page tree, 3: font, then page + content per page.
  objects.add('<< /Type /Catalog /Pages 2 0 R >>');
  for (var index = 0; index < pages; index++) {
    kids.add('${4 + index * 2} 0 R');
  }
  objects.add('<< /Type /Pages /Kids [${kids.join(' ')}] /Count $pages >>');
  objects.add('<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>');
  for (var index = 0; index < pages; index++) {
    final content = 'BT /F1 18 Tf 60 ${height - 80} Td (Page ${index + 1}) Tj ET';
    objects.add('<< /Type /Page /Parent 2 0 R /MediaBox [0 0 $width $height] /Contents ${5 + index * 2} 0 R '
        '/Resources << /Font << /F1 3 0 R >> >> >>');
    objects.add('<< /Length ${content.length} >>\nstream\n$content\nendstream');
  }
  final out = StringBuffer('%PDF-1.4\n');
  final offsets = <int>[];
  for (var index = 0; index < objects.length; index++) {
    offsets.add(out.length);
    out.write('${index + 1} 0 obj\n${objects[index]}\nendobj\n');
  }
  final xref = out.length;
  out.write('xref\n0 ${objects.length + 1}\n0000000000 65535 f \n');
  for (final offset in offsets) {
    out.write('${offset.toString().padLeft(10, '0')} 00000 n \n');
  }
  out.write('trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n$xref\n%%EOF\n');
  return Uint8List.fromList(latin1.encode(out.toString()));
}

class FakeSigningApi implements SigningApi {
  FakeSigningApi({Uint8List? pdfBytes, this.pageCount = 2}) : _pdf = pdfBytes ?? buildSamplePdf(pages: pageCount);

  final Uint8List _pdf;
  final int pageCount;

  /// Throws this on the next call, once.
  SigningException? failNext;

  final Map<String, SigningDocument> _documents = <String, SigningDocument>{};
  final Map<String, List<SigningParty>> _parties = <String, List<SigningParty>>{};
  final Map<String, List<SigningField>> _fields = <String, List<SigningField>>{};
  final Map<String, List<SigningEvent>> _events = <String, List<SigningEvent>>{};
  final Map<String, List<SigningValue>> _values = <String, List<SigningValue>>{};
  int _counter = 0;

  String _id(String prefix) => '$prefix${++_counter}';

  void _maybeFail() {
    final error = failNext;
    if (error != null) {
      failNext = null;
      throw error;
    }
  }

  SigningException get _missing => const SigningException(SigningErrorKind.notFound, statusCode: 404);

  SigningDocument _document(String id) => _documents[id] ?? (throw _missing);

  SigningDetail _detail(String id) => SigningDetail(
    document: _document(id),
    parties: List<SigningParty>.of(_parties[id] ?? const <SigningParty>[]),
    fields: List<SigningField>.of(_fields[id] ?? const <SigningField>[]),
  );

  void _requireEditable(String id) {
    if (!_document(id).status.isEditable) throw const SigningException(SigningErrorKind.conflict, statusCode: 409);
  }

  void _log(String id, SigningEventType type, {String? partyId}) {
    (_events[id] ??= <SigningEvent>[]).add(SigningEvent(id: _id('e'), type: type, rawType: type.name, partyId: partyId, at: DateTime.now()));
  }

  SigningDocument _copy(SigningDocument d, {String? title, SigningStatus? status, SigningOrder? order, DateTime? expiresAt, String? locale, bool clearExpiry = false, String? finalNodeId}) =>
      SigningDocument(
        id: d.id,
        title: title ?? d.title,
        status: status ?? d.status,
        signingOrder: order ?? d.signingOrder,
        locale: locale ?? d.locale,
        expiresAt: clearExpiry ? null : (expiresAt ?? d.expiresAt),
        pageCount: d.pageCount,
        sourceNodeId: d.sourceNodeId,
        finalNodeId: finalNodeId ?? d.finalNodeId,
        pageSizes: d.pageSizes,
        createdAt: d.createdAt,
        updatedAt: DateTime.now(),
        sentAt: status == SigningStatus.sent ? DateTime.now() : d.sentAt,
        completedAt: d.completedAt,
      );

  /// Test hook: puts a party into a state, as if they signed.
  void setPartyStatus(String documentId, String partyId, PartyStatus status) {
    final parties = _parties[documentId] ?? (throw _missing);
    _parties[documentId] = <SigningParty>[
      for (final party in parties)
        party.id == partyId
            ? SigningParty(id: party.id, name: party.name, position: party.position, color: party.color, email: party.email, status: status, completedAt: status == PartyStatus.completed ? DateTime.now() : null, hasLink: party.hasLink, linkHint: party.linkHint)
            : party,
    ];
    final all = _parties[documentId]!;
    final next = all.every((party) => party.status == PartyStatus.completed)
        ? SigningStatus.completed
        : (all.any((party) => party.status != PartyStatus.pending) ? SigningStatus.inProgress : _document(documentId).status);
    _documents[documentId] = _copy(_document(documentId), status: next);
  }

  /// Test hook: the signed file of a completed document.
  void setFinalNode(String documentId, String nodeId) {
    _documents[documentId] = _copy(_document(documentId), status: SigningStatus.completed, finalNodeId: nodeId);
  }

  /// Test hook: values to show in the progress preview.
  void setPreviewValues(String documentId, List<SigningValue> values) => _values[documentId] = values;

  /// Whether office documents count as convertible.
  bool officeConversion = false;

  @override
  Future<SigningCapabilities> capabilities() async {
    _maybeFail();
    return SigningCapabilities(office: officeConversion);
  }

  @override
  Future<SigningDetail> createDocument({required String nodeId, String? title, String? locale}) async {
    _maybeFail();
    final id = _id('doc');
    final now = DateTime.now();
    _documents[id] = SigningDocument(id: id, title: title ?? 'Document', pageCount: pageCount, sourceNodeId: nodeId, createdAt: now, updatedAt: now);
    _parties[id] = <SigningParty>[SigningParty(id: _id('p'), name: 'Party 1', color: '#c9a227')];
    _fields[id] = <SigningField>[];
    _log(id, SigningEventType.created);
    return _detail(id);
  }

  @override
  Future<SigningListPage> list({SigningStatus? status, int? limit, String? cursor}) async {
    _maybeFail();
    final items = _documents.values.where((doc) => status == null || doc.status == status).map((doc) => SigningListItem(
      id: doc.id,
      title: doc.title,
      status: doc.status,
      parties: <SigningListParty>[
        for (final party in _parties[doc.id] ?? const <SigningParty>[]) SigningListParty(id: party.id, name: party.name, color: party.color, status: party.status),
      ],
      createdAt: doc.createdAt,
      updatedAt: doc.updatedAt,
      completedAt: doc.completedAt,
      finalNodeId: doc.finalNodeId,
      sourceNodeId: doc.sourceNodeId,
    ));
    final sorted = items.toList()..sort((a, b) => (b.updatedAt ?? DateTime(0)).compareTo(a.updatedAt ?? DateTime(0)));
    return SigningListPage(items: limit == null ? sorted : sorted.take(limit).toList());
  }

  @override
  Future<SigningDetail> get(String id) async {
    _maybeFail();
    return _detail(id);
  }

  @override
  Future<Uint8List> pdf(String id) async {
    _maybeFail();
    _document(id);
    return _pdf;
  }

  @override
  Future<List<SigningValue>> previewValues(String id) async {
    _maybeFail();
    return List<SigningValue>.of(_values[id] ?? const <SigningValue>[]);
  }

  @override
  Future<SigningDocument> update(String id, SigningSettingsPatch patch) async {
    _maybeFail();
    _requireEditable(id);
    return _documents[id] = _copy(_document(id), title: patch.title, order: patch.signingOrder, expiresAt: patch.expiresAt, clearExpiry: patch.clearExpiry, locale: patch.locale);
  }

  @override
  Future<List<SigningField>> saveFields(String id, List<SigningField> fields) async {
    _maybeFail();
    _requireEditable(id);
    final parties = _parties[id] ?? const <SigningParty>[];
    for (final field in fields) {
      if (!parties.any((party) => party.id == field.partyId)) throw const SigningException(SigningErrorKind.invalid, statusCode: 400, message: 'Unknown party');
    }
    final stored = <SigningField>[
      for (final (index, field) in fields.indexed) field.copyWith(id: field.isLocal ? _id('f') : field.id, position: index),
    ];
    _fields[id] = stored;
    _documents[id] = _copy(_document(id));
    return List<SigningField>.of(stored);
  }

  @override
  Future<List<SigningParty>> saveParties(String id, List<PartyDraft> parties) async {
    _maybeFail();
    _requireEditable(id);
    if (parties.isEmpty || parties.length > 20) throw const SigningException(SigningErrorKind.invalid, statusCode: 400);
    final existing = <String, SigningParty>{for (final party in _parties[id] ?? const <SigningParty>[]) party.id: party};
    final stored = <SigningParty>[
      for (final (index, draft) in parties.indexed)
        SigningParty(
          id: draft.id != null && existing.containsKey(draft.id) ? draft.id! : _id('p'),
          name: draft.name,
          position: index,
          color: draft.color ?? '',
          email: draft.email,
        ),
    ];
    _parties[id] = stored;
    final keep = stored.map((party) => party.id).toSet();
    _fields[id] = (_fields[id] ?? const <SigningField>[]).where((field) => keep.contains(field.partyId)).toList();
    return List<SigningParty>.of(stored);
  }

  @override
  Future<int> importFields(String id) async {
    _maybeFail();
    _requireEditable(id);
    return 0;
  }

  @override
  Future<SigningSendResult> send(String id) async {
    _maybeFail();
    _requireEditable(id);
    final parties = _parties[id] ?? const <SigningParty>[];
    final links = <SigningLink>[for (final party in parties) SigningLink(partyId: party.id, url: 'https://cloud.example/app/sign/fake-${party.id}-${_counter++}')];
    _parties[id] = <SigningParty>[
      for (final (index, party) in parties.indexed)
        SigningParty(
          id: party.id,
          name: party.name,
          position: party.position,
          color: party.color,
          email: party.email,
          status: _document(id).signingOrder == SigningOrder.preserved && index > 0 ? PartyStatus.waiting : PartyStatus.pending,
          hasLink: true,
          linkHint: 'a1b2',
        ),
    ];
    _documents[id] = _copy(_document(id), status: SigningStatus.sent);
    _log(id, SigningEventType.sent);
    return SigningSendResult(document: _document(id), links: links);
  }

  @override
  Future<SigningLink> regenerateLink(String id, String partyId) async {
    _maybeFail();
    _document(id);
    _log(id, SigningEventType.linkRegenerated, partyId: partyId);
    return SigningLink(partyId: partyId, url: 'https://cloud.example/app/sign/fake-$partyId-${_counter++}');
  }

  @override
  Future<SigningDocument> cancel(String id) async {
    _maybeFail();
    _log(id, SigningEventType.canceled);
    return _documents[id] = _copy(_document(id), status: SigningStatus.canceled);
  }

  @override
  Future<void> revokeParty(String id, String partyId) async {
    _maybeFail();
    _document(id);
    _log(id, SigningEventType.revoked, partyId: partyId);
  }

  @override
  Future<void> delete(String id) async {
    _maybeFail();
    _document(id);
    _documents.remove(id);
    _parties.remove(id);
    _fields.remove(id);
    _events.remove(id);
  }

  @override
  Future<SigningDetail> duplicate(String id) async {
    _maybeFail();
    final source = _detail(id);
    final copy = await createDocument(nodeId: source.document.sourceNodeId ?? '', title: source.document.title);
    final copyId = copy.document.id;
    final parties = await saveParties(copyId, <PartyDraft>[for (final party in source.parties) PartyDraft(name: party.name, email: party.email, color: party.color)]);
    final idByPosition = <String, String>{for (final (index, party) in source.parties.indexed) party.id: parties[index].id};
    await saveFields(copyId, <SigningField>[
      for (final field in source.fields) SigningField.local(partyId: idByPosition[field.partyId]!, type: field.type, areas: field.areas, name: field.name, required: field.required, prefs: field.prefs),
    ]);
    return _detail(copyId);
  }

  final Map<String, Uint8List> _valueImages = <String, Uint8List>{};

  /// Test hook: the picture [valueImage] returns for a field.
  void setValueImage(String fieldId, Uint8List bytes) => _valueImages[fieldId] = bytes;

  @override
  Future<Uint8List> valueImage(String id, String fieldId) async {
    _maybeFail();
    return _valueImages[fieldId] ?? (throw _missing);
  }

  @override
  Future<SigningDetail> retryFinalization(String id) async {
    _maybeFail();
    return _detail(id);
  }

  @override
  Future<SigningEventLog> events(String id) async {
    _maybeFail();
    _document(id);
    return SigningEventLog(events: List<SigningEvent>.of(_events[id] ?? const <SigningEvent>[]), chainValid: true);
  }

  @override
  Future<Map<String, dynamic>> auditJson(String id) async {
    _maybeFail();
    return <String, dynamic>{'documentId': id};
  }

  @override
  Future<Uint8List> finalPdf(String id, {bool certificate = true}) async {
    _maybeFail();
    if (_document(id).status != SigningStatus.completed) throw _missing;
    finalRequests.add(certificate);
    return certificate ? _pdf : (cleanBytes ?? _pdf);
  }

  /// The `certificate` flag of every [finalPdf] call, for assertions.
  final List<bool> finalRequests = <bool>[];

  /// What the signature-pages-free download returns (default: the sample PDF).
  Uint8List? cleanBytes;

  /// Node ids asked for through [cleanPdfForNode].
  final List<String> cleanRequests = <String>[];

  @override
  Future<Uint8List> cleanPdfForNode(String nodeId) async {
    _maybeFail();
    cleanRequests.add(nodeId);
    return cleanBytes ?? _pdf;
  }
}

/// One signer's view of a document, held in memory. Behaves like the server
/// where the flow depends on it: values are checked per field type, writes
/// need consent and the party's turn, `complete` names the empty required
/// fields.
class FakePublicSigningApi implements PublicSigningApi {
  FakePublicSigningApi({required PublicSigningView view, Uint8List? pdfBytes, this.finalBytes})
    : _view = view,
      _pdf = pdfBytes ?? buildSamplePdf(pages: view.document.pageCount == 0 ? 2 : view.document.pageCount) {
    for (final value in view.myValues) {
      _values[value.fieldId] = value;
    }
  }

  PublicSigningView _view;
  final Uint8List _pdf;
  final Map<String, SigningValue> _values = <String, SigningValue>{};
  final Map<String, Uint8List> _images = <String, Uint8List>{};
  final List<SigningException> _failures = <SigningException>[];

  /// The signed PDF; null: not available yet.
  Uint8List? finalBytes;

  /// Everything the signer has done, in order, for assertions: `view`,
  /// `consent`, `save:<fieldId>`, `upload:<fieldId>`, `complete`, `decline`.
  final List<String> calls = <String>[];

  /// What the last upload said about itself, by field id.
  final Map<String, SignatureMethod?> uploadMethods = <String, SignatureMethod?>{};

  String? lastSignerName;
  String? lastDeclineReason;

  /// Throws this on the next call, once.
  SigningException? get failNext => _failures.isEmpty ? null : _failures.first;
  set failNext(SigningException? error) {
    _failures.clear();
    if (error != null) _failures.add(error);
  }

  /// Throws [error] on the next [times] calls.
  void failTimes(int times, SigningException error) {
    _failures
      ..clear()
      ..addAll(List<SigningException>.filled(times, error));
  }

  /// The server moved on (the turn came, the sender canceled, …).
  void serverMoves(PublicSigningView view) => _view = view;

  void _maybeFail() {
    if (_failures.isNotEmpty) throw _failures.removeAt(0);
  }

  PublicSigningView _rebuild({PublicSigningState? state, PublicParty? party, PublicProgress? progress}) => PublicSigningView(
    state: state ?? _view.state,
    document: _view.document,
    party: party ?? _view.party,
    progress: progress ?? _view.progress,
    fields: _view.fields,
    myValues: _values.values.toList(growable: false),
    turn: _view.turn,
  );

  @override
  Future<PublicSigningView> view() async {
    _maybeFail();
    calls.add('view');
    return _view = _rebuild();
  }

  @override
  Future<Uint8List> pdf() async {
    _maybeFail();
    return _pdf;
  }

  @override
  Future<void> consent({required String signerName, String? timezone, int? tzOffsetMinutes}) async {
    _maybeFail();
    _guardReady();
    final party = _view.party;
    _view = _rebuild(
      party: PublicParty(id: party.id, name: party.name, color: party.color, status: party.status, consentedAt: DateTime.now(), signerName: signerName),
    );
    lastSignerName = signerName;
    calls.add('consent');
  }

  SigningField _own(String fieldId) {
    for (final field in _view.ownFields) {
      if (field.id == fieldId && !field.readonly) return field;
    }
    throw const SigningException(SigningErrorKind.notFound, statusCode: 404);
  }

  @override
  Future<void> saveValue(String fieldId, Object? value) async {
    _maybeFail();
    _guardWritable();
    final field = _own(fieldId);
    final spec = FieldTypes.of(field.type);
    final issue = value == null ? null : spec.validate(field.copyWith(required: false), value);
    if (issue != null) throw const SigningException(SigningErrorKind.invalid, statusCode: 400, code: 'INVALID_VALUE', message: 'The value is not valid.');
    if (value == null || spec.isEmpty(value)) {
      _values.remove(fieldId);
    } else {
      _values[fieldId] = SigningValue(fieldId: fieldId, partyId: _view.party.id, value: value);
    }
    calls.add('save:$fieldId');
  }

  @override
  Future<void> uploadValue(
    String fieldId, {
    required Uint8List bytes,
    required String fileName,
    String contentType = 'image/png',
    SignatureMethod? method,
    String? typedText,
  }) async {
    _maybeFail();
    _guardWritable();
    _own(fieldId);
    _values[fieldId] = SigningValue(fieldId: fieldId, partyId: _view.party.id, hasFile: true, method: method, fileName: fileName);
    _images[fieldId] = bytes;
    uploadMethods[fieldId] = method;
    calls.add('upload:$fieldId');
  }

  void _guardReady() {
    if (_view.state != PublicSigningState.ready) throw const SigningException(SigningErrorKind.conflict, statusCode: 409, code: 'DOCUMENT_CLOSED');
  }

  void _guardWritable() {
    _guardReady();
    if (_view.party.consentedAt == null) throw const SigningException(SigningErrorKind.conflict, statusCode: 409, code: 'CONSENT_REQUIRED');
  }

  @override
  Future<Uint8List> valueImage(String fieldId) async {
    _maybeFail();
    return _images[fieldId] ?? (throw const SigningException(SigningErrorKind.notFound, statusCode: 404));
  }

  @override
  Future<PublicSigningState> complete() async {
    _maybeFail();
    _guardWritable();
    final missing = <String>[
      for (final field in _view.ownFields)
        if (field.required && !field.readonly && FieldTypes.of(field.type).signerFillable && !_isFilled(field)) field.id,
    ];
    if (missing.isNotEmpty) {
      throw SigningException(SigningErrorKind.invalid, statusCode: 400, code: 'FIELDS_INCOMPLETE', message: 'Some required fields are still empty.', fieldIds: missing);
    }
    calls.add('complete');
    final progress = _view.progress;
    _view = _rebuild(
      state: PublicSigningState.completed,
      progress: PublicProgress(done: math.min(progress.total, progress.done + 1), total: progress.total),
    );
    return _view.state;
  }

  bool _isFilled(SigningField field) {
    final value = _values[field.id];
    return value != null && (value.hasFile || !FieldTypes.of(field.type).isEmpty(value.value));
  }

  @override
  Future<void> decline({String? reason}) async {
    _maybeFail();
    _guardReady();
    calls.add('decline');
    lastDeclineReason = reason;
    _view = _rebuild(state: PublicSigningState.declined);
  }

  @override
  Future<bool> finalAvailable() async {
    _maybeFail();
    return finalBytes != null;
  }

  @override
  Future<Uint8List> finalPdf({bool certificate = true}) async {
    _maybeFail();
    finalRequests.add(certificate);
    final bytes = finalBytes;
    if (bytes == null) throw const SigningException(SigningErrorKind.notFound, statusCode: 404);
    return certificate ? bytes : (cleanBytes ?? bytes);
  }

  /// The `certificate` flag of every [finalPdf] call, for assertions.
  final List<bool> finalRequests = <bool>[];

  /// What the download without signature pages returns (default: the same).
  Uint8List? cleanBytes;
}
