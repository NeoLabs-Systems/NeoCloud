part of 'main.dart';

// /app/pair?code=…: the "Can't scan?" link of the pairing dialog, opened in
// a browser. On a phone it explains that pairing needs the NeoCloud app and
// hands the code to it; on a computer it says to open the link on the phone.

/// Store pages for the mobile app. Placeholders until the apps are listed.
const String _appStoreUrl = 'https://apps.apple.com/search?term=NeoCloud';
const String _playStoreUrl = 'https://play.google.com/store/search?q=NeoCloud&c=apps';

bool get _onPhoneBrowser => kIsWeb && (defaultTargetPlatform == TargetPlatform.iOS || defaultTargetPlatform == TargetPlatform.android);

class PairLandingView extends StatelessWidget {
  const PairLandingView({super.key, required this.controller});

  final NeoCloudController controller;

  /// The server this page belongs to, as the app link carries it.
  Uri get _server {
    final base = controller.client.uri('/');
    return base.replace(path: base.path.endsWith('/') ? base.path.substring(0, base.path.length - 1) : base.path, query: '');
  }

  List<Widget> _phone(PairingLinkData? link) {
    final ios = defaultTargetPlatform == TargetPlatform.iOS;
    return <Widget>[
      Text(appStrings.pairLandingPhoneBody, style: TextStyle(color: _textSecondary, height: 1.45)),
      const SizedBox(height: AppSpacing.lg),
      if (link != null)
        FilledButton.icon(
          onPressed: () => unawaited(launchUrl(link.appLink, mode: LaunchMode.externalApplication)),
          icon: const Icon(Icons.open_in_new_rounded, size: 18),
          label: Text(appStrings.pairLandingOpenApp),
        ),
      const SizedBox(height: AppSpacing.sm),
      OutlinedButton.icon(
        onPressed: () => unawaited(launchUrl(Uri.parse(ios ? _appStoreUrl : _playStoreUrl), mode: LaunchMode.externalApplication)),
        icon: Icon(ios ? Icons.apple_rounded : Icons.shop_rounded, size: 18),
        label: Text(ios ? appStrings.pairLandingAppStore : appStrings.pairLandingPlayStore),
      ),
      const SizedBox(height: AppSpacing.sm),
      TextButton(
        onPressed: () => unawaited(launchUrl(Uri.parse(ios ? _playStoreUrl : _appStoreUrl), mode: LaunchMode.externalApplication)),
        child: Text(ios ? appStrings.pairLandingPlayStore : appStrings.pairLandingAppStore),
      ),
    ];
  }

  List<Widget> _computer() {
    return <Widget>[
      Text(appStrings.pairLandingComputerBody, style: TextStyle(color: _textSecondary, height: 1.45)),
      const SizedBox(height: AppSpacing.lg),
      FilledButton(onPressed: controller.finishPairLanding, child: Text(appStrings.openNeoCloud)),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final code = controller.pendingPairCode ?? '';
    final link = parsePairingLink(pairingWebLink(_server, code).toString());
    return _ControlSurfaceBackdrop(
      child: Scaffold(
        backgroundColor: Colors.transparent,
        body: SafeArea(
          child: Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(AppSpacing.lg),
              child: _EntranceMotion(
                child: _GlassSurface(
                  width: 420,
                  padding: const EdgeInsets.all(AppSpacing.xl),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: <Widget>[
                      const Center(child: _BrandLockup(logoSize: 44, showName: false)),
                      const SizedBox(height: AppSpacing.md),
                      Text(appStrings.pairLandingTitle, style: _displayTitleStyle(24)),
                      const SizedBox(height: AppSpacing.sm),
                      if (link == null) ...<Widget>[
                        _InlineNote(message: appStrings.pairLandingInvalid),
                        const SizedBox(height: AppSpacing.lg),
                        FilledButton(onPressed: controller.finishPairLanding, child: Text(appStrings.openNeoCloud)),
                      ] else if (_onPhoneBrowser)
                        ..._phone(link)
                      else
                        ..._computer(),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
