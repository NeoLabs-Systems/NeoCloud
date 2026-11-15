part of 'main.dart';

// The other screens of the signer's page: consent, review, and the states a
// link can be in (waiting, done, declined, expired, canceled, not valid).

/// A sentence for a failed action, in words for the signer.
String _signingActionError(SigningException error) {
  final strings = appStrings;
  switch (error.kind) {
    case SigningErrorKind.network:
      return strings.signingPublicFinishOffline;
    case SigningErrorKind.rateLimited:
      return strings.signingPublicRateBody;
    case SigningErrorKind.conflict:
      return strings.signingPublicChanged;
    case SigningErrorKind.invalid:
      return error.fieldIds.isEmpty && error.message.isNotEmpty ? error.message : strings.signingPublicFinishFailed;
    case SigningErrorKind.notFound:
    case SigningErrorKind.expired:
    case SigningErrorKind.unauthorized:
    case SigningErrorKind.server:
      return strings.signingPublicFinishFailed;
  }
}

/// A centred card with an icon, a title, a sentence and actions.
class _SigningStateCard extends StatelessWidget {
  const _SigningStateCard({required this.icon, required this.title, this.body, this.actions = const <Widget>[], this.extra});

  final IconData icon;
  final String title;
  final String? body;
  final List<Widget> actions;
  final Widget? extra;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(AppSpacing.md),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 480),
          child: _EntranceMotion(
            child: _AppCard(
              padding: const EdgeInsets.all(AppSpacing.lg),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  Icon(icon, size: 34, color: _textSecondary),
                  const SizedBox(height: AppSpacing.sm),
                  Semantics(
                    header: true,
                    liveRegion: true,
                    child: Text(title, style: _displayTitleStyle(22)),
                  ),
                  if (body != null) ...<Widget>[
                    const SizedBox(height: AppSpacing.xs),
                    Text(body!, style: TextStyle(color: _textSecondary, height: 1.45)),
                  ],
                  if (extra != null) ...<Widget>[const SizedBox(height: AppSpacing.md), extra!],
                  if (actions.isNotEmpty) ...<Widget>[
                    const SizedBox(height: AppSpacing.lg),
                    for (final action in actions) ...<Widget>[action, const SizedBox(height: AppSpacing.xs)],
                  ],
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

// A link that can't be opened

class _LoadFailedCard extends StatelessWidget {
  const _LoadFailedCard({required this.flow});

  final PublicSigningController flow;

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    final error = flow.loadError;
    final kind = error?.kind ?? SigningErrorKind.server;
    final retry = FilledButton(
      key: const ValueKey<String>('load-retry'),
      onPressed: () => unawaited(flow.load()),
      style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
      child: Text(strings.retry),
    );
    switch (kind) {
      case SigningErrorKind.notFound:
      case SigningErrorKind.unauthorized:
        return _SigningStateCard(icon: Icons.link_off_rounded, title: strings.signingPublicInvalidTitle, body: strings.signingPublicInvalidBody);
      case SigningErrorKind.expired:
        return _ClosedView(state: PublicSigningState.expired);
      case SigningErrorKind.rateLimited:
        return _SigningStateCard(icon: Icons.hourglass_empty_rounded, title: strings.signingPublicRateTitle, body: strings.signingPublicRateBody, actions: <Widget>[retry]);
      case SigningErrorKind.network:
        return _SigningStateCard(icon: Icons.cloud_off_rounded, title: strings.signingPublicOfflineTitle, body: strings.signingPublicOfflineBody, actions: <Widget>[retry]);
      case SigningErrorKind.conflict:
      case SigningErrorKind.invalid:
      case SigningErrorKind.server:
        return _SigningStateCard(icon: Icons.error_outline_rounded, title: strings.signingPublicServerTitle, body: strings.signingPublicServerBody, actions: <Widget>[retry]);
    }
  }
}

class _ClosedView extends StatelessWidget {
  const _ClosedView({required this.state});

  final PublicSigningState state;

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    switch (state) {
      case PublicSigningState.declined:
        return _SigningStateCard(icon: Icons.block_rounded, title: strings.signingPublicDeclinedTitle, body: strings.signingPublicDeclinedBody);
      case PublicSigningState.expired:
        return _SigningStateCard(icon: Icons.timer_off_outlined, title: strings.signingPublicExpiredTitle, body: strings.signingPublicExpiredBody);
      case PublicSigningState.canceled:
        return _SigningStateCard(icon: Icons.cancel_outlined, title: strings.signingPublicCanceledTitle, body: strings.signingPublicCanceledBody);
      case PublicSigningState.ready:
      case PublicSigningState.waiting:
      case PublicSigningState.completed:
      case PublicSigningState.locked:
        return _SigningStateCard(icon: Icons.lock_outline_rounded, title: strings.signingPublicLockedTitle, body: strings.signingPublicLockedBody);
    }
  }
}

