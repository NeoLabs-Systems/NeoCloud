part of 'main.dart';

/// The pages of the app. Settings is reached from the account button at the
/// foot of the sidebar, like in every NeoLabs app.
enum AppSection { files, photos, notes, review, shared, signing, favorites, recent, activity, trash, settings, admin }

/// The sidebar's top-level entries; a group with several sections expands.
enum SidebarGroup { files, notes, photos, admin, settings }

extension AppSectionInfo on AppSection {
  String get label {
    switch (this) {
      case AppSection.files:
        return appStrings.navAllFiles;
      case AppSection.photos:
        return appStrings.photosNav;
      case AppSection.notes:
        return appStrings.notesNav;
      case AppSection.review:
        return appStrings.aiReviewNav;
      case AppSection.shared:
        return appStrings.navSharedWithMe;
      case AppSection.signing:
        return appStrings.signingNav;
      case AppSection.favorites:
        return appStrings.filesNavFavorites;
      case AppSection.recent:
        return appStrings.navRecent;
      case AppSection.activity:
        return appStrings.activityNav;
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
      case AppSection.photos:
        return Icons.photo_library_outlined;
      case AppSection.notes:
        return Icons.sticky_note_2_outlined;
      case AppSection.review:
        return Icons.auto_awesome_outlined;
      case AppSection.shared:
        return Icons.folder_shared_outlined;
      case AppSection.signing:
        return Icons.draw_outlined;
      case AppSection.favorites:
        return Icons.star_outline_rounded;
      case AppSection.recent:
        return Icons.schedule_rounded;
      case AppSection.activity:
        return Icons.timeline_rounded;
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
      case AppSection.signing:
      case AppSection.favorites:
      case AppSection.recent:
      case AppSection.activity:
      case AppSection.trash:
        return SidebarGroup.files;
      case AppSection.photos:
        return SidebarGroup.photos;
      case AppSection.notes:
        return SidebarGroup.notes;
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
      case SidebarGroup.notes:
        return appStrings.notesNav;
      case SidebarGroup.photos:
        return appStrings.photosNav;
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
      case SidebarGroup.notes:
        return Icons.sticky_note_2_outlined;
      case SidebarGroup.photos:
        return Icons.photo_library_outlined;
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
    // AI suggestions wait here while AI runs for the account; its
    // Duplicates tab needs no AI, so the page is always there.
    AppSection.review,
    // Documents to sign and their progress; hidden for nobody (every account
    // can sign its own files).
    AppSection.signing,
    // Recent, Favorites and Shared with me are views of Files, not places:
    // chips above the file list open them; Activity is a Files toolbar action
    // and a palette command. The sidebar stays short.
    AppSection.trash,
    // Notes: Markdown files under /Notes, its own sidebar entry and phone tab.
    AppSection.notes,
    // Its own sidebar entry and phone tab: the timeline of photos and videos.
    AppSection.photos,
    AppSection.settings,
    // Shown for admin accounts only; the server enforces it on every request.
    if (controller.isAdmin) AppSection.admin,
  ];
}

List<AppSection> _groupSections(NeoCloudController controller, SidebarGroup group) {
  return _mainSections(controller).where((section) => section.group == group).toList();
}
