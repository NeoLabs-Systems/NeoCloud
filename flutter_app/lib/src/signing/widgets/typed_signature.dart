import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../l10n/app_language.dart';
import '../../theme/palette.dart';
import 'signature_render.dart';

/// A bundled script font a typed signature can use. The files live in
/// `assets/fonts/signing/` and are SIL Open Font License 1.1.
class SignatureFont {
  const SignatureFont(this.family, this.label);

  /// The family name registered in pubspec.yaml.
  final String family;

  /// Shown only to screen readers; sighted users see the name set in it.
  final String label;
}

const List<SignatureFont> signatureFonts = <SignatureFont>[
  SignatureFont('DancingScript', 'Dancing Script'),
  SignatureFont('Caveat', 'Caveat'),
  SignatureFont('GreatVibes', 'Great Vibes'),
  SignatureFont('Sacramento', 'Sacramento'),
];

/// The name typed and the style picked.
class TypedSignatureController extends ChangeNotifier {
  TypedSignatureController({String text = '', int fontIndex = 0}) : _text = text, _fontIndex = fontIndex;

  String _text;
  int _fontIndex;

  String get text => _text;
  int get fontIndex => _fontIndex;
  SignatureFont get font => signatureFonts[_fontIndex];
  bool get isEmpty => _text.trim().isEmpty;

  set text(String value) {
    if (value == _text) return;
    _text = value;
    notifyListeners();
  }

  set fontIndex(int value) {
    if (value == _fontIndex || value < 0 || value >= signatureFonts.length) return;
    _fontIndex = value;
    notifyListeners();
  }

  /// The signature as a transparent PNG; null while the name is blank.
  Future<Uint8List?> exportPng() => renderTypedPng(_text, fontFamily: font.family);
}

/// A name field and a choice of styles, each previewed with the typed name.
class TypedSignatureEditor extends StatefulWidget {
  const TypedSignatureEditor({super.key, required this.controller, this.autofocus = true});

  final TypedSignatureController controller;
  final bool autofocus;

  @override
  State<TypedSignatureEditor> createState() => _TypedSignatureEditorState();
}

class _TypedSignatureEditorState extends State<TypedSignatureEditor> {
  late final TextEditingController _field = TextEditingController(text: widget.controller.text);

  @override
  void dispose() {
    _field.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final palette = paletteFor(Theme.of(context).brightness);
    return ListenableBuilder(
      listenable: widget.controller,
      builder: (context, _) {
        final typed = widget.controller.text.trim();
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            TextField(
              controller: _field,
              autofocus: widget.autofocus,
              textCapitalization: TextCapitalization.words,
              textInputAction: TextInputAction.done,
              maxLength: 60,
              decoration: InputDecoration(labelText: appStrings.signingTypeHint, counterText: ''),
              onChanged: (value) => widget.controller.text = value,
            ),
            const SizedBox(height: 12),
            for (final (index, font) in signatureFonts.indexed)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Semantics(
                  button: true,
                  selected: index == widget.controller.fontIndex,
                  label: font.label,
                  child: InkWell(
                    key: ValueKey<String>('signature-style-$index'),
                    borderRadius: BorderRadius.circular(12),
                    onTap: () => widget.controller.fontIndex = index,
                    child: Container(
                      height: 64,
                      alignment: Alignment.centerLeft,
                      padding: const EdgeInsets.symmetric(horizontal: 16),
                      decoration: BoxDecoration(
                        color: Colors.white,
                        borderRadius: BorderRadius.circular(12),
                        border: Border.all(
                          color: index == widget.controller.fontIndex ? palette.accent : palette.borderLight,
                          width: index == widget.controller.fontIndex ? 2 : 1,
                        ),
                      ),
                      child: FittedBox(
                        fit: BoxFit.scaleDown,
                        alignment: Alignment.centerLeft,
                        child: Text(
                          typed.isEmpty ? appStrings.signingTypePreview : typed,
                          style: TextStyle(
                            fontFamily: font.family,
                            fontSize: 34,
                            color: typed.isEmpty ? const Color(0xFF9CA3AF) : const Color(0xFF111827),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              ),
          ],
        );
      },
    );
  }
}
