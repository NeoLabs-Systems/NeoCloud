part of 'main.dart';

// /app/sign/<token>: the page a signer opens from a link, with no account.
// It is a standalone page (small brand header, no sidebar, no account menu)
// that shows the document with the signer's fields highlighted and asks for
// one field at a time. The logic is PublicSigningController; the states
// (waiting, done, closed, errors) are in main_signing_public_states.dart and
// the inputs in main_signing_public_fields.dart.

/// Draws the document's pages. Tests swap in a page list that doesn't need
/// PDFium.
typedef PublicPagesBuilder = Widget Function(BuildContext context, SigningPageController pages, PageOverlayBuilder overlay);

@visibleForTesting
PublicPagesBuilder? debugPublicPagesBuilder;

const double _signingWideBreakpoint = 900;
const double _signingPanelWidth = 400;

class SigningPublicPage extends StatefulWidget {
  const SigningPublicPage({super.key, required this.controller, required this.flow});

  final NeoCloudController controller;
  final PublicSigningController flow;

  @override
  State<SigningPublicPage> createState() => _SigningPublicPageState();
}

class _SigningPublicPageState extends State<SigningPublicPage> {
  final SigningPageController _pages = SigningPageController();
  String? _scrolledTo;

  PublicSigningController get _flow => widget.flow;

  @override
  void initState() {
    super.initState();
    _flow.addListener(_onFlow);
    _onFlow();
  }

  @override
  void didUpdateWidget(covariant SigningPublicPage old) {
    super.didUpdateWidget(old);
    if (old.flow != widget.flow) {
      old.flow.removeListener(_onFlow);
      _flow.addListener(_onFlow);
      _onFlow();
    }
  }

  @override
  void dispose() {
    _flow.removeListener(_onFlow);
    setUnloadGuard(false);
    _pages.dispose();
    super.dispose();
  }

  void _onFlow() {
    final flow = _flow;
    setUnloadGuard(flow.hasUnsaved);
    final view = flow.view;
    if (view == null) return;
    if (!flow.languageApplied) {
      flow.languageApplied = true;
      final language = AppLanguage.fromCode(view.document.locale);
      if (language != null && language != currentAppLanguage) {
        WidgetsBinding.instance.addPostFrameCallback((_) => widget.controller.useSigningLanguage(language));
      }
    }
    if (flow.state == PublicSigningState.ready) unawaited(flow.ensurePdf());
    _scrollToActive();
  }

