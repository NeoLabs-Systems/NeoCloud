import 'package:flutter/widgets.dart';

import '../l10n/app_language.dart';
import '../pairing/pairing_link.dart';
import '../pairing/scanned_code.dart';
import 'qr_scanner_stub.dart' if (dart.library.io) 'qr_scanner_io.dart' as platform;

/// Whether this device scans QR codes with its camera (the iOS and Android
/// apps).
bool get qrScannerSupported => platform.qrScannerSupported;

/// Opens the camera full screen until a NeoCloud code that [accept] takes is
/// seen (see [parseScannedCode]). Asks for camera access first, with an
/// explanation; resolves to null when the user backs out. [hint] says where
/// the code is.
Future<ScannedCode?> scanQrCode(BuildContext context, {required bool Function(ScannedCode code) accept, required String hint}) =>
    platform.scanQrCode(context, accept: accept, hint: hint);

/// The connect screen's scan: only the web app's pairing code signs in.
Future<PairingLinkData?> scanPairingCode(BuildContext context) async {
  final code = await scanQrCode(context, accept: (code) => code is PairingScan, hint: appStrings.scanHint);
  return code is PairingScan ? code.link : null;
}
