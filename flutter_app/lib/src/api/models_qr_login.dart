// Models for QR sign-in (docs/api.md, Auth): the code a signed-out browser
// shows, what it learns while a phone decides, and the preview the phone
// approves. Ported from NeoAgent's main_models.dart.
part of 'models.dart';

/// The code this signed-out browser shows, and the token it polls with.
class QrLoginChallenge {
  const QrLoginChallenge({
    required this.challengeId,
    required this.pollToken,
    required this.qrPayload,
    required this.status,
    required this.expiresAt,
  });

  factory QrLoginChallenge.fromJson(Map<String, dynamic> json) => QrLoginChallenge(
    challengeId: _string(json['challengeId']),
    pollToken: _string(json['pollToken']),
    qrPayload: _string(json['qrPayload']),
    status: _string(json['status']).isEmpty ? 'pending' : _string(json['status']),
    expiresAt: _date(json['expiresAt']),
  );

  final String challengeId;
  final String pollToken;

  /// `neocloud://qr-login?v=1&backend=…&challenge=…&secret=…`
  final String qrPayload;
  final String status;
  final DateTime? expiresAt;

  bool get isUsable => challengeId.isNotEmpty && pollToken.isNotEmpty && qrPayload.isNotEmpty && status != 'expired';

  bool get isExpired => status == 'expired' || (expiresAt != null && expiresAt!.isBefore(DateTime.now()));

  int get secondsRemaining {
    final expires = expiresAt;
    if (expires == null) return 0;
    final left = expires.difference(DateTime.now()).inSeconds;
    return left < 0 ? 0 : left;
  }
}

/// A signed-in phone scanned this browser's code and is deciding. Names only
/// the scanning device and what the phone is shown, never the account.
class QrLoginScan {
  const QrLoginScan({
    required this.scannerLabel,
    required this.scannerDeviceClass,
    required this.requestedDeviceLabel,
    required this.requestLocationLabel,
  });

  factory QrLoginScan.fromStatus(Map<String, dynamic> json) {
    Map<String, dynamic> section(String key) => (json[key] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{};
    final scanner = section('scannedBy');
    return QrLoginScan(
      scannerLabel: _string(scanner['label']),
      scannerDeviceClass: _string(scanner['deviceClass']),
      requestedDeviceLabel: _string(section('requestedDevice')['label']),
      requestLocationLabel: _string(section('requestLocation')['label']),
    );
  }

  final String scannerLabel;
  final String scannerDeviceClass;
  final String requestedDeviceLabel;
  final String requestLocationLabel;
}

/// What the phone shows before it approves: which browser asks, from where.
class QrLoginPreview {
  const QrLoginPreview({
    required this.challengeId,
    required this.status,
    required this.requestedAt,
    required this.expiresAt,
    required this.deviceLabel,
    required this.platformLabel,
    required this.browserLabel,
    required this.deviceClass,
    required this.locationLabel,
  });

  factory QrLoginPreview.fromJson(Map<String, dynamic> json) {
    final device = (json['requestedDevice'] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{};
    final location = (json['requestLocation'] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{};
    return QrLoginPreview(
      challengeId: _string(json['challengeId']),
      status: _string(json['status']).isEmpty ? 'pending' : _string(json['status']),
      requestedAt: _date(json['requestedAt']),
      expiresAt: _date(json['expiresAt']),
      deviceLabel: _string(device['label']),
      platformLabel: _string(device['platformLabel']),
      browserLabel: _string(device['browserLabel']),
      deviceClass: _string(device['deviceClass']),
      locationLabel: _string(location['label']),
    );
  }

  final String challengeId;
  final String status;
  final DateTime? requestedAt;
  final DateTime? expiresAt;
  final String deviceLabel;
  final String platformLabel;
  final String browserLabel;

  /// mobile, tablet, desktop, server or unknown.
  final String deviceClass;

  /// The address the browser asked from (NeoCloud does no geo lookup).
  final String locationLabel;

  bool get canApprove => status == 'pending' || status == 'scanned' || status == 'approved';
  bool get isClaimed => status == 'claimed';
  bool get isExpired => status == 'expired' || (expiresAt != null && expiresAt!.isBefore(DateTime.now()));
}
