import 'dart:math' as math;

/// Upload speed and time left, smoothed so the numbers in the upload panel
/// don't jump with every part: the speed is an exponential moving average,
/// and the estimate counts down between samples and only drifts towards
/// the new raw value.
class TransferEstimate {
  TransferEstimate({this.speedWeight = 0.3, this.etaWeight = 0.25, this.minInterval = const Duration(milliseconds: 500)});

  /// How much a new speed sample counts (0..1).
  final double speedWeight;

  /// How fast the estimate moves towards the raw remaining/speed value.
  final double etaWeight;

  /// Samples closer together than this are ignored.
  final Duration minInterval;

  double? _speed;
  double? _eta;
  int? _lastBytes;
  DateTime? _lastAt;

  /// Bytes per second, once there are two samples.
  double? get bytesPerSecond => _speed;

  /// Time left, once known.
  Duration? get eta => _eta == null ? null : Duration(milliseconds: (_eta! * 1000).round());

  void reset() {
    _speed = null;
    _eta = null;
    _lastBytes = null;
    _lastAt = null;
  }

  /// Feeds the bytes sent so far out of [totalBytes] at [now].
  void sample(int sentBytes, int totalBytes, DateTime now) {
    final lastAt = _lastAt;
    final lastBytes = _lastBytes;
    if (lastAt == null || lastBytes == null || sentBytes < lastBytes) {
      _lastAt = now;
      _lastBytes = sentBytes;
      return;
    }
    final elapsed = now.difference(lastAt);
    if (elapsed < minInterval) return;
    final seconds = elapsed.inMicroseconds / Duration.microsecondsPerSecond;
    final rate = (sentBytes - lastBytes) / seconds;
    final speed = _speed == null ? rate : _speed! + speedWeight * (rate - _speed!);
    _speed = speed;
    _lastAt = now;
    _lastBytes = sentBytes;
    final remaining = math.max(0, totalBytes - sentBytes);
    if (speed <= 0) return;
    final raw = remaining / speed;
    final previous = _eta;
    if (previous == null) {
      _eta = raw;
      return;
    }
    final counted = math.max(0.0, previous - seconds);
    _eta = counted + etaWeight * (raw - counted);
  }
}
