part of 'main.dart';

// The QR code on the connect screen (docs/clients.md §5): the phone scans
// the code the web app shows.

/// Scans the web app's pairing code and signs in with it.
Future<void> _scanAndPair(BuildContext context, NeoCloudController controller) async {
  controller.clearInlineError();
  final link = await scanPairingCode(context);
  if (link != null) await controller.pair(link);
}

/// "Scan QR code", with the line that says where the code is.
class _ScanToPairButton extends StatelessWidget {
  const _ScanToPairButton({required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            Expanded(child: Divider(color: _border)),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: Text(appStrings.connectOr, style: TextStyle(color: _textMuted, fontSize: 12.5)),
            ),
            Expanded(child: Divider(color: _border)),
          ],
        ),
        const SizedBox(height: AppSpacing.md),
        OutlinedButton.icon(
          onPressed: controller.isConnecting ? null : () => unawaited(_scanAndPair(context, controller)),
          icon: const Icon(Icons.qr_code_scanner_rounded, size: 18),
          label: Text(appStrings.connectScanQr),
        ),
        const SizedBox(height: AppSpacing.sm),
        Text(appStrings.connectScanQrHint, textAlign: TextAlign.center, style: TextStyle(color: _textMuted, fontSize: 12.5, height: 1.4)),
      ],
    );
  }
}
