part of 'main.dart';

// Building blocks of the vault screens: the passphrase fields with a
// strength meter and a generator, the recovery-word grid and sheet, the
// banner inside an unlocked vault and "Show recovery key".

/// The passphrase being chosen, shared by "new vault" and "forgot
/// passphrase".
class _PassphraseDraft extends ChangeNotifier {
  _PassphraseDraft() {
    passphrase.addListener(notifyListeners);
    repeat.addListener(notifyListeners);
  }

  final TextEditingController passphrase = TextEditingController();
  final TextEditingController repeat = TextEditingController();
  String? _generated;
  bool visible = false;

  bool get isGenerated => _generated != null && passphrase.text == _generated;

  PassphraseStrength get strength => estimatePassphrase(passphrase.text, generated: isGenerated);

  /// A generated passphrase is shown in clear, so it needs no repeat.
  bool get matches => isGenerated || passphrase.text == repeat.text;

  void generate() {
    final phrase = generatePassphrase();
    _generated = phrase;
    visible = true;
    passphrase.text = phrase;
    repeat.text = phrase;
  }

  void toggleVisible() {
    visible = !visible;
    notifyListeners();
  }

  /// Why the passphrase can't be used yet, or null.
  String? get problem {
    final value = strength;
    if (passphrase.text.isEmpty || value.tooShort) return appStrings.vaultHintTooShort(minVaultPassphraseLength);
    if (!value.acceptable) return appStrings.vaultHintTooWeak;
    if (!matches) return appStrings.passwordsDoNotMatch;
    return null;
  }

  @override
  void dispose() {
    passphrase.dispose();
    repeat.dispose();
    super.dispose();
  }
}

class _PassphraseFields extends StatelessWidget {
  const _PassphraseFields({required this.draft, required this.enabled, this.label});

  final _PassphraseDraft draft;
  final bool enabled;
  final String? label;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: draft,
      builder: (context, _) {
        final generated = draft.isGenerated;
        return Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            TextField(
              controller: draft.passphrase,
              obscureText: !draft.visible,
              enabled: enabled,
              autocorrect: false,
              enableSuggestions: false,
              style: draft.visible ? GoogleFonts.geistMono(fontSize: 13.5, color: _textPrimary) : null,
              decoration: InputDecoration(
                labelText: label ?? appStrings.vaultPassphrase,
                suffixIcon: IconButton(
                  tooltip: appStrings.vaultShowPassphrase,
                  onPressed: draft.toggleVisible,
                  icon: Icon(draft.visible ? Icons.visibility_off_outlined : Icons.visibility_outlined, size: 18),
                ),
              ),
            ),
            const SizedBox(height: 6),
            Wrap(
              spacing: 8,
              children: <Widget>[
                TextButton.icon(
                  onPressed: enabled ? draft.generate : null,
                  icon: const Icon(Icons.casino_outlined, size: 16),
                  label: Text(appStrings.vaultGeneratePassphrase),
                ),
                if (generated)
                  TextButton.icon(
                    onPressed: () => unawaited(Clipboard.setData(ClipboardData(text: draft.passphrase.text))),
                    icon: const Icon(Icons.copy_rounded, size: 16),
                    label: Text(appStrings.vaultCopyPassphrase),
                  ),
              ],
            ),
            const SizedBox(height: 4),
            _PassphraseMeter(strength: draft.strength),
            if (generated) ...<Widget>[
              const SizedBox(height: AppSpacing.sm),
              _InlineNote(message: appStrings.vaultGeneratedHint),
            ] else ...<Widget>[
              const SizedBox(height: AppSpacing.sm),
              TextField(
                controller: draft.repeat,
                obscureText: !draft.visible,
                enabled: enabled,
                autocorrect: false,
                enableSuggestions: false,
                decoration: InputDecoration(labelText: appStrings.vaultPassphraseRepeat),
              ),
            ],
          ],
        );
      },
    );
  }
}

/// Four segments and a word; red until the passphrase is acceptable.
class _PassphraseMeter extends StatelessWidget {
  const _PassphraseMeter({required this.strength});

  final PassphraseStrength strength;

