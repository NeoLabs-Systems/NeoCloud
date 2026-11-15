import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/foundation.dart';

import 'field_types.dart';
import 'models.dart';
import 'signing_api.dart';
import 'value_format.dart';

/// The signer's flow in four steps.
enum SigningStep { intro, fill, review }

/// Where the autosave stands: [offline] means writes are waiting and being
/// retried.
enum SaveState { idle, saving, offline }

/// A picture or file the signer chose for a field. [preview] holds the bytes
/// while they are known on this device; after a reload they are fetched from
/// the server on demand.
class UploadedValue {
  const UploadedValue({this.preview, this.fileName, this.method, this.isImage = true});

  final Uint8List? preview;
  final String? fileName;
  final SignatureMethod? method;
  final bool isImage;
}

/// Everything the public signing page does, apart from drawing: loading the
/// link, whose turn it is, the current field, values and their autosave
/// (with retries), consent, finishing and declining.
///
/// Nothing is stored on the device: values live in memory and on the server.
/// The token never leaves the [PublicSigningApi].
class PublicSigningController extends ChangeNotifier {
  PublicSigningController({
    required this.api,
    this.pollInterval = const Duration(seconds: 20),
    this.resultPollInterval = const Duration(seconds: 4),
    this.saveDelay = const Duration(milliseconds: 600),
    this.retryBase = const Duration(seconds: 1),
    this.retryMax = const Duration(seconds: 30),
    DateTime Function()? now,
  }) : _now = now ?? DateTime.now;

  final PublicSigningApi api;

  /// How often a waiting page asks whether its turn has come.
  final Duration pollInterval;

  /// How often a finished page asks whether the signed file is ready.
  final Duration resultPollInterval;

  /// Quiet time after the last keystroke before a value is saved.
  final Duration saveDelay;
  final Duration retryBase;
  final Duration retryMax;
  final DateTime Function() _now;

  bool _disposed = false;

  // Loading

  bool isLoading = true;

  /// Why the link could not be opened (or reloaded).
  SigningException? loadError;
  PublicSigningView? view;

  PublicSigningState get state => view?.state ?? PublicSigningState.locked;

  /// Fields to fill in, in reading order.
  List<SigningField> fields = const <SigningField>[];

  /// Own fields that are not filled in by the signer (headings, locked
  /// values), drawn as they are.
  List<SigningField> fixedFields = const <SigningField>[];

  /// Other parties' fields that already carry a value, drawn read-only.
  List<SigningField> othersFields = const <SigningField>[];

  Future<void> start() async {
    await load();
  }

  Future<void> load({bool quiet = false}) async {
    if (!quiet) {
      isLoading = true;
      loadError = null;
      _notify();
    }
    try {
      final next = await api.view();
      if (_disposed) return;
      loadError = null;
      _ingest(next);
    } on SigningException catch (error) {
      if (_disposed) return;
      if (!quiet || view == null) loadError = error;
      _offline = error.isNetwork;
    } finally {
      if (!_disposed) {
        isLoading = false;
        _notify();
      }
    }
  }

  /// True after a refresh that could not reach the server.
  bool get isOffline => _offline || saveState == SaveState.offline;
  bool _offline = false;

  void _ingest(PublicSigningView next) {
    final first = view == null;
    final previousState = view?.state;
    view = next;
    _offline = false;
    final own = next.ownFields;
    fixedFields = own.where((field) => field.readonly || !FieldTypes.of(field.type).signerFillable).toList(growable: false);
    fields = _readingOrder(own.where((field) => !field.readonly && FieldTypes.of(field.type).signerFillable));
    othersFields = next.otherFields.where((field) => field.value != null).toList(growable: false);

    if (next.state == PublicSigningState.ready) {
      _readValues(next);
      if (first || previousState != PublicSigningState.ready) {
        step = next.party.consentedAt == null ? SigningStep.intro : SigningStep.fill;
        activeIndex = _firstOpen();
        if (step == SigningStep.fill && fields.isEmpty) step = SigningStep.review;
        if (step == SigningStep.fill && _firstOpenOrNull() == null && fields.isNotEmpty) step = SigningStep.review;
        if (next.party.consentedAt != null) _queueTodayDefaults(next);
      }
    }
    _syncPolling();
    if (next.state == PublicSigningState.completed) unawaited(_afterCompleted());
  }

