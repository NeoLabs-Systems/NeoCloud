part of 'main.dart';

/// The pages of the app. Settings is reached from the account button at the
/// foot of the sidebar, like in every NeoLabs app.
enum AppSection { files, review, shared, favorites, recent, trash, settings, admin }

/// The sidebar's top-level entries; a group with several sections expands.
enum SidebarGroup { files, admin, settings }

extension AppSectionInfo on AppSection {
  String get label {
    switch (this) {
      case AppSection.files:
        return appStrings.navAllFiles;
      case AppSection.review:
        return appStrings.aiReviewNav;
      case AppSection.shared:
        return appStrings.navSharedWithMe;
      case AppSection.favorites:
        return appStrings.filesNavFavorites;
      case AppSection.recent:
        return appStrings.navRecent;
      case AppSection.trash:
        return appStrings.navTrash;
      case AppSection.settings:
        return appStrings.navSettings;
      case AppSection.admin:
        return appStrings.navAdmin;
    }
  }

  IconData get icon {
    switch (this) {
      case AppSection.files:
        return Icons.folder_outlined;
      case AppSection.review:
        return Icons.auto_awesome_outlined;
      case AppSection.shared:
        return Icons.folder_shared_outlined;
      case AppSection.favorites:
        return Icons.star_outline_rounded;
      case AppSection.recent:
        return Icons.schedule_rounded;
      case AppSection.trash:
        return Icons.delete_outline_rounded;
      case AppSection.settings:
        return Icons.settings_outlined;
      case AppSection.admin:
        return Icons.admin_panel_settings_outlined;
    }
  }

  SidebarGroup get group {
    switch (this) {
      case AppSection.files:
      case AppSection.review:
      case AppSection.shared:
      case AppSection.favorites:
      case AppSection.recent:
      case AppSection.trash:
        return SidebarGroup.files;
      case AppSection.settings:
        return SidebarGroup.settings;
      case AppSection.admin:
        return SidebarGroup.admin;
    }
  }
}

extension SidebarGroupInfo on SidebarGroup {
  String get label {
    switch (this) {
      case SidebarGroup.files:
        return appStrings.navFiles;
      case SidebarGroup.admin:
        return appStrings.navAdmin;
      case SidebarGroup.settings:
        return appStrings.navSettings;
    }
  }

  IconData get icon {
    switch (this) {
      case SidebarGroup.files:
        return Icons.cloud_outlined;
      case SidebarGroup.admin:
        return Icons.admin_panel_settings_outlined;
      case SidebarGroup.settings:
        // NeoAgent's sliders, also the phone's Settings tab.
        return Icons.tune;
    }
  }
}

List<AppSection> _mainSections(NeoCloudController controller) {
  return <AppSection>[
    AppSection.files,
    // AI suggestions wait here; only while AI runs for the account.
    if (controller.aiReview.enabled) AppSection.review,
    AppSection.shared,
    AppSection.favorites,
    AppSection.recent,
    AppSection.trash,
    AppSection.settings,
    // Shown for admin accounts only; the server enforces it on every request.
    if (controller.isAdmin) AppSection.admin,
  ];
}

List<AppSection> _groupSections(NeoCloudController controller, SidebarGroup group) {
  return _mainSections(controller).where((section) => section.group == group).toList();
}
