part of 'main.dart';

/// The search chips: type, last modified, size and "this folder".
class _SearchFilters {
  const _SearchFilters({this.type, this.modified, this.size, this.thisFolder = false});

  /// image, video, audio, document, pdf, archive or folder.
  final String? type;

  /// day, week, month or year.
  final String? modified;

  /// Minimum size: 10m, 100m or 1g.
  final String? size;
  final bool thisFolder;

  bool get active => type != null || modified != null || size != null || thisFolder;

  _SearchFilters copyWith({String? type, String? modified, String? size, bool? thisFolder, bool clearType = false, bool clearModified = false, bool clearSize = false}) {
    return _SearchFilters(
      type: clearType ? null : (type ?? this.type),
      modified: clearModified ? null : (modified ?? this.modified),
      size: clearSize ? null : (size ?? this.size),
      thisFolder: thisFolder ?? this.thisFolder,
    );
  }

  DateTime? modifiedAfter(DateTime now) {
    switch (modified) {
      case 'day':
        return now.subtract(const Duration(days: 1));
      case 'week':
        return now.subtract(const Duration(days: 7));
      case 'month':
        return now.subtract(const Duration(days: 30));
      case 'year':
        return now.subtract(const Duration(days: 365));
    }
    return null;
  }

  int? get minSize {
    switch (size) {
      case '10m':
        return 10 * 1024 * 1024;
      case '100m':
        return 100 * 1024 * 1024;
      case '1g':
        return 1024 * 1024 * 1024;
    }
    return null;
  }
}

List<(String, String)> get _searchTypeOptions => <(String, String)>[
  ('folder', appStrings.searchTypeFolders),
  ('image', appStrings.searchTypeImages),
  ('video', appStrings.searchTypeVideos),
  ('audio', appStrings.searchTypeAudio),
  ('document', appStrings.searchTypeDocuments),
  ('pdf', appStrings.searchTypePdf),
  ('archive', appStrings.searchTypeArchives),
];

List<(String, String)> get _searchModifiedOptions => <(String, String)>[
  ('day', appStrings.searchModifiedDay),
  ('week', appStrings.searchModifiedWeek),
  ('month', appStrings.searchModifiedMonth),
  ('year', appStrings.searchModifiedYear),
];

List<(String, String)> get _searchSizeOptions => <(String, String)>[
  ('10m', appStrings.searchSize10m),
  ('100m', appStrings.searchSize100m),
  ('1g', appStrings.searchSize1g),
];

/// One row of filter chips under the search field; each chip opens its
/// choices and shows the chosen one.
class _SearchChips extends StatelessWidget {
  const _SearchChips({required this.filters, required this.onChanged, required this.canLimitToFolder, this.contents, this.onContentsChanged});

  final _SearchFilters filters;
  final ValueChanged<_SearchFilters> onChanged;
  final bool canLimitToFolder;

  /// The "File contents" chip; left out when the server has no index.
  final bool? contents;
  final ValueChanged<bool>? onContentsChanged;

  Widget _menuChip({
    required String label,
    required String? value,
    required List<(String, String)> options,
    required ValueChanged<String> onSelect,
    required VoidCallback onClear,
  }) {
    final chosen = options.where((option) => option.$1 == value).firstOrNull;
    return MenuAnchor(
      alignmentOffset: const Offset(0, 4),
      menuChildren: <Widget>[
        for (final (key, text) in options)
          MenuItemButton(
            leadingIcon: Icon(value == key ? Icons.check_rounded : null, size: 18),
            onPressed: () => onSelect(key),
            child: Text(text),
          ),
        if (value != null) ...<Widget>[
          const Divider(),
          MenuItemButton(onPressed: onClear, child: Text(appStrings.searchAny)),
        ],
      ],
      builder: (context, menu, _) => FilterChip(
        selected: chosen != null,
        label: Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Text(chosen?.$2 ?? label),
            const SizedBox(width: 2),
            Icon(Icons.arrow_drop_down_rounded, size: 18, color: _textMuted),
          ],
        ),
        onSelected: (_) => menu.isOpen ? menu.close() : menu.open(),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: <Widget>[
        if (contents != null && onContentsChanged != null) _ContentSearchChip(selected: contents!, onChanged: onContentsChanged!),
        _menuChip(
          label: appStrings.searchType,
          value: filters.type,
          options: _searchTypeOptions,
          onSelect: (type) => onChanged(filters.copyWith(type: type)),
          onClear: () => onChanged(filters.copyWith(clearType: true)),
        ),
        _menuChip(
          label: appStrings.searchModified,
          value: filters.modified,
          options: _searchModifiedOptions,
          onSelect: (modified) => onChanged(filters.copyWith(modified: modified)),
          onClear: () => onChanged(filters.copyWith(clearModified: true)),
        ),
        _menuChip(
          label: appStrings.searchSize,
          value: filters.size,
          options: _searchSizeOptions,
          onSelect: (size) => onChanged(filters.copyWith(size: size)),
          onClear: () => onChanged(filters.copyWith(clearSize: true)),
        ),
        if (canLimitToFolder)
          FilterChip(
            selected: filters.thisFolder,
            avatar: Icon(Icons.folder_outlined, size: 16, color: filters.thisFolder ? _textPrimary : _textMuted),
            label: Text(appStrings.searchThisFolder),
            onSelected: (value) => onChanged(filters.copyWith(thisFolder: value)),
          ),
        if (filters.active)
          TextButton(onPressed: () => onChanged(const _SearchFilters()), child: Text(appStrings.searchClearFilters)),
      ],
    );
  }
}

/// No results: says so and offers the next steps that could help.
class _SearchEmpty extends StatelessWidget {
  const _SearchEmpty({required this.query, required this.filters, required this.onFilters, required this.onOpenTrash});

  final String query;
  final _SearchFilters filters;
  final ValueChanged<_SearchFilters> onFilters;
  final VoidCallback onOpenTrash;

  @override
  Widget build(BuildContext context) {
    return _EmptyCard(
      icon: Icons.search_off_rounded,
      title: query.isEmpty ? appStrings.searchNothingFiltered : appStrings.noSearchResults(query),
      subtitle: appStrings.searchEmptyHint,
      action: Wrap(
        spacing: 8,
        runSpacing: 8,
        alignment: WrapAlignment.center,
        children: <Widget>[
          if (filters.thisFolder)
            OutlinedButton(onPressed: () => onFilters(filters.copyWith(thisFolder: false)), child: Text(appStrings.searchEverywhere)),
          if (filters.type != null || filters.modified != null || filters.size != null)
            OutlinedButton(
              onPressed: () => onFilters(_SearchFilters(thisFolder: filters.thisFolder)),
              child: Text(appStrings.searchClearFilters),
            ),
          OutlinedButton.icon(
            onPressed: onOpenTrash,
            icon: const Icon(Icons.delete_outline_rounded, size: 18),
            label: Text(appStrings.searchLookInTrash),
          ),
        ],
      ),
    );
  }
}
