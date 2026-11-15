import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/signing/fake_signing_account_api.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';

import 'support/signing_harness.dart';

Finder key(String value) => find.byKey(ValueKey<String>(value));

SigningField _field(String partyId, FieldType type, {double y = 0.2}) => SigningField.local(
  partyId: partyId,
  type: type,
  areas: <FieldArea>[FieldArea(page: 0, x: 0.1, y: y, w: 0.3, h: 0.04)],
);

Future<void> _teardown(WidgetTester tester) async {
  await tester.pumpWidget(const SizedBox());
  await tester.pump(const Duration(seconds: 2));
}

void main() {
  setUpAll(initSigningTests);

  group('send dialog', () {
    Future<(FakeSigningApi, FakeSigningAccountApi, NeoCloudController, String)> open(WidgetTester tester, {required bool mail, bool email = true}) async {
      useScreen(tester, const Size(1440, 1000));
      final api = FakeSigningApi()..emailConfigured = mail;
      final account = FakeSigningAccountApi();
      final controller = await signingController(api, account: account, emailConfigured: mail);
      final draft = await api.createDocument(nodeId: 'n1', title: 'Lease');
      final id = draft.document.id;
      final parties = await api.saveParties(id, <PartyDraft>[PartyDraft(name: 'Ada', email: email ? 'ada@example.com' : null), const PartyDraft(name: 'Grace')]);
      await api.saveFields(id, <SigningField>[_field(parties[0].id, FieldType.signature), _field(parties[1].id, FieldType.text, y: 0.4)]);
      await tester.pumpWidget(app(SigningEditorPage(controller: controller, detail: await api.get(id), pagesBuilder: fakePages)));
      await tester.pump();
      await tester.pump();
      await tester.tap(key('signing-send'));
      await tester.pumpAndSettle();
      addTearDown(() => _teardown(tester));
      return (api, account, controller, id);
    }

    testWidgets('without mail the dialog is what it was: no mail options at all', (tester) async {
      await open(tester, mail: false);
      expect(key('send-mail-toggle'), findsNothing);
      expect(key('send-message'), findsNothing);
      expect(key('send-reminders'), findsNothing);
      expect(key('send-self-0'), findsNothing);
      expect(find.text('The invitation goes to this address'), findsNothing);
    });

    testWidgets('with mail: options, a message with a counter, reminders; the send carries them', (tester) async {
      final (api, _, _, _) = await open(tester, mail: true);
      expect(find.text('The invitation goes to this address'), findsNWidgets(2));
      expect(tester.widget<Switch>(find.descendant(of: key('send-mail-toggle'), matching: find.byType(Switch))).value, isTrue, reason: 'on because a party has an address');
      expect(key('send-message'), findsOneWidget);
      expect(find.text('0/1000'), findsOneWidget);
      await tester.enterText(key('send-message'), 'Please sign by Friday');
      await tester.pump();
      expect(find.text('21/1000'), findsOneWidget);

      await tester.tap(key('send-reminders'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Every 3 days').last);
      await tester.pumpAndSettle();
      await tester.enterText(key('send-name-1'), 'Grace Hopper');
      await tester.enterText(key('send-email-1'), 'nope');
      await tester.tap(key('send-confirm'));
      await tester.pump();
      expect(find.text("That email address doesn't look right."), findsOneWidget);
      expect(api.lastSendOptions, isNull, reason: 'invalid: nothing sent');

      await tester.enterText(key('send-email-1'), '');
      await tester.tap(key('send-confirm'));
      await tester.pumpAndSettle();
      final options = api.lastSendOptions!;
      expect(options.deliverEmail, isTrue);
      expect(options.message, 'Please sign by Friday');
      expect(options.reminderDays, 3);
      // Per party: Sent for the one with an address, No email address for the other.
      expect(find.text('Invitations are on their way'), findsOneWidget);
      expect(find.text('1 invitation email is on its way'), findsOneWidget);
      expect(find.text('Email sent'), findsOneWidget);
      expect(find.text('No email address'), findsOneWidget);
      expect(find.textContaining('https://cloud.example/app/sign/'), findsNWidgets(2), reason: 'the links are still shown');
    });

    testWidgets('the mail switch can be turned off', (tester) async {
      final (api, _, _, _) = await open(tester, mail: true);
      await tester.tap(find.descendant(of: key('send-mail-toggle'), matching: find.byType(Switch)));
      await tester.pump();
      expect(key('send-message'), findsNothing, reason: 'no message when nothing is mailed');
      await tester.enterText(key('send-name-1'), 'Grace');
      await tester.tap(key('send-confirm'));
      await tester.pumpAndSettle();
      expect(api.lastSendOptions!.deliverEmail, isFalse);
      expect(find.text('Email sent'), findsNothing);
      expect(find.text('Links are ready'), findsOneWidget);
    });

    testWidgets('a failed mail says so and leaves the link to copy', (tester) async {
      final (api, _, _, id) = await open(tester, mail: true);
      final parties = (await api.get(id)).parties;
      api.deliveryOverrides[parties.first.id] = DeliveryStatus.failed;
      await tester.tap(key('send-confirm'));
      await tester.pumpAndSettle();
      expect(find.text('Email failed'), findsOneWidget);
      expect(key('link-failed-${parties.first.id}'), findsOneWidget);
      expect(find.textContaining('Copy the link instead'), findsWidgets);
      expect(find.textContaining('Mailbox unavailable'), findsOneWidget, reason: 'the sanitized reason');
      expect(find.textContaining("Some emails couldn't be sent"), findsOneWidget);
      expect(key('link-copy'), findsWidgets);
    });

    testWidgets('"This is me" marks a party; after sending "Sign yourself now" opens the signing page', (tester) async {
      final (api, account, controller, id) = await open(tester, mail: true);
      await tester.tap(key('send-self-1'));
      await tester.pump();
      expect(find.text('This is you'), findsOneWidget);
      await tester.tap(key('send-confirm'));
      await tester.pumpAndSettle();
      final party = (await api.get(id)).parties[1];
      expect(party.isSelf, isTrue);
      expect(key('send-sign-now'), findsOneWidget);
      expect(tester.widget<Widget>(key('send-done')), isA<TextButton>(), reason: 'one gold button: sign now');
      await tester.tap(key('send-sign-now'));
      await tester.pump();
      await tester.pump();
      expect(account.opened, <String>[party.id]);
      expect(controller.signingToken, 'grant-${party.id}');
      controller.finishSigning();
    });
  });

  group('progress page', () {
    Future<(FakeSigningApi, FakeSigningAccountApi, NeoCloudController, String, List<SigningParty>)> pump(WidgetTester tester, {bool mail = true, bool send = true}) async {
      useScreen(tester, const Size(1440, 1000));
      final api = FakeSigningApi()..emailConfigured = mail;
      final account = FakeSigningAccountApi();
      final controller = await signingController(api, account: account, emailConfigured: mail);
      final draft = await api.createDocument(nodeId: 'n1', title: 'Lease');
      final id = draft.document.id;
      final parties = await api.saveParties(id, const <PartyDraft>[PartyDraft(name: 'Ada', email: 'ada@example.com'), PartyDraft(name: 'Grace', email: 'grace@example.com')]);
      await api.saveFields(id, <SigningField>[_field(parties[0].id, FieldType.signature), _field(parties[1].id, FieldType.text, y: 0.4)]);
      await api.update(id, const SigningSettingsPatch(signingOrder: SigningOrder.random));
      if (send) await api.send(id, options: mail ? const SendOptions(deliverEmail: true, reminderDays: 3) : null);
      final detail = await api.get(id);
      await tester.pumpWidget(app(SigningStatusPage(controller: controller, detail: detail, pagesBuilder: fakePages)));
      await tester.pump();
      await tester.pump();
      await tester.pump();
      addTearDown(() => _teardown(tester));
      return (api, account, controller, id, detail.parties);
    }

    testWidgets('shows a delivery chip, the last send, the reminder counter and the next reminder per party', (tester) async {
      final (api, _, _, id, parties) = await pump(tester);
      api.setPartyDelivery(id, parties[0].id, PartyDelivery(status: DeliveryStatus.sent, lastSentAt: DateTime.now().subtract(const Duration(days: 2)), remindersSent: 2, nextReminderAt: DateTime.now().add(const Duration(days: 1))));
      api.setPartyDelivery(id, parties[1].id, const PartyDelivery(status: DeliveryStatus.failed, errorCode: 'SMTP_UNREACHABLE', lastError: 'raw server text 10.0.0.5'));
      await tester.pumpWidget(const SizedBox());
      final controller = await signingController(api, account: FakeSigningAccountApi(), emailConfigured: true);
      await tester.pumpWidget(app(SigningStatusPage(controller: controller, detail: await api.get(id), pagesBuilder: fakePages)));
      await tester.pump();
      await tester.pump();
      await tester.pump();
      expect(find.text('Email sent'), findsOneWidget);
      expect(find.text('Email failed'), findsOneWidget);
      expect(key('reminders-${parties[0].id}'), findsOneWidget);
      expect(find.text('Reminders sent: 2'), findsOneWidget);
      expect(key('next-reminder-${parties[0].id}'), findsOneWidget);
      expect(find.text('Automatic reminders every 3 days'), findsOneWidget);
      expect(find.text('Mail delivery failed for 1 party'), findsOneWidget);
      expect(find.textContaining('The mail server could not be reached.'), findsOneWidget);
      expect(find.textContaining('10.0.0.5'), findsNothing);
    });

    testWidgets('"Send reminder" works, then waits with a countdown', (tester) async {
      final (_, account, _, _, parties) = await pump(tester);
      expect(key('remind-${parties[0].id}'), findsOneWidget);
      await tester.tap(key('remind-${parties[0].id}'));
      await tester.pump();
      await tester.pump();
      expect(account.reminded, <String>[parties[0].id]);
      expect(find.text('Reminder sent to Ada'), findsOneWidget);
      final button = tester.widget<OutlinedButton>(key('remind-${parties[0].id}'));
      expect(button.onPressed, isNull);
      expect(find.textContaining('Next reminder in 23 h'), findsOneWidget);
    });

    testWidgets('429: disabled with the server\'s countdown', (tester) async {
      final (_, account, _, _, parties) = await pump(tester);
      account.remindError = const SigningException(SigningErrorKind.rateLimited, statusCode: 429, code: 'REMINDER_TOO_SOON', retryAfter: Duration(minutes: 90));
      await tester.tap(key('remind-${parties[0].id}'));
      await tester.pump();
      await tester.pump();
      expect(find.textContaining('Next reminder in 1 h'), findsOneWidget);
      expect(tester.widget<OutlinedButton>(key('remind-${parties[0].id}')).onPressed, isNull);
      expect(account.reminded, isEmpty);
    });

    for (final (code, status, text) in <(String, int, String)>[
      ('PARTY_NOT_ACTIVE', 409, "Ada can't sign right now, so there is nothing to remind."),
      ('NO_EMAIL', 400, 'There is no email address for Ada.'),
      ('EMAIL_NOT_CONFIGURED', 503, "This server can't send email right now."),
    ]) {
      testWidgets('$code says what is wrong and keeps the button', (tester) async {
        final (_, account, _, _, parties) = await pump(tester);
        account.remindError = SigningException.fromStatus(status, code: code);
        await tester.tap(key('remind-${parties[0].id}'));
        await tester.pump();
        await tester.pump();
        expect(find.text(text), findsOneWidget);
        expect(tester.widget<OutlinedButton>(key('remind-${parties[0].id}')).onPressed, isNotNull);
      });
    }

    testWidgets('"Remind everyone" asks the server once and reports', (tester) async {
      final (_, account, _, _, _) = await pump(tester);
      await tester.tap(key('signing-remind-all'));
      await tester.pump();
      await tester.pump();
      expect(find.text('No reminders sent.'), findsOneWidget);
      expect(account.remindError, isNull);
    });

    testWidgets('"Send again" for failed mails sends them again', (tester) async {
      final (api, account, _, id, parties) = await pump(tester);
      api.setPartyDelivery(id, parties[1].id, const PartyDelivery(status: DeliveryStatus.failed));
      api.setPartyStatus(id, parties[0].id, PartyStatus.pending);
      final controller = await signingController(api, account: account, emailConfigured: true);
      await tester.pumpWidget(const SizedBox());
      await tester.pumpWidget(app(SigningStatusPage(controller: controller, detail: await api.get(id), pagesBuilder: fakePages)));
      await tester.pump();
      await tester.pump();
      await tester.pump();
      await tester.tap(key('signing-mail-resend'));
      await tester.pump();
      await tester.pump();
      expect(account.reminded, <String>[parties[1].id]);
      expect(find.text('The invitation was sent again.'), findsOneWidget);
    });

    testWidgets('a finished document says who got the copies', (tester) async {
      final (api, account, _, id, parties) = await pump(tester);
      for (final party in parties) {
        api.setPartyDelivery(id, party.id, PartyDelivery(status: DeliveryStatus.sent, lastSentAt: DateTime.now()));
        api.setPartyStatus(id, party.id, PartyStatus.completed);
      }
      final controller = await signingController(api, account: account, emailConfigured: true);
      await tester.pumpWidget(const SizedBox());
      await tester.pumpWidget(app(SigningStatusPage(controller: controller, detail: await api.get(id), pagesBuilder: fakePages)));
      await tester.pump();
      await tester.pump();
      await tester.pump();
      expect(find.textContaining('Completed. Copies were emailed to ada@example.com, grace@example.com.'), findsOneWidget);
    });

    testWidgets('without mail there is no delivery, reminder or banner', (tester) async {
      final (_, _, _, _, parties) = await pump(tester, mail: false, send: true);
      expect(key('remind-${parties[0].id}'), findsNothing);
      expect(key('signing-remind-all'), findsNothing);
      expect(find.text('Email sent'), findsNothing);
    });
  });
}
