part of 'main.dart';

// The progress page of a document that was sent (/app/signing/<id>): status,
// who signed, links, the preview with every value on the pages, the audit
// trail and, at the end, the signed file.

class SigningStatusPage extends StatefulWidget {
  const SigningStatusPage({super.key, required this.controller, required this.detail, this.pagesBuilder = _pdfPagesBuilder});

  final NeoCloudController controller;
  final SigningDetail detail;
  final SigningPagesBuilder pagesBuilder;

  @override
  State<SigningStatusPage> createState() => _SigningStatusPageState();
}

class _SigningStatusPageState extends State<SigningStatusPage> {
  late SigningDetail _detail = widget.detail;
  SigningEventLog? _log;
  Map<String, SigningValue> _values = const <String, SigningValue>{};
  Uint8List? _pdf;
  String? _error;
  String? _verifyMessage;
  bool _verifyOk = true;
  bool _mail = false;
  bool _remindingAll = false;
  final SigningPageController _pages = SigningPageController();
  final Map<String, Future<Uint8List>> _images = <String, Future<Uint8List>>{};

  NeoCloudController get _controller => widget.controller;
  SigningApi get _api => _controller.signing;
  SigningDocument get _document => _detail.document;

  @override
  void initState() {
    super.initState();
    _controller.signingRevision.addListener(_changedElsewhere);
    unawaited(
      _controller.signingMailAvailable().then((available) {
        if (mounted && available) setState(() => _mail = true);
      }),
    );
    unawaited(_loadPdf());
    unawaited(_reload());
  }

  @override
  void dispose() {
    _controller.signingRevision.removeListener(_changedElsewhere);
    _pages.dispose();
    super.dispose();
  }

  void _changedElsewhere() => unawaited(_reload());

  Future<void> _loadPdf() async {
    try {
      final bytes = await _api.pdf(_document.id);
      if (mounted) setState(() => _pdf = bytes);
    } catch (_) {
      // The page works without the preview.
    }
  }

  Future<void> _reload() async {
    try {
      final detail = await _api.get(_document.id);
      final log = await _api.events(_document.id);
      final values = await _api.previewValues(_document.id);
      if (!mounted) return;
      setState(() {
        _detail = detail;
        _log = log;
        _values = <String, SigningValue>{for (final value in values) value.fieldId: value};
        _error = null;
      });
    } on SigningException catch (error) {
      if (!mounted) return;
      if (error.kind == SigningErrorKind.notFound) {
        _controller.openSigning(null);
      } else {
        setState(() => _error = _controller.friendlyError(error));
      }
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    }
  }