  static List<SigningField> _readingOrder(Iterable<SigningField> source) {
    int page(SigningField f) => f.areas.isEmpty ? 0 : f.areas.first.page;
    double top(SigningField f) => f.areas.isEmpty ? 0 : f.areas.first.y;
    double left(SigningField f) => f.areas.isEmpty ? 0 : f.areas.first.x;
    final byTop = source.toList()
      ..sort((a, b) {
        final byPage = page(a).compareTo(page(b));
        if (byPage != 0) return byPage;
        final byY = top(a).compareTo(top(b));
        return byY != 0 ? byY : a.position.compareTo(b.position);
      });
    // Boxes within 1.5 % of the page height of the line's first box share the
    // line and are read from left to right.
    final ordered = <SigningField>[];
    var line = <SigningField>[];
    void endLine() {
      line.sort((a, b) => left(a).compareTo(left(b)));
      ordered.addAll(line);
      line = <SigningField>[];
    }

    for (final field in byTop) {
      if (line.isNotEmpty && (page(field) != page(line.first) || top(field) - top(line.first) > 0.015)) endLine();
      line.add(field);
    }
    endLine();
    return List<SigningField>.unmodifiable(ordered);
  }

  // Steps and the current field

  SigningStep step = SigningStep.intro;
  int activeIndex = 0;

  SigningField? get active => fields.isEmpty ? null : fields[activeIndex.clamp(0, fields.length - 1)];

  int get fieldCount => fields.length;

  /// Fields with a value (or optional and left empty counts as not done).
  int get filledCount => fields.where((field) => !_isEmpty(field)).length;

  void go(int index) {
    if (fields.isEmpty) return;
    activeIndex = index.clamp(0, fields.length - 1);
    step = SigningStep.fill;
    _notify();
  }

  void goToField(String fieldId) {
    final index = fields.indexWhere((field) => field.id == fieldId);
    if (index >= 0) go(index);
  }

  /// Next field, or the review after the last one. Stays put (and shows why)
  /// when the current value isn't acceptable.
  bool next() {
    final field = active;
    if (field != null && issueFor(field) != null) {
      _attempted.add(field.id);
      _notify();
      return false;
    }
    if (activeIndex >= fields.length - 1) {
      step = SigningStep.review;
    } else {
      activeIndex++;
    }
    _notify();
    return true;
  }

  void back() {
    if (step == SigningStep.review) {
      step = fields.isEmpty ? SigningStep.intro : SigningStep.fill;
      activeIndex = fields.isEmpty ? 0 : fields.length - 1;
    } else if (activeIndex > 0) {
      activeIndex--;
    }
    _notify();
  }

  bool get canGoBack => step == SigningStep.review ? fields.isNotEmpty : activeIndex > 0;

  /// Skipping is for optional fields without a value.
  bool get canSkip {
    final field = active;
    return field != null && !field.required;
  }

  void backToFields() {
    if (fields.isEmpty) return;
    step = SigningStep.fill;
    _notify();
  }

  int _firstOpen() => _firstOpenOrNull() ?? 0;

  int? _firstOpenOrNull() {
    for (var index = 0; index < fields.length; index++) {
      if (issueFor(fields[index]) != null || (_isEmpty(fields[index]) && fields[index].required)) return index;
    }
    return null;
  }

  // Values

  final Map<String, Object?> values = <String, Object?>{};
  final Set<String> _touched = <String>{};
  final Set<String> _attempted = <String>{};

  /// Fields the server reported as empty on finishing.
  Set<String> missingIds = <String>{};

  /// What the server said about a value it refused.
  final Map<String, String> fieldErrors = <String, String>{};

  void _readValues(PublicSigningView source) {
    for (final field in fields) {
      if (_dirty.containsKey(field.id) || _uploads.containsKey(field.id)) continue;
      final stored = source.valueOf(field.id);
      if (stored == null) {
        values.remove(field.id);
      } else if (stored.hasFile) {
        final old = values[field.id];
        values[field.id] = UploadedValue(
          preview: old is UploadedValue ? old.preview : null,
          fileName: stored.fileName,
          method: stored.method,
          isImage: FieldTypes.of(field.type).imageAccept || FieldTypes.of(field.type).needsPad,
        );
      } else {
        values[field.id] = stored.value;
      }
    }
  }

  bool _isEmpty(SigningField field) => FieldTypes.of(field.type).isEmpty(values[field.id]);

