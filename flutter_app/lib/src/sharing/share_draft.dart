import '../api/models.dart';

// The share dialog's form as plain data: what POST /api/shares needs for a
// new link, and the PATCH body with only what changed for an existing one.

/// Share passwords the server accepts (roadmap contract: ≥ 8 characters).
const int minSharePasswordLength = 8;
const int maxShareNoteLength = 500;
const int maxShareDownloads = 1000000;

enum ShareExpiryChoice { day, week, month, never, custom }

enum ShareLimitChoice { none, once, custom }

enum ShareDraftProblem { passwordTooShort, limitInvalid, dateInPast }

class ShareDraft {
  ShareDraft({
    this.mode = ShareMode.view,
    this.expiry = ShareExpiryChoice.week,
    this.customDate,
    this.protect = false,
    this.password = '',
    this.limit = ShareLimitChoice.none,
    this.customLimit = 5,
    this.note = '',
  }) : original = null;

  /// The form for editing [link]: its expiry shows as a custom date.
  ShareDraft.fromLink(ShareLink link)
    : original = link,
      mode = link.mode,
      expiry = link.expiresAt == null ? ShareExpiryChoice.never : ShareExpiryChoice.custom,
      customDate = link.expiresAt,
      protect = link.hasPassword,
      password = '',
      limit = switch (link.maxDownloads) {
        null => ShareLimitChoice.none,
        1 => ShareLimitChoice.once,
        _ => ShareLimitChoice.custom,
      },
      customLimit = link.maxDownloads ?? 5,
      note = link.note ?? '';

  /// The link being edited; null for a new one.
  final ShareLink? original;

  ShareMode mode;
  ShareExpiryChoice expiry;
  DateTime? customDate;
  bool protect;

  /// Empty while editing a protected link keeps its password.
  String password;
  ShareLimitChoice limit;
  int customLimit;
  String note;

  bool get editing => original != null;

  DateTime? expiresAt(DateTime now) => switch (expiry) {
    ShareExpiryChoice.day => now.add(const Duration(days: 1)),
    ShareExpiryChoice.week => now.add(const Duration(days: 7)),
    ShareExpiryChoice.month => now.add(const Duration(days: 30)),
    ShareExpiryChoice.never => null,
    ShareExpiryChoice.custom => customDate,
  };

  /// Upload-only links have nothing to download, so no limit.
  int? get maxDownloads {
    if (mode == ShareMode.uploadOnly) return null;
    return switch (limit) {
      ShareLimitChoice.none => null,
      ShareLimitChoice.once => 1,
      ShareLimitChoice.custom => customLimit,
    };
  }

  String? get trimmedNote {
    final text = note.trim();
    return text.isEmpty ? null : text;
  }

  ShareDraftProblem? validate(DateTime now) {
    final keepsPassword = editing && original!.hasPassword && password.isEmpty;
    if (protect && !keepsPassword && password.length < minSharePasswordLength) return ShareDraftProblem.passwordTooShort;
    if (mode != ShareMode.uploadOnly && limit == ShareLimitChoice.custom && (customLimit < 1 || customLimit > maxShareDownloads)) {
      return ShareDraftProblem.limitInvalid;
    }
    final date = expiresAt(now);
    final unchangedDate = editing && expiry == ShareExpiryChoice.custom && date == original!.expiresAt;
    if (date != null && !date.isAfter(now) && !unchangedDate) return ShareDraftProblem.dateInPast;
    return null;
  }

  /// The PATCH body for [original]: only fields that changed.
  Map<String, Object?> patch(DateTime now) {
    final link = original;
    if (link == null) throw StateError('Not editing a link.');
    final body = <String, Object?>{};
    if (mode != link.mode) body['mode'] = mode.apiName;
    final date = expiresAt(now);
    if (date != link.expiresAt) body['expiresAt'] = date?.toUtc().toIso8601String();
    if (protect && password.isNotEmpty) {
      body['password'] = password;
    } else if (!protect && link.hasPassword) {
      body['password'] = null;
    }
    if (maxDownloads != link.maxDownloads) body['maxDownloads'] = maxDownloads;
    if (trimmedNote != link.note) body['note'] = trimmedNote;
    return body;
  }
}
