// Drives the public signing API of a real server through HttpPublicSigningApi
// and PublicSigningController. Skipped unless SIGNING_E2E_BASE (the server's
// origin) and SIGNING_E2E_TOKENS (two links' tokens, comma separated, party 1
// then party 2) are set; the server must be a throwaway one with a document
// that has every field type for party 1 and a signature for party 2.
//
//   SIGNING_E2E_BASE=http://127.0.0.1:3344 SIGNING_E2E_TOKENS=a,b \
//     flutter test test/signing_public_e2e_test.dart

import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/signing/public_signing_http.dart';
import 'package:neocloud/src/signing/signing.dart';

final String _base = Platform.environment['SIGNING_E2E_BASE'] ?? '';
final List<String> _tokens = (Platform.environment['SIGNING_E2E_TOKENS'] ?? '').split(',');

/// A real 1x1 PNG.
final Uint8List _png = base64Decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==');

HttpPublicSigningApi _api(String token) => HttpPublicSigningApi(token: token, resolve: (path) => Uri.parse('$_base$path'));

SigningField _named(PublicSigningController flow, String name) => flow.fields.firstWhere((field) => field.name == name);

Future<void> _until(bool Function() check, {int seconds = 20}) async {
  final end = DateTime.now().add(Duration(seconds: seconds));
  while (!check()) {
    if (DateTime.now().isAfter(end)) fail('timed out');
    await Future<void>.delayed(const Duration(milliseconds: 100));
  }
}

