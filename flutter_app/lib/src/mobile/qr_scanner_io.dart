import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:url_launcher/url_launcher.dart';

import '../l10n/app_language.dart';
import '../pairing/pairing_link.dart';
import '../theme/palette.dart';

bool get qrScannerSupported => Platform.isIOS || Platform.isAndroid;

Future<PairingLinkData?> scanPairingCode(BuildContext context) =>
    Navigator.of(context).push<PairingLinkData>(MaterialPageRoute<PairingLinkData>(fullscreenDialog: true, builder: (_) => const _ScanPage()));

enum _ScanState { priming, scanning, denied, failed }

/// The camera is a dark surface in either theme.
const NeoCloudPalette _palette = darkPalette;

class _ScanPage extends StatefulWidget {
  const _ScanPage();

  @override
  State<_ScanPage> createState() => _ScanPageState();
}

class _ScanPageState extends State<_ScanPage> {
  final MobileScannerController _camera = MobileScannerController(autoStart: false, formats: const <BarcodeFormat>[BarcodeFormat.qrCode]);
  _ScanState _state = _ScanState.priming;
  bool _wrongCode = false;
  Timer? _wrongCodeTimer;
  bool _done = false;

  @override
  void dispose() {
    _wrongCodeTimer?.cancel();
    unawaited(_camera.dispose());
    super.dispose();
  }

  /// Starting the camera is what triggers the system prompt, so it only
  /// happens after the user read why it is needed.
  Future<void> _start() async {
    setState(() => _state = _ScanState.scanning);
    try {
      await _camera.start();
    } on MobileScannerException catch (error) {
      if (!mounted) return;
      setState(() => _state = error.errorCode == MobileScannerErrorCode.permissionDenied ? _ScanState.denied : _ScanState.failed);
    }
  }

  void _onDetect(BarcodeCapture capture) {
    if (_done) return;
    for (final barcode in capture.barcodes) {
      final link = parsePairingLink(barcode.rawValue ?? '');
      if (link != null) {
        _done = true;
        unawaited(_camera.stop());
        Navigator.of(context).pop(link);
        return;
      }
    }
    if (capture.barcodes.isEmpty) return;
    _wrongCodeTimer?.cancel();
    setState(() => _wrongCode = true);
    _wrongCodeTimer = Timer(const Duration(seconds: 2), () {
      if (mounted) setState(() => _wrongCode = false);
    });
  }

  /// iOS opens the app's own settings page; Android has no such link, so
  /// the text says where to go.
  Future<void> _openSettings() => launchUrl(Uri.parse('app-settings:'));

  Widget _message({required IconData icon, required String title, required String body, required List<Widget> actions}) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Icon(icon, size: 40, color: _palette.textSecondary),
            const SizedBox(height: 16),
            Text(title, textAlign: TextAlign.center, style: TextStyle(color: _palette.textPrimary, fontSize: 20, fontWeight: FontWeight.w600)),
            const SizedBox(height: 8),
            Text(body, textAlign: TextAlign.center, style: TextStyle(color: _palette.textSecondary, height: 1.45)),
            const SizedBox(height: 24),
            ...actions,
          ],
        ),
      ),
    );
  }

  Widget _scanner() {
    return Stack(
      fit: StackFit.expand,
      children: <Widget>[
        MobileScanner(
          controller: _camera,
          onDetect: _onDetect,
          errorBuilder: (context, error) => _message(
            icon: Icons.no_photography_outlined,
            title: appStrings.scanFailedTitle,
            body: appStrings.scanFailedBody,
            actions: const <Widget>[],
          ),
        ),
        Center(
          child: Container(
            width: 240,
            height: 240,
            decoration: BoxDecoration(border: Border.all(color: _palette.textPrimary, width: 2), borderRadius: BorderRadius.circular(18)),
          ),
        ),
        Positioned(
          left: 24,
          right: 24,
          bottom: 48,
          child: Column(
            children: <Widget>[
              Text(
                _wrongCode ? appStrings.scanWrongCode : appStrings.scanHint,
                textAlign: TextAlign.center,
                style: TextStyle(color: _palette.textPrimary, fontSize: 15, fontWeight: FontWeight.w500),
              ),
              const SizedBox(height: 20),
              ValueListenableBuilder<MobileScannerState>(
                valueListenable: _camera,
                builder: (context, state, _) {
                  if (state.torchState == TorchState.unavailable) return const SizedBox.shrink();
                  final on = state.torchState == TorchState.on;
                  return IconButton.filledTonal(
                    tooltip: on ? appStrings.scanTorchOff : appStrings.scanTorchOn,
                    onPressed: () => unawaited(_camera.toggleTorch()),
                    icon: Icon(on ? Icons.flashlight_off_rounded : Icons.flashlight_on_rounded),
                  );
                },
              ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _body() {
    switch (_state) {
      case _ScanState.priming:
        return _message(
          icon: Icons.qr_code_scanner_rounded,
          title: appStrings.scanPrimeTitle,
          body: appStrings.scanPrimeBody,
          actions: <Widget>[FilledButton(onPressed: () => unawaited(_start()), child: Text(appStrings.scanPrimeAllow))],
        );
      case _ScanState.scanning:
        return _scanner();
      case _ScanState.denied:
        return _message(
          icon: Icons.no_photography_outlined,
          title: appStrings.scanDeniedTitle,
          body: Platform.isIOS ? appStrings.scanDeniedBodyIos : appStrings.scanDeniedBodyAndroid,
          actions: <Widget>[
            if (Platform.isIOS) FilledButton(onPressed: () => unawaited(_openSettings()), child: Text(appStrings.scanOpenSettings)),
            if (Platform.isAndroid) FilledButton(onPressed: () => unawaited(_start()), child: Text(appStrings.retry)),
          ],
        );
      case _ScanState.failed:
        return _message(
          icon: Icons.no_photography_outlined,
          title: appStrings.scanFailedTitle,
          body: appStrings.scanFailedBody,
          actions: <Widget>[FilledButton(onPressed: () => unawaited(_start()), child: Text(appStrings.retry))],
        );
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: _palette.bgPrimary,
      appBar: AppBar(
        backgroundColor: Colors.transparent,
        foregroundColor: _palette.textPrimary,
        elevation: 0,
        title: Text(appStrings.scanTitle),
      ),
      extendBodyBehindAppBar: _state == _ScanState.scanning,
      body: SafeArea(top: _state != _ScanState.scanning, child: _body()),
    );
  }
}
