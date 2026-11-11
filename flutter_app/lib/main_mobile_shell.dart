part of 'main.dart';

// The shell on phones (below 640px): a flat bottom navigation bar with the
// gold selection instead of the drawer, a "More" menu in the app bar for
// the pages that don't fit (favorites, trash, admin) and the camera-backup
// chip. 640–980px keeps the drawer.

enum _PhoneTab { files, recent, camera, settings }

List<_PhoneTab> _phoneTabs(NeoCloudController controller) => <_PhoneTab>[
  _PhoneTab.files,
  _PhoneTab.recent,
  if (controller.cameraUploads.supported) _PhoneTab.camera,
  _PhoneTab.settings,
];

_PhoneTab _currentPhoneTab(NeoCloudController controller) {
  switch (controller.selectedSection) {
    case AppSection.recent:
      return _PhoneTab.recent;
    case AppSection.settings:
      return controller.settingsPage == SettingsPage.cameraUpload && controller.cameraUploads.supported ? _PhoneTab.camera : _PhoneTab.settings;
    default:
      return _PhoneTab.files;
  }
}

void _openPhoneTab(NeoCloudController controller, _PhoneTab tab) {
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
  }
}

class _PhoneNavigationBar extends StatelessWidget {
  const _PhoneNavigationBar({required this.controller});

  final NeoCloudController controller;

  (String, IconData) _info(_PhoneTab tab) => switch (tab) {
    _PhoneTab.files => (appStrings.mobileNavFiles, Icons.folder_outlined),
    _PhoneTab.recent => (appStrings.navRecent, Icons.schedule_rounded),
    _PhoneTab.camera => (appStrings.mobileNavCamera, Icons.photo_camera_outlined),
    _PhoneTab.settings => (appStrings.navSettings, Icons.settings_outlined),
  };

  @override
  Widget build(BuildContext context) {
    final tabs = _phoneTabs(controller);
    final current = tabs.indexOf(_currentPhoneTab(controller));
    return DecoratedBox(
      decoration: BoxDecoration(color: _bgSecondary, border: Border(top: BorderSide(color: _border))),
      child: NavigationBarTheme(
        data: NavigationBarThemeData(
          labelTextStyle: WidgetStateProperty.resolveWith(
            (states) => TextStyle(
              fontSize: 12,
              fontWeight: states.contains(WidgetState.selected) ? FontWeight.w600 : FontWeight.w500,
              color: states.contains(WidgetState.selected) ? _accentHover : _textMuted,
            ),
          ),
          iconTheme: WidgetStateProperty.resolveWith(
            (states) => IconThemeData(size: 22, color: states.contains(WidgetState.selected) ? _accent : _textMuted),
          ),
        ),
        child: NavigationBar(
          height: 64,
          elevation: 0,
          backgroundColor: Colors.transparent,
          surfaceTintColor: Colors.transparent,
          indicatorColor: _accentMuted,
          selectedIndex: current < 0 ? 0 : current,
          onDestinationSelected: (index) => _openPhoneTab(controller, tabs[index]),
          destinations: <Widget>[
            for (final tab in tabs)
              NavigationDestination(icon: Icon(_info(tab).$2), label: _info(tab).$1),
          ],
        ),
      ),
    );
  }
}

/// The app bar's overflow on phones: pages without a tab.
class _PhoneMoreMenu extends StatelessWidget {
  const _PhoneMoreMenu({required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    final sections = _mainSections(controller)
        .where((section) => section == AppSection.favorites || section == AppSection.trash || section == AppSection.admin)
        .toList();
    if (sections.isEmpty) return const SizedBox.shrink();
    return PopupMenuButton<AppSection>(
      tooltip: appStrings.mobileMore,
      icon: Icon(Icons.more_vert_rounded, color: _textSecondary),
      onSelected: controller.setSelectedSection,
      itemBuilder: (context) => <PopupMenuEntry<AppSection>>[
        for (final section in sections)
          PopupMenuItem<AppSection>(
            value: section,
            child: Row(
              children: <Widget>[
                Icon(section.icon, size: 18, color: section == controller.selectedSection ? _accent : _textSecondary),
                const SizedBox(width: 12),
                Text(section.label),
              ],
            ),
          ),
      ],
    );
  }
}
