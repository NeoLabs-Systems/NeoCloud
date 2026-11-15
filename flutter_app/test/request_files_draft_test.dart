import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/sharing/share_draft.dart';

ShareLink _link({ShareMode mode = ShareMode.uploadOnly, int? cap}) => ShareLink(
  id: '1',
  url: 'https://cloud.example/s/x',
  nodeId: 'n',
  nodeName: 'Inbox',
  nodeType: 'folder',
  expiresAt: null,
  hasPassword: false,
  createdAt: null,
  mode: mode,
  maxUploadBytes: cap,
);

void main() {
  test('a file request keeps its upload cap and only a view link drops it', () {
    final draft = ShareDraft.fromLink(_link(cap: 1000));
    expect(draft.uploadCap, 1000);
    expect(draft.patch(DateTime(2026)), isEmpty);
    draft.maxUploadBytes = null;
    expect(draft.patch(DateTime(2026))['maxUploadBytes'], isNull);
    expect(draft.patch(DateTime(2026)).containsKey('maxUploadBytes'), isTrue);
    final view = ShareDraft.fromLink(_link(mode: ShareMode.view, cap: 1000));
    expect(view.uploadCap, isNull);
  });

  test('the link reads its cap from the server', () {
    final link = ShareLink.fromJson(<String, dynamic>{'id': 1, 'mode': 'upload_only', 'maxUploadBytes': 2048, 'nodeType': 'folder'});
    expect(link.mode, ShareMode.uploadOnly);
    expect(link.maxUploadBytes, 2048);
  });
}