void main() {
  final enabled = _base.isNotEmpty && _tokens.length >= 2;

  test('two parties sign a document of every field type', skip: enabled ? false : 'SIGNING_E2E_BASE / SIGNING_E2E_TOKENS not set', () async {
    final tenantApi = _api(_tokens[0]);
    final landlordApi = _api(_tokens[1]);

    // An unknown link is a plain 404.
    await expectLater(
      _api('x' * 43).view(),
      throwsA(isA<SigningException>().having((e) => e.kind, 'kind', SigningErrorKind.notFound)),
    );

    // The second party has to wait.
    final waiting = await landlordApi.view();
    expect(waiting.state, PublicSigningState.waiting);
    expect(waiting.turn.waitingCount, 1);
    await expectLater(landlordApi.pdf(), throwsA(isA<SigningException>().having((e) => e.kind, 'kind', SigningErrorKind.conflict)));

    // The first party: through the controller, as the page does.
    final flow = PublicSigningController(
      api: tenantApi,
      saveDelay: const Duration(milliseconds: 20),
      retryBase: const Duration(milliseconds: 100),
    );
    await flow.start();
    expect(flow.state, PublicSigningState.ready);
    expect(flow.step, SigningStep.intro);
    expect(flow.fixedFields.map((f) => f.type), contains(FieldType.heading));
    expect(flow.fields.length, 15);

    // Nothing can be written before consent.
    await expectLater(
      tenantApi.saveValue(_named(flow, 'Full name').id, 'x'),
      throwsA(isA<SigningException>().having((e) => e.code, 'code', 'CONSENT_REQUIRED')),
    );

    final pdf = await tenantApi.pdf();
    expect(String.fromCharCodes(pdf.take(5)), '%PDF-');

    expect(await flow.consent('Tess Tenant', timezone: 'Europe/Berlin', tzOffsetMinutes: 60), isTrue);
    expect(flow.step, SigningStep.fill);
    expect(flow.view!.party.signerName, 'Tess Tenant');

    // The server's rules reach the signer as messages.
    await expectLater(
      tenantApi.saveValue(_named(flow, 'Phone').id, 'abc'),
      throwsA(isA<SigningException>().having((e) => e.kind, 'kind', SigningErrorKind.invalid)),
    );

    flow.setValue(_named(flow, 'Full name'), 'Tess Tenant');
    flow.setValue(_named(flow, 'Persons'), '3');
    flow.setValue(_named(flow, 'Phone'), '+49 151 2345678');
    flow.setValue(_named(flow, 'Start'), '07.03.2026');
    flow.setValue(_named(flow, 'Code'), 'AB12');
    flow.setValue(_named(flow, 'House rules'), true, immediate: true);
    flow.setValue(_named(flow, 'Floor'), 'Upstairs', immediate: true);
    flow.setValue(_named(flow, 'Parking'), 'Garage', immediate: true);
    flow.setValue(_named(flow, 'Extras'), <String>['Balcony', 'Cellar'], immediate: true);
    flow.setValue(_named(flow, 'Note'), 'Please call first.');
    expect(await flow.flush(), isTrue);

    // Finishing early names what is missing.
    await expectLater(
      tenantApi.complete(),
      throwsA(isA<SigningException>().having((e) => e.code, 'code', 'FIELDS_INCOMPLETE').having((e) => e.fieldIds.length, 'missing', greaterThanOrEqualTo(4))),
    );

    flow.setUpload(_named(flow, 'Signature'), bytes: _png, fileName: 'signature.png', method: SignatureMethod.typed, typedText: 'Tess Tenant');
    flow.setUpload(_named(flow, 'Initials'), bytes: _png, fileName: 'initials.png', method: SignatureMethod.drawn);
    flow.setUpload(_named(flow, 'ID photo'), bytes: _png, fileName: 'id.png');
    flow.setUpload(_named(flow, 'Stamp'), bytes: _png, fileName: 'stamp.png');
    flow.setUpload(_named(flow, 'Attachment'), bytes: Uint8List.fromList(utf8.encode('hello')), fileName: 'note.txt', contentType: 'text/plain');
    expect(await flow.flush(), isTrue);
    expect(flow.problems, isEmpty);

    // What the server stored comes back as the page would resume it.
    final resumed = await tenantApi.view();
    final byName = <String, SigningValue?>{for (final field in resumed.ownFields) field.name: resumed.valueOf(field.id)};
    expect(byName['Full name']!.value, 'Tess Tenant');
    expect(byName['Persons']!.value, '3');
    expect(byName['Start']!.value, '2026-03-07');
    expect(byName['House rules']!.value, true);
    expect(byName['Extras']!.value, <String>['Cellar', 'Balcony']);
    expect(byName['Signature']!.hasFile, isTrue);
    expect(byName['Signature']!.method, SignatureMethod.typed);
    expect(byName['Attachment']!.fileName, 'note.txt');
    final picture = await tenantApi.valueImage(resumed.ownFields.firstWhere((f) => f.name == 'Signature').id);
    expect(picture.take(4), <int>[0x89, 0x50, 0x4e, 0x47]);

    await flow.finish();
    expect(flow.actionError, isNull);
    expect(flow.state, PublicSigningState.completed);
    expect(flow.finalReady, isFalse, reason: 'the second party has not signed');
    expect(flow.othersPending, isTrue);
    await expectLater(tenantApi.saveValue(_named(flow, 'Full name').id, 'late'), throwsA(isA<SigningException>().having((e) => e.kind, 'kind', SigningErrorKind.conflict)));
    flow.dispose();

    // Now the second party's turn has come.
    final landlord = PublicSigningController(api: landlordApi, saveDelay: const Duration(milliseconds: 20));
    await landlord.start();
    expect(landlord.state, PublicSigningState.ready);
    // It sees what the tenant filled in, read-only, and the tenant's picture.
    expect(landlord.othersFields.map((f) => f.type), containsAll(<FieldType>[FieldType.text, FieldType.signature]));
    final tenantSignature = landlord.othersFields.firstWhere((f) => f.type == FieldType.signature);
    expect((await landlordApi.valueImage(tenantSignature.id)).length, greaterThan(20));
    expect(await landlord.consent('Linda Landlord'), isTrue);
    landlord.setUpload(landlord.fields.single, bytes: _png, fileName: 'signature.png', method: SignatureMethod.drawn);
    expect(await landlord.flush(), isTrue);
    await landlord.finish();
    expect(landlord.state, PublicSigningState.completed);
    expect(landlord.othersPending, isFalse);

    // The signed copy appears after finalization, for both.
    await _until(() => landlord.finalReady);
    final finalPdf = await landlord.downloadFinal();
    expect(String.fromCharCodes(finalPdf.take(5)), '%PDF-');
    expect(finalPdf.length, greaterThan(pdf.length));
    expect(await tenantApi.finalAvailable(), isTrue);
    expect((await tenantApi.finalPdf()).length, finalPdf.length);
    landlord.dispose();
  });
}
