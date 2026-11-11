import '../api/backend_client.dart';
import 'camera_upload.dart';

CameraUploads createCameraUploads() => NoCameraUploads();

class NoCameraUploads extends CameraUploads {
  @override
  bool get supported => false;

  @override
  CameraUploadSettings get settings => const CameraUploadSettings();

  @override
  bool get running => false;

  @override
  CameraProgress? get progress => null;

  @override
  CameraStatus get status => const CameraStatus(CameraStatusKind.off);

  @override
  CameraCounts get counts => const CameraCounts();

  @override
  List<CameraRecord> records(CameraRecordStatus? status) => const <CameraRecord>[];

  @override
  DateTime? get lastRun => null;

  @override
  bool get limitedAccess => false;

  @override
  List<String> get log => const <String>[];

  @override
  Future<void> load() async {}

  @override
  Future<void> signIn(String accountKey) async {}

  @override
  Future<void> signOut() async {}

  @override
  bool get needsConfirmation => false;

  @override
  Future<void> dismissConfirmation() async {}

  @override
  Future<CameraAccess> requestAccess() async => CameraAccess.denied;

  @override
  Future<void> openSystemSettings() async {}

  @override
  Future<CameraLibrarySize> librarySize({required bool includeVideos}) async => const CameraLibrarySize(count: 0);

  @override
  Future<void> update(CameraUploadSettings next) async {}

  @override
  Future<void> runNow(BackendClient client, {bool manual = false}) async {}

  @override
  Future<void> retryFailed() async {}
}
