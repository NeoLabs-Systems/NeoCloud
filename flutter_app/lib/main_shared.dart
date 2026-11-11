part of 'main.dart';

// Shared building blocks, ported from NeoRouter's main_shared.dart: flat
// surfaces with hairline borders, gold only for primary actions and the
// selection, status as a dot plus a word.

EdgeInsets _pagePadding(BuildContext context) {
  final width = MediaQuery.sizeOf(context).width;
  if (width >= 1280) return const EdgeInsets.fromLTRB(48, 40, 48, 48);
  if (width >= 900) return const EdgeInsets.fromLTRB(32, 32, 32, 40);
  return const EdgeInsets.fromLTRB(16, 16, 16, 32);
}

/// Scrollable page frame: header plus content, centered at a readable width.
class _PageScaffold extends StatelessWidget {
  const _PageScaffold({required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: _pagePadding(context),
      children: <Widget>[
        Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: kContentMaxWidth),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                for (var index = 0; index < children.length; index++) ...<Widget>[
                  if (index > 1) const SizedBox(height: 20),
                  children[index],
                ],
              ],
            ),
          ),
        ),
      ],
    );
  }
}

/// Flat page backdrop for the app shell and the sign-in screen.
class _ControlSurfaceBackdrop extends StatelessWidget {
  const _ControlSurfaceBackdrop({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(decoration: BoxDecoration(color: _bgPrimary), child: child);
  }
}

/// A short fade-in for content that appears after loading.
class _EntranceMotion extends StatefulWidget {
  const _EntranceMotion({required this.child});

  final Widget child;

  @override
  State<_EntranceMotion> createState() => _EntranceMotionState();
}

class _EntranceMotionState extends State<_EntranceMotion> {
  bool _visible = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) setState(() => _visible = true);
    });
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedOpacity(
      duration: const Duration(milliseconds: 220),
      curve: Curves.easeOut,
      opacity: _visible ? 1 : 0,
      child: widget.child,
    );
  }
}

/// Raised card for standalone forms (sign-in) and floating panels.
class _GlassSurface extends StatelessWidget {
  const _GlassSurface({required this.child, this.width, this.padding});

  final Widget child;
  final double? width;
  final EdgeInsetsGeometry? padding;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: width,
      padding: padding,
      decoration: BoxDecoration(
        color: _bgSecondary,
        borderRadius: BorderRadius.circular(AppRadius.panel),
        border: Border.all(color: _border),
        boxShadow: _softPanelShadow,
      ),
      child: child,
    );
  }
}

/// The one card style: surface, hairline border, no shadow.
class _AppCard extends StatelessWidget {
  const _AppCard({required this.child, this.padding = const EdgeInsets.all(20), this.onTap});

  final Widget child;
  final EdgeInsetsGeometry padding;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final radius = BorderRadius.circular(AppRadius.card);
    return Material(
      color: _bgCard,
      shape: RoundedRectangleBorder(borderRadius: radius, side: BorderSide(color: _border)),
      clipBehavior: Clip.antiAlias,
      child: onTap == null
          ? Padding(padding: padding, child: child)
          : InkWell(onTap: onTap, child: Padding(padding: padding, child: child)),
    );
  }
}

/// Asks before something destructive; Cancel left, a red action right.
Future<void> _confirmDelete(
  BuildContext context, {
  required String title,
  required String message,
  required Future<void> Function() onConfirm,
  String? confirmLabel,
}) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: Text(title),
      content: SizedBox(
        width: 400,
        child: Text(message, style: TextStyle(color: _textSecondary, height: 1.45)),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(appStrings.cancel)),
        FilledButton(
          onPressed: () => Navigator.of(dialogContext).pop(true),
          style: FilledButton.styleFrom(backgroundColor: _danger, foregroundColor: Colors.white),
          child: Text(confirmLabel ?? appStrings.delete),
        ),
      ],
    ),
  );
  if (confirmed == true) await onConfirm();
}

/// Page title, one line of description and the page's main actions
/// (NeoRouter's ScreenHeader).
class _PageTitle extends StatelessWidget {
  const _PageTitle({required this.title, required this.subtitle, this.trailing});

  final String title;
  final String subtitle;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final compact = MediaQuery.sizeOf(context).width < 760;
    final heading = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(title, style: _displayTitleStyle(compact ? 24 : 28)),
        if (subtitle.isNotEmpty) ...<Widget>[
          const SizedBox(height: 6),
          ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: Text(subtitle, style: TextStyle(color: _textSecondary, fontSize: 14.5, height: 1.45)),
          ),
        ],
      ],
    );
    return Padding(
      padding: EdgeInsets.only(bottom: compact ? 20 : 28),
      child: compact
          ? Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                heading,
                if (trailing != null) ...<Widget>[const SizedBox(height: 14), trailing!],
              ],
            )
          : Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: <Widget>[
                Expanded(child: heading),
                if (trailing != null) ...<Widget>[const SizedBox(width: 16), trailing!],
              ],
            ),
    );
  }
}

