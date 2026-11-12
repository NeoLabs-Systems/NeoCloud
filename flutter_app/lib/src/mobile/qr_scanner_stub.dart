import 'package:flutter/widgets.dart';

import '../pairing/scanned_code.dart';

bool get qrScannerSupported => false;

Future<ScannedCode?> scanQrCode(BuildContext context, {required bool Function(ScannedCode code) accept, required String hint}) async => null;