class _WaitingView extends StatelessWidget {
  const _WaitingView({required this.flow});

  final PublicSigningController flow;

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    final count = flow.waitingCount;
    return _SigningStateCard(
      icon: Icons.hourglass_top_rounded,
      title: strings.signingPublicWaitingTitle,
      body: count <= 0 ? strings.signingPublicLockedBody : strings.signingPublicWaitingBody(count),
      actions: <Widget>[
        OutlinedButton(
          key: const ValueKey<String>('waiting-check'),
          onPressed: () => unawaited(flow.load(quiet: true)),
          style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(48)),
          child: Text(strings.signingPublicCheckNow),
        ),
      ],
    );
  }
}

// Consent

class _IntroView extends StatefulWidget {
  const _IntroView({required this.flow, required this.onClose});

  final PublicSigningController flow;
  final VoidCallback onClose;

  @override
  State<_IntroView> createState() => _IntroViewState();
}

class _IntroViewState extends State<_IntroView> {
  late final TextEditingController _name = TextEditingController(text: widget.flow.signerName);
  bool _agreed = false;
  bool _tried = false;

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  Future<void> _continue() async {
    setState(() => _tried = true);
    if (!_agreed || _name.text.trim().isEmpty) return;
    final now = DateTime.now();
    final zone = RegExp(r'^[A-Za-z0-9_+\-/]{1,64}$').hasMatch(now.timeZoneName) ? now.timeZoneName : null;
    await widget.flow.consent(_name.text, timezone: zone, tzOffsetMinutes: now.timeZoneOffset.inMinutes);
  }

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    final flow = widget.flow;
    final view = flow.view!;
    final info = view.document;
    final expires = info.expiresAt;
    final count = flow.fieldCount;
    final nameMissing = _tried && _name.text.trim().isEmpty;
    final agreeMissing = _tried && !_agreed;
    final error = flow.actionError;
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(AppSpacing.md),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 560),
          child: _EntranceMotion(
            child: _AppCard(
              padding: const EdgeInsets.all(AppSpacing.lg),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  Semantics(header: true, child: Text(strings.signingPublicIntroTitle(info.title), style: _displayTitleStyle(22))),
                  const SizedBox(height: AppSpacing.xs),
                  Text(strings.signingPublicIntroLead, style: TextStyle(color: _textSecondary, height: 1.45)),
                  const SizedBox(height: AppSpacing.sm),
                  Wrap(
                    spacing: AppSpacing.md,
                    runSpacing: 4,
                    children: <Widget>[
                      if (count > 0) _IntroFact(icon: Icons.edit_note_rounded, text: strings.signingPublicFieldCount(count)),
                      if (expires != null) _IntroFact(icon: Icons.schedule_rounded, text: strings.signingPublicExpires(MaterialLocalizations.of(context).formatMediumDate(expires.toLocal()))),
                    ],
                  ),
                  const SizedBox(height: AppSpacing.lg),
                  Text(strings.signingPublicConsentTitle, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w700, fontSize: 15)),
                  const SizedBox(height: 6),
                  Container(
                    key: const ValueKey<String>('consent-text'),
                    padding: const EdgeInsets.all(AppSpacing.sm),
                    decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.control)),
                    child: Text(info.consentText, style: TextStyle(color: _textSecondary, fontSize: 13.5, height: 1.5)),
                  ),
                  const SizedBox(height: AppSpacing.sm),
                  _ChoiceRow(
                    key: const ValueKey<String>('consent-check'),
                    label: strings.signingPublicConsentCheck,
                    selected: _agreed,
                    multiple: true,
                    onTap: () => setState(() => _agreed = !_agreed),
                  ),
                  if (agreeMissing)
                    Padding(
                      padding: const EdgeInsets.only(top: 6),
                      child: Text(strings.signingPublicConsentRequired, style: TextStyle(color: _danger, fontSize: 13)),
                    ),
                  const SizedBox(height: AppSpacing.md),
                  TextField(
                    key: const ValueKey<String>('consent-name'),
                    controller: _name,
                    maxLength: 120,
                    autofillHints: const <String>[AutofillHints.name],
                    textCapitalization: TextCapitalization.words,
                    textInputAction: TextInputAction.done,
                    onChanged: (_) => setState(() {}),
                    onSubmitted: (_) => unawaited(_continue()),
                    decoration: InputDecoration(
                      labelText: strings.signingPublicNameLabel,
                      counterText: '',
                      errorText: nameMissing ? strings.signingPublicNameRequired : null,
                    ),
                  ),
                  if (error != null) ...<Widget>[
                    const SizedBox(height: AppSpacing.sm),
                    _InlineError(message: _signingActionError(error), onDismiss: flow.dismissActionError),
                  ],
                  const SizedBox(height: AppSpacing.md),
                  FilledButton(
                    key: const ValueKey<String>('consent-continue'),
                    onPressed: flow.isConsenting ? null : () => unawaited(_continue()),
                    style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(50)),
                    child: flow.isConsenting
                        ? const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2))
                        : Text(strings.continueLabel),
                  ),
                  const SizedBox(height: AppSpacing.xs),
                  TextButton(
                    key: const ValueKey<String>('intro-decline'),
                    onPressed: () => unawaited(_askDecline(context, flow)),
                    style: TextButton.styleFrom(minimumSize: const Size.fromHeight(44)),
                    child: Text(strings.signingPublicDecline),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _IntroFact extends StatelessWidget {
  const _IntroFact({required this.icon, required this.text});

  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    if (text.isEmpty) return const SizedBox.shrink();
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Icon(icon, size: 16, color: _textMuted),
        const SizedBox(width: 6),
        Flexible(child: Text(text, style: TextStyle(color: _textSecondary, fontSize: 13))),
      ],
    );
  }
}

