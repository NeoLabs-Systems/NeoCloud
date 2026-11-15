import 'package:flutter/widgets.dart';

/// Phones and tablets open the editor in the system browser instead.
const bool officeEditorEmbeds = false;

Widget officeEditorFrame({required String editorUrl, required String accessToken, required int accessTokenTtl}) => const SizedBox.shrink();
