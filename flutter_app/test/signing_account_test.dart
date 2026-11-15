import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/signing/fake_signing_account_api.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';

import 'helpers/public_signing_harness.dart' as pub;
import 'support/signing_harness.dart' as creator;

Finder key(String value) => find.byKey(ValueKey<String>(value));

void main() {
  setUpAll(() async {
    GoogleFonts.config.allowRuntimeFetching = false;
    await creator.initSigningTests();
  });

  group('the signer with an account', () {
    pub.testSigning('a signed-in signer is offered the saved details; using them fills the form and the flow jumps to review', (tester) async {
      final api = FakePublicSigningApi(view: pub.viewOf(fields: <SigningField>[pub.field('t', FieldType.text, name: 'Full name')]));
      final account = FakeSigningAccountApi()
        ..prefillFieldIds = <String>['t']
        ..onApply = () => api.prefill(<String, Object?>{'t': 'Ada Lovelace'});
      await pub.pumpSigning(tester, api, account: account, signedIn: true);
      expect(key('claim-banner'), findsOneWidget);
      expect(find.text("You're signed in as Ada Lovelace"), findsOneWidget);
      expect(find.text('Use your saved details to sign faster.'), findsOneWidget);
      expect(key('claim-signin'), findsNothing);

      await pub.tapKey(tester, 'claim-use');
      expect(account.claimApplied, <bool>[true]);
      expect(account.claimed, <String>[pub.testToken]);
      expect(key('claim-banner'), findsNothing);
      expect(find.textContaining('1 field filled in from your profile'), findsOneWidget);

      // The consent still happens.
      await pub.tapKey(tester, 'consent-continue');
      expect(find.text('Please agree to continue.'), findsOneWidget);
      await pub.tapKey(tester, 'consent-check');
      await tester.enterText(key('consent-name'), 'Ada Lovelace');
      await pub.tapKey(tester, 'consent-continue');
      expect(api.calls, contains('consent'));
      expect(find.text('Everything is filled in. Review and finish.'), findsOneWidget);
      expect(find.textContaining('From your profile'), findsOneWidget, reason: 'the row says where the value came from');
      expect(key('review-finish'), findsOneWidget);
    });

    pub.testSigning('without the profile fill the flow goes to the first field as before', (tester) async {
      final api = FakePublicSigningApi(view: pub.viewOf(fields: <SigningField>[pub.field('t', FieldType.text, name: 'Full name')]));
      await pub.pumpSigning(tester, api, account: FakeSigningAccountApi(), signedIn: true);
      await pub.tapKey(tester, 'claim-skip');
      expect(key('claim-banner'), findsNothing);
      await pub.tapKey(tester, 'consent-check');
      await tester.enterText(key('consent-name'), 'Ada');
      await pub.tapKey(tester, 'consent-continue');
      expect(key('typed-input'), findsOneWidget);
      expect(find.text('Everything is filled in. Review and finish.'), findsNothing);
    });

    pub.testSigning('a prefilled value can be edited and then loses its mark', (tester) async {
      final api = FakePublicSigningApi(view: pub.viewOf(consentedAt: pub.consented, fields: <SigningField>[pub.field('t', FieldType.text, name: 'City')]));
      final account = FakeSigningAccountApi()
        ..prefillFieldIds = <String>['t']
        ..onApply = () => api.prefill(<String, Object?>{'t': 'Berlin'});
      final controller = await pub.pumpSigning(tester, api, account: account, signedIn: true);
      // Already consented: the banner is only for the first step.
      expect(key('claim-banner'), findsNothing);
      await controller.signingFlow!.applyProfileFill(<String>['t']);
      await pub.settle(tester);
      expect(key('from-profile'), findsOneWidget);
      await tester.enterText(key('typed-input'), 'Hamburg');
      await pub.settle(tester);
      expect(key('from-profile'), findsNothing);
    });

    pub.testSigning('ALREADY_CLAIMED is ignored silently', (tester) async {
      final api = FakePublicSigningApi(view: pub.viewOf(fields: <SigningField>[pub.field('t', FieldType.text)]));
      final account = FakeSigningAccountApi()..claimError = const SigningException(SigningErrorKind.conflict, statusCode: 409, code: 'ALREADY_CLAIMED');
      await pub.pumpSigning(tester, api, account: account, signedIn: true);
      // The preview already learns it, so the banner never offers it.
      expect(key('claim-banner'), findsNothing);
      expect(find.textContaining("couldn't be loaded"), findsNothing);
      expect(key('consent-continue'), findsOneWidget);
    });

    pub.testSigning('another failure says so and keeps the banner', (tester) async {
      final api = FakePublicSigningApi(view: pub.viewOf(fields: <SigningField>[pub.field('t', FieldType.text)]));
      final account = FakeSigningAccountApi()..claimError = const SigningException(SigningErrorKind.server, statusCode: 500);
      await pub.pumpSigning(tester, api, account: account, signedIn: true);
      await pub.tapKey(tester, 'claim-use');
      expect(key('claim-banner'), findsOneWidget);
      expect(find.textContaining("saved details couldn't be loaded"), findsOneWidget);
    });

    pub.testSigning('a signed-out visitor sees one quiet line and the page is otherwise unchanged', (tester) async {
      final api = FakePublicSigningApi(view: pub.viewOf(fields: <SigningField>[pub.field('t', FieldType.text)]));
      final controller = await pub.pumpSigning(tester, api);
      expect(key('claim-banner'), findsNothing);
      expect(find.text('Have an account? Sign in to fill this in faster'), findsOneWidget);
      expect(find.byType(FilledButton), findsOneWidget, reason: 'still one primary button: Continue');
      expect(controller.signingToken, pub.testToken);
      await pub.tapKey(tester, 'claim-signin');
      expect(controller.signingFlow, isNull, reason: 'left for the sign-in; the link waits in memory');
      expect(controller.signingToken, isNull);
    });

    pub.testSigning('after everyone signed a signed-in signer is told where the copy went', (tester) async {
      final api = FakePublicSigningApi(
        view: pub.viewOf(state: PublicSigningState.completed, consentedAt: pub.consented, progress: const PublicProgress(done: 2, total: 2)),
        finalBytes: pub.tinyPng,
      );
      final account = FakeSigningAccountApi(
        doneItems: <InboxItem>[const InboxItem(documentId: 'd1', partyId: 'me', title: 'Rental agreement', savedNodeId: 'node9', status: PartyStatus.completed)],
      );
      await pub.pumpSigning(tester, api, account: account, signedIn: true);
      await pub.settle(tester);
      expect(key('saved-copy'), findsOneWidget);
      expect(find.text('A copy was saved to Signed documents in your cloud.'), findsOneWidget);
      expect(key('saved-copy-open'), findsOneWidget);
      expect(key('saved-copy-neocloud'), findsOneWidget);
    });

    pub.testSigning('nothing is claimed about a copy when none was saved', (tester) async {
      final api = FakePublicSigningApi(
        view: pub.viewOf(state: PublicSigningState.completed, consentedAt: pub.consented, progress: const PublicProgress(done: 2, total: 2)),
        finalBytes: pub.tinyPng,
      );
      await pub.pumpSigning(tester, api, account: FakeSigningAccountApi(), signedIn: true);
      await pub.settle(tester);
      expect(key('saved-copy'), findsNothing);
    });
  });

  group('signing profile', () {
    Future<(FakeSigningAccountApi, NeoCloudController)> pump(WidgetTester tester, {SigningProfile? profile, bool mail = true, Size size = const Size(1100, 2400)}) async {
      creator.useScreen(tester, size);
      final account = FakeSigningAccountApi(profile: profile ?? const SigningProfile(displayName: 'Ada Lovelace', email: 'ada@example.com'));
      final controller = await creator.signingController(FakeSigningApi(), account: account, emailConfigured: mail);
      controller.setSettingsPage(SettingsPage.signingProfile);
      await tester.pumpWidget(creator.app(SettingsPanel(controller: controller)));
      await tester.pump();
      await tester.pump();
      await tester.pump();
      addTearDown(() async {
        await tester.pumpWidget(const SizedBox());
        await tester.pump(const Duration(seconds: 1));
      });
      return (account, controller);
    }

    testWidgets('shows the saved details; Save is the one primary action and only lights up for changes', (tester) async {
      final (account, _) = await pump(tester);
      expect(find.text('Signing profile'), findsWidgets);
      expect(tester.widget<TextField>(key('profile-name')).controller!.text, 'Ada Lovelace');
      expect(tester.widget<FilledButton>(key('profile-save')).onPressed, isNull);
      expect(find.text('All changes are saved.'), findsOneWidget);
      await tester.enterText(key('profile-phone'), '+49 170 000');
      await tester.pump();
      expect(tester.widget<FilledButton>(key('profile-save')).onPressed, isNotNull);
      expect(find.text('You have unsaved changes.'), findsOneWidget);
      await tester.tap(key('profile-save'));
      await tester.pump();
      await tester.pump();
      expect(account.current.phone, '+49 170 000');
      expect(account.profileSaves, 1);
      expect(find.text('All changes are saved.'), findsOneWidget);
    });

    testWidgets('an invalid email is refused', (tester) async {
      final (account, _) = await pump(tester);
      await tester.enterText(key('profile-email'), 'nope');
      await tester.pump();
      await tester.tap(key('profile-save'));
      await tester.pump();
      expect(find.text("That email address doesn't look right."), findsOneWidget);
      expect(account.profileSaves, 0);
    });

    testWidgets('presets: suggestions, editing, removing; empty rows are not saved', (tester) async {
      final (account, _) = await pump(tester);
      await tester.ensureVisible(key('preset-suggest-Address'));
      await tester.tap(key('preset-suggest-Address'));
      await tester.pump();
      expect(key('preset-suggest-Address'), findsNothing, reason: 'a suggestion is used once');
      await tester.enterText(key('preset-value-0'), 'Main Street 1');
      await tester.tap(key('preset-add'));
      await tester.pump();
      await tester.tap(key('profile-save'));
      await tester.pump();
      await tester.pump();
      expect(account.current.presets.map((preset) => '${preset.label}=${preset.value}'), <String>['Address=Main Street 1'], reason: 'the blank row was left out');
      await tester.tap(key('preset-remove-0'));
      await tester.pump();
      expect(tester.widget<FilledButton>(key('profile-save')).onPressed, isNotNull);
    });

    testWidgets('toggles are saved; the mail toggle exists only with mail', (tester) async {
      final (account, _) = await pump(tester);
      expect(key('profile-notify'), findsOneWidget);
      await tester.ensureVisible(key('profile-autofill'));
      await tester.tap(key('profile-autofill'));
      await tester.tap(key('profile-notify'));
      await tester.pump();
      await tester.tap(key('profile-save'));
      await tester.pump();
      await tester.pump();
      expect(account.current.autoFill, isFalse);
      expect(account.current.notifyOnEachSignature, isTrue);
    });

    testWidgets('without mail there is no notification switch', (tester) async {
      await pump(tester, mail: false);
      expect(key('profile-notify'), findsNothing);
      expect(key('profile-autosave'), findsOneWidget);
    });

    testWidgets('turning the cloud copy off hides the folder', (tester) async {
      await pump(tester);
      await tester.ensureVisible(key('profile-folder'));
      expect(key('profile-folder'), findsOneWidget);
      expect(find.text('Signed documents'), findsOneWidget);
      await tester.tap(key('profile-autosave'));
      await tester.pump();
      expect(key('profile-folder'), findsNothing);
    });

    testWidgets('pictures: listed with Default, make default, rename, delete; the cap of 5 disables Add', (tester) async {
      final images = <ProfileImage>[
        for (var i = 0; i < 5; i++) ProfileImage(id: 'img$i', kind: SavedKind.signature, method: SignatureMethod.typed, label: 'Sig $i', isDefault: i == 0),
        const ProfileImage(id: 'ini', kind: SavedKind.initials, label: 'AL', isDefault: true),
      ];
      final (account, _) = await pump(tester, profile: SigningProfile(displayName: 'Ada', signatures: images));
      expect(find.text('Sig 0'), findsOneWidget);
      expect(find.text('Default'), findsNWidgets(2), reason: 'one per kind');
      expect(find.text('You can keep up to 5.'), findsOneWidget);
      await tester.ensureVisible(key('profile-add-signature'));
      expect(tester.widget<OutlinedButton>(key('profile-add-signature')).onPressed, isNull);
      expect(tester.widget<OutlinedButton>(key('profile-add-initials')).onPressed, isNotNull);

      await tester.ensureVisible(key('image-default-img2'));
      await tester.tap(key('image-default-img2'));
      await tester.pump();
      await tester.pump();
      expect(account.current.signatures.where((image) => image.isDefault && image.kind == SavedKind.signature).map((image) => image.id), <String>['img2']);

      await tester.tap(key('image-rename-img2'));
      await tester.pumpAndSettle();
      await tester.enterText(key('profile-rename-field'), 'Work');
      await tester.tap(key('profile-rename-save'));
      await tester.pumpAndSettle();
      expect(account.current.signatures.firstWhere((image) => image.id == 'img2').label, 'Work');

      await tester.ensureVisible(key('image-delete-img4'));
      await tester.tap(key('image-delete-img4'));
      await tester.pumpAndSettle();
      await tester.tap(find.descendant(of: find.byType(AlertDialog), matching: find.text('Delete')));
      await tester.pumpAndSettle();
      expect(account.current.signatures.any((image) => image.id == 'img4'), isFalse);
      await tester.ensureVisible(key('profile-add-signature'));
      expect(tester.widget<OutlinedButton>(key('profile-add-signature')).onPressed, isNotNull, reason: 'room again');
    });

    testWidgets('a drawn signature is added and becomes the default', (tester) async {
      final (account, _) = await pump(tester);
      await tester.ensureVisible(key('profile-add-signature'));
      await tester.tap(key('profile-add-signature'));
      await tester.pumpAndSettle();
      final gesture = await tester.startGesture(tester.getCenter(find.byType(SignaturePad)));
      await gesture.moveBy(const Offset(60, 20));
      await gesture.moveBy(const Offset(40, -30));
      await gesture.up();
      await tester.pump();
      await tester.runAsync(() async {
        await tester.tap(find.widgetWithText(FilledButton, 'Apply'));
        await Future<void>.delayed(const Duration(milliseconds: 400));
      });
      await tester.pumpAndSettle();
      expect(account.current.signatures, hasLength(1));
      expect(account.current.signatures.single.isDefault, isTrue);
      expect(account.current.signatures.single.method, SignatureMethod.drawn);
      expect(find.text('Default'), findsOneWidget);
    });

    testWidgets('fits a phone', (tester) async {
      await pump(tester, size: const Size(390, 2600));
      expect(tester.takeException(), isNull);
      expect(key('profile-save'), findsOneWidget);
    });
  });

  group('inbox', () {
    InboxItem item(String party, String title, {PartyStatus status = PartyStatus.pending, String? saved}) =>
        InboxItem(documentId: 'd$party', partyId: party, title: title, creatorName: 'Grace', status: status, expiresAt: DateTime.now().add(const Duration(days: 5)), completedAt: saved == null ? null : DateTime.now(), savedNodeId: saved);

    Future<(FakeSigningAccountApi, NeoCloudController)> pump(WidgetTester tester, {List<InboxItem> todo = const <InboxItem>[], List<InboxItem> done = const <InboxItem>[], bool mail = true}) async {
      creator.useScreen(tester, const Size(1200, 1000));
      final account = FakeSigningAccountApi(inboxItems: todo, doneItems: done);
      final controller = await creator.signingController(FakeSigningApi(), account: account, emailConfigured: mail);
      await controller.refreshSigningTodo();
      await tester.pumpWidget(creator.app(SigningListScreen(controller: controller)));
      await tester.pump();
      await tester.pump();
      addTearDown(() async {
        await tester.pumpWidget(const SizedBox());
        await tester.pump(const Duration(seconds: 1));
      });
      return (account, controller);
    }

    testWidgets('three tabs; "To sign" carries the count', (tester) async {
      final (_, controller) = await pump(tester, todo: <InboxItem>[item('a', 'Lease'), item('b', 'NDA', status: PartyStatus.waiting)]);
      expect(controller.signingTodo, 2);
      expect(key('signing-tab-mine'), findsOneWidget);
      expect(key('signing-tab-todo'), findsOneWidget);
      expect(key('signing-tab-done'), findsOneWidget);
      expect(find.descendant(of: key('signing-tab-todo'), matching: find.text('2')), findsOneWidget);
    });

    testWidgets('"To sign" shows who asked, the state and opens the request in the same tab', (tester) async {
      final (account, controller) = await pump(tester, todo: <InboxItem>[item('a', 'Lease'), item('b', 'NDA', status: PartyStatus.waiting)]);
      await tester.tap(key('signing-tab-todo'));
      await tester.pump();
      await tester.pump();
      expect(find.text('Lease'), findsOneWidget);
      expect(find.text('From Grace'), findsNWidgets(2));
      expect(find.text('Ready to sign'), findsOneWidget);
      expect(find.text('Waiting for others'), findsOneWidget);
      await tester.tap(key('inbox-open-a'));
      await tester.pump();
      await tester.pump();
      expect(account.opened, <String>['a']);
      expect(controller.signingToken, 'grant-a', reason: 'the grant link was opened as the signer page');
      controller.finishSigning();
    });

    testWidgets('empty "To sign" explains itself', (tester) async {
      await pump(tester);
      await tester.tap(key('signing-tab-todo'));
      await tester.pump();
      await tester.pump();
      expect(find.text('Nothing to sign right now'), findsOneWidget);
      expect(find.textContaining('confirmed email address'), findsOneWidget);
    });

    testWidgets('"Signed by me" lists finished requests and links the saved file', (tester) async {
      await pump(tester, done: <InboxItem>[item('a', 'Lease', status: PartyStatus.completed, saved: 'node1'), item('b', 'NDA', status: PartyStatus.completed)]);
      await tester.tap(key('signing-tab-done'));
      await tester.pump();
      await tester.pump();
      expect(find.text('Lease'), findsOneWidget);
      expect(key('inbox-saved-a'), findsOneWidget);
      expect(key('inbox-saved-b'), findsNothing, reason: 'no saved copy, no link');
    });

    testWidgets('without mail the Signing page has no tabs', (tester) async {
      await pump(tester, mail: false);
      expect(key('signing-tab-todo'), findsNothing);
    });
  });
}
