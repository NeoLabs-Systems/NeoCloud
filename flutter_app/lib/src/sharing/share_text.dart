import 'package:intl/intl.dart';

import '../../l10n/gen/app_l10n.dart';
import '../api/models.dart';

// How a share link reads to its owner, in the share dialog and in
// Settings › Shared links.

String _shortDate(DateTime date, String locale, DateTime now) =>
    date.year == now.year ? DateFormat.MMMd(locale).format(date) : DateFormat.yMMMd(locale).format(date);

/// What a link lets visitors do, as one sentence.
String shareModeSentence(AppL10n s, ShareMode mode) => switch (mode) {
  ShareMode.view => s.shareSummaryView,
  ShareMode.viewUpload => s.shareSummaryViewUpload,
  ShareMode.uploadOnly => s.shareSummaryUploadOnly,
};

/// "Anyone with this link can view and download · password required ·
/// expires 18 Nov · one-time link".
String shareSummary(AppL10n s, ShareLink link, {required String locale, DateTime? now}) {
  final at = now ?? DateTime.now();
  final expiresAt = link.expiresAt;
  final parts = <String>[shareModeSentence(s, link.mode)];
  if (link.hasPassword) parts.add(s.shareSummaryPassword);
  if (link.expiredFlag || (expiresAt != null && !expiresAt.isAfter(at))) {
    parts.add(s.shareSummaryExpired);
  } else if (expiresAt == null) {
    parts.add(s.shareSummaryNoExpiry);
  } else {
    parts.add(s.shareSummaryExpires(_shortDate(expiresAt, locale, at)));
  }
  final limit = link.maxDownloads;
  if (limit != null && link.mode != ShareMode.uploadOnly) {
    if (link.exhausted) {
      parts.add(s.shareSummaryUsedUp);
    } else if (limit == 1) {
      parts.add(s.shareSummaryOneTime);
    } else {
      parts.add(s.shareSummaryDownloadsLeft(limit - link.downloadCount));
    }
  }
  return parts.join(' · ');
}

/// When a link stops working, relative to [now]: "expires in 2 days".
String shareExpiryPhrase(AppL10n s, ShareLink link, {required String locale, DateTime? now}) {
  final at = now ?? DateTime.now();
  final expiresAt = link.expiresAt;
  if (expiresAt == null) return link.expiredFlag ? s.shareSummaryExpired : s.shareSummaryNoExpiry;
  if (link.expiredFlag || !expiresAt.isAfter(at)) return s.shareExpiredOn(_shortDate(expiresAt, locale, at));
  final left = expiresAt.difference(at);
  if (left.inHours < 24) return s.shareExpiresInHours(left.inHours);
  // Whole days, rounded up: 1 day 3 h left still reads "in 2 days".
  final days = (left.inMinutes / (24 * 60)).ceil();
  return s.shareExpiresInDays(days);
}

/// The list line: "Downloaded 3× · expires in 2 days · password required".
String shareListLine(AppL10n s, ShareLink link, {required String locale, DateTime? now}) {
  final parts = <String>[
    if (link.mode != ShareMode.uploadOnly) s.shareDownloadedTimes(link.downloadCount),
    shareExpiryPhrase(s, link, locale: locale, now: now),
    if (link.hasPassword) s.shareSummaryPassword,
    if (link.exhausted) s.shareSummaryUsedUp else if (link.maxDownloads == 1) s.shareSummaryOneTime,
  ];
  final text = parts.join(' · ');
  return text.isEmpty ? text : '${text[0].toUpperCase()}${text.substring(1)}';
}
