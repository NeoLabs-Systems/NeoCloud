import 'package:flutter/foundation.dart';

import '../files/upload_queue.dart';
import 'share_intake.dart';

ShareIntake createShareIntake() => NoShareIntake();

class NoShareIntake implements ShareIntake {
  @override
  final ValueListenable<List<SharedBatch>> pending = ValueNotifier<List<SharedBatch>>(const <SharedBatch>[]);

  @override
  Future<void> start() async {}

  @override
  void remove(SharedBatch batch) {}

  @override
  Future<void> release(Iterable<UploadSource> files) async {}
}
