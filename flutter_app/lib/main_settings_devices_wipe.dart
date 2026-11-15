part of 'main.dart';

// Settings > Devices: sign a lost device out and have it erase its NeoCloud
// data the next time it connects.

/// Desktop clients can also delete their synced files; phones erase the app's
/// own data only.
bool _canEraseFiles(DeviceInfo device) => const <String>{'windows', 'macos', 'linux', 'lite'}.contains(device.platform.toLowerCase());

/// What to do with a lost [device]: `false` signs it out and clears the app's
/// data, `true` also erases the NeoCloud files on it; null when cancelled.
Future<bool?> _askWipeChoice(BuildContext context, DeviceInfo device) {
  var eraseFiles = false;
  final choices = _canEraseFiles(device);
  return showAdaptiveSheet<bool>(
    context: context,
    builder: (dialogContext) => StatefulBuilder(
      builder: (context, setState) => _AdaptiveDialog(
        title: Text(appStrings.deviceWipeTitle(device.name)),
        content: SizedBox(
          width: 420,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Text(appStrings.deviceWipeMessage, style: TextStyle(color: _textSecondary, height: 1.45)),
              if (choices) ...<Widget>[
                const SizedBox(height: 12),
                _WipeChoice(label: appStrings.deviceWipeSignOutOnly, selected: !eraseFiles, onTap: () => setState(() => eraseFiles = false)),
                _WipeChoice(label: appStrings.deviceWipeEraseFiles, selected: eraseFiles, onTap: () => setState(() => eraseFiles = true)),
              ],
              const SizedBox(height: 12),
              Text(appStrings.deviceWipeNeverConnects, style: TextStyle(color: _textMuted, fontSize: 12.5, height: 1.4)),
            ],
          ),
        ),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.cancel)),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(eraseFiles),
            style: FilledButton.styleFrom(backgroundColor: _danger, foregroundColor: Colors.white),
            child: Text(appStrings.deviceWipeConfirm),
          ),
        ],
      ),
    ),
  );
}

class _WipeChoice extends StatelessWidget {
  const _WipeChoice({required this.label, required this.selected, required this.onTap});

  final String label;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(8),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Icon(selected ? Icons.radio_button_checked_rounded : Icons.radio_button_off_rounded, size: 20, color: selected ? _accent : _textMuted),
            const SizedBox(width: 10),
            Expanded(child: Text(label, style: TextStyle(color: _textPrimary, height: 1.35))),
          ],
        ),
      ),
    );
  }
}