  void _toast(String message) => ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(message)));

  Future<void> _guard(Future<void> Function() action) async {
    try {
      await action();
    } catch (error) {
      if (mounted) _toast(signingErrorText(_controller, error, appStrings.signingLoadFailed));
    }
  }

  Future<void> _cancel() => _confirmDelete(
    context,
    title: appStrings.signingCancelTitle,
    message: appStrings.signingCancelBody,
    confirmLabel: appStrings.signingCancelDoc,
    onConfirm: () => _guard(() async {
      await _api.cancel(_document.id);
      await _reload();
    }),
  );

  Future<void> _delete() => _confirmDelete(
    context,
    title: appStrings.signingDeleteTitle,
    message: appStrings.signingDeleteBody,
    confirmLabel: appStrings.signingDeleteDoc,
    onConfirm: () => _guard(() async {
      await _api.delete(_document.id);
      _controller.openSigning(null);
    }),
  );

  Future<void> _duplicate() => _guard(() async {
    final copy = await _api.duplicate(_document.id);
    _controller.openSigning(copy.document.id);
  });

  Future<void> _retryFinalization() => _guard(() async {
    await _api.retryFinalization(_document.id);
    await _reload();
  });

  Future<void> _openSigned() => _guard(() async {
    final node = await _controller.client.getNode(_document.finalNodeId!);
    _controller.openFolder(node.parentId);
  });

  Future<void> _downloadSigned({bool certificate = true}) => _guard(() async {
    final bytes = await _api.finalPdf(_document.id, certificate: certificate);
    await FilePicker.platform.saveFile(fileName: '${_document.title} (${currentAppLanguage.code == 'de' ? 'signiert' : 'signed'}).pdf', bytes: bytes);
  });

  Future<void> _downloadAudit() => _guard(() async {
    final json = await _api.auditJson(_document.id);
    await FilePicker.platform.saveFile(
      fileName: 'signing-audit-${_document.id}.json',
      bytes: Uint8List.fromList(utf8.encode(const JsonEncoder.withIndent('  ').convert(json))),
    );
  });

  Future<void> _verify() => _guard(() async {
    final log = await _api.events(_document.id);
    if (!mounted) return;
    setState(() {
      _log = log;
      _verifyOk = log.chainValid != false;
      _verifyMessage = _verifyOk ? appStrings.signingChainOk('${log.events.length}') : appStrings.signingChainBroken(log.brokenAt ?? '?');
    });
  });

  Future<void> _regenerate(SigningParty party) => _confirmDelete(
    context,
    title: appStrings.signingRegenerateTitle,
    message: appStrings.signingRegenerateBody,
    confirmLabel: appStrings.signingRegenerate,
    onConfirm: () => _guard(() async {
      final link = await _api.regenerateLink(_document.id, party.id);
      if (!mounted) return;
      await showAdaptiveSheet<void>(
        context: context,
        dismissible: false,
        builder: (dialogContext) => _AdaptiveDialog(
          icon: const Icon(Icons.link_rounded),
          title: Text(appStrings.signingNewLinkTitle(party.name)),
          content: SizedBox(
            width: 520,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                _Notice(message: appStrings.signingLinksWarning, color: _warning, icon: Icons.warning_amber_rounded),
                const SizedBox(height: 12),
                _LinkTile(name: party.name, color: resolvePartyColor(party.color, party.position), url: link.url),
              ],
            ),
          ),
          actions: <Widget>[FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.signingDone))],
        ),
      );
      await _reload();
    }),
  );

  Future<void> _revoke(SigningParty party) => _confirmDelete(
    context,
    title: appStrings.signingRevokeTitle,
    message: appStrings.signingRevokeBody,
    confirmLabel: appStrings.signingRevoke,
    onConfirm: () => _guard(() async {
      await _api.revokeParty(_document.id, party.id);
      await _reload();
    }),
  );

  // Mail

  bool _isActive(SigningParty party) =>
      (_document.status == SigningStatus.sent || _document.status == SigningStatus.inProgress) && (party.status == PartyStatus.pending || party.status == PartyStatus.opened);

  /// Reminders go to parties the creator is waiting for, never to themselves.
  bool _remindable(SigningParty party) => _isActive(party) && !party.isSelf;

  Future<void> _remind(SigningParty party) async {
    await _controller.signingAccount.remind(_document.id, party.id);
  }

  Future<void> _remindAll() async {
    setState(() => _remindingAll = true);
    try {
      final results = await _controller.signingAccount.remindAll(_document.id);
      final sent = results.where((result) => result.ok).length;
      final refused = results.length - sent;
      if (mounted) _toast(refused > 0 ? '${appStrings.signingRemindAllDone(sent)} ${appStrings.signingRemindAllSkipped(refused)}' : appStrings.signingRemindAllDone(sent));
      await _reload();
    } on SigningException catch (error) {
      if (mounted) _toast(_reminderErrorText(error, ''));
    } finally {
      if (mounted) setState(() => _remindingAll = false);
    }
  }

  /// Sends the invitation again to everyone whose mail failed.
  Future<void> _resendFailed() async {
    var failed = 0;
    for (final party in _detail.parties.where((party) => party.delivery.status == DeliveryStatus.failed)) {
      try {
        await _controller.signingAccount.remind(_document.id, party.id);
      } on SigningException catch (error) {
        failed++;
        if (mounted) _toast(_reminderErrorText(error, party.name));
      }
    }
    if (mounted && failed == 0) _toast(appStrings.signingMailResent);
    await _reload();
  }

  Future<void> _signNow(SigningParty party) => _guard(() async {
    final url = await _controller.signingAccount.openAsOwner(_document.id, party.id);
    _controller.openSigningLink(url);
  });

  // Preview

  Future<Uint8List> _imageOf(String fieldId) => _images.putIfAbsent(fieldId, () => _api.valueImage(_document.id, fieldId));

  List<Widget> _overlay(BuildContext context, int pageIndex, Size pageSize) {
    final boxes = <Widget>[];
    for (final field in _detail.fields) {
      final index = _detail.parties.indexWhere((party) => party.id == field.partyId);
      final color = resolvePartyColor(index < 0 ? null : _detail.parties[index].color, math.max(0, index));
      final spec = FieldTypes.of(field.type);
      final value = _values[field.id];
      for (final area in field.areas) {
        if (area.page != pageIndex) continue;
        boxes.add(
          Positioned.fromRect(
            key: ValueKey<String>('status-box-${field.id}'),
            rect: fractionToPixels(areaRect(area), pageSize),
            child: FieldBox(
              color: color,
              icon: spec.icon,
              readonly: true,
              showLabel: false,
              filled: value?.isFilled ?? false,
              child: _valueView(field, spec, value, pageSize.height * area.h),
            ),
          ),
        );
      }
    }
    return <Widget>[Positioned.fill(child: Stack(clipBehavior: Clip.none, children: boxes))];
  }

  Widget? _valueView(SigningField field, FieldTypeSpec spec, SigningValue? value, double heightPx) {
    if (value != null && value.hasFile && spec.isImageValue) {
      if (!spec.imageAccept && !spec.needsPad) return Align(alignment: Alignment.centerLeft, child: Text(value.fileName ?? '', maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: Colors.black87, fontSize: 11)));
      return FutureBuilder<Uint8List>(
        future: _imageOf(field.id),
        builder: (context, snapshot) => snapshot.hasData ? Image.memory(snapshot.data!, fit: BoxFit.contain, alignment: Alignment.centerLeft) : const SizedBox.shrink(),
      );
    }
    final shown = value?.value ?? (field.readonly || !spec.signerFillable ? field.defaultValue : null);
    if (shown == null) return null;
    if (field.type == FieldType.checkbox) {
      return shown == true ? const FittedBox(child: Icon(Icons.check_rounded, color: Colors.black87)) : null;
    }
    final text = spec.signerFillable ? spec.display(field, shown, locale: currentAppLanguage.code) : '$shown';
    if (text.isEmpty) return null;
    return Align(
      alignment: Alignment.centerLeft,
      child: Text(text, maxLines: field.prefs.multiline ? 4 : 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: Colors.black87, fontSize: math.max(8, math.min(heightPx * 0.62, 15)))),
    );
  }

  // Layout

  /// Failed mails, the "remind everyone" button, the automatic reminders and,
  /// at the end, who got the signed copies.
  List<Widget> _mailBanners() {
    final failed = _detail.parties.where((party) => party.delivery.status == DeliveryStatus.failed).toList();
    final active = _detail.parties.where((party) => _remindable(party) && (party.email ?? '').isNotEmpty).toList();
    final completedTo = _document.status == SigningStatus.completed
        ? _detail.parties.where((party) => party.delivery.status == DeliveryStatus.sent && (party.email ?? '').isNotEmpty).map((party) => party.email!).toList()
        : const <String>[];
    return <Widget>[
      if (failed.isNotEmpty)
        Column(
          key: const ValueKey<String>('signing-mail-failed'),
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            _Notice(message: appStrings.signingMailFailedBanner(failed.length), color: _danger, icon: Icons.error_outline_rounded),
            const SizedBox(height: 6),
            TextButton.icon(
              key: const ValueKey<String>('signing-mail-resend'),
              onPressed: () => unawaited(_resendFailed()),
              icon: const Icon(Icons.refresh_rounded, size: 18),
              label: Text(appStrings.signingMailResend),
            ),
          ],
        ),
      if (active.length > 1 || (active.isNotEmpty && _document.reminderDays > 0))
        Wrap(
          spacing: 12,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            if (active.isNotEmpty)
              OutlinedButton.icon(
                key: const ValueKey<String>('signing-remind-all'),
                onPressed: _remindingAll ? null : () => unawaited(_remindAll()),
                icon: const Icon(Icons.notifications_active_outlined, size: 18),
                label: Text(appStrings.signingRemindAll),
              ),
            if (_document.reminderDays > 0) Text(appStrings.signingAutoRemind(_document.reminderDays), style: TextStyle(color: _textMuted, fontSize: 12.5)),
          ],
        ),
      if (completedTo.isNotEmpty)
        KeyedSubtree(
          key: const ValueKey<String>('signing-mail-copies'),
          child: _Notice(message: appStrings.signingMailCopies(completedTo.join(', ')), color: _success, icon: Icons.mark_email_read_outlined),
        ),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final wide = MediaQuery.sizeOf(context).width >= 1100;
    final header = _StatusHeader(
      detail: _detail,
      onBack: () => _controller.openSigning(null),
      onCancel: () => unawaited(_cancel()),
      onDelete: () => unawaited(_delete()),
      onDuplicate: () => unawaited(_duplicate()),
      onOpenSigned: () => unawaited(_openSigned()),
      onDownloadSigned: (certificate) => unawaited(_downloadSigned(certificate: certificate)),
      onRetryFinalization: () => unawaited(_retryFinalization()),
    );
    final parties = _AppCard(
      padding: EdgeInsets.zero,
      child: Column(
        children: <Widget>[
          for (final (index, party) in _detail.parties.indexed) ...<Widget>[
            if (index > 0) Divider(height: 1, color: _border),
            _PartyCard(
              party: party,
              index: index,
              live: _document.status == SigningStatus.sent || _document.status == SigningStatus.inProgress,
              mail: _mail,
              autoDays: _document.reminderDays,
              remind: _mail && _remindable(party) && (party.email ?? '').isNotEmpty ? () => _remind(party) : null,
              onReminded: () => unawaited(_reload()),
              onSignNow: party.isSelf && _isActive(party) ? () => unawaited(_signNow(party)) : null,
              onRegenerate: () => unawaited(_regenerate(party)),
              onRevoke: () => unawaited(_revoke(party)),
            ),
          ],
        ],
      ),
    );
    final preview = _AppCard(
      padding: EdgeInsets.zero,
      child: SizedBox(
        key: const ValueKey<String>('signing-preview'),
        height: wide ? 720 : 520,
        child: ClipRRect(
          borderRadius: BorderRadius.circular(AppRadius.card),
          child: _pdf == null
              ? const Center(child: CircularProgressIndicator())
              : Stack(
                  children: <Widget>[
                    Positioned.fill(child: ColoredBox(color: _bgTertiary, child: widget.pagesBuilder(context, _pdf!, _pages, _overlay))),
                    Positioned(right: 12, bottom: 12, child: _ZoomBar(pages: _pages)),
                  ],
                ),
        ),
      ),
    );
    final timeline = _Timeline(
      log: _log,
      parties: _detail.parties,
      verifyMessage: _verifyMessage,
      verifyOk: _verifyOk,
      onVerify: () => unawaited(_verify()),
      onDownloadAudit: () => unawaited(_downloadAudit()),
    );
    return _PageScaffold(
      onRefresh: _reload,
      children: <Widget>[
        header,
        if (_mail) ..._mailBanners(),
        if (_error != null) _LoadError(message: _error!, onRetry: () => unawaited(_reload())),
        if (wide)
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Expanded(flex: 5, child: Column(children: <Widget>[parties, const SizedBox(height: 20), timeline])),
              const SizedBox(width: 20),
              Expanded(flex: 6, child: preview),
            ],
          )
        else ...<Widget>[parties, const SizedBox(height: 20), preview, const SizedBox(height: 20), timeline],
      ],
    );
  }
}