/// Icon, title and message in the middle of an empty area.
class _EmptyState extends StatelessWidget {
  const _EmptyState({required this.title, required this.subtitle, this.icon = Icons.folder_open_outlined, this.action});

  final IconData icon;
  final String title;
  final String subtitle;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 48, horizontal: 16),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 400),
          child: Column(
            children: <Widget>[
              Container(
                width: 48,
                height: 48,
                decoration: BoxDecoration(color: _bgTertiary, shape: BoxShape.circle),
                child: Icon(icon, size: 22, color: _textSecondary),
              ),
              const SizedBox(height: 16),
              Text(title, textAlign: TextAlign.center, style: _displayTitleStyle(18)),
              const SizedBox(height: 6),
              Text(subtitle, textAlign: TextAlign.center, style: TextStyle(color: _textSecondary, fontSize: 14, height: 1.45)),
              if (action != null) ...<Widget>[const SizedBox(height: 18), action!],
            ],
          ),
        ),
      ),
    );
  }
}

/// An empty state on a card, for lists that have nothing yet.
class _EmptyCard extends StatelessWidget {
  const _EmptyCard({required this.title, required this.subtitle, this.icon = Icons.folder_open_outlined, this.action});

  final IconData icon;
  final String title;
  final String subtitle;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    return _EntranceMotion(
      child: _AppCard(
        padding: EdgeInsets.zero,
        child: _EmptyState(title: title, subtitle: subtitle, icon: icon, action: action),
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle(this.text);

  final String text;

  @override
  Widget build(BuildContext context) => Text(text, style: _sectionTitleStyle());
}

class _SidebarButton extends StatefulWidget {
  const _SidebarButton({
    required this.label,
    required this.icon,
    required this.onTap,
    this.active = false,
    this.compact = false,
    this.trailing,
  });

  final String label;
  final IconData icon;
  final VoidCallback? onTap;
  final bool active;
  final bool compact;
  final Widget? trailing;

  @override
  State<_SidebarButton> createState() => _SidebarButtonState();
}

class _SidebarButtonState extends State<_SidebarButton> {
  bool _hovering = false;

  @override
  Widget build(BuildContext context) {
    final active = widget.active;
    final radius = BorderRadius.circular(widget.compact ? 12 : 14);
    // Flat finish: the active row is one accent wash plus a gold glyph, no
    // gradient, shadow or rail bar.
    return Padding(
      padding: const EdgeInsets.only(bottom: 2),
      child: MouseRegion(
        onEnter: (_) => setState(() => _hovering = true),
        onExit: (_) => setState(() => _hovering = false),
        child: Material(
          color: Colors.transparent,
          child: InkWell(
            borderRadius: radius,
            onTap: widget.onTap,
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 160),
              curve: Curves.easeOutCubic,
              width: double.infinity,
              padding: EdgeInsets.symmetric(horizontal: widget.compact ? 10 : 11, vertical: widget.compact ? 7 : 9),
              decoration: BoxDecoration(
                color: active ? _accentMuted : (_hovering ? _bgTertiary.withValues(alpha: 0.66) : Colors.transparent),
                borderRadius: radius,
              ),
              child: Row(
                children: <Widget>[
                  Icon(widget.icon, size: widget.compact ? 16 : 18, color: active ? _accent : _textMuted),
                  SizedBox(width: widget.compact ? 9 : 11),
                  Expanded(
                    child: Text(
                      widget.label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: GoogleFonts.geist(
                        fontSize: 13,
                        fontWeight: FontWeight.w600,
                        letterSpacing: -0.1,
                        color: active ? _textPrimary : _textSecondary,
                      ),
                    ),
                  ),
                  if (widget.trailing != null) ...<Widget>[const SizedBox(width: 8), widget.trailing!],
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Indented sub-navigation of an expanded group, behind a thin guide line.
class _SidebarSubnav extends StatelessWidget {
  const _SidebarSubnav({required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.fromLTRB(14, 2, 0, 2),
      padding: const EdgeInsets.only(left: 12),
      decoration: BoxDecoration(border: Border(left: BorderSide(color: _border))),
      child: Column(children: children),
    );
  }
}

/// Dot plus a short status word, e.g. "● Working". The color never fills.
class _StatusPill extends StatelessWidget {
  const _StatusPill({required this.label, required this.color});

  final String label;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Container(width: 7, height: 7, decoration: BoxDecoration(color: color, shape: BoxShape.circle)),
        const SizedBox(width: 7),
        Text(label, style: TextStyle(color: _textSecondary, fontSize: 13)),
      ],
    );
  }
}

class _Notice extends StatelessWidget {
  const _Notice({required this.message, required this.color, required this.icon, this.onDismiss});

  final String message;
  final Color color;
  final IconData icon;
  final VoidCallback? onDismiss;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: EdgeInsets.fromLTRB(14, 11, onDismiss == null ? 14 : 4, 11),
      decoration: BoxDecoration(color: color.withValues(alpha: 0.08), borderRadius: BorderRadius.circular(AppRadius.control)),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Padding(padding: const EdgeInsets.only(top: 1), child: Icon(icon, color: color, size: 17)),
          const SizedBox(width: 10),
          Expanded(child: Text(message, style: TextStyle(color: _textPrimary, fontSize: 13, height: 1.4))),
          if (onDismiss != null)
            InkWell(
              onTap: onDismiss,
              borderRadius: BorderRadius.circular(8),
              child: Padding(padding: const EdgeInsets.all(2), child: Icon(Icons.close_rounded, size: 16, color: _textMuted)),
            ),
        ],
      ),
    );
  }
}

class _InlineError extends StatelessWidget {
  const _InlineError({required this.message, this.onDismiss});

  final String message;
  final VoidCallback? onDismiss;

  @override
  Widget build(BuildContext context) =>
      _Notice(message: message, color: _danger, icon: Icons.error_outline_rounded, onDismiss: onDismiss);
}

class _InlineSuccess extends StatelessWidget {
  const _InlineSuccess({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) => _Notice(message: message, color: _success, icon: Icons.check_circle_outline_rounded);
}

/// Neutral hint box.
class _InlineNote extends StatelessWidget {
  const _InlineNote({required this.message, this.icon = Icons.info_outline_rounded, this.color});

  final String message;
  final IconData icon;
  final Color? color;

  @override
  Widget build(BuildContext context) => _Notice(message: message, color: color ?? _textSecondary, icon: icon);
}

/// A failed load, with a way to try again.
class _LoadError extends StatelessWidget {
  const _LoadError({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        _InlineError(message: message),
        const SizedBox(height: AppSpacing.sm),
        OutlinedButton.icon(onPressed: onRetry, icon: const Icon(Icons.refresh, size: 16), label: Text(appStrings.retry)),
      ],
    );
  }
}

class _LoadingPlaceholder extends StatelessWidget {
  const _LoadingPlaceholder();

  @override
  Widget build(BuildContext context) {
    return const Padding(
      padding: EdgeInsets.all(48),
      child: Center(child: SizedBox.square(dimension: 22, child: CircularProgressIndicator(strokeWidth: 2))),
    );
  }
}

/// What an empty list says instead of rows.
class _EmptyText extends StatelessWidget {
  const _EmptyText(this.text);

  final String text;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Text(text, style: TextStyle(fontSize: 13, color: _textMuted)),
    );
  }
}

