part of 'main.dart';

// Sending a draft: who signs and how, then the links, which are shown once.
// The links live only in this dialog's state; nothing is stored.

/// Opens the send dialog. Resolves with the sent document's detail once the
/// links were shown and the dialog closed with "Done"; null when cancelled.
Future<SigningDetail?> showSigningSendSheet(BuildContext context, NeoCloudController controller, SigningEditorState state) {
  return showAdaptiveSheet<SigningDetail>(
    context: context,
    dismissible: false,
    builder: (dialogContext) => _SendDialog(controller: controller, state: state),
  );
}

final RegExp _emailPattern = RegExp(r'^[^\s@]+@[^\s@]+\.[^\s@]+$');

class _SendDialog extends StatefulWidget {
  const _SendDialog({required this.controller, required this.state});

  final NeoCloudController controller;
  final SigningEditorState state;

  @override
  State<_SendDialog> createState() => _SendDialogState();
}

class _SendDialogState extends State<_SendDialog> {
  late final Map<String, TextEditingController> _names = <String, TextEditingController>{
    for (final party in widget.state.parties) party.id: TextEditingController(text: party.name),
  };
  late final Map<String, TextEditingController> _emails = <String, TextEditingController>{
    for (final party in widget.state.parties) party.id: TextEditingController(text: party.email ?? ''),
  };
  late SigningOrder _order = widget.state.order;
  late String _locale = widget.state.document.locale.isEmpty ? currentAppLanguage.code : widget.state.document.locale;
  late DateTime? _expiry = widget.state.document.expiresAt ?? DateTime.now().add(const Duration(days: 30));
  bool _validated = false;
  bool _sending = false;
  String? _error;
  List<SigningLink>? _links;

  // Mail (only when the server can send it).
  bool _mail = false;
  bool _deliverTouched = false;
  bool _deliverChoice = true;
  final TextEditingController _message = TextEditingController();
  int _reminderDays = 0;
  late final Set<String> _selfIds = <String>{for (final party in widget.state.parties) if (party.isSelf) party.id};
  List<SendDelivery> _delivery = const <SendDelivery>[];

  @override
  void initState() {
    super.initState();
    _message.text = widget.state.document.message ?? '';
    _reminderDays = SendOptions.reminderChoices.contains(widget.state.document.reminderDays) ? widget.state.document.reminderDays : 0;
    unawaited(
      widget.controller.signingMailAvailable().then((available) {
        if (mounted && available) setState(() => _mail = true);
      }),
    );
  }

  int get _mailCount => _state.parties.where((party) => (_emails[party.id]?.text.trim() ?? '').isNotEmpty).length;

  /// Mail goes out unless the creator switched it off; with no address
  /// anywhere there is nothing to switch on yet.
  bool get _deliver => _mail && (_deliverTouched ? _deliverChoice : _mailCount > 0);

  bool _isSelf(String id) => _selfIds.contains(id);

  void _toggleSelf(SigningParty party) {
    final me = widget.controller.user;
    setState(() {
      if (_isSelf(party.id)) {
        _selfIds.remove(party.id);
        return;
      }
      _selfIds
        ..clear()
        ..add(party.id);
      if (me != null) {
        _names[party.id]!.text = (me.displayName ?? '').trim().isEmpty ? me.username : me.displayName!.trim();
        if ((me.email ?? '').isNotEmpty) _emails[party.id]!.text = me.email!;
      }
    });
  }

  Future<void> _signNow(String partyId) async {
    final navigator = Navigator.of(context);
    try {
      final url = await widget.controller.signingAccount.openAsOwner(_state.document.id, partyId);
      SigningDetail? detail;
      try {
        detail = await _controller.signing.get(_state.document.id);
      } catch (_) {
        // The page reloads it anyway.
      }
      navigator.pop(detail);
      widget.controller.openSigningLink(url);
    } catch (error) {
      if (mounted) setState(() => _error = signingErrorText(_controller, error, appStrings.signingSignNowFailed));
    }
  }

  SigningEditorState get _state => widget.state;
  NeoCloudController get _controller => widget.controller;

  @override
  void dispose() {
    for (final controller in <TextEditingController>[..._names.values, ..._emails.values, _message]) {
      controller.dispose();
    }
    _links = null;
    super.dispose();
  }

  String? _nameError(String id) => _validated && _names[id]!.text.trim().isEmpty ? appStrings.signingNameRequired : null;

