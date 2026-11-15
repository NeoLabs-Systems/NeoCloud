// Models for cloud storage the account connected itself (docs/api.md,
// "Cloud storage"), parsed with models.dart's helpers.
part of 'models.dart';

enum ExternalProvider { s3, webdav, sftp }

ExternalProvider _externalProvider(Object? value) =>
    ExternalProvider.values.firstWhere((provider) => provider.name == value, orElse: () => ExternalProvider.webdav);

/// One cloud storage connection. [config] never holds a secret: secret
/// fields show up as `<field>Set` booleans.
class ExternalMount {
  const ExternalMount({required this.id, required this.name, required this.provider, required this.readOnly, required this.config});

  factory ExternalMount.fromJson(Map<String, dynamic> json) => ExternalMount(
    id: _string(json['id']),
    name: _string(json['name']),
    provider: _externalProvider(json['provider']),
    readOnly: json['readOnly'] == true,
    config: json['config'] is Map ? Map<String, dynamic>.from(json['config'] as Map) : const <String, dynamic>{},
  );

  final String id;
  final String name;
  final ExternalProvider provider;
  final bool readOnly;
  final Map<String, dynamic> config;
}

/// A file or folder inside a connection; [path] is relative to its root.
class ExternalItem {
  const ExternalItem({required this.name, required this.path, required this.isFolder, required this.size, this.modified, this.mime});

  factory ExternalItem.fromJson(Map<String, dynamic> json) => ExternalItem(
    name: _string(json['name']),
    path: _string(json['path']),
    isFolder: json['type'] == 'folder',
    size: _int(json['size']),
    modified: _date(json['mtime']),
    mime: json['mime']?.toString(),
  );

  final String name;
  final String path;
  final bool isFolder;
  final int size;
  final DateTime? modified;
  final String? mime;
}

class ExternalListing {
  const ExternalListing({required this.path, required this.items, required this.truncated});

  factory ExternalListing.fromJson(Map<String, dynamic> json) => ExternalListing(
    path: _string(json['path']),
    items: ((json['items'] as List?) ?? const <Object?>[]).map((e) => ExternalItem.fromJson(Map<String, dynamic>.from(e as Map))).toList(),
    truncated: json['truncated'] == true,
  );

  final String path;
  final List<ExternalItem> items;
  final bool truncated;
}

/// The answer of a connection test: [error] is set when it did not work.
class ExternalTest {
  const ExternalTest({required this.ok, this.items = 0, this.error});

  factory ExternalTest.fromJson(Map<String, dynamic> json) =>
      ExternalTest(ok: json['ok'] == true, items: _int(json['items']), error: json['error']?.toString());

  final bool ok;
  final int items;
  final String? error;
}