// Review

class _ReviewView extends StatelessWidget {
  const _ReviewView({required this.flow});

  final PublicSigningController flow;

  String _summary(SigningField field) {
    final strings = appStrings;
    final spec = FieldTypes.of(field.type);
    final value = flow.valueOf(field.id);
    if (spec.isEmpty(value)) return strings.signingPublicStateEmpty;
    if (value is UploadedValue) return value.isImage ? strings.signingPublicUploaded : (value.fileName ?? strings.signingPublicUploaded);
    final text = spec.display(field, value, locale: currentAppLanguage.code);
    return text.isEmpty ? strings.signingPublicUploaded : text;
  }

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    final problems = flow.problems;
    final showMissing = flow.missingIds.isNotEmpty || problems.isNotEmpty;
    final error = flow.actionError;
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(AppSpacing.md),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 640),
          child: _AppCard(
            padding: const EdgeInsets.all(AppSpacing.lg),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                Semantics(header: true, liveRegion: true, child: Text(strings.signingPublicReviewTitle, style: _displayTitleStyle(22))),
                const SizedBox(height: AppSpacing.xs),
                Text(flow.fields.isEmpty ? strings.signingPublicNoFields : strings.signingPublicReviewLead, style: TextStyle(color: _textSecondary, height: 1.45)),
                if (showMissing && flow.missingIds.isNotEmpty) ...<Widget>[
                  const SizedBox(height: AppSpacing.sm),
                  Semantics(
                    liveRegion: true,
                    child: KeyedSubtree(key: const ValueKey<String>('review-missing'), child: _InlineError(message: strings.signingPublicMissing(flow.missingIds.length))),
                  ),
                ],
                if (error != null && error.fieldIds.isEmpty) ...<Widget>[
                  const SizedBox(height: AppSpacing.sm),
                  _InlineError(message: _signingActionError(error), onDismiss: flow.dismissActionError),
                ],
                const SizedBox(height: AppSpacing.md),
                for (var index = 0; index < flow.fields.length; index++) _ReviewRow(flow: flow, index: index, summary: _summary(flow.fields[index])),
                const SizedBox(height: AppSpacing.md),
                Row(
                  children: <Widget>[
                    if (flow.fields.isNotEmpty)
                      OutlinedButton(
                        key: const ValueKey<String>('review-back'),
                        onPressed: flow.backToFields,
                        style: OutlinedButton.styleFrom(minimumSize: const Size(96, 50)),
                        child: Text(strings.signingPublicBack),
                      ),
                    if (flow.fields.isNotEmpty) const SizedBox(width: AppSpacing.xs),
                    Expanded(
                      child: FilledButton(
                        key: const ValueKey<String>('review-finish'),
                        onPressed: flow.isFinishing ? null : () => unawaited(flow.finish()),
                        style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(50)),
                        child: Text(flow.isFinishing ? strings.signingPublicFinishing : strings.signingPublicFinish),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: AppSpacing.xs),
                _PublicSaveStatus(flow: flow),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _ReviewRow extends StatelessWidget {
  const _ReviewRow({required this.flow, required this.index, required this.summary});

  final PublicSigningController flow;
  final int index;
  final String summary;

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    final field = flow.fields[index];
    final spec = FieldTypes.of(field.type);
    final value = flow.valueOf(field.id);
    final empty = spec.isEmpty(value);
    final wrong = flow.issueFor(field) != null;
    final title = field.name.isEmpty ? spec.label(strings) : field.name;
    final Color dot = wrong ? _danger : (empty ? _textMuted : _success);
    return Semantics(
      button: true,
      label: '$title: $summary',
      excludeSemantics: true,
      onTap: () => flow.go(index),
      child: InkWell(
        key: ValueKey<String>('review-row-${field.id}'),
        onTap: () => flow.go(index),
        borderRadius: BorderRadius.circular(AppRadius.control),
        child: Container(
          constraints: const BoxConstraints(minHeight: 56),
          padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 8),
          decoration: BoxDecoration(border: Border(top: BorderSide(color: _borderLight))),
          child: Row(
            children: <Widget>[
              Container(width: 8, height: 8, decoration: BoxDecoration(color: dot, shape: BoxShape.circle)),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(title, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
                    const SizedBox(height: 2),
                    if (value is UploadedValue && value.isImage)
                      Align(
                        alignment: Alignment.centerLeft,
                        child: _PaperTile(height: 46, child: _RemoteImage(flow: flow, fieldId: field.id, revision: value)),
                      )
                    else
                      Text(
                        summary,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(color: empty ? _textMuted : _textPrimary, fontStyle: empty ? FontStyle.italic : FontStyle.normal),
                      ),
                  ],
                ),
              ),
              const SizedBox(width: 8),
              Text(strings.signingPublicEdit, style: TextStyle(color: _accent, fontSize: 13, fontWeight: FontWeight.w600)),
            ],
          ),
        ),
      ),
    );
  }
}