  String? _emailError(String id) {
    final text = _emails[id]!.text.trim();
    return _validated && text.isNotEmpty && !_emailPattern.hasMatch(text) ? appStrings.signingEmailInvalid : null;
  }

  bool get _valid => _state.parties.every((party) => _names[party.id]!.text.trim().isNotEmpty && (_emails[party.id]!.text.trim().isEmpty || _emailPattern.hasMatch(_emails[party.id]!.text.trim())));

  SendCheck get _check => checkSend(_state.parties, _state.fields, invalidFieldIds: _state.fieldsWithProblems);

  String _partyName(String id) => _state.partyById(id)?.name ?? '';

  Future<void> _pickExpiry() async {
    final now = DateTime.now();
    final picked = await showDatePicker(
      context: context,
      initialDate: (_expiry ?? now.add(const Duration(days: 30))).isBefore(now) ? now.add(const Duration(days: 1)) : (_expiry ?? now.add(const Duration(days: 30))),
      firstDate: now.add(const Duration(days: 1)),
      lastDate: now.add(const Duration(days: 365)),
    );
    if (picked != null) setState(() => _expiry = DateTime(picked.year, picked.month, picked.day, 23, 59));
  }

  Future<void> _send() async {
    setState(() {
      _validated = true;
      _error = null;
    });
    if (!_valid || _check.blocked) return;
    setState(() => _sending = true);
    try {
      for (final party in _state.parties) {
        _state.renameParty(party.id, _names[party.id]!.text);
        _state.setPartyEmail(party.id, _emails[party.id]!.text.trim());
        if (_mail) _state.setPartySelf(party.id, _isSelf(party.id));
      }
      _state.setOrder(_order);
      if (!await _state.flush()) throw _state.lastError ?? const SigningException(SigningErrorKind.server);
      await _controller.signing.update(
        _state.document.id,
        SigningSettingsPatch(signingOrder: _order, locale: _locale, expiresAt: _expiry, clearExpiry: _expiry == null),
      );
      final result = await _controller.signing.send(
        _state.document.id,
        options: _mail ? SendOptions(message: _message.text, deliverEmail: _deliver, reminderDays: _deliver ? _reminderDays : 0) : null,
      );
      if (mounted) {
        setState(() {
          _delivery = result.delivery;
          _links = result.links;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = signingErrorText(_controller, error, appStrings.signingSendFailed));
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  Future<void> _done() async {
    try {
      final detail = await _controller.signing.get(_state.document.id);
      if (mounted) Navigator.of(context).pop(detail);
    } catch (_) {
      if (mounted) Navigator.of(context).pop();
    }
  }

  @override
  Widget build(BuildContext context) {
    final links = _links;
    if (links != null) return _linksView(links);
    final check = _check;
    final needsConfirm = check.hasWarnings;
    return _AdaptiveDialog(
      title: Text(appStrings.signingSendTitle),
      content: SizedBox(
        width: 520,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Text(appStrings.signingSendIntro, style: TextStyle(color: _textSecondary, height: 1.45)),
            const SizedBox(height: 12),
            for (final (index, party) in _state.parties.indexed) ...<Widget>[
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Padding(
                    padding: const EdgeInsets.only(top: 14, right: 10),
                    child: Container(width: 12, height: 12, decoration: BoxDecoration(color: _state.colorOf(party.id), shape: BoxShape.circle)),
                  ),
                  Expanded(
                    child: TextField(
                      key: ValueKey<String>('send-name-$index'),
                      controller: _names[party.id],
                      onChanged: (_) => setState(() {}),
                      decoration: InputDecoration(labelText: appStrings.signingPartyName, errorText: _nameError(party.id), isDense: true),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: TextField(
                      key: ValueKey<String>('send-email-$index'),
                      controller: _emails[party.id],
                      keyboardType: TextInputType.emailAddress,
                      onChanged: (_) => setState(() {}),
                      decoration: InputDecoration(labelText: appStrings.signingPartyEmail, errorText: _emailError(party.id), helperText: _mail ? appStrings.signingMailInvitationHint : null, helperMaxLines: 2, isDense: true),
                    ),
                  ),
                ],
              ),
              if (_mail)
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(
                    key: ValueKey<String>('send-self-$index'),
                    onPressed: () => _toggleSelf(party),
                    style: TextButton.styleFrom(visualDensity: VisualDensity.compact, padding: const EdgeInsets.symmetric(horizontal: 6)),
                    icon: Icon(_isSelf(party.id) ? Icons.check_circle_rounded : Icons.person_outline_rounded, size: 18),
                    label: Text(_isSelf(party.id) ? appStrings.signingThisIsMeOn : appStrings.signingThisIsMe),
                  ),
                ),
              const SizedBox(height: 10),
            ],
            if (_mail) ...<Widget>[
              _SendMailOptions(
                deliver: _deliver,
                onDeliver: (value) => setState(() {
                  _deliverTouched = true;
                  _deliverChoice = value;
                }),
                message: _message,
                onMessage: () => setState(() {}),
                reminderDays: _reminderDays,
                onReminderDays: (days) => setState(() => _reminderDays = days),
                mailCount: _mailCount,
              ),
              const SizedBox(height: 14),
            ],
            if (_state.parties.length > 1) ...<Widget>[
              Text(appStrings.signingOrderLabel, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
              const SizedBox(height: 6),
              SegmentedButton<SigningOrder>(
                key: const ValueKey<String>('send-order'),
                showSelectedIcon: false,
                segments: <ButtonSegment<SigningOrder>>[
                  ButtonSegment<SigningOrder>(value: SigningOrder.preserved, label: Text(appStrings.signingOrderPreserved)),
                  ButtonSegment<SigningOrder>(value: SigningOrder.random, label: Text(appStrings.signingOrderRandom)),
                ],
                selected: <SigningOrder>{_order},
                onSelectionChanged: (value) => setState(() => _order = value.first),
              ),
              const SizedBox(height: 14),
            ],
            Text(appStrings.signingExpiry, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
            const SizedBox(height: 6),
            Wrap(
              spacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: <Widget>[
                OutlinedButton.icon(
                  key: const ValueKey<String>('send-expiry'),
                  onPressed: _pickExpiry,
                  icon: const Icon(Icons.event_outlined, size: 18),
                  label: Text(_expiry == null ? appStrings.signingNoExpiry : DateFormat.yMMMd(currentAppLanguage.code).format(_expiry!)),
                ),
                if (_expiry != null) TextButton(onPressed: () => setState(() => _expiry = null), child: Text(appStrings.signingNoExpiry)),
              ],
            ),
            const SizedBox(height: 14),
            Text(appStrings.signingLanguage, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
            const SizedBox(height: 6),
            SegmentedButton<String>(
              key: const ValueKey<String>('send-language'),
              showSelectedIcon: false,
              segments: <ButtonSegment<String>>[
                for (final language in AppLanguage.values) ButtonSegment<String>(value: language.code, label: Text(language.label)),
              ],
              selected: <String>{_locale},
              onSelectionChanged: (value) => setState(() => _locale = value.first),
            ),
            for (final id in check.withoutFields)
              Padding(
                padding: const EdgeInsets.only(top: 10),
                child: _Notice(message: appStrings.signingPartyNoFields(_partyName(id)), color: _danger, icon: Icons.error_outline_rounded),
              ),
            if (check.invalidFields.isNotEmpty)
              Padding(padding: const EdgeInsets.only(top: 10), child: _Notice(message: appStrings.signingProblemsBlock, color: _danger, icon: Icons.error_outline_rounded)),
            for (final id in check.withoutRequired)
              Padding(
                padding: const EdgeInsets.only(top: 10),
                child: _Notice(message: appStrings.signingPartyNoRequired(_partyName(id)), color: _warning, icon: Icons.warning_amber_rounded),
              ),
            if (_error != null) Padding(padding: const EdgeInsets.only(top: 10), child: _Notice(message: _error!, color: _danger, icon: Icons.error_outline_rounded)),
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(key: const ValueKey<String>('send-cancel'), onPressed: _sending ? null : () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(
          key: const ValueKey<String>('send-confirm'),
          onPressed: _sending || check.blocked ? null : () => unawaited(_send()),
          child: _sending
              ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2))
              : Text(needsConfirm ? appStrings.signingSendAnyway : appStrings.signingSend),
        ),
      ],
    );
  }

  Widget _linksView(List<SigningLink> links) {
    final sent = _delivery.where((entry) => entry.status == DeliveryStatus.sent || entry.status == DeliveryStatus.queued).length;
    final failed = _delivery.where((entry) => entry.status == DeliveryStatus.failed).length;
    final selfLink = links.where((link) => _isSelf(link.partyId)).firstOrNull;
    final done = selfLink == null
        ? FilledButton(key: const ValueKey<String>('send-done'), onPressed: () => unawaited(_done()), child: Text(appStrings.signingDone))
        : TextButton(key: const ValueKey<String>('send-done'), onPressed: () => unawaited(_done()), child: Text(appStrings.signingDone));
    return _AdaptiveDialog(
      icon: const Icon(Icons.link_rounded),
      title: Text(sent > 0 ? appStrings.signingMailSentTitle : appStrings.signingLinksTitle),
      content: SizedBox(
        width: 520,
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              if (sent > 0) ...<Widget>[
                _Notice(message: appStrings.signingMailSummary(sent), color: _success, icon: Icons.mark_email_read_outlined),
                const SizedBox(height: 10),
              ],
              if (failed > 0) ...<Widget>[
                _Notice(message: appStrings.signingMailSomeFailed, color: _danger, icon: Icons.error_outline_rounded),
                const SizedBox(height: 10),
              ],
              _Notice(message: appStrings.signingLinksWarning, color: _warning, icon: Icons.warning_amber_rounded),
              const SizedBox(height: 12),
              for (final link in links)
                _LinkTile(
                  key: ValueKey<String>('link-${link.partyId}'),
                  name: _partyName(link.partyId),
                  color: _state.colorOf(link.partyId),
                  url: link.url,
                  delivery: _delivery.where((entry) => entry.partyId == link.partyId).firstOrNull,
                  waitsTurn: _order == SigningOrder.preserved && links.indexOf(link) > 0,
                ),
              if (_error != null) _Notice(message: _error!, color: _danger, icon: Icons.error_outline_rounded),
            ],
          ),
        ),
      ),
      actions: <Widget>[
        if (selfLink != null)
          FilledButton.icon(
            key: const ValueKey<String>('send-sign-now'),
            onPressed: () => unawaited(_signNow(selfLink.partyId)),
            icon: const Icon(Icons.draw_outlined, size: 18),
            label: Text(appStrings.signingSignNow),
          ),
        done,
      ],
    );
  }
}

/// One link with its copy button and a QR code on demand. Used after
/// sending and after making a new link.
class _LinkTile extends StatefulWidget {
  const _LinkTile({super.key, required this.name, required this.color, required this.url, this.delivery, this.waitsTurn = false});

