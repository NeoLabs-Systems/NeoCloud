import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';

import '../../l10n/app_language.dart';
import '../../theme/palette.dart';
import '../models.dart';
import '../saved_signatures.dart';
import 'signature_pad.dart';
import 'signature_render.dart';
import 'typed_signature.dart';

/// What the signature dialog hands back.
class SignatureResult {
  const SignatureResult({required this.png, required this.method, this.typedText, this.useForInitials = false});

  /// A transparent PNG, at most 900 x 300 px.
  final Uint8List png;
  final SignatureMethod method;

  /// The name, for a typed signature.
  final String? typedText;

  /// The signer ticked "use this for my initials too".
  final bool useForInitials;
}

/// Opens the system picker for an image; null when cancelled.
Future<Uint8List?> pickSignatureImage() async {
  final result = await FilePicker.platform.pickFiles(type: FileType.image, withData: true);
  return result?.files.single.bytes;
}

enum _Tab { draw, type, upload }

/// Draw / Type / Upload for a signature or initials, with its own Cancel and
/// Apply buttons. Put it in a dialog or a sheet ([showSignatureDialog] does).
class SignaturePanel extends StatefulWidget {
  const SignaturePanel({
    super.key,
    required this.onResult,
    required this.onCancel,
    this.offerInitials = false,
    this.initialName = '',
    this.pickImage = pickSignatureImage,
    this.store,
    this.kind = SavedKind.signature,
  });

  final ValueChanged<SignatureResult> onResult;
  final VoidCallback onCancel;

  /// Show "use this for my initials too" (when asking for a signature).
  final bool offerInitials;

  /// Pre-fills the Type tab, e.g. with the name the signer gave.
  final String initialName;

  /// Replaceable in tests.
  final Future<Uint8List?> Function() pickImage;

  /// Where signatures are kept on this device; null turns saving off.
  final SavedSignatureStore? store;

  /// Whether this panel is for a signature or for initials (kept apart).
  final SavedKind kind;

  @override
  State<SignaturePanel> createState() => _SignaturePanelState();
}

class _SignaturePanelState extends State<SignaturePanel> {
  final SignaturePadController _pad = SignaturePadController();
  late final TypedSignatureController _typed = TypedSignatureController(text: widget.initialName);
  _Tab _tab = _Tab.draw;
  Uint8List? _uploaded;
  bool _uploadFailed = false;
  bool _useForInitials = false;
  bool _busy = false;
  bool _saveOnDevice = true;
  late List<SavedSignature> _saved = widget.store?.list(widget.kind) ?? const <SavedSignature>[];

  @override
  void initState() {
    super.initState();
    _pad.addListener(_changed);
    _typed.addListener(_changed);
  }

  @override
  void dispose() {
    _pad.dispose();
    _typed.dispose();
    super.dispose();
  }

  void _changed() => setState(() {});

  bool get _ready {
    switch (_tab) {
      case _Tab.draw:
        return !_pad.isEmpty;
      case _Tab.type:
        return !_typed.isEmpty;
      case _Tab.upload:
        return _uploaded != null;
    }
  }

  Future<void> _choose() async {
    final bytes = await widget.pickImage();
    if (bytes == null || !mounted) return;
    final png = await normalizeImagePng(bytes);
    if (!mounted) return;
    setState(() {
      _uploaded = png;
      _uploadFailed = png == null;
    });
  }

  Future<void> _apply() async {
    if (!_ready || _busy) return;
    setState(() => _busy = true);
    final Uint8List? png;
    final SignatureMethod method;
    switch (_tab) {
      case _Tab.draw:
        png = await _pad.exportPng();
        method = SignatureMethod.drawn;
      case _Tab.type:
        png = await _typed.exportPng();
        method = SignatureMethod.typed;
      case _Tab.upload:
        png = _uploaded;
        method = SignatureMethod.uploaded;
    }
    if (!mounted) return;
    setState(() => _busy = false);
    if (png == null) return;
    final store = widget.store;
    final typedText = method == SignatureMethod.typed ? _typed.text.trim() : null;
    if (store != null && _saveOnDevice) {
      try {
        await store.add(widget.kind, png, method: method, label: typedText);
      } catch (_) {
        // Storage may be full or blocked; signing carries on.
      }
    }
    if (!mounted) return;
    widget.onResult(
      SignatureResult(
        png: png,
        method: method,
        typedText: typedText,
        useForInitials: widget.offerInitials && _useForInitials,
      ),
    );
  }

