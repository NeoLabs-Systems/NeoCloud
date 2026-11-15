import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';

import 'helpers/fake_http.dart';

void main() {
  test('the one wipe 401 reaches the app before the session handling', () async {
    final http = FakeHttp((call) => const FakeReply(401, <String, dynamic>{'error': 'x', 'code': 'DEVICE_WIPE_REQUESTED', 'eraseFiles': false}));
    final client = BackendClient(baseUrl: 'http://localhost:1', http: http);
    final events = <String>[];
    client.onDeviceWiped = () => events.add('wipe');
    client.onSessionExpired = () => events.add('expired');
    await expectLater(client.devices(), throwsA(isA<ApiException>()));
    expect(events.first, 'wipe');
  });

  test('a device row tells pending from erased', () {
    final pending = DeviceInfo.fromJson(<String, dynamic>{
      'id': 1,
      'name': 'Laptop',
      'platform': 'macos',
      'wipe': <String, dynamic>{'requestedAt': '2026-11-01 10:00:00', 'eraseFiles': true, 'deliveredAt': null},
    });
    expect(pending.wipePending, isTrue);
    expect(pending.wiped, isFalse);
    final erased = DeviceInfo.fromJson(<String, dynamic>{
      'id': 2,
      'name': 'Phone',
      'platform': 'android',
      'wipe': <String, dynamic>{'requestedAt': '2026-11-01 10:00:00', 'deliveredAt': '2026-11-02 08:00:00'},
    });
    expect(erased.wiped, isTrue);
    expect(DeviceInfo.fromJson(<String, dynamic>{'id': 3, 'name': 'x', 'platform': ''}).wipePending, isFalse);
  });
}