  /// Brings the active field into view after a step or a tap on the list.
  void _scrollToActive() {
    final flow = _flow;
    final field = flow.active;
    if (flow.step != SigningStep.fill || field == null || field.areas.isEmpty) return;
    if (_scrolledTo == field.id) return;
    _scrolledTo = field.id;
    final area = field.areas.first;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      unawaited(_pages.scrollToRect(area.page, areaRect(area)));
    });
  }

  Future<void> _confirmLeave() async {
    final leave = await showAdaptiveSheet<bool>(
      context: context,
      builder: (dialogContext) => _AdaptiveDialog(
        title: Text(appStrings.signingPublicLeaveTitle),
        content: SizedBox(width: 400, child: Text(appStrings.signingPublicLeaveBody, style: TextStyle(color: _textSecondary, height: 1.45))),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(appStrings.signingPublicStay)),
          FilledButton(onPressed: () => Navigator.of(dialogContext).pop(true), child: Text(appStrings.signingPublicLeave)),
        ],
      ),
    );
    if (leave == true && mounted) widget.controller.finishSigning();
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: _flow,
      builder: (context, _) {
        final flow = _flow;
        return PopScope(
          canPop: !flow.hasUnsaved,
          onPopInvokedWithResult: (didPop, _) {
            if (!didPop) unawaited(_confirmLeave());
          },
          child: CallbackShortcuts(
            bindings: <ShortcutActivator, VoidCallback>{
              const SingleActivator(LogicalKeyboardKey.arrowRight, alt: true): () {
                if (flow.step == SigningStep.fill) flow.next();
              },
              const SingleActivator(LogicalKeyboardKey.arrowLeft, alt: true): () {
                if (flow.step != SigningStep.intro && flow.canGoBack) flow.back();
              },
            },
            child: _ControlSurfaceBackdrop(
              child: Scaffold(
                backgroundColor: Colors.transparent,
                body: SafeArea(
                  bottom: false,
                  child: Column(
                    children: <Widget>[
                      _SigningHeader(controller: widget.controller, flow: flow),
                      if (flow.isOffline && flow.view != null) _OfflineBar(flow: flow),
                      Expanded(child: _stage(context, flow)),
                    ],
                  ),
                ),
              ),
            ),
          ),
        );
      },
    );
  }

  Widget _stage(BuildContext context, PublicSigningController flow) {
    final view = flow.view;
    if (view == null) {
      if (flow.isLoading) return const _SigningSkeleton();
      return _LoadFailedCard(flow: flow);
    }
    switch (view.state) {
      case PublicSigningState.ready:
        switch (flow.step) {
          case SigningStep.intro:
            return _IntroView(flow: flow, controller: widget.controller, onClose: widget.controller.finishSigning);
          case SigningStep.fill:
            return _FillView(flow: flow, pages: _pages, overlay: _overlay);
          case SigningStep.review:
            return _ReviewView(flow: flow);
        }
      case PublicSigningState.waiting:
        return _WaitingView(flow: flow);
      case PublicSigningState.completed:
        return _CompletedView(flow: flow, controller: widget.controller);
      case PublicSigningState.declined:
      case PublicSigningState.expired:
      case PublicSigningState.canceled:
      case PublicSigningState.locked:
        return _ClosedView(state: view.state);
    }
  }

  // The overlay of one page: the signer's fields as hotspots, the fixed and
  // other parties' values as they will look.

  List<Widget> _overlay(BuildContext context, int pageIndex, Size pageSize) {
    final flow = _flow;
    final view = flow.view;
    if (view == null) return const <Widget>[];
    final color = resolvePartyColor(view.party.color, 0);
    final widgets = <Widget>[];
    for (final field in flow.othersFields) {
      for (final area in field.areas.where((area) => area.page == pageIndex)) {
        widgets.add(_FixedValue(flow: flow, field: field, value: field.value, rect: fractionToPixels(areaRect(area), pageSize)));
      }
    }
    for (final field in flow.fixedFields) {
      final value = field.defaultValue;
      if (value == null) continue;
      for (final area in field.areas.where((area) => area.page == pageIndex)) {
        widgets.add(_FixedValue(flow: flow, field: field, value: SigningValue(fieldId: field.id, value: value), rect: fractionToPixels(areaRect(area), pageSize)));
      }
    }
    for (var index = 0; index < flow.fields.length; index++) {
      final field = flow.fields[index];
      for (final area in field.areas.where((area) => area.page == pageIndex)) {
        widgets.add(
          _FieldHotspot(
            key: ValueKey<String>('hotspot-${field.id}-${area.hashCode}'),
            flow: flow,
            field: field,
            index: index,
            color: color,
            rect: fractionToPixels(areaRect(area), pageSize),
            option: area.option,
          ),
        );
      }
    }
    return widgets;
  }
}

// Header

class _SigningHeader extends StatelessWidget {
  const _SigningHeader({required this.controller, required this.flow});

  final NeoCloudController controller;
  final PublicSigningController flow;

