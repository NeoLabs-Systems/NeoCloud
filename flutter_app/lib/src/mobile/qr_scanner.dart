import 'package:flutter/widgets.dart';

import '../pairing/pairing_link.dart';
import 'qr_scanner_stub.dart' if (dart.library.io) 'qr_scanner_io.dart' as platform;

/// Whether this device scans pairing QR codes with its camera (the iOS and
/// Android apps).
bool get qrScannerSupported => platform.qrScannerSupported;

/// Opens the camera full screen until a NeoCloud pairing code is seen.
/// Asks for camera access first, with an explanation; resolves to null when
/// the user backs out.
Future<PairingLinkData?> scanPairingCode(BuildContext context) => platform.scanPairingCode(context);
