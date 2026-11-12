/// A place in the app as the web address shows it below /app/:
///
/// * `files` and `files/<folderId>`
/// * `shared` and `shared/<folderId>` (a folder another account shares)
/// * `recent`, `favorites`, `trash`, `review` (AI suggestions)
/// * `settings` and `settings/<page>`, e.g. `settings/shared-links`
/// * `admin` and `admin/<tab>`, e.g. `admin/backups`
///
/// The same text is kept in the preferences, so the native apps reopen where
/// they were left. [section] and [detail] are names; the controller maps them
/// to its pages and ignores what it doesn't know.
class AppRoute {
  const AppRoute(this.section, [this.detail]);

  /// No place given: the app's root, or a page the router doesn't own
  /// (`/device?code=…`, `/pair?code=…`).
  static const AppRoute none = AppRoute('');

  static const Set<String> sections = <String>{'files', 'review', 'shared', 'recent', 'favorites', 'trash', 'settings', 'admin'};

  final String section;

  /// The folder id, settings page or admin tab; null for the section's start.
  final String? detail;

  bool get isNone => section.isEmpty;

  /// The route [location] names, e.g. `/files/abc` or `settings/security`.
  /// A leading `app` segment is skipped, so a full path works too.
  static AppRoute parse(String location) {
    final uri = Uri.tryParse(location.trim());
    if (uri == null) return none;
    var segments = uri.pathSegments.where((segment) => segment.isNotEmpty).toList();
    if (segments.isNotEmpty && segments.first == 'app') segments = segments.sublist(1);
    if (segments.isEmpty || !sections.contains(segments.first)) return none;
    final detail = segments.length > 1 ? segments[1] : null;
    return AppRoute(segments.first, detail);
  }

  /// The path below /app/, starting with a slash.
  String get location {
    if (isNone) return '/';
    final tail = detail;
    return tail == null || tail.isEmpty ? '/$section' : '/$section/${Uri.encodeComponent(tail)}';
  }

  /// `sharedLinks` → `shared-links`: how enum names appear in addresses.
  static String slug(String name) =>
      name.replaceAllMapped(RegExp('[A-Z]'), (match) => '-${match.group(0)!.toLowerCase()}');

  @override
  bool operator ==(Object other) => other is AppRoute && other.section == section && other.detail == detail;

  @override
  int get hashCode => Object.hash(section, detail);

  @override
  String toString() => location;
}