  Object? valueOf(String fieldId) => values[fieldId];

  /// The problem with the field's value, or null. Empty required: a problem.
  ValueIssue? issueFor(SigningField field) => FieldTypes.of(field.type).validate(field, values[field.id]);

  /// The issue to show now: after the signer left the field or tried to go on.
  ValueIssue? visibleIssue(SigningField field) {
    final issue = issueFor(field);
    if (issue == null) return null;
    if (_attempted.contains(field.id) || missingIds.contains(field.id)) return issue;
    if (issue.code == ValueIssueCode.required) return null;
    return _touched.contains(field.id) ? issue : null;
  }

  void touch(SigningField field) {
    if (_touched.add(field.id)) _notify();
  }

  /// Required fields that are empty or wrong, in reading order.
  List<SigningField> get problems => fields.where((field) => issueFor(field) != null).toList(growable: false);

  /// Optional fields left empty.
  List<SigningField> get skipped => fields.where((field) => !field.required && _isEmpty(field)).toList(growable: false);

  /// A typed or picked value. It is saved after a short pause, or at once
  /// with [immediate] (choices, checkboxes).
  void setValue(SigningField field, Object? raw, {bool immediate = false}) {
    final spec = FieldTypes.of(field.type);
    final value = raw == null ? null : spec.normalize(field.prefs, raw);
    if (spec.isEmpty(value)) {
      values.remove(field.id);
    } else {
      values[field.id] = value;
    }
    fieldErrors.remove(field.id);
    missingIds.remove(field.id);
    final sendable = spec.isEmpty(value) ? null : value;
    if (sendable != null && spec.validate(field.copyWith(required: false), sendable) != null) {
      // Not valid yet (a half-typed phone number): keep it here, save later.
      _dirty.remove(field.id);
      _notify();
      return;
    }
    _dirty[field.id] = _Pending(sendable, ++_version);
    _notify();
    _schedule(immediate);
  }

  /// Takes a picture or file for the field and saves it at once.
  void setUpload(
    SigningField field, {
    required Uint8List bytes,
    required String fileName,
    String contentType = 'image/png',
    SignatureMethod? method,
    String? typedText,
  }) {
    final spec = FieldTypes.of(field.type);
    values[field.id] = UploadedValue(preview: bytes, fileName: fileName, method: method, isImage: spec.needsPad || spec.imageAccept);
    _images.remove(field.id);
    fieldErrors.remove(field.id);
    missingIds.remove(field.id);
    _dirty.remove(field.id);
    _uploads[field.id] = _Upload(bytes, fileName, contentType, method, typedText, ++_version);
    _notify();
    _schedule(true);
  }

  void clearValue(SigningField field) => setValue(field, null, immediate: true);

  /// Whole-document fill for initials: the signature the signer just made.
  bool get hasEmptyInitials => fields.any((field) => field.type == FieldType.initials && _isEmpty(field));

  // Autosave

  final Map<String, _Pending> _dirty = <String, _Pending>{};
  final Map<String, _Upload> _uploads = <String, _Upload>{};
  int _version = 0;
  Timer? _saveTimer;
  Timer? _retryTimer;
  int _attemptCount = 0;
  Future<bool>? _draining;
  SaveState saveState = SaveState.idle;

  /// Something typed or picked has not reached the server yet.
  bool get hasUnsaved => _dirty.isNotEmpty || _uploads.isNotEmpty;

  void _schedule(bool immediate) {
    _saveTimer?.cancel();
    if (!_canWrite) return;
    if (immediate) {
      unawaited(_drain());
    } else {
      _saveTimer = Timer(saveDelay, () => unawaited(_drain()));
    }
  }

  bool get _canWrite => view?.state == PublicSigningState.ready && view?.party.consentedAt != null;

  /// Saves everything that is waiting. Resolves true when nothing is left,
  /// false when the server could not be reached (it keeps retrying).
  Future<bool> flush() {
    _saveTimer?.cancel();
    _retryTimer?.cancel();
    return _drain();
  }

  Future<bool> _drain() {
    final running = _draining;
    if (running != null) return running;
    final done = _runQueue().whenComplete(() => _draining = null);
    return _draining = done;
  }

