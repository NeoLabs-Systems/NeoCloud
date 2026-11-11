part of 'main.dart';

// The options of a share link (mode, expiry, password, download limit,
// note), shared by "create" and "edit" in the share dialog. The form edits
// a ShareDraft in place and calls onChanged.

extension on ShareExpiryChoice {
  String label(DateTime? custom, String locale) {
    switch (this) {
      case ShareExpiryChoice.day:
        return appStrings.shareExpiryDay;
      case ShareExpiryChoice.week:
        return appStrings.shareExpiryWeek;
      case ShareExpiryChoice.month:
        return appStrings.shareExpiryMonth;
      case ShareExpiryChoice.never:
        return appStrings.shareExpiryNever;
      case ShareExpiryChoice.custom:
        return custom == null ? appStrings.shareExpiryCustom : DateFormat.MMMd(locale).format(custom);
    }
  }
}

String _shareModeLabel(ShareMode mode) => switch (mode) {
  ShareMode.view => appStrings.shareModeView,
  ShareMode.viewUpload => appStrings.shareModeViewUpload,
  ShareMode.uploadOnly => appStrings.shareModeUploadOnly,
};

String _shareModeHint(ShareMode mode) => switch (mode) {
  ShareMode.view => appStrings.shareModeViewHint,
  ShareMode.viewUpload => appStrings.shareModeViewUploadHint,
  ShareMode.uploadOnly => appStrings.shareModeUploadOnlyHint,
};

String _shareDraftProblemText(ShareDraftProblem problem) => switch (problem) {
  ShareDraftProblem.passwordTooShort => appStrings.sharePasswordTooShort(minSharePasswordLength),
  ShareDraftProblem.limitInvalid => appStrings.shareLimitInvalid,
  ShareDraftProblem.dateInPast => appStrings.shareExpiryInPast,
};

class _ShareForm extends StatefulWidget {
  const _ShareForm({super.key, required this.draft, required this.isFolder, required this.enabled, required this.onChanged});

  final ShareDraft draft;
  final bool isFolder;
  final bool enabled;
  final VoidCallback onChanged;

  @override
  State<_ShareForm> createState() => _ShareFormState();
}

class _ShareFormState extends State<_ShareForm> {
  late final TextEditingController _password = TextEditingController(text: widget.draft.password);
  late final TextEditingController _limit = TextEditingController(text: '${widget.draft.customLimit}');
  late final TextEditingController _note = TextEditingController(text: widget.draft.note);
  bool _showPassword = false;
  bool _passwordCopied = false;

  ShareDraft get _draft => widget.draft;

  @override
  void dispose() {
    _password.dispose();
    _limit.dispose();
    _note.dispose();
    super.dispose();
  }

  void _change(VoidCallback update) {
    setState(update);
    widget.onChanged();
  }

  Future<void> _pickDate() async {
    final now = DateTime.now();
    final picked = await showDatePicker(
      context: context,
      helpText: appStrings.shareExpiryPick,
      initialDate: _draft.customDate != null && _draft.customDate!.isAfter(now) ? _draft.customDate! : now.add(const Duration(days: 14)),
      firstDate: now.add(const Duration(days: 1)),
      lastDate: now.add(const Duration(days: 3650)),
    );
    if (picked == null) return;
    // The end of the picked day, so "expires 18 Nov" holds all of the 18th.
    _change(() {
      _draft.customDate = DateTime(picked.year, picked.month, picked.day, 23, 59);
      _draft.expiry = ShareExpiryChoice.custom;
    });
  }

  void _generatePassword() {
    final password = generatePassphrase(count: 4);
    _password.text = password;
    _change(() {
      _draft.password = password;
      _showPassword = true;
      _passwordCopied = false;
    });
  }

  Future<void> _copyPassword() async {
    await Clipboard.setData(ClipboardData(text: _draft.password));
    if (mounted) setState(() => _passwordCopied = true);
  }