  @override
  Widget build(BuildContext context) {
    final canDecline = flow.state == PublicSigningState.ready && flow.step != SigningStep.intro;
    return Container(
      height: 56,
      padding: const EdgeInsets.symmetric(horizontal: AppSpacing.md),
      decoration: BoxDecoration(color: _bgPrimary, border: Border(bottom: BorderSide(color: _border))),
      child: Row(
        children: <Widget>[
          Expanded(
            child: Align(
              alignment: Alignment.centerLeft,
              child: FittedBox(fit: BoxFit.scaleDown, child: Semantics(header: true, label: appStrings.appName, child: const _BrandLockup(logoSize: 28))),
            ),
          ),
          PopupMenuButton<AppLanguage>(
            tooltip: appStrings.signingPublicLanguage,
            icon: Icon(Icons.language_rounded, color: _textSecondary),
            constraints: const BoxConstraints(minWidth: 160),
            onSelected: controller.useSigningLanguage,
            itemBuilder: (context) => <PopupMenuEntry<AppLanguage>>[
              for (final language in AppLanguage.values)
                CheckedPopupMenuItem<AppLanguage>(value: language, checked: language == currentAppLanguage, child: Text(language.label)),
            ],
          ),
          if (canDecline)
            PopupMenuButton<String>(
              tooltip: appStrings.signingPublicMore,
              icon: Icon(Icons.more_vert_rounded, color: _textSecondary),
              onSelected: (_) => unawaited(_askDecline(context, flow)),
              itemBuilder: (context) => <PopupMenuEntry<String>>[
                PopupMenuItem<String>(value: 'decline', child: Text(appStrings.signingPublicDecline)),
              ],
            ),
        ],
      ),
    );
  }
}

Future<void> _askDecline(BuildContext context, PublicSigningController flow) async {
  final reason = await showAdaptiveSheet<String>(context: context, builder: (dialogContext) => const _DeclineDialog());
  if (reason != null) await flow.decline(reason);
}

/// Asks for an optional reason; pops with it, or with nothing on Cancel.
class _DeclineDialog extends StatefulWidget {
  const _DeclineDialog();

  @override
  State<_DeclineDialog> createState() => _DeclineDialogState();
}

class _DeclineDialogState extends State<_DeclineDialog> {
  final TextEditingController _reason = TextEditingController();

  @override
  void dispose() {
    _reason.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return _AdaptiveDialog(
      title: Text(appStrings.signingPublicDeclineTitle),
      content: SizedBox(
        width: 400,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(appStrings.signingPublicDeclineBody, style: TextStyle(color: _textSecondary, height: 1.45)),
            const SizedBox(height: AppSpacing.md),
            TextField(
              key: const ValueKey<String>('decline-reason'),
              controller: _reason,
              maxLength: 500,
              minLines: 2,
              maxLines: 4,
              decoration: InputDecoration(labelText: appStrings.signingPublicDeclineReason),
            ),
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(
          key: const ValueKey<String>('decline-confirm'),
          onPressed: () => Navigator.of(context).pop(_reason.text),
          style: FilledButton.styleFrom(backgroundColor: _danger, foregroundColor: Colors.white),
          child: Text(appStrings.signingPublicDeclineConfirm),
        ),
      ],
    );
  }
}

/// Shown while writes are waiting for the connection.
class _OfflineBar extends StatelessWidget {
  const _OfflineBar({required this.flow});

  final PublicSigningController flow;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(AppSpacing.md, AppSpacing.xs, AppSpacing.md, 0),
      child: Semantics(
        liveRegion: true,
        child: Row(
          children: <Widget>[
            Expanded(child: _Notice(message: appStrings.signingPublicOfflineBanner, color: _warning, icon: Icons.cloud_off_rounded)),
            const SizedBox(width: AppSpacing.xs),
            TextButton(onPressed: () => unawaited(flow.retryNow()), child: Text(appStrings.signingPublicRetryNow)),
          ],
        ),
      ),
    );
  }
}

class _SigningSkeleton extends StatelessWidget {
  const _SigningSkeleton();

