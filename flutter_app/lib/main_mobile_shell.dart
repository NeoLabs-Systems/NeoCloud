part of 'main.dart';

// The shell on phones (below 640px): NeoAgent's bottom tab bar
// (_MobileTabBar in NeoAgent's main_app_shell.dart) with NeoCloud's tabs,
// in place of the drawer. "More" holds the pages that don't get a tab
// (shared with me, favorites, trash, admin). 640–980px keeps the drawer.

enum _PhoneTab { files, recent, camera, settings, more }

/// The pages behind the "More" tab.
List<AppSection> _phoneMoreSections(NeoCloudController controller) => _mainSections(controller)
    .where((section) => section == AppSection.shared || section == AppSection.favorites || section == AppSection.trash || section == AppSection.admin)
    .toList();

List<_PhoneTab> _phoneTabs(NeoCloudController controller) => <_PhoneTab>[
  _PhoneTab.files,
  _PhoneTab.recent,
  if (controller.cameraUploads.supported) _PhoneTab.camera,
  _PhoneTab.settings,
  if (_phoneMoreSections(controller).isNotEmpty) _PhoneTab.more,
];

_PhoneTab _currentPhoneTab(NeoCloudController controller) {
  switch (controller.selectedSection) {
    case AppSection.recent:
      return _PhoneTab.recent;
    case AppSection.settings:
      return controller.settingsPage == SettingsPage.cameraUpload && controller.cameraUploads.supported ? _PhoneTab.camera : _PhoneTab.settings;
    case AppSection.shared:
    case AppSection.favorites:
    case AppSection.trash:
    case AppSection.admin:
      return _PhoneTab.more;
    case AppSection.files:
      return _PhoneTab.files;
  }
}

void _openPhoneTab(BuildContext context, NeoCloudController controller, _PhoneTab tab) {
  switch (tab) {
    case _PhoneTab.files:
      controller.setSelectedSection(AppSection.files);
    case _PhoneTab.recent:
      controller.setSelectedSection(AppSection.recent);
    case _PhoneTab.camera:
      controller.setSettingsPage(SettingsPage.cameraUpload);
      controller.setSelectedSection(AppSection.settings);
    case _PhoneTab.settings:
      if (controller.settingsPage == SettingsPage.cameraUpload) controller.setSettingsPage(null);
      controller.setSelectedSection(AppSection.settings);
    case _PhoneTab.more:
      _showPhoneMoreSheet(context, controller);
  }
}

extension _PhoneTabInfo on _PhoneTab {
  String get label => switch (this) {
    _PhoneTab.files => appStrings.mobileNavFiles,
    _PhoneTab.recent => appStrings.navRecent,
    _PhoneTab.camera => appStrings.mobileNavCamera,
    _PhoneTab.settings => appStrings.navSettings,
    _PhoneTab.more => appStrings.mobileMore,
  };

  // NeoAgent's tab icons: outlined Material icons, one glyph for both
  // states; the colour carries the selection.
  IconData get icon => switch (this) {
    _PhoneTab.files => Icons.folder_outlined,
    _PhoneTab.recent => Icons.schedule_outlined,
    _PhoneTab.camera => Icons.photo_camera_outlined,
    _PhoneTab.settings => Icons.tune,
    _PhoneTab.more => Icons.more_horiz,
  };
}

/// One tab per phone destination (NeoAgent's _MobileTabBar).
class _MobileTabBar extends StatelessWidget {
  const _MobileTabBar({required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    final tabs = _phoneTabs(controller);
    final selected = _currentPhoneTab(controller);
    return Container(
      decoration: BoxDecoration(
        color: _bgPrimary,
        border: Border(top: BorderSide(color: _border)),
      ),
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(8, 8, 8, 6),
          child: Row(
            children: tabs
                .map(
                  (tab) => Expanded(
                    child: _MobileTab(
                      tab: tab,
                      active: tab == selected,
                      onTap: () => _openPhoneTab(context, controller, tab),
                    ),
                  ),
                )
                .toList(growable: false),
          ),
        ),
      ),
    );
  }
}

class _MobileTab extends StatelessWidget {
  const _MobileTab({required this.tab, required this.active, required this.onTap});

  final _PhoneTab tab;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final color = active ? _accent : _textMuted;
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: BorderRadius.circular(14),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 7),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Icon(tab.icon, size: 21, color: color),
              const SizedBox(height: 5),
              Text(
                tab.label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: GoogleFonts.geist(
                  fontSize: 10.5,
                  fontWeight: FontWeight.w600,
                  color: color,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// The "More" tab: the pages without a tab of their own.
Future<void> _showPhoneMoreSheet(BuildContext context, NeoCloudController controller) {
  return showModalBottomSheet<void>(
    context: context,
    backgroundColor: _bgCard,
    builder: (sheetContext) => SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 12, 12, 8),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            for (final section in _phoneMoreSections(controller))
              _SidebarButton(
                label: section.label,
                icon: section.icon,
                active: section == controller.selectedSection,
                onTap: () {
                  Navigator.of(sheetContext).pop();
                  controller.setSelectedSection(section);
                },
              ),
          ],
        ),
      ),
    ),
  );
}