  Widget _label(String text) => Padding(
    padding: const EdgeInsets.only(bottom: 8),
    child: Text(text, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w500, fontSize: 14)),
  );

  Widget _hint(String text) => Padding(
    padding: const EdgeInsets.only(top: 6),
    child: Text(text, style: TextStyle(color: _textMuted, fontSize: 12.5, height: 1.4)),
  );

  List<Widget> _modeSection() {
    return <Widget>[
      _label(appStrings.shareModeTitle),
      SegmentedButton<ShareMode>(
        segments: <ButtonSegment<ShareMode>>[
          for (final mode in ShareMode.values) ButtonSegment<ShareMode>(value: mode, label: Text(_shareModeLabel(mode))),
        ],
        selected: <ShareMode>{_draft.mode},
        showSelectedIcon: false,
        onSelectionChanged: widget.enabled ? (selection) => _change(() => _draft.mode = selection.first) : null,
      ),
      _hint(_shareModeHint(_draft.mode)),
      const SizedBox(height: AppSpacing.md),
    ];
  }

  List<Widget> _expirySection() {
    final locale = currentAppLanguage.code;
    return <Widget>[
      _label(appStrings.shareExpiry),
      Wrap(
        spacing: 8,
        runSpacing: 8,
        children: <Widget>[
          for (final choice in ShareExpiryChoice.values)
            ChoiceChip(
              label: Text(choice.label(_draft.customDate, locale)),
              selected: _draft.expiry == choice,
              showCheckmark: false,
              onSelected: !widget.enabled
                  ? null
                  : (_) {
                      if (choice == ShareExpiryChoice.custom) {
                        unawaited(_pickDate());
                      } else {
                        _change(() => _draft.expiry = choice);
                      }
                    },
            ),
        ],
      ),
      const SizedBox(height: AppSpacing.sm),
    ];
  }

  List<Widget> _passwordSection() {
    final keeps = _draft.editing && _draft.original!.hasPassword;
    return <Widget>[
      SwitchListTile(
        contentPadding: EdgeInsets.zero,
        value: _draft.protect,
        onChanged: widget.enabled ? (value) => _change(() => _draft.protect = value) : null,
        title: Text(appStrings.sharePasswordToggle, style: const TextStyle(fontSize: 14)),
        subtitle: Text(appStrings.sharePasswordHint, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
      ),
      if (_draft.protect) ...<Widget>[
        TextField(
          controller: _password,
          obscureText: !_showPassword,
          enabled: widget.enabled,
          autocorrect: false,
          onChanged: (value) => _change(() {
            _draft.password = value;
            _passwordCopied = false;
          }),
          style: _showPassword ? GoogleFonts.geistMono(fontSize: 13.5, color: _textPrimary) : null,
          decoration: InputDecoration(
            labelText: appStrings.sharePassword,
            helperText: keeps ? appStrings.sharePasswordKeep : null,
            suffixIcon: IconButton(
              tooltip: appStrings.vaultShowPassphrase,
              onPressed: () => setState(() => _showPassword = !_showPassword),
              icon: Icon(_showPassword ? Icons.visibility_off_outlined : Icons.visibility_outlined, size: 18),
            ),
          ),
        ),
        const SizedBox(height: 6),
        Wrap(
          spacing: 8,
          children: <Widget>[
            TextButton.icon(
              onPressed: widget.enabled ? _generatePassword : null,
              icon: const Icon(Icons.casino_outlined, size: 16),
              label: Text(appStrings.sharePasswordGenerate),
            ),
            TextButton.icon(
              onPressed: _draft.password.isEmpty ? null : () => unawaited(_copyPassword()),
              icon: Icon(_passwordCopied ? Icons.check_rounded : Icons.copy_rounded, size: 16),
              label: Text(_passwordCopied ? appStrings.sharePasswordCopied : appStrings.sharePasswordCopy),
            ),
          ],
        ),
        _InlineNote(message: appStrings.sharePasswordSendSeparately),
        const SizedBox(height: AppSpacing.sm),
      ],
    ];
  }

  List<Widget> _limitSection() {
    if (_draft.mode == ShareMode.uploadOnly) return const <Widget>[];
    return <Widget>[
      const SizedBox(height: AppSpacing.sm),
      _label(appStrings.shareLimitTitle),
      SegmentedButton<ShareLimitChoice>(
        segments: <ButtonSegment<ShareLimitChoice>>[
          ButtonSegment<ShareLimitChoice>(value: ShareLimitChoice.none, label: Text(appStrings.shareLimitNone)),
          ButtonSegment<ShareLimitChoice>(value: ShareLimitChoice.once, label: Text(appStrings.shareLimitOnce)),
          ButtonSegment<ShareLimitChoice>(value: ShareLimitChoice.custom, label: Text(appStrings.shareLimitCustom)),
        ],
        selected: <ShareLimitChoice>{_draft.limit},
        showSelectedIcon: false,
        onSelectionChanged: widget.enabled ? (selection) => _change(() => _draft.limit = selection.first) : null,
      ),
      if (_draft.limit == ShareLimitChoice.custom) ...<Widget>[
        const SizedBox(height: AppSpacing.sm),
        TextField(
          controller: _limit,
          enabled: widget.enabled,
          keyboardType: TextInputType.number,
          inputFormatters: <TextInputFormatter>[FilteringTextInputFormatter.digitsOnly],
          onChanged: (value) => _change(() => _draft.customLimit = int.tryParse(value) ?? 0),
          decoration: InputDecoration(labelText: appStrings.shareLimitCount),
        ),
      ],
      if (_draft.limit != ShareLimitChoice.none) _hint(appStrings.shareLimitHint),
    ];
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (widget.isFolder) ..._modeSection(),
        ..._expirySection(),
        ..._passwordSection(),
        ..._limitSection(),
        const SizedBox(height: AppSpacing.md),
        TextField(
          controller: _note,
          enabled: widget.enabled,
          minLines: 1,
          maxLines: 3,
          maxLength: maxShareNoteLength,
          onChanged: (value) => _change(() => _draft.note = value),
          decoration: InputDecoration(labelText: appStrings.shareNote, helperText: appStrings.shareNoteHint),
        ),
      ],
    );
  }
}
