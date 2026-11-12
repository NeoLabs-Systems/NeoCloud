part of 'main.dart';

/// Flat placeholder rows (or tiles) while a slow folder loads; shown only
/// after a short delay so fast folders never flash it.
Widget _skeletonSliver({required bool grid, required int columns, required double tileExtent}) {
  Widget bar(double width, double height) => Container(
    width: width,
    height: height,
    decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.tag)),
  );
  if (grid) {
    return SliverGrid(
      gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(crossAxisCount: columns, mainAxisSpacing: 12, crossAxisSpacing: 12, mainAxisExtent: tileExtent),
      delegate: SliverChildBuilderDelegate(
        (context, index) => Container(
          padding: const EdgeInsets.all(14),
          decoration: BoxDecoration(color: _bgCard, borderRadius: BorderRadius.circular(AppRadius.card), border: Border.all(color: _border)),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[Expanded(child: bar(double.infinity, double.infinity)), const SizedBox(height: 12), bar(110, 12), const SizedBox(height: 6), bar(60, 10)],
          ),
        ),
        childCount: columns * 2,
      ),
    );
  }
  return DecoratedSliver(
    decoration: BoxDecoration(color: _bgCard, borderRadius: BorderRadius.circular(AppRadius.card), border: Border.all(color: _border)),
    sliver: SliverFixedExtentList(
      itemExtent: _rowExtent,
      delegate: SliverChildBuilderDelegate(
        (context, index) => Container(
          padding: const EdgeInsets.fromLTRB(10, 7, 8, 7),
          decoration: BoxDecoration(border: index == 0 ? null : Border(top: BorderSide(color: _border))),
          child: Row(
            children: <Widget>[
              bar(34, 34),
              const SizedBox(width: 14),
              Expanded(child: Align(alignment: Alignment.centerLeft, child: bar(140.0 + (index * 37) % 120, 12))),
              bar(60, 10),
              const SizedBox(width: 40),
            ],
          ),
        ),
        childCount: 8,
      ),
    ),
  );
}

/// Per-account flags of the Files page kept on this device ("don't show
/// the welcome again", Quick Access folded). The server has no setting for
/// them, so they are keyed by the account id.
String _filesFlagKey(NeoCloudController controller, String name) => 'files.$name.${controller.user?.id ?? 0}';

bool _filesFlag(NeoCloudController controller, String name) => controller._preferences.getBool(_filesFlagKey(controller, name)) ?? false;

Future<void> _setFilesFlag(NeoCloudController controller, String name, bool value) =>
    controller._preferences.setBool(_filesFlagKey(controller, name), value);

/// Closed with ✕: gone until the next start. "Don't show again" keeps it
/// gone for the account on this device.
bool _welcomeClosed = false;

/// First run: an empty Home gets one slim line with the next steps, which
/// the user can close or turn off for good.
class _WelcomeBanner extends StatelessWidget {
  const _WelcomeBanner({required this.controller, required this.onClose});

  final NeoCloudController controller;
  final VoidCallback onClose;

  static bool visibleFor(NeoCloudController controller) => !_welcomeClosed && !_filesFlag(controller, 'welcomeHidden');

  Widget _action(String label, String hint, VoidCallback onPressed) {
    return Tooltip(
      message: hint,
      child: TextButton(
        style: TextButton.styleFrom(visualDensity: VisualDensity.compact, padding: const EdgeInsets.symmetric(horizontal: 10)),
        onPressed: onPressed,
        child: Text(label),
      ),
    );
  }

  List<Widget> _actions() => <Widget>[
    _action(
      appStrings.filesWelcomeDesktopAction,
      appStrings.filesWelcomeDesktopBody,
      () => unawaited(launchUrl(controller.client.uri('/'), webOnlyWindowName: '_blank')),
    ),
    if (!controller.usesDeviceLogin)
      _action(appStrings.filesWelcomeImportAction, appStrings.filesWelcomeImportBody, () {
        controller.setSettingsPage(SettingsPage.imports);
        controller.setSelectedSection(AppSection.settings);
      }),
    if (controller.isAdmin)
      _action(appStrings.filesWelcomeKeyAction, appStrings.filesWelcomeKeyBody, () => controller.setSelectedSection(AppSection.admin)),
  ];

  Widget _dontShowAgain() {
    return TextButton(
      style: TextButton.styleFrom(visualDensity: VisualDensity.compact, foregroundColor: _textMuted),
      onPressed: () {
        unawaited(_setFilesFlag(controller, 'welcomeHidden', true));
        onClose();
      },
      child: Text(appStrings.filesWelcomeDontShowAgain),
    );
  }

  Widget _closeButton() {
    return IconButton(
      tooltip: appStrings.close,
      visualDensity: VisualDensity.compact,
      icon: Icon(Icons.close_rounded, size: 18, color: _textMuted),
      onPressed: () {
        _welcomeClosed = true;
        onClose();
      },
    );
  }

  /// A phone: title and close on top, the line below, then the next steps
  /// and "Don't show again" on rows of their own.
  Widget _narrow() {
    return _AppCard(
      padding: const EdgeInsets.fromLTRB(16, 6, 4, 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              Icon(Icons.waving_hand_outlined, size: 18, color: _textSecondary),
              const SizedBox(width: 10),
              Expanded(
                child: Text(appStrings.filesWelcomeTitle, style: TextStyle(color: _textPrimary, fontSize: 14.5, fontWeight: FontWeight.w600)),
              ),
              _closeButton(),
            ],
          ),
          Padding(
            padding: const EdgeInsets.only(right: 12),
            child: Text(appStrings.filesWelcomeUploadBody, style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.45)),
          ),
          const SizedBox(height: 4),
          Transform.translate(
            // Line the buttons' text up with the body text.
            offset: const Offset(-10, 0),
            child: Wrap(spacing: 4, children: <Widget>[..._actions(), _dontShowAgain()]),
          ),
        ],
      ),
    );
  }

  Widget _wide() {
    return _AppCard(
      padding: const EdgeInsets.fromLTRB(14, 6, 6, 6),
      child: Row(
        children: <Widget>[
          Icon(Icons.waving_hand_outlined, size: 18, color: _textSecondary),
          const SizedBox(width: 10),
          Expanded(
            child: Wrap(
              crossAxisAlignment: WrapCrossAlignment.center,
              spacing: 4,
              children: <Widget>[
                Padding(
                  padding: const EdgeInsets.only(right: 6),
                  child: Text.rich(
                    TextSpan(
                      children: <InlineSpan>[
                        TextSpan(text: '${appStrings.filesWelcomeTitle} · ', style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600)),
                        TextSpan(text: appStrings.filesWelcomeUploadBody),
                      ],
                    ),
                    style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.4),
                  ),
                ),
                ..._actions(),
              ],
            ),
          ),
          _dontShowAgain(),
          _closeButton(),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return _EntranceMotion(
      child: LayoutBuilder(builder: (context, constraints) => constraints.maxWidth < 600 ? _narrow() : _wide()),
    );
  }
}
