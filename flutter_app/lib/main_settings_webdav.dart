part of 'main.dart';

// Settings › Devices › Connect apps (WebDAV): the address, the user name and
// app passwords for Finder, Explorer, rclone and friends (docs/clients.md §6).

class _WebDavCard extends StatefulWidget {
  const _WebDavCard({required this.controller, required this.busy, required this.onCreatePassword});

  final NeoCloudController controller;
  final bool busy;
  final VoidCallback onCreatePassword;

  @override
  State<_WebDavCard> createState() => _WebDavCardState();
}

class _WebDavCardState extends State<_WebDavCard> {
  bool _showHints = false;

  List<(String, String)> get _hints => <(String, String)>[
    (appStrings.webDavHintFinderTitle, appStrings.webDavHintFinder),
    (appStrings.webDavHintWindowsTitle, appStrings.webDavHintWindows),
    (appStrings.webDavHintRcloneTitle, appStrings.webDavHintRclone),
    (appStrings.webDavHintObsidianTitle, appStrings.webDavHintObsidian),
    (appStrings.webDavHintKeepassTitle, appStrings.webDavHintKeepass),
  ];

  Widget _copyable(String value) {
    return Row(
      children: <Widget>[
        Expanded(
          child: SelectableText(value, style: GoogleFonts.geistMono(fontSize: 12.5, color: _textPrimary)),
        ),
        IconButton(
          tooltip: appStrings.copy,
          visualDensity: VisualDensity.compact,
          onPressed: () {
            unawaited(Clipboard.setData(ClipboardData(text: value)));
            ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(appStrings.copied)));
          },
          icon: const Icon(Icons.copy_rounded, size: 16),
        ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final username = widget.controller.user?.username ?? '';
    final url = widget.controller.client.webDavUri(username).toString();
    return _SettingsGroup(
      title: appStrings.webDavTitle,
      description: appStrings.webDavDescription,
      children: <Widget>[
        _SettingsRow(label: appStrings.webDavAddress, fillControl: true, control: _copyable(url)),
        _SettingsRow(label: appStrings.accountUsername, fillControl: true, control: _copyable(username)),
        _SettingsRow(
          label: appStrings.webDavPassword,
          description: appStrings.webDavPasswordHint,
          control: OutlinedButton.icon(
            onPressed: widget.busy ? null : widget.onCreatePassword,
            icon: const Icon(Icons.password_rounded, size: 18),
            label: Text(appStrings.webDavCreatePassword),
          ),
        ),
        InkWell(
          onTap: () => setState(() => _showHints = !_showHints),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
            child: Row(
              children: <Widget>[
                Expanded(child: Text(appStrings.webDavHowTo, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: _textPrimary))),
                Icon(_showHints ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 20, color: _textMuted),
              ],
            ),
          ),
        ),
        if (_showHints)
          for (final (title, body) in _hints) _SettingsRow(label: title, description: body),
      ],
    );
  }
}
