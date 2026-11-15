import 'dart:convert';
import 'dart:typed_data';

import 'package:shared_preferences/shared_preferences.dart';

import 'models.dart';

/// Which of the two a saved picture is for. They are kept apart.
enum SavedKind { signature, initials }

/// A signature or initials the signer chose to keep on this device.
class SavedSignature {
  const SavedSignature({required this.id, required this.kind, required this.png, required this.method, required this.createdAt, this.label});

  factory SavedSignature.fromJson(SavedKind kind, Map<String, dynamic> json) => SavedSignature(
    id: '${json['id']}',
    kind: kind,
    png: base64Decode('${json['png']}'),
    method: SignatureMethod.parse(json['method']) ?? SignatureMethod.drawn,
    label: json['label'] is String && (json['label'] as String).isNotEmpty ? json['label'] as String : null,
    createdAt: DateTime.tryParse('${json['createdAt']}') ?? DateTime.fromMillisecondsSinceEpoch(0),
  );

  final String id;
  final SavedKind kind;

  /// The transparent PNG exactly as it is uploaded.
  final Uint8List png;
  final SignatureMethod method;

  /// The typed text of a typed signature.
  final String? label;
  final DateTime createdAt;

  Map<String, Object?> toJson() => <String, Object?>{
    'id': id,
    'png': base64Encode(png),
    'method': method.name,
    if (label != null) 'label': label,
    'createdAt': createdAt.toUtc().toIso8601String(),
  };
}

/// Signatures and initials kept in the browser's or app's local storage
/// (`shared_preferences`: `localStorage` on the web), so the next signing
/// starts with one tap. Nothing here is ever sent anywhere; it holds only the
/// pictures, never a link token or any field value.
class SavedSignatureStore {
  SavedSignatureStore(this._prefs, {this.cap = 5, DateTime Function()? now}) : _now = now ?? DateTime.now;

  /// Opens the store; null when the device's storage can't be used.
  static Future<SavedSignatureStore?> open() async {
    try {
      return SavedSignatureStore(await SharedPreferences.getInstance());
    } catch (_) {
      return null;
    }
  }

  /// At most this many of each kind; the oldest go first.
  final int cap;
  final SharedPreferences _prefs;
  final DateTime Function() _now;

  static String keyFor(SavedKind kind) => 'neocloud.signing.saved.${kind.name}';

  /// Newest first. A damaged entry is skipped, never thrown.
  List<SavedSignature> list(SavedKind kind) {
    final raw = _prefs.getString(keyFor(kind));
    if (raw == null || raw.isEmpty) return const <SavedSignature>[];
    try {
      final decoded = jsonDecode(raw);
      if (decoded is! List) return const <SavedSignature>[];
      final result = <SavedSignature>[];
      for (final entry in decoded) {
        if (entry is! Map) continue;
        try {
          result.add(SavedSignature.fromJson(kind, entry.cast<String, dynamic>()));
        } catch (_) {
          // Skip an entry that can't be read.
        }
      }
      result.sort((a, b) => b.createdAt.compareTo(a.createdAt));
      return result;
    } catch (_) {
      return const <SavedSignature>[];
    }
  }

  /// Saves [png]; the same picture again just moves to the front.
  Future<SavedSignature> add(SavedKind kind, Uint8List png, {required SignatureMethod method, String? label}) async {
    final current = list(kind).where((item) => !_same(item.png, png)).toList();
    final stamp = _now();
    final entry = SavedSignature(
      id: '${stamp.microsecondsSinceEpoch}',
      kind: kind,
      png: png,
      method: method,
      label: label == null || label.trim().isEmpty ? null : label.trim(),
      createdAt: stamp,
    );
    final next = <SavedSignature>[entry, ...current].take(cap).toList(growable: false);
    await _write(kind, next);
    return entry;
  }

  Future<void> remove(SavedKind kind, String id) => _write(kind, list(kind).where((item) => item.id != id).toList(growable: false));

  Future<void> clear(SavedKind kind) async {
    await _prefs.remove(keyFor(kind));
  }

  Future<void> clearAll() async {
    for (final kind in SavedKind.values) {
      await clear(kind);
    }
  }

  Future<void> _write(SavedKind kind, List<SavedSignature> items) async {
    if (items.isEmpty) {
      await _prefs.remove(keyFor(kind));
      return;
    }
    await _prefs.setString(keyFor(kind), jsonEncode(items.map((item) => item.toJson()).toList(growable: false)));
  }

  static bool _same(Uint8List a, Uint8List b) {
    if (a.length != b.length) return false;
    for (var index = 0; index < a.length; index++) {
      if (a[index] != b[index]) return false;
    }
    return true;
  }
}