  @override
  Widget build(BuildContext context) {
    Widget bar(double width, double height) => Container(
      width: width,
      height: height,
      decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(6)),
    );
    return Semantics(
      label: appStrings.signingPublicLoadingDocument,
      liveRegion: true,
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 560),
          child: Padding(
            padding: const EdgeInsets.all(AppSpacing.lg),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                bar(220, 22),
                const SizedBox(height: AppSpacing.md),
                bar(double.infinity, 12),
                const SizedBox(height: 8),
                bar(double.infinity, 12),
                const SizedBox(height: 8),
                bar(160, 12),
                const SizedBox(height: AppSpacing.lg),
                bar(double.infinity, 120),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

// The document and the panel

class _FillView extends StatelessWidget {
  const _FillView({required this.flow, required this.pages, required this.overlay});

  final PublicSigningController flow;
  final SigningPageController pages;
  final PageOverlayBuilder overlay;

  @override
  Widget build(BuildContext context) {
    final document = _DocumentPane(flow: flow, pages: pages, overlay: overlay);
    return LayoutBuilder(
      builder: (context, constraints) {
        if (constraints.maxWidth >= _signingWideBreakpoint) {
          return Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Expanded(child: document),
              Container(
                width: _signingPanelWidth,
                decoration: BoxDecoration(color: _bgSecondary, border: Border(left: BorderSide(color: _border))),
                child: SafeArea(top: false, child: _FieldPanel(flow: flow, maxHeight: constraints.maxHeight, fill: true)),
              ),
            ],
          );
        }
        return Column(
          children: <Widget>[
            Expanded(child: document),
            Container(
              decoration: BoxDecoration(
                color: _bgCard,
                border: Border(top: BorderSide(color: _border)),
                boxShadow: <BoxShadow>[BoxShadow(color: _palette.shadow, blurRadius: 18, offset: const Offset(0, -4))],
              ),
              child: SafeArea(top: false, child: _FieldPanel(flow: flow, maxHeight: constraints.maxHeight * 0.62)),
            ),
          ],
        );
      },
    );
  }
}

class _DocumentPane extends StatelessWidget {
  const _DocumentPane({required this.flow, required this.pages, required this.overlay});

  final PublicSigningController flow;
  final SigningPageController pages;
  final PageOverlayBuilder overlay;

  @override
  Widget build(BuildContext context) {
    final builder = debugPublicPagesBuilder;
    if (builder != null) return builder(context, pages, overlay);
    final bytes = flow.pdfBytes;
    if (bytes == null) {
      if (flow.pdfFailed) {
        return Center(
          child: Padding(
            padding: const EdgeInsets.all(AppSpacing.lg),
            child: _LoadError(message: appStrings.signingPdfLoadFailed, onRetry: () => unawaited(flow.ensurePdf())),
          ),
        );
      }
      return Semantics(label: appStrings.signingPublicLoadingDocument, child: const Center(child: CircularProgressIndicator()));
    }
    return SigningPdfPages(
      bytes: bytes,
      controller: pages,
      overlayBuilder: overlay,
      padding: const EdgeInsets.fromLTRB(12, 28, 12, 28),
      maxPageWidth: 900,
    );
  }
}

/// A field of the signer on a page: tap to make it the active one.
class _FieldHotspot extends StatelessWidget {
  const _FieldHotspot({super.key, required this.flow, required this.field, required this.index, required this.color, required this.rect, this.option});

  final PublicSigningController flow;
  final SigningField field;
  final int index;
  final Color color;
  final Rect rect;
  final String? option;

  static const double _minTarget = 44;

