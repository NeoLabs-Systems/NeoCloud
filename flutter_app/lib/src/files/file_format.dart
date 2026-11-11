import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../api/models.dart';

/// "1.4 MB", "820 KB" — binary units like file managers show.
String formatBytes(int bytes) {
  if (bytes < 1024) return '$bytes B';
  const units = <String>['KB', 'MB', 'GB', 'TB'];
  var value = bytes / 1024;
  var unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return '${value.toStringAsFixed(value >= 10 ? 0 : 1)} ${units[unit]}';
}

String formatDate(DateTime? date, String locale) {
  if (date == null) return '—';
  final now = DateTime.now();
  if (date.year == now.year && date.month == now.month && date.day == now.day) {
    return DateFormat.Hm(locale).format(date);
  }
  return DateFormat.yMMMd(locale).add_Hm().format(date);
}

enum FileKind { folder, image, video, audio, pdf, document, spreadsheet, presentation, text, code, archive, other }

FileKind fileKindOf(FileNode node) {
  if (node.isFolder) return FileKind.folder;
  final mime = node.mime ?? '';
  final ext = node.name.contains('.') ? node.name.split('.').last.toLowerCase() : '';
  if (mime.startsWith('image/')) return FileKind.image;
  if (mime.startsWith('video/')) return FileKind.video;
  if (mime.startsWith('audio/')) return FileKind.audio;
  if (mime == 'application/pdf') return FileKind.pdf;
  if (const <String>{'doc', 'docx', 'odt', 'rtf'}.contains(ext)) return FileKind.document;
  if (const <String>{'xls', 'xlsx', 'ods', 'csv'}.contains(ext)) return FileKind.spreadsheet;
  if (const <String>{'ppt', 'pptx', 'odp'}.contains(ext)) return FileKind.presentation;
  if (const <String>{'zip', 'gz', 'tar', '7z', 'rar'}.contains(ext)) return FileKind.archive;
  if (const <String>{'js', 'ts', 'dart', 'py', 'sh', 'json', 'yaml', 'yml', 'xml', 'html', 'css'}.contains(ext)) return FileKind.code;
  if (mime.startsWith('text/')) return FileKind.text;
  return FileKind.other;
}

IconData fileKindIcon(FileKind kind) {
  switch (kind) {
    case FileKind.folder:
      return Icons.folder_rounded;
    case FileKind.image:
      return Icons.image_outlined;
    case FileKind.video:
      return Icons.movie_outlined;
    case FileKind.audio:
      return Icons.music_note_outlined;
    case FileKind.pdf:
      return Icons.picture_as_pdf_outlined;
    case FileKind.document:
      return Icons.description_outlined;
    case FileKind.spreadsheet:
      return Icons.table_chart_outlined;
    case FileKind.presentation:
      return Icons.slideshow_outlined;
    case FileKind.text:
      return Icons.notes_outlined;
    case FileKind.code:
      return Icons.code_rounded;
    case FileKind.archive:
      return Icons.inventory_2_outlined;
    case FileKind.other:
      return Icons.insert_drive_file_outlined;
  }
}

const Map<String, String> _mimeByExtension = <String, String>{
  'jpg': 'image/jpeg',
  'jpeg': 'image/jpeg',
  'png': 'image/png',
  'gif': 'image/gif',
  'webp': 'image/webp',
  'heic': 'image/heic',
  'bmp': 'image/bmp',
  'svg': 'image/svg+xml',
  'mp4': 'video/mp4',
  'mov': 'video/quicktime',
  'webm': 'video/webm',
  'mkv': 'video/x-matroska',
  'mp3': 'audio/mpeg',
  'm4a': 'audio/mp4',
  'wav': 'audio/wav',
  'ogg': 'audio/ogg',
  'flac': 'audio/flac',
  'pdf': 'application/pdf',
  'txt': 'text/plain',
  'md': 'text/markdown',
  'markdown': 'text/markdown',
  'csv': 'text/csv',
  'log': 'text/plain',
};

/// A MIME type from the file name, for nodes the server can't type itself
/// (vault files, whose stored names are ciphertext).
String? guessMimeType(String name) {
  final dot = name.lastIndexOf('.');
  if (dot < 0) return null;
  return _mimeByExtension[name.substring(dot + 1).toLowerCase()];
}

/// Text and code files up to this size open in the built-in editor.
const int editableTextLimitBytes = 2 * 1024 * 1024;

const Set<String> _textExtensions = <String>{'txt', 'md', 'markdown', 'log', 'ini', 'toml', 'conf', 'env', 'csv', 'tsv', 'sql', 'go', 'rs', 'java', 'kt', 'swift', 'c', 'h', 'cpp', 'rb', 'php', 'scss'};

/// Whether [node] is plain text the editor can change: text, Markdown or code.
bool isEditableText(FileNode node) {
  if (node.isFolder || node.size > editableTextLimitBytes) return false;
  final kind = fileKindOf(node);
  if (kind == FileKind.text || kind == FileKind.code) return true;
  final dot = node.name.lastIndexOf('.');
  return dot >= 0 && _textExtensions.contains(node.name.substring(dot + 1).toLowerCase());
}

/// The name "keep both" gives an upload or copy: "report (1).txt", or the
/// first number not in [taken].
String nextFreeName(String name, Set<String> taken, {bool isFolder = false}) {
  final dot = isFolder ? -1 : name.lastIndexOf('.');
  final stem = dot > 0 ? name.substring(0, dot) : name;
  final extension = dot > 0 ? name.substring(dot) : '';
  for (var number = 1;; number++) {
    final candidate = '$stem ($number)$extension';
    if (!taken.contains(candidate)) return candidate;
  }
}
