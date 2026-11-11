import 'package:url_launcher/url_launcher.dart';

/// Opens a download URL with the system, for platforms without their own
/// saver.
Future<void> startDownload(Uri uri, {String? fileName, Map<String, String> headers = const <String, String>{}}) async {
  await launchUrl(uri, mode: LaunchMode.externalApplication);
}
