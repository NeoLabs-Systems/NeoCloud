import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:http/http.dart' as http;

import 'models.dart';
import 'signing_api.dart';

/// [PublicSigningApi] over HTTP, for `/api/public/sign/:token`.
///
/// It owns a plain `package:http` client instead of the app's
/// [BackendClient]: no `Authorization` header, no stored session, no
/// sign-out on 401. That keeps it working in a browser tab where nobody is
/// signed in, and on a phone that is signed in to another account. (A
/// same-origin browser request still carries cookies the page has; the
/// server ignores them on these routes.)
class HttpPublicSigningApi implements PublicSigningApi {
  /// [resolve] turns a path (`/api/public/sign/…`) into a full URL: pass
  /// `controller.client.uri`.
  HttpPublicSigningApi({
    required this.token,
    required Uri Function(String path) resolve,
    http.Client? client,
    this.timeout = const Duration(seconds: 30),
  }) : _resolve = resolve,
       _client = client ?? http.Client();

  final String token;
  final Duration timeout;
  final Uri Function(String path) _resolve;
  final http.Client _client;

  String _path(String tail) => '/api/public/sign/${Uri.encodeComponent(token)}$tail';

  static String _fieldPath(String fieldId) => '/fields/${Uri.encodeComponent(fieldId)}';

  Future<http.Response> _send(String method, String tail, {Object? json, http.BaseRequest? request}) async {
    try {
      final http.BaseRequest outgoing;
      if (request != null) {
        outgoing = request;
      } else {
        final plain = http.Request(method, _resolve(_path(tail)));
        if (json != null) {
          plain.headers['Content-Type'] = 'application/json';
          plain.body = jsonEncode(json);
        }
        outgoing = plain;
      }
      outgoing.headers['Accept'] = 'application/json, application/pdf, image/*';
      final streamed = await _client.send(outgoing).timeout(timeout);
      final response = await http.Response.fromStream(streamed).timeout(timeout);
      if (response.statusCode >= 200 && response.statusCode < 300) return response;
      throw _failure(response);
    } on SigningException {
      rethrow;
    } on TimeoutException {
      throw const SigningException(SigningErrorKind.network);
    } catch (_) {
      throw const SigningException(SigningErrorKind.network);
    }
  }

  SigningException _failure(http.Response response) {
    String message = '';
    String? code;
    int? retry;
    var fieldIds = const <String>[];
    try {
      final body = jsonDecode(utf8.decode(response.bodyBytes));
      if (body is Map) {
        message = '${body['error'] ?? ''}';
        code = body['code']?.toString();
        retry = int.tryParse('${body['retryAfter'] ?? ''}');
        final ids = body['fieldIds'];
        if (ids is List) fieldIds = ids.map((id) => '$id').toList(growable: false);
      }
    } catch (_) {
      // Not JSON.
    }
    retry ??= int.tryParse(response.headers['retry-after'] ?? '');
    return SigningException.fromStatus(
      response.statusCode,
      message: message,
      code: code,
      retryAfter: retry == null ? null : Duration(seconds: retry),
      fieldIds: fieldIds,
    );
  }

  Map<String, dynamic> _json(http.Response response) {
    try {
      final body = jsonDecode(utf8.decode(response.bodyBytes));
      return body is Map ? body.cast<String, dynamic>() : const <String, dynamic>{};
    } catch (_) {
      return const <String, dynamic>{};
    }
  }

  @override
  Future<PublicSigningView> view() async => PublicSigningView.fromJson(_json(await _send('GET', '')));

  @override
  Future<Uint8List> pdf() async => (await _send('GET', '/pdf')).bodyBytes;

  @override
  Future<void> consent({required String signerName, String? timezone, int? tzOffsetMinutes}) async {
    await _send(
      'POST',
      '/consent',
      json: <String, Object?>{
        'accepted': true,
        'signerName': signerName,
        'timezone': ?timezone,
        'tzOffsetMinutes': ?tzOffsetMinutes,
      },
    );
  }

  @override
  Future<void> saveValue(String fieldId, Object? value) async {
    await _send('PUT', _fieldPath(fieldId), json: <String, Object?>{'value': value});
  }

  /// The body is the raw image or file; the details travel in the query.
  @override
  Future<void> uploadValue(
    String fieldId, {
    required Uint8List bytes,
    required String fileName,
    String contentType = 'image/png',
    SignatureMethod? method,
    String? typedText,
  }) async {
    final query = <String, String>{
      if (method != null) 'method': method.wire,
      if (method == SignatureMethod.typed && typedText != null) 'text': typedText,
      if (method == null) 'name': fileName,
    };
    final uri = _resolve(_path('${_fieldPath(fieldId)}/upload')).replace(queryParameters: query.isEmpty ? null : query);
    final request = http.Request('POST', uri)
      ..headers['Content-Type'] = contentType
      ..bodyBytes = bytes;
    await _send('POST', '', request: request);
  }

  @override
  Future<Uint8List> valueImage(String fieldId) async => (await _send('GET', '/values/${Uri.encodeComponent(fieldId)}/image')).bodyBytes;

  @override
  Future<PublicSigningState> complete() async {
    final body = _json(await _send('POST', '/complete', json: const <String, Object?>{}));
    return PublicSigningState.parse(body['state']);
  }

  @override
  Future<void> decline({String? reason}) async {
    await _send('POST', '/decline', json: <String, Object?>{if (reason != null && reason.trim().isNotEmpty) 'reason': reason.trim()});
  }

  @override
  Future<bool> finalAvailable() async => _json(await _send('GET', '/result'))['finalAvailable'] == true;

  @override
  Future<Uint8List> finalPdf({bool certificate = true}) async => (await _send('GET', certificate ? '/final' : '/final?certificate=0')).bodyBytes;

  void close() => _client.close();
}