  void _useSaved(SavedSignature item) {
    widget.onResult(
      SignatureResult(
        png: item.png,
        method: item.method,
        typedText: item.method == SignatureMethod.typed ? item.label : null,
        useForInitials: widget.offerInitials && _useForInitials,
      ),
    );
  }

  Future<void> _removeSaved(SavedSignature item) async {
    await widget.store?.remove(widget.kind, item.id);
    if (mounted) setState(() => _saved = widget.store?.list(widget.kind) ?? const <SavedSignature>[]);
  }

  Future<void> _removeAllSaved() async {
    await widget.store?.clear(widget.kind);
    if (mounted) setState(() => _saved = const <SavedSignature>[]);
  }

  @override
  Widget build(BuildContext context) {
    final palette = paletteFor(Theme.of(context).brightness);
    final strings = appStrings;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (_saved.isNotEmpty) ...<Widget>[
          _SavedChoices(items: _saved, palette: palette, onUse: _useSaved, onRemove: _removeSaved, onRemoveAll: _removeAllSaved),
          const SizedBox(height: 16),
        ],
        SegmentedButton<_Tab>(
          showSelectedIcon: false,
          segments: <ButtonSegment<_Tab>>[
            ButtonSegment<_Tab>(value: _Tab.draw, icon: const Icon(Icons.gesture_rounded, size: 18), label: Text(strings.signingTabDraw)),
            ButtonSegment<_Tab>(value: _Tab.type, icon: const Icon(Icons.keyboard_rounded, size: 18), label: Text(strings.signingTabType)),
            ButtonSegment<_Tab>(value: _Tab.upload, icon: const Icon(Icons.image_outlined, size: 18), label: Text(strings.signingTabUpload)),
          ],
          selected: <_Tab>{_tab},
          onSelectionChanged: (selection) => setState(() => _tab = selection.first),
        ),
        const SizedBox(height: 16),
        switch (_tab) {
          _Tab.draw => Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              SignaturePad(controller: _pad),
              const SizedBox(height: 8),
              Row(
                children: <Widget>[
                  TextButton.icon(
                    onPressed: _pad.isEmpty ? null : _pad.undo,
                    icon: const Icon(Icons.undo_rounded, size: 18),
                    label: Text(strings.signingPadUndo),
                  ),
                  TextButton.icon(
                    onPressed: _pad.isEmpty ? null : _pad.clear,
                    icon: const Icon(Icons.delete_outline_rounded, size: 18),
                    label: Text(strings.signingPadClear),
                  ),
                ],
              ),
            ],
          ),
          _Tab.type => TypedSignatureEditor(controller: _typed),
          _Tab.upload => _UploadTab(bytes: _uploaded, failed: _uploadFailed, onChoose: _choose, palette: palette),
        },
        if (widget.offerInitials) ...<Widget>[
          const SizedBox(height: 4),
          CheckboxListTile(
            contentPadding: EdgeInsets.zero,
            controlAffinity: ListTileControlAffinity.leading,
            dense: true,
            value: _useForInitials,
            onChanged: (value) => setState(() => _useForInitials = value ?? false),
            title: Text(strings.signingUseForInitials),
          ),
        ],
        if (widget.store != null) ...<Widget>[
          CheckboxListTile(
            key: const ValueKey<String>('save-on-device'),
            contentPadding: EdgeInsets.zero,
            controlAffinity: ListTileControlAffinity.leading,
            dense: true,
            value: _saveOnDevice,
            onChanged: (value) => setState(() => _saveOnDevice = value ?? false),
            title: Text(strings.signingSaveOnDevice),
            subtitle: Text(strings.signingSaveOnDeviceNote, style: TextStyle(color: palette.textMuted, fontSize: 12)),
          ),
        ],
        const SizedBox(height: 12),
        Row(
          children: <Widget>[
            Expanded(child: OutlinedButton(onPressed: widget.onCancel, child: Text(strings.cancel))),
            const SizedBox(width: 12),
            Expanded(child: FilledButton(onPressed: _ready && !_busy ? _apply : null, child: Text(strings.signingApply))),
          ],
        ),
      ],
    );
  }
}