  @override
  Widget build(BuildContext context) {
    final (String label, String hint) = switch (strength.level) {
      PassphraseLevel.tooWeak => (appStrings.vaultStrengthTooWeak, appStrings.vaultHintTooWeak),
      PassphraseLevel.weak => (appStrings.vaultStrengthWeak, appStrings.vaultHintTooWeak),
      PassphraseLevel.good => (appStrings.vaultStrengthGood, appStrings.vaultHintGood),
      PassphraseLevel.strong => (appStrings.vaultStrengthStrong, appStrings.vaultHintStrong),
    };
    final empty = strength.length == 0;
    final color = empty ? _borderLight : (strength.acceptable ? _success : _danger);
    final message = strength.tooShort ? appStrings.vaultHintTooShort(minVaultPassphraseLength) : hint;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Row(
          children: <Widget>[
            for (var i = 0; i < 4; i++) ...<Widget>[
              if (i > 0) const SizedBox(width: 4),
              Expanded(
                child: Container(
                  height: 5,
                  decoration: BoxDecoration(
                    color: i < strength.score ? color : _bgTertiary,
                    borderRadius: BorderRadius.circular(AppRadius.pill),
                  ),
                ),
              ),
            ],
            const SizedBox(width: 10),
            Text(
              empty ? appStrings.strengthEmpty : appStrings.vaultStrengthLabel(label),
              style: TextStyle(color: _textSecondary, fontSize: 12.5, fontWeight: FontWeight.w500),
            ),
          ],
        ),
        if (!empty) ...<Widget>[
          const SizedBox(height: 4),
          Text(message, style: TextStyle(color: _textMuted, fontSize: 12, height: 1.35)),
        ],
      ],
    );
  }
}

/// Where this account lives, for the recovery sheet.
String _serverLabel(NeoCloudController controller) {
  if (controller.connectedServer.isNotEmpty) return controller.connectedServer;
  if (controller.client.baseUrl.isNotEmpty) return controller.client.baseUrl;
  return kIsWeb ? Uri.base.origin : '';
}

/// The 24 recovery words, numbered, in reading order.
class _RecoveryWordsGrid extends StatelessWidget {
  const _RecoveryWordsGrid({required this.words});

  final List<String> words;

  @override
  Widget build(BuildContext context) {
    return _AppCard(
      padding: const EdgeInsets.all(14),
      child: LayoutBuilder(
        builder: (context, constraints) {
          final columns = constraints.maxWidth >= 420 ? 4 : 3;
          final width = (constraints.maxWidth - (columns - 1) * 8) / columns;
          return Wrap(
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[
              for (var i = 0; i < words.length; i++)
                SizedBox(
                  width: width,
                  child: Text.rich(
                    TextSpan(
                      children: <InlineSpan>[
                        TextSpan(text: '${i + 1}'.padLeft(2), style: TextStyle(color: _textMuted)),
                        const TextSpan(text: '  '),
                        TextSpan(text: words[i], style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w500)),
                      ],
                    ),
                    style: GoogleFonts.geistMono(fontSize: 13),
                  ),
                ),
            ],
          );
        },
      ),
    );
  }
}

/// Copy and download for the recovery words.
class _RecoveryWordActions extends StatefulWidget {
  const _RecoveryWordActions({required this.words, required this.vaultName, required this.server});

  final List<String> words;
  final String vaultName;
  final String server;

  @override
  State<_RecoveryWordActions> createState() => _RecoveryWordActionsState();
}

class _RecoveryWordActionsState extends State<_RecoveryWordActions> {
  bool _copied = false;

  String _sheet() {
    final locale = currentAppLanguage.code;
    final words = widget.words;
    final half = words.length ~/ 2;
    final rows = <String>[
      for (var i = 0; i < half; i++) '${'${i + 1}.'.padLeft(3)} ${words[i].padRight(12)}${'${i + half + 1}.'.padLeft(3)} ${words[i + half]}',
    ];
    return <String>[
      appStrings.vaultSheetTitle,
      '=' * appStrings.vaultSheetTitle.length,
      '',
      appStrings.vaultSheetVault(widget.vaultName),
      if (widget.server.isNotEmpty) appStrings.vaultSheetServer(widget.server),
      appStrings.vaultSheetCreated(DateFormat.yMMMMd(locale).format(DateTime.now())),
      '',
      ...rows,
      '',
      appStrings.vaultSheetUse,
      '',
      appStrings.vaultSheetKeep,
      '',
    ].join('\n');
  }

  Future<void> _download() async {
    final safeName = widget.vaultName.replaceAll(RegExp(r'[^\w\- ]+'), '').trim();
    await FilePicker.platform.saveFile(
      fileName: 'NeoCloud recovery key - ${safeName.isEmpty ? 'vault' : safeName}.txt',
      bytes: Uint8List.fromList(utf8.encode(_sheet())),
    );
  }

  Future<void> _copy() async {
    await Clipboard.setData(ClipboardData(text: widget.words.join(' ')));
    if (mounted) setState(() => _copied = true);
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: <Widget>[
            OutlinedButton.icon(
              onPressed: () => unawaited(_download().catchError((Object _) {})),
              icon: const Icon(Icons.download_rounded, size: 16),
              label: Text(appStrings.vaultRecoveryDownload),
            ),
            TextButton.icon(
              onPressed: () => unawaited(_copy()),
              icon: Icon(_copied ? Icons.check_rounded : Icons.copy_rounded, size: 16),
              label: Text(appStrings.vaultRecoveryCopy),
            ),
          ],
        ),
        if (_copied) ...<Widget>[
          const SizedBox(height: 6),
          Text(appStrings.vaultRecoveryCopied, style: TextStyle(color: _textMuted, fontSize: 12.5)),
        ],
      ],
    );
  }
}
