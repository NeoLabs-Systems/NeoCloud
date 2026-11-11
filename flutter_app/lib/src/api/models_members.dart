// Sharing with other accounts on the server (docs/api.md, "Sharing with
// accounts"), parsed with models.dart's helpers.
part of 'models.dart';

/// What a member may do in a shared folder.
enum MemberPermission {
  read,
  write;

  static MemberPermission fromApi(Object? value) => value == 'write' ? write : read;

  bool get canWrite => this == write;
}

/// Someone the owner shared a node with, as the owner sees them: the name
/// the member chose and what the owner typed, nothing else.
class ShareMember {
  const ShareMember({
    required this.id,
    required this.nodeId,
    required this.recipient,
    required this.displayName,
    required this.permission,
    required this.createdAt,
    required this.expiresAt,
    required this.expired,
  });

  factory ShareMember.fromJson(Map<String, dynamic> json) {
    final displayName = json['displayName'] as String?;
    return ShareMember(
      id: _int(json['id']),
      nodeId: _string(json['nodeId']),
      recipient: _string(json['recipient']),
      displayName: displayName == null || displayName.trim().isEmpty ? null : displayName,
      permission: MemberPermission.fromApi(json['permission']),
      createdAt: _date(json['createdAt']),
      expiresAt: _date(json['expiresAt']),
      expired: json['expired'] == true,
    );
  }

  final int id;
  final String nodeId;

  /// The email address or username the owner typed.
  final String recipient;
  final String? displayName;
  final MemberPermission permission;
  final DateTime? createdAt;
  final DateTime? expiresAt;
  final bool expired;

  /// The display name, else what the owner typed.
  String get label => displayName ?? recipient;
}

/// A file or folder another account shared with this one.
class SharedWithMe {
  const SharedWithMe({
    required this.id,
    required this.permission,
    required this.ownerName,
    required this.sharedAt,
    required this.expiresAt,
    required this.node,
  });

  factory SharedWithMe.fromJson(Map<String, dynamic> json) => SharedWithMe(
    id: _int(json['id']),
    permission: MemberPermission.fromApi(json['permission']),
    ownerName: _string(json['ownerName']),
    sharedAt: _date(json['sharedAt']),
    expiresAt: _date(json['expiresAt']),
    node: FileNode.fromJson((json['node'] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{}),
  );

  /// The share's id, for leaving it.
  final int id;
  final MemberPermission permission;
  final String ownerName;
  final DateTime? sharedAt;
  final DateTime? expiresAt;
  final FileNode node;
}
