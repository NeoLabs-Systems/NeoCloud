import 'package:intl/intl.dart';

import '../api/models.dart';
import '../files/file_format.dart';
import '../files/transfer_estimate.dart';
import '../l10n/app_language.dart';

/// How far an import got, 0..1: by bytes once the size is known, by files
/// while only the count is, null while the source is still being scanned.
double? importFraction(ImportJob job) {
  if (job.status == ImportStatus.done) return 1;
  if (job.totalBytes > 0) return (job.doneBytes / job.totalBytes).clamp(0, 1).toDouble();
  if (job.totalFiles > 0) return (job.doneFiles / job.totalFiles).clamp(0, 1).toDouble();
  return null;
}

/// "1,204 of 5,000 files · 1.2 GB of 4.0 GB", in the app's language. While
/// the totals are unknown only what is done is counted.
String importCountsLine(ImportJob job) {
  final numbers = NumberFormat.decimalPattern(currentAppLanguage.code);
  final files = job.totalFiles > 0
      ? appStrings.importFilesOf(numbers.format(job.doneFiles), numbers.format(job.totalFiles))
      : appStrings.importFilesDone(numbers.format(job.doneFiles));
  final bytes = job.totalBytes > 0
      ? appStrings.importBytesOf(formatBytes(job.doneBytes), formatBytes(job.totalBytes))
      : formatBytes(job.doneBytes);
  return '$files · $bytes';
}

/// "38 MB/s" for a measured speed; null before there is one.
String? formatTransferSpeed(double? bytesPerSecond) {
  if (bytesPerSecond == null || bytesPerSecond <= 0) return null;
  return '${formatBytes(bytesPerSecond.round())}/s';
}

/// How much more space an import of [bytes] needs than the account has
/// left; null when it fits or the quota is unlimited.
int? importShortfall(int bytes, StorageUsage usage) {
  if (usage.unlimited) return null;
  final free = usage.quotaBytes - usage.usedBytes;
  return bytes > free ? bytes - free : null;
}

/// One speed meter per running job, fed by each poll.
class ImportSpeedMeters {
  final Map<String, TransferEstimate> _meters = <String, TransferEstimate>{};

  /// Records [job]'s progress at [now]; meters of jobs that stopped moving
  /// are dropped, so a resumed job starts measuring afresh.
  void sample(Iterable<ImportJob> jobs, DateTime now) {
    final running = <String>{};
    for (final job in jobs) {
      if (job.status != ImportStatus.running) continue;
      running.add(job.id);
      _meters.putIfAbsent(job.id, TransferEstimate.new).sample(job.doneBytes, job.totalBytes, now);
    }
    _meters.removeWhere((id, _) => !running.contains(id));
  }

  double? speedOf(String jobId) => _meters[jobId]?.bytesPerSecond;

  Duration? etaOf(String jobId) => _meters[jobId]?.eta;
}
