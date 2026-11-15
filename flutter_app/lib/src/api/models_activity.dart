// The activity feed and comments (docs/api.md, "Activity" and "Comments"),
// parsed with models.dart's helpers.
part of 'models.dart';

/// A person in the feed, by the name the server shows and whether it is the
/// signed-in account.
class ActivityPerson {
  const ActivityPerson({required this.name, required this.self});

  factory ActivityPerson.fromJson(Object? json) {
    final map = json is Map ? json.cast<String, dynamic>() : const <String, dynamic>{};
    return ActivityPerson(name: _string(map['name']), self: map['self'] == true);
  }

  final String name;
  final bool self;
}

/// One thing that happened to a file or folder. A burst of the same kind in
/// one folder arrives as one item with a [count] and the first [names].
class ActivityItem {
  const ActivityItem({
    required this.id,
    required this.kind,
    required this.at,
    required this.count,
    required this.actor,
    required this.owner,
    required this.nodeId,
    required this.nodeName,
    required this.nodeIsFolder,
    required this.folder,
    required this.names,
    required this.from,
    required this.member,
  });

  factory ActivityItem.fromJson(Map<String, dynamic> json) {
    final node = json['node'] is Map ? (json['node'] as Map).cast<String, dynamic>() : const <String, dynamic>{};
    return ActivityItem(
      id: _int(json['id']),
      kind: _string(json['kind']),
      at: _date(json['at']),
      count: _int(json['count']),
      actor: ActivityPerson.fromJson(json['actor']),
      owner: ActivityPerson.fromJson(json['owner']),
      nodeId: _string(node['id']),
      nodeName: _string(node['name']),
      nodeIsFolder: node['type'] == 'folder',
      folder: json['folder'] == null ? null : _string(json['folder']),
      names: ((json['names'] as List?) ?? const <dynamic>[]).map(_string).toList(),
      from: json['from'] == null ? null : _string(json['from']),
      member: json['member'] == null ? null : ActivityPerson.fromJson(json['member']),
    );
  }

  final int id;

  /// upload, folder, copy, version, rename, move, trash, restore, share, comment.
  final String kind;
  final DateTime? at;
  final int count;
  final ActivityPerson actor;

  /// Whose files these are; differs from the viewer in shared folders.
  final ActivityPerson owner;
  final String nodeId;
  final String nodeName;
  final bool nodeIsFolder;

  /// The folder it happened in, when the viewer can see it.
  final String? folder;
  final List<String> names;

  /// The old name of a rename.
  final String? from;

  /// Who a share was made with.
  final ActivityPerson? member;
}

class ActivityPage {
  const ActivityPage({required this.items, required this.nextCursor});

  factory ActivityPage.fromJson(Map<String, dynamic> json) => ActivityPage(
    items: ((json['items'] as List?) ?? const <dynamic>[])
        .map((item) => ActivityItem.fromJson((item as Map).cast<String, dynamic>()))
        .toList(),
    nextCursor: json['nextCursor'] == null ? null : _int(json['nextCursor']),
  );

  final List<ActivityItem> items;

  /// Pass to the next request; null at the end of the feed.
  final int? nextCursor;
}

/// Where the feed looks: everything, the account's own files, or what others share with it.
enum ActivityScope { all, mine, shared }

class NodeComment {
  const NodeComment({
    required this.id,
    required this.author,
    required this.mine,
    required this.body,
    required this.createdAt,
    required this.editedAt,
    required this.canDelete,
  });

  factory NodeComment.fromJson(Map<String, dynamic> json) => NodeComment(
    id: _int(json['id']),
    author: _string(json['author']),
    mine: json['mine'] == true,
    body: _string(json['body']),
    createdAt: _date(json['createdAt']),
    editedAt: _date(json['editedAt']),
    canDelete: json['canDelete'] == true,
  );

  final int id;
  final String author;
  final bool mine;
  final String body;
  final DateTime? createdAt;
  final DateTime? editedAt;
  final bool canDelete;
}

class CommentPage {
  const CommentPage({required this.comments, required this.nextCursor});

  factory CommentPage.fromJson(Map<String, dynamic> json) => CommentPage(
    comments: ((json['comments'] as List?) ?? const <dynamic>[])
        .map((item) => NodeComment.fromJson((item as Map).cast<String, dynamic>()))
        .toList(),
    nextCursor: json['nextCursor'] == null ? null : _int(json['nextCursor']),
  );

  final List<NodeComment> comments;
  final int? nextCursor;
}
