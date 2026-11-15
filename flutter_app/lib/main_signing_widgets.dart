part of 'main.dart';

// Signing: the pieces both the editor and the public signing flow use. The
// widgets themselves live in src/signing/widgets (importable without this
// library); what is here needs the app's dialogs.

/// Asks for a signature (or [initials]) with Draw / Type / Upload tabs: a
/// dialog on wide screens, a bottom sheet on phones. Returns the PNG and how
/// it was made, or null when cancelled.
///
/// [offerInitials] adds "use this for my initials too" (when asking for a
/// signature that initials may reuse); [initialName] pre-fills the Type tab.
Future<SignatureResult?> showSignatureDialog(
  BuildContext context, {
  bool initials = false,
  bool offerInitials = false,
  String initialName = '',
  SavedSignatureStore? store,
  bool useSavedStore = true,
}) async {
  // Pictures kept on this device (local storage); never sent anywhere.
  final saved = store ?? (useSavedStore ? await SavedSignatureStore.open() : null);
  if (!context.mounted) return null;
  return showAdaptiveSheet<SignatureResult>(
    context: context,
    builder: (dialogContext) => _AdaptiveDialog(
      title: Text(initials ? appStrings.signingInitialsTitle : appStrings.signingSignatureTitle),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 520),
        child: SizedBox(
          width: 520,
          child: SignaturePanel(
            offerInitials: offerInitials && !initials,
            initialName: initialName,
            store: saved,
            kind: initials ? SavedKind.initials : SavedKind.signature,
            onCancel: () => Navigator.of(dialogContext).pop(),
            onResult: (result) => Navigator.of(dialogContext).pop(result),
          ),
        ),
      ),
    ),
  );
}

/// "Download signed PDF" with a menu next to it for the same file without
/// the appended signature pages. The button is the quiet kind: the page's
/// one gold button is another action.
class _DownloadSplitButton extends StatelessWidget {
  const _DownloadSplitButton({required this.keyPrefix, required this.label, required this.onDownload});

  final String keyPrefix;
  final String label;

  /// Called with whether the signature pages are included.
  final ValueChanged<bool> onDownload;

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    const radius = Radius.circular(AppRadius.control);
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        OutlinedButton.icon(
          key: ValueKey<String>(keyPrefix),
          onPressed: () => onDownload(true),
          style: OutlinedButton.styleFrom(shape: const RoundedRectangleBorder(borderRadius: BorderRadius.horizontal(left: radius))),
          icon: const Icon(Icons.download_rounded, size: 18),
          label: Text(label),
        ),
        PopupMenuButton<bool>(
          key: ValueKey<String>('$keyPrefix-menu'),
          tooltip: strings.signingDownloadMore,
          onSelected: onDownload,
          itemBuilder: (context) => <PopupMenuEntry<bool>>[
            PopupMenuItem<bool>(value: true, child: Text(strings.signingDownloadWithCertificate)),
            PopupMenuItem<bool>(key: ValueKey<String>('$keyPrefix-clean'), value: false, child: Text(strings.signingDownloadWithoutCertificate)),
          ],
          child: Container(
            width: 40,
            height: 40,
            decoration: BoxDecoration(
              border: Border(top: BorderSide(color: _borderLight), right: BorderSide(color: _borderLight), bottom: BorderSide(color: _borderLight)),
              borderRadius: const BorderRadius.horizontal(right: radius),
            ),
            child: Icon(Icons.arrow_drop_down_rounded, color: _textSecondary),
          ),
        ),
      ],
    );
  }
}

/// A picture of ink (a signature, initials, a stamp) on a small sheet of
/// white paper. The ink is dark, so it is never drawn straight on the dark
/// theme's background.
class _PaperTile extends StatelessWidget {
  const _PaperTile({required this.child, this.height});

  final Widget child;
  final double? height;

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const ValueKey<String>('paper-tile'),
      height: height,
      constraints: const BoxConstraints(minWidth: 88),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(color: Colors.white, borderRadius: BorderRadius.circular(8), border: Border.all(color: _borderLight)),
      child: child,
    );
  }
}