  @override
  Widget build(BuildContext context) {
    final spec = FieldTypes.of(field.type);
    final isActive = flow.step == SigningStep.fill && flow.activeIndex == index;
    final value = flow.valueOf(field.id);
    final filled = !spec.isEmpty(value);
    final hit = Rect.fromCenter(
      center: rect.center,
      width: math.max(rect.width, _minTarget),
      height: math.max(rect.height, _minTarget),
    );
    final label = field.name.isEmpty ? spec.label(appStrings) : field.name;
    final state = filled ? appStrings.signingPublicStateDone : appStrings.signingPublicStateOpen;
    return Positioned.fromRect(
      rect: hit,
      child: Semantics(
        button: true,
        selected: isActive,
        label: '$label, $state',
        child: GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTap: () => flow.go(index),
          child: Center(
            child: SizedBox(
              width: rect.width,
              height: rect.height,
              child: FieldBox(
                color: color,
                icon: spec.icon,
                label: isActive ? label : null,
                filled: filled,
                highlighted: isActive,
                hasError: flow.missingIds.contains(field.id),
                showHandles: false,
                child: filled ? _ValueOnPage(flow: flow, field: field, value: value, option: option) : null,
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Another party's value, or a heading: drawn on the page, no box.
class _FixedValue extends StatelessWidget {
  const _FixedValue({required this.flow, required this.field, required this.value, required this.rect});

  final PublicSigningController flow;
  final SigningField field;
  final SigningValue? value;
  final Rect rect;

  @override
  Widget build(BuildContext context) {
    final current = value;
    if (current == null) return const SizedBox.shrink();
    return Positioned.fromRect(
      rect: rect,
      child: ExcludeSemantics(child: _ValueOnPage(flow: flow, field: field, value: current.hasFile ? current : current.value)),
    );
  }
}

/// What a filled field looks like inside its box on the page.
class _ValueOnPage extends StatelessWidget {
  const _ValueOnPage({required this.flow, required this.field, required this.value, this.option});

  final PublicSigningController flow;
  final SigningField field;
  final Object? value;
  final String? option;

  @override
  Widget build(BuildContext context) {
    final spec = FieldTypes.of(field.type);
    final current = value;
    if (current is UploadedValue || (current is SigningValue && current.hasFile)) {
      final isImage = current is UploadedValue ? current.isImage : (spec.needsPad || spec.imageAccept);
      if (isImage) return _RemoteImage(flow: flow, fieldId: field.id, revision: current);
      final name = current is UploadedValue ? current.fileName : (current as SigningValue).fileName;
      return Row(
        children: <Widget>[
          const Icon(Icons.attach_file_rounded, size: 14, color: Colors.black87),
          Expanded(child: Text(name ?? '', maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(color: Colors.black87, fontSize: 11))),
        ],
      );
    }
    final plain = current is SigningValue ? current.value : current;
    if (field.type == FieldType.checkbox) {
      return plain == true ? const FittedBox(child: Icon(Icons.check_rounded, color: Colors.black87)) : const SizedBox.shrink();
    }
    if (field.type == FieldType.radio && option != null && plain != option) return const SizedBox.shrink();
    final text = spec.display(field, plain, locale: currentAppLanguage.code);
    if (text.isEmpty) return const SizedBox.shrink();
    return LayoutBuilder(
      builder: (context, constraints) {
        final size = (constraints.maxHeight * 0.62).clamp(7.0, 22.0).toDouble();
        return Align(
          alignment: field.prefs.align == 'center' ? Alignment.center : (field.prefs.align == 'right' ? Alignment.centerRight : Alignment.centerLeft),
          child: FittedBox(
            fit: BoxFit.scaleDown,
            alignment: Alignment.centerLeft,
            child: Text(text, maxLines: field.prefs.multiline ? null : 1, style: TextStyle(color: Colors.black87, fontSize: size, height: 1.15)),
          ),
        );
      },
    );
  }
}

/// A picture held by a field (a signature, a stamp, an image).
class _RemoteImage extends StatefulWidget {
  const _RemoteImage({required this.flow, required this.fieldId, required this.revision});

  final PublicSigningController flow;
  final String fieldId;

  /// Changes when the picture does.
  final Object? revision;

  @override
  State<_RemoteImage> createState() => _RemoteImageState();
}

class _RemoteImageState extends State<_RemoteImage> {
  late Future<Uint8List> _bytes = widget.flow.imageFor(widget.fieldId);

  @override
  void didUpdateWidget(covariant _RemoteImage old) {
    super.didUpdateWidget(old);
    if (!identical(old.revision, widget.revision) || old.fieldId != widget.fieldId) {
      _bytes = widget.flow.imageFor(widget.fieldId);
    }
  }

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<Uint8List>(
      future: _bytes,
      builder: (context, snapshot) {
        final bytes = snapshot.data;
        if (bytes == null) return const SizedBox.shrink();
        return Image.memory(bytes, fit: BoxFit.contain, gaplessPlayback: true, errorBuilder: (_, _, _) => const SizedBox.shrink());
      },
    );
  }
}
