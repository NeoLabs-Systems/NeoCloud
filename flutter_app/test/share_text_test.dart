import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:intl/date_symbol_data_local.dart';
import 'package:neocloud/l10n/gen/app_l10n.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/sharing/share_draft.dart';
import 'package:neocloud/src/sharing/share_text.dart';

ShareLink _link({
  String mode = 'view',
  String? expiresAt,
  bool password = false,
  int? maxDownloads,
  int downloads = 0,
  bool exhausted = false,
  String type = 'file',
}) => ShareLink.fromJson(<String, dynamic>{
  'id': 1,
  'url': 'https://cloud.example/s/abc',
  'nodeId': 'n1',
  'nodeName': 'Report.pdf',
  'nodeType': type,
  'mode': mode,
  'expiresAt': expiresAt,
  'hasPassword': password,
  'maxDownloads': maxDownloads,
  'downloadCount': downloads,
  'exhausted': exhausted,
  'createdAt': '2026-11-01 10:00:00',
});

void main() {
  final s = lookupAppL10n(const Locale('en'));
  final now = DateTime(2026, 11, 11, 12);

  setUpAll(() => initializeDateFormatting('en'));

  test('summary names the mode, password, expiry and limit', () {
    final link = _link(expiresAt: '2026-11-18T12:00:00Z');
    expect(shareSummary(s, link, locale: 'en', now: now), startsWith('Anyone with this link can view and download · expires Nov 18'));
    expect(
      shareSummary(s, _link(password: true, maxDownloads: 1), locale: 'en', now: now),
      'Anyone with this link can view and download · password required · no expiry · one-time link',
    );
    expect(shareSummary(s, _link(maxDownloads: 5, downloads: 2), locale: 'en', now: now), endsWith('3 downloads left'));
    expect(shareSummary(s, _link(mode: 'upload_only', type: 'folder', maxDownloads: 3), locale: 'en', now: now),
        'Anyone with this link can add files but can’t see what’s in the folder · no expiry');
    expect(shareSummary(s, _link(maxDownloads: 1, downloads: 1, exhausted: true), locale: 'en', now: now), endsWith('download limit reached'));
    expect(shareSummary(s, _link(expiresAt: '2027-02-01T00:00:00Z'), locale: 'en', now: now), contains('expires Feb 1, 2027'));
  });

  test('list line counts downloads and time left', () {
    final link = _link(expiresAt: now.add(const Duration(days: 1, hours: 3)).toUtc().toIso8601String(), downloads: 3);
    expect(shareListLine(s, link, locale: 'en', now: now), 'Downloaded 3× · expires in 2 days');
    expect(shareListLine(s, _link(expiresAt: now.add(const Duration(hours: 5)).toUtc().toIso8601String()), locale: 'en', now: now),
        'Not downloaded yet · expires in 5 hours');
    expect(shareListLine(s, _link(), locale: 'en', now: now), 'Not downloaded yet · no expiry');
  });

  test('a draft sends only what changed and validates passwords', () {
    final link = _link(password: true, maxDownloads: 1, type: 'folder');
    final draft = ShareDraft.fromLink(link);
    expect(draft.patch(now), isEmpty);
    expect(draft.validate(now), isNull, reason: 'empty password keeps the current one');
    draft
      ..mode = ShareMode.viewUpload
      ..limit = ShareLimitChoice.none
      ..protect = false
      ..note = '  hi  ';
    expect(draft.patch(now), <String, Object?>{'mode': 'view_upload', 'password': null, 'maxDownloads': null, 'note': 'hi'});
    final fresh = ShareDraft()
      ..protect = true
      ..password = 'short';
    expect(fresh.validate(now), ShareDraftProblem.passwordTooShort);
    fresh.password = 'long enough';
    expect(fresh.validate(now), isNull);
  });
}