// Done

class _CompletedView extends StatefulWidget {
  const _CompletedView({required this.flow, required this.controller});

  final PublicSigningController flow;
  final NeoCloudController controller;

  @override
  State<_CompletedView> createState() => _CompletedViewState();
}

class _CompletedViewState extends State<_CompletedView> {
  bool _downloading = false;
  bool _failed = false;
  bool _closed = false;

  String _fileName() {
    final title = widget.flow.view?.document.title ?? '';
    final clean = title.replaceAll(RegExp(r'[\\/:*?"<>|\u0000-\u001f]'), ' ').trim();
    return '${clean.isEmpty ? 'document' : clean}.pdf';
  }

  Future<void> _download({bool certificate = true}) async {
    if (_downloading) return;
    setState(() {
      _downloading = true;
      _failed = false;
    });
    try {
      final bytes = await widget.flow.downloadFinal(certificate: certificate);
      await saveBytes(bytes, _fileName(), mime: 'application/pdf');
    } catch (_) {
      if (mounted) setState(() => _failed = true);
    } finally {
      if (mounted) setState(() => _downloading = false);
    }
  }

  void _close() {
    if (kIsWeb) {
      setState(() => _closed = true);
    } else {
      widget.controller.finishSigning();
    }
  }

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    final flow = widget.flow;
    if (_closed) {
      return _SigningStateCard(icon: Icons.check_circle_outline_rounded, title: strings.signingPublicPartDoneTitle, body: strings.signingPublicClosedNote);
    }
    final others = flow.othersPending;
    final ready = flow.finalReady;
    final String title;
    final String body;
    if (others) {
      title = strings.signingPublicPartDoneTitle;
      body = strings.signingPublicPartDoneBody(flow.progress.done, flow.progress.total);
    } else {
      title = strings.signingPublicAllDoneTitle;
      body = ready ? strings.signingPublicAllDoneBody : strings.signingPublicPreparing;
    }
    return _SigningStateCard(
      icon: Icons.check_circle_outline_rounded,
      title: title,
      body: body,
      extra: _failed
          ? _InlineError(message: strings.signingPublicDownloadFailed)
          : (!others && !ready ? const LinearProgressIndicator(minHeight: 3) : null),
      actions: <Widget>[
        if (ready) ...<Widget>[
          FilledButton.icon(
            key: const ValueKey<String>('download-final'),
            onPressed: _downloading ? null : () => unawaited(_download()),
            style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(50)),
            icon: _downloading ? const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.download_rounded),
            label: Text(strings.signingPublicDownload),
          ),
          OutlinedButton(
            key: const ValueKey<String>('download-final-clean'),
            onPressed: _downloading ? null : () => unawaited(_download(certificate: false)),
            style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(48)),
            child: Text(strings.signingDownloadWithoutCertificate),
          ),
        ],
        OutlinedButton(
          key: const ValueKey<String>('completed-close'),
          onPressed: _close,
          style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(48)),
          child: Text(strings.close),
        ),
      ],
    );
  }
}