/// A search box with a clear button once something is typed.
class _SearchField extends StatelessWidget {
  const _SearchField({required this.controller, required this.hintText, required this.onChanged, required this.onClear});

  final TextEditingController controller;
  final String hintText;
  final ValueChanged<String> onChanged;
  final VoidCallback onClear;

  @override
  Widget build(BuildContext context) {
    return TextField(
      controller: controller,
      onChanged: onChanged,
      textInputAction: TextInputAction.search,
      decoration: InputDecoration(
        hintText: hintText,
        prefixIcon: Icon(Icons.search, size: 18, color: _textMuted),
        suffixIcon: controller.text.isEmpty
            ? null
            : IconButton(tooltip: appStrings.clear, onPressed: onClear, icon: Icon(Icons.close, size: 18, color: _textMuted)),
      ),
    );
  }
}

/// Bordered panel for a content block; the title sits above the card.
class _SectionCard extends StatelessWidget {
  const _SectionCard({required this.title, required this.child, this.trailing});

  final String title;
  final Widget? trailing;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.fromLTRB(2, 0, 0, 10),
          child: Row(
            children: <Widget>[
              Expanded(child: _SectionTitle(title)),
              if (trailing != null) trailing!,
            ],
          ),
        ),
        _AppCard(child: child),
      ],
    );
  }
}