  Future<bool> _runQueue() async {
    while (!_disposed && _canWrite && (_dirty.isNotEmpty || _uploads.isNotEmpty)) {
      saveState = SaveState.saving;
      _notify();
      final String id;
      final bool isUpload;
      if (_uploads.isNotEmpty) {
        id = _uploads.keys.first;
        isUpload = true;
      } else {
        id = _dirty.keys.first;
        isUpload = false;
      }
      try {
        if (isUpload) {
          final job = _uploads[id]!;
          await api.uploadValue(id, bytes: job.bytes, fileName: job.fileName, contentType: job.contentType, method: job.method, typedText: job.typedText);
          if (_uploads[id]?.version == job.version) _uploads.remove(id);
        } else {
          final job = _dirty[id]!;
          await api.saveValue(id, job.value);
          if (_dirty[id]?.version == job.version) _dirty.remove(id);
        }
        _attemptCount = 0;
      } on SigningException catch (error) {
        if (_disposed) return false;
        if (!await _handleSaveFailure(id, isUpload, error)) return false;
      }
    }
    if (_disposed) return false;
    _attemptCount = 0;
    saveState = SaveState.idle;
    _notify();
    return !hasUnsaved;
  }

  /// Returns true when the queue can go on with the next job.
  Future<bool> _handleSaveFailure(String id, bool isUpload, SigningException error) async {
    switch (error.kind) {
      case SigningErrorKind.invalid:
        // The server refused this value: tell the signer, drop the job.
        fieldErrors[id] = error.message;
        if (isUpload) {
          _uploads.remove(id);
          values.remove(id);
        } else {
          _dirty.remove(id);
        }
        return true;
      case SigningErrorKind.conflict:
      case SigningErrorKind.notFound:
      case SigningErrorKind.expired:
        // The document moved on (closed, completed, link replaced).
        _dirty.clear();
        _uploads.clear();
        saveState = SaveState.idle;
        unawaited(load(quiet: true));
        return false;
      case SigningErrorKind.network:
      case SigningErrorKind.rateLimited:
      case SigningErrorKind.unauthorized:
      case SigningErrorKind.server:
        saveState = SaveState.offline;
        _scheduleRetry(error.retryAfter);
        _notify();
        return false;
    }
  }

  void _scheduleRetry(Duration? hint) {
    _attemptCount++;
    final backoff = retryBase * math.pow(2, math.min(_attemptCount - 1, 8)).toInt();
    var wait = backoff > retryMax ? retryMax : backoff;
    if (hint != null && hint > wait) wait = hint;
    _retryTimer?.cancel();
    _retryTimer = Timer(wait, () => unawaited(_drain()));
  }

  /// Tries the waiting writes right now (the "try again" button).
  Future<bool> retryNow() {
    _retryTimer?.cancel();
    return _drain();
  }

  // Dates that start as today

  void _queueTodayDefaults(PublicSigningView source) {
    for (final field in fields) {
      if (field.type != FieldType.date || !field.prefs.defaultToday) continue;
      if (source.valueOf(field.id) != null || values.containsKey(field.id)) continue;
      values[field.id] = toSigningIsoDate(_now());
      _dirty[field.id] = _Pending(values[field.id], ++_version);
    }
    if (_dirty.isNotEmpty) _schedule(true);
  }

  // Consent

  bool isConsenting = false;

  /// Set once the language of the document has been applied to the page.
  bool languageApplied = false;

  /// The error of the last consent, finish or decline attempt.
  SigningException? actionError;

  Future<bool> consent(String name, {String? timezone, int? tzOffsetMinutes}) async {
    if (isConsenting) return false;
    isConsenting = true;
    actionError = null;
    _notify();
    try {
      await api.consent(signerName: name.trim(), timezone: timezone, tzOffsetMinutes: tzOffsetMinutes);
      await load(quiet: true);
      if (_disposed) return false;
      final current = view;
      if (current == null || current.party.consentedAt == null) return false;
      step = fields.isEmpty ? SigningStep.review : SigningStep.fill;
      activeIndex = _firstOpen();
      _queueTodayDefaults(current);
      return true;
    } on SigningException catch (error) {
      actionError = error;
      return false;
    } finally {
      isConsenting = false;
      _notify();
    }
  }

  /// The name the signer typed at consent, to suggest for the signature.
  String get signerName => view?.party.signerName ?? '';

  // Finish and decline

  bool isFinishing = false;