class _UploadTab extends StatelessWidget {
  const _UploadTab({required this.bytes, required this.failed, required this.onChoose, required this.palette});

  final Uint8List? bytes;
  final bool failed;
  final VoidCallback onChoose;
  final NeoCloudPalette palette;

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    final picked = bytes;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Container(
          height: 160,
          alignment: Alignment.center,
          decoration: BoxDecoration(
            color: Colors.white,
            borderRadius: BorderRadius.circular(14),
            border: Border.all(color: palette.borderLight),
          ),
          child: picked == null
              ? Padding(
                  padding: const EdgeInsets.all(16),
                  child: Text(strings.signingUploadHint, textAlign: TextAlign.center, style: const TextStyle(color: Color(0xFF6B7280))),
                )
              : Padding(padding: const EdgeInsets.all(12), child: Image.memory(picked, fit: BoxFit.contain)),
        ),
        if (failed)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(strings.signingUploadFailed, style: TextStyle(color: palette.danger)),
          ),
        const SizedBox(height: 8),
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            onPressed: onChoose,
            icon: const Icon(Icons.upload_file_rounded, size: 18),
            label: Text(picked == null ? strings.signingUploadChoose : strings.signingUploadChange),
          ),
        ),
      ],
    );
  }
}

/// The signatures kept on this device, one tap each.
class _SavedChoices extends StatelessWidget {
  const _SavedChoices({required this.items, required this.palette, required this.onUse, required this.onRemove, required this.onRemoveAll});

  final List<SavedSignature> items;
  final NeoCloudPalette palette;
  final ValueChanged<SavedSignature> onUse;
  final ValueChanged<SavedSignature> onRemove;
  final VoidCallback onRemoveAll;

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Row(
          children: <Widget>[
            Expanded(
              child: Text(strings.signingSavedTitle, style: TextStyle(color: palette.textSecondary, fontSize: 12.5, fontWeight: FontWeight.w600)),
            ),
            TextButton(
              key: const ValueKey<String>('saved-remove-all'),
              onPressed: onRemoveAll,
              style: TextButton.styleFrom(visualDensity: VisualDensity.compact, minimumSize: const Size(0, 32)),
              child: Text(strings.signingSavedRemoveAll),
            ),
          ],
        ),
        const SizedBox(height: 4),
        Wrap(
          spacing: 10,
          runSpacing: 10,
          children: <Widget>[
            for (final item in items)
              Stack(
                clipBehavior: Clip.none,
                children: <Widget>[
                  Semantics(
                    button: true,
                    label: strings.signingSavedUse,
                    child: InkWell(
                      key: ValueKey<String>('saved-${item.id}'),
                      onTap: () => onUse(item),
                      borderRadius: BorderRadius.circular(12),
                      child: Container(
                        width: 132,
                        height: 64,
                        padding: const EdgeInsets.all(8),
                        decoration: BoxDecoration(
                          color: Colors.white,
                          borderRadius: BorderRadius.circular(12),
                          border: Border.all(color: palette.borderLight),
                        ),
                        child: Image.memory(item.png, fit: BoxFit.contain, errorBuilder: (_, _, _) => const SizedBox.shrink()),
                      ),
                    ),
                  ),
                  Positioned(
                    top: -8,
                    right: -8,
                    child: Tooltip(
                      message: strings.signingSavedRemove,
                      child: InkWell(
                        key: ValueKey<String>('saved-remove-${item.id}'),
                        onTap: () => onRemove(item),
                        customBorder: const CircleBorder(),
                        child: Container(
                          width: 24,
                          height: 24,
                          decoration: BoxDecoration(color: palette.bgSecondary, shape: BoxShape.circle, border: Border.all(color: palette.borderLight)),
                          child: Icon(Icons.close_rounded, size: 14, color: palette.textSecondary),
                        ),
                      ),
                    ),
                  ),
                ],
              ),
          ],
        ),
      ],
    );
  }
}