/// A text-field dialog with Cancel and a primary action. Resolves to the
/// entered text, or null.
Future<String?> _promptText(
  BuildContext context, {
  required String title,
  required String label,
  required String confirmLabel,
  String initialValue = '',
  bool obscure = false,
  bool selectStem = false,
}) {
  final controller = TextEditingController(text: initialValue);
  final dot = initialValue.lastIndexOf('.');
  controller.selection = TextSelection(baseOffset: 0, extentOffset: selectStem && dot > 0 ? dot : initialValue.length);
  return showDialog<String>(
    context: context,
    builder: (dialogContext) {
      void submit() {
        final value = controller.text.trim();
        if (value.isNotEmpty || obscure) Navigator.of(dialogContext).pop(obscure ? controller.text : value);
      }

      return AlertDialog(
        title: Text(title),
        content: SizedBox(
          width: 420,
          child: TextField(
            controller: controller,
            autofocus: true,
            obscureText: obscure,
            decoration: InputDecoration(labelText: label),
            onSubmitted: (_) => submit(),
          ),
        ),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.cancel)),
          FilledButton(onPressed: submit, child: Text(confirmLabel)),
        ],
      );
    },
  ).whenComplete(controller.dispose);
}

/// The NeoCloud logo: NeoRouter's orbital mark, turned so it reads as its own.
class NeoCloudMark extends StatelessWidget {
  const NeoCloudMark({super.key, required this.size});

  final double size;

  @override
  Widget build(BuildContext context) {
    return ClipRRect(
      borderRadius: BorderRadius.circular(size * 0.24),
      child: SvgPicture.asset('assets/branding/logo.svg', width: size, height: size),
    );
  }
}

/// Logo and, optionally, the product name (NeoRouter's BrandLockup).
class _BrandLockup extends StatelessWidget {
  const _BrandLockup({this.logoSize = 48, this.showName = true});

  final double logoSize;
  final bool showName;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        NeoCloudMark(size: logoSize),
        if (showName) ...<Widget>[
          const SizedBox(width: 10),
          Text(
            appStrings.appName,
            style: TextStyle(color: _textPrimary, fontSize: 15.5, fontWeight: FontWeight.w700, letterSpacing: -0.3),
          ),
        ],
      ],
    );
  }
}

/// In-app boot screen while the session is restored. Mirrors the HTML
/// splash in web/index.html so the hand-over to Flutter is seamless.
class SplashView extends StatefulWidget {
  const SplashView({super.key});

  @override
  State<SplashView> createState() => _SplashViewState();
}

class _SplashViewState extends State<SplashView> with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(vsync: this, duration: const Duration(milliseconds: 2400))..repeat();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    return Scaffold(
      body: DecoratedBox(
        decoration: BoxDecoration(
          gradient: RadialGradient(
            center: const Alignment(0, -0.24),
            radius: 1.2,
            colors: dark
                ? const <Color>[Color(0xFF131B16), Color(0xFF0A0F0C), Color(0xFF0E1511)]
                : const <Color>[Color(0xFFFDFCF8), Color(0xFFEDE9DC), Color(0xFFF4F1E8)],
            stops: const <double>[0, 0.7, 1],
          ),
        ),
        child: Center(
          child: AnimatedBuilder(
            animation: _controller,
            builder: (context, _) {
              final phase = _controller.value;
              return Column(
                mainAxisSize: MainAxisSize.min,
                children: <Widget>[
                  Transform.scale(
                    scale: 1 + 0.045 * math.sin(phase * 2 * math.pi).abs(),
                    child: DecoratedBox(
                      decoration: BoxDecoration(
                        borderRadius: BorderRadius.circular(20),
                        boxShadow: <BoxShadow>[
                          BoxShadow(color: Colors.black.withValues(alpha: dark ? 0.35 : 0.18), blurRadius: 18, offset: const Offset(0, 6)),
                        ],
                      ),
                      child: SvgPicture.asset('assets/branding/logo.svg', width: 88, height: 88),
                    ),
                  ),
                  const SizedBox(height: 20),
                  Text(
                    'NEOCLOUD',
                    style: TextStyle(color: _textSecondary, fontSize: 15, fontWeight: FontWeight.w600, letterSpacing: 4.2),
                  ),
                  const SizedBox(height: 20),
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: <Widget>[
                      for (var index = 0; index < 3; index++)
                        Builder(builder: (context) {
                          final local = ((phase * 2 - index * 0.125) % 1 + 1) % 1;
                          final lift = local < 0.4 ? math.sin(local / 0.4 * math.pi) : 0.0;
                          return Padding(
                            padding: const EdgeInsets.symmetric(horizontal: 3),
                            child: Transform.translate(
                              offset: Offset(0, -3 * lift),
                              child: Container(
                                width: 5,
                                height: 5,
                                decoration: BoxDecoration(color: _accent.withValues(alpha: 0.35 + 0.65 * lift), shape: BoxShape.circle),
                              ),
                            ),
                          );
                        }),
                    ],
                  ),
                ],
              );
            },
          ),
        ),
      ),
    );
  }
}