  Future<void> finish() async {
    if (isFinishing) return;
    final open = problems;
    if (open.isNotEmpty) {
      missingIds = <String>{for (final field in open) field.id};
      _notify();
      return;
    }
    isFinishing = true;
    actionError = null;
    _notify();
    try {
      final saved = await flush();
      if (!saved) {
        actionError = const SigningException(SigningErrorKind.network);
        return;
      }
      await api.complete();
      await load(quiet: true);
    } on SigningException catch (error) {
      if (error.fieldIds.isNotEmpty) {
        missingIds = error.fieldIds.toSet();
        final first = fields.indexWhere((field) => missingIds.contains(field.id));
        if (first >= 0) go(first);
      } else if (error.kind == SigningErrorKind.conflict) {
        await load(quiet: true);
      }
      actionError = error;
    } finally {
      isFinishing = false;
      _notify();
    }
  }

  bool isDeclining = false;

  Future<bool> decline(String reason) async {
    if (isDeclining) return false;
    isDeclining = true;
    actionError = null;
    _notify();
    try {
      await api.decline(reason: reason);
      _dirty.clear();
      _uploads.clear();
      await load(quiet: true);
      return true;
    } on SigningException catch (error) {
      actionError = error;
      if (error.kind == SigningErrorKind.conflict) await load(quiet: true);
      return false;
    } finally {
      isDeclining = false;
      _notify();
    }
  }

  void dismissActionError() {
    actionError = null;
    _notify();
  }

  // After finishing

  /// The signed file can be downloaded.
  bool finalReady = false;

  /// Other signers have not finished yet.
  bool get othersPending {
    final progress = view?.progress;
    if (progress == null || progress.total == 0) return false;
    return progress.done < progress.total;
  }

  /// How many signers have finished and how many there are (no names).
  PublicProgress get progress => view?.progress ?? const PublicProgress();

  bool _resultPending = false;

  Future<void> _afterCompleted() async {
    if (_resultPending || finalReady) return;
    _resultPending = true;
    try {
      finalReady = await api.finalAvailable();
    } on SigningException {
      finalReady = false;
    } finally {
      _resultPending = false;
    }
    _syncPolling();
    _notify();
  }

  /// The signed file; with [certificate] false the appended signature pages
  /// (certificate and audit trail) are left out.
  Future<Uint8List> downloadFinal({bool certificate = true}) => api.finalPdf(certificate: certificate);

  /// How many other signers go first (a number, never their names).
  int get waitingCount => view?.turn.waitingCount ?? 0;

  // The PDF and pictures

  Uint8List? pdfBytes;
  bool pdfFailed = false;
  bool _pdfLoading = false;

  Future<void> ensurePdf() async {
    if (pdfBytes != null || _pdfLoading) return;
    _pdfLoading = true;
    pdfFailed = false;
    try {
      pdfBytes = await api.pdf();
    } on SigningException {
      pdfFailed = true;
    } finally {
      _pdfLoading = false;
      _notify();
    }
  }

  final Map<String, Future<Uint8List>> _images = <String, Future<Uint8List>>{};

  /// The picture a field holds (own, after a reload, or another party's).
  Future<Uint8List> imageFor(String fieldId) {
    final own = values[fieldId];
    if (own is UploadedValue && own.preview != null) return Future<Uint8List>.value(own.preview);
    return _images.putIfAbsent(fieldId, () => api.valueImage(fieldId));
  }

  // Polling

  Timer? _poll;

  /// While waiting: whether the turn has come. After finishing: whether the
  /// others have, and the signed file is ready.
  void _syncPolling() {
    final current = view?.state;
    final Duration? every;
    if (current == PublicSigningState.waiting) {
      every = pollInterval;
    } else if (current == PublicSigningState.completed && !finalReady) {
      every = othersPending ? pollInterval * 2 : resultPollInterval;
    } else {
      every = null;
    }
    _poll?.cancel();
    _poll = every == null ? null : Timer.periodic(every, (_) => unawaited(_refresh()));
  }

  Future<void> _refresh() async {
    if (_disposed) return;
    await load(quiet: true);
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _saveTimer?.cancel();
    _retryTimer?.cancel();
    _poll?.cancel();
    super.dispose();
  }
}

class _Pending {
  const _Pending(this.value, this.version);

  final Object? value;
  final int version;
}

class _Upload {
  const _Upload(this.bytes, this.fileName, this.contentType, this.method, this.typedText, this.version);

  final Uint8List bytes;
  final String fileName;
  final String contentType;
  final SignatureMethod? method;
  final String? typedText;
  final int version;
}