  final String name;
  final Color color;
  final String url;

  /// How this party's invitation mail went, when mail was used.
  final SendDelivery? delivery;

  /// Their invitation follows when their turn comes (one after another).
  final bool waitsTurn;

  @override
  State<_LinkTile> createState() => _LinkTileState();
}

class _LinkTileState extends State<_LinkTile> {
  bool _qr = false;

  Future<void> _copy() async {
    await Clipboard.setData(ClipboardData(text: widget.url));
    if (mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(appStrings.signingLinkCopied)));
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(color: _bgSecondary, borderRadius: BorderRadius.circular(AppRadius.control), border: Border.all(color: _border)),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Row(
              children: <Widget>[
                Container(width: 12, height: 12, decoration: BoxDecoration(color: widget.color, shape: BoxShape.circle)),
                const SizedBox(width: 8),
                Expanded(child: Text(widget.name, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600))),
                if (widget.delivery != null) _DeliveryChip(key: ValueKey<String>('link-delivery-${widget.delivery!.partyId}'), status: widget.delivery!.status, waitsTurn: widget.waitsTurn),
                IconButton(key: const ValueKey<String>('link-copy'), tooltip: appStrings.signingCopyLink, onPressed: () => unawaited(_copy()), icon: const Icon(Icons.copy_rounded, size: 18)),
                IconButton(key: const ValueKey<String>('link-qr'), tooltip: appStrings.signingShowQr, onPressed: () => setState(() => _qr = !_qr), icon: const Icon(Icons.qr_code_2_rounded, size: 20)),
              ],
            ),
            if (widget.delivery?.status == DeliveryStatus.failed)
              Padding(
                padding: const EdgeInsets.only(bottom: 6),
                child: Text(
                  '${appStrings.signingDeliveryFailedHelp}${widget.delivery!.error == null ? '' : ' (${widget.delivery!.error})'}',
                  key: ValueKey<String>('link-failed-${widget.delivery!.partyId}'),
                  style: TextStyle(color: _danger, fontSize: 12.5, height: 1.35),
                ),
              ),
            SelectableText(widget.url, maxLines: 2, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
            if (_qr)
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Center(child: QrImageView(data: widget.url, size: 180, backgroundColor: Colors.white, padding: const EdgeInsets.all(8))),
              ),
          ],
        ),
      ),
    );
  }
}
