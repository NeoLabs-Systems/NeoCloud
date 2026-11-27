# Signing in the Flutter app: module layout

The client side of e-signing. Non-UI logic is plain Dart in
`flutter_app/lib/src/signing/`; widgets that don't need the app's private
theme are importable without `main.dart`; the rest are `part of 'main.dart'`.
The server contract is the signing design (`/api/signing`,
`/api/public/sign/:token`).

## Import

```dart
import 'package:neocloud/src/signing/signing.dart';          // everything below
import 'package:neocloud/src/signing/fake_signing_api.dart';  // fakes (dev/tests)
```

Inside `main.dart` parts the barrel is already imported.

## Files

| File | What |
| --- | --- |
| `models.dart` | Immutable models with tolerant `fromJson`/`toJson`: `SigningDocument`, `SigningParty`, `PartyDraft`, `SigningField`, `FieldArea` (page is 0-based; optional `option` for radio), `FieldPrefs` + `PrefKeys`, `SigningValue`, `SigningEvent(Log)`, `SigningListItem/Page`, `SigningDetail`, `SigningSendResult`, `PublicSigningView`; enums (`SigningStatus`, `PartyStatus`, `PublicSigningState`, `FieldType`, `SignatureMethod`, …) fall back on unknown values. |
| `field_types.dart` | Registry: `FieldTypes.of(type)` -> `FieldTypeSpec` (icon, label, default/min size, `prefs` schema for the inspector, `signerFillable`, `needsPad`, `needsUpload`, `validate`, `normalize`, `display`, `initialValue`, `lockedAspect`). `FieldTypes.create(...)` makes a new local field. `ValueIssue.message(strings)` gives friendly text. |
| `value_format.dart` | Date (`DD.MM.YYYY`, `YYYY-MM-DD`, `MM/DD/YYYY`), number and phone parsing/formatting. |
| `geometry.dart` | Fractions<->pixels, `clampRect`, `moveRect`, `resizeRect` (8 `ResizeHandle`s, min size, aspect lock, grid), `snapToNeighbours`, `hitTestHandle`, `hitTestBoxes`, `placeAt`, `rectFromDrag`. |
| `signing_api.dart` | Interfaces `SigningApi` (creator) and `PublicSigningApi` (signer), `SigningException` with `SigningErrorKind` (network, rateLimited, notFound, expired, conflict, invalid, unauthorized, server). |
| `../api/backend_client_signing.dart` | `BackendSigningApi`, the real creator client (part of `backend_client.dart`). |
| `public_signing_http.dart` | `HttpPublicSigningApi`: plain `package:http`, no auth header, no session handling; multipart uploads. |
| `fake_signing_api.dart` | `FakeSigningApi`, `FakePublicSigningApi`, `buildSamplePdf()`. |
| `widgets/` | `SigningPdfPages`, `SigningPageList`, `SigningPageController`, `FieldBox`, `SignaturePad`, `TypedSignatureEditor`, `SignaturePanel`, party colours, PNG rendering. |
| `public_flow.dart` | `PublicSigningController`: the signer's flow without any widget (see below). |
| `../../main_signing_public*.dart` | The signer's page (`/app/sign/<token>`): page and layout, field inputs, states. |
| `main_signing_widgets.dart` | `showSignatureDialog(context, ...)`: dialog on wide screens, bottom sheet on phones. |
| `saved_signatures.dart` | `SavedSignatureStore`: signatures and initials the signer chose to keep, in `shared_preferences` (`localStorage` on the web) under `neocloud.signing.saved.signature|initials`; PNG as base64 plus method, label, `createdAt`; at most 5 of each kind. Never the link token or field values, never sent anywhere. |

## Calling the API

```dart
final detail = await controller.signing.createDocument(nodeId: file.id);   // creator
final api = controller.publicSigning(token);                                // signer, signed out is fine
final view = await api.view();
```

Tests: `NeoCloudController(signing: FakeSigningApi(), publicSigningFactory: (t) => FakePublicSigningApi(view: ...))`.
`saveFields` / `saveParties` are replace-all; a field made on the client has a
`local_` id (`isLocal`) which is left out of the JSON, and the server's reply
carries the real ids. Links are returned only by `send` and `regenerateLink`.

## Pages with overlays

```dart
final pages = SigningPageController();
SigningPdfPages(
  bytes: pdfBytes,            // from signing.pdf(id) or public.pdf()
  controller: pages,          // scrollToPage, scrollToRect, setZoom, currentPage
  overlayBuilder: (context, pageIndex, pageSize) => [
    for (final f in fields) for (final a in f.areas.where((a) => a.page == pageIndex))
      Positioned.fromRect(
        rect: fractionToPixels(areaRect(a), pageSize),
        child: FieldBox(color: partyColor(i), icon: spec.icon, label: f.name, selected: ...),
      ),
  ],
)
```

`SigningPageList` is the same without pdfrx (give it `pageSizes` and a
`pageContent` builder); use it in tests, since PDFium isn't available in
`flutter test`. Zoom with Ctrl/Cmd + wheel or pinch, or via the controller.

## Signatures

`showSignatureDialog` returns a `SignatureResult` (transparent PNG at most
900x300, `SignatureMethod`, typed text, `useForInitials`). A typed signature
is rendered to PNG on the client, so every signature reaches the server as an
image: `publicApi.uploadValue(fieldId, bytes: png, fileName: 'signature.png', method: result.method)`.
Four OFL script fonts live in `assets/fonts/signing/` (licences beside them).

## Prefs keys

Field prefs use the keys the server accepts (`services/signing/field_types.js`;
unknown keys are refused). Look: `fontSize`, `fontType` (`normal|bold`), `color`
(`#rrggbb`), `align`, `valign`, `format`, `multiline`, `cells` (character
boxes). Validation: `min`, `max`, `decimals`, `minLength`, `maxLength`,
`minSelect`, `maxSelect`, `charset`, `options`, `placeholder`, `autoToday`.
`test/signing_server_schema_test.dart` compares the registry with
`test/fixtures/signing_server_schema.json`, which
`UPDATE_SIGNING_SCHEMA=1 go test -run TestFieldSchemaFixture ./internal/signing` writes from the server.

## Wording

Say "digital seal" for the server's seal in any text; never name the
signature standards in the UI. Strings are `signing...` keys in both ARB files.

## The creator's screens

Routes: `/app/signing` (list) and `/app/signing/<id>` (the editor while the
document is a draft, the progress page afterwards). The Files menu entry
"Prepare for signing…" (`startSigningFromFile`) makes a draft from a PDF,
image or, when `GET /capabilities` says the server converts them, an office
document. Hidden for vault files and shared files.

| File | What |
| --- | --- |
| `src/signing/editor_state.dart` | `SigningEditorState`: fields, parties, settings, undo/redo (bounded), clipboard, snapping, and the debounced replace-all autosave (`SigningSaveState`); a 409 locks the editor. |
| `src/signing/field_problems.dart`, `send_check.dart`, `signable.dart` | Settings the server would refuse, what sending would run into, which files can be signed. |
| `main_signing_list.dart` | List (`SigningListScreen`), `SigningDocumentPage` (editor or progress), file picker, `startSigningFromFile`. |
| `main_signing_editor*.dart`, `main_signing_inspector.dart` | Editor frame and shortcuts, pages with per-page overlays and the thumbnail rail, parties, palette and inspector. |
| `main_signing_editor_pages.dart` | Shared by the editor and quick sign: `_PagesArea` with the zoom bar and the rotate bar (turn the page in view left or right, or every page from its menu), and `_DraftPages`, which loads the draft's PDF and loads it again when `snapshotSha256` changes (pages were turned). |
| `main_signing_send.dart` | Recipients dialog and the one-time links (copy, QR). Links live only in the dialog's state. |
| `main_signing_status*.dart` | Progress page: party cards, link actions, preview with all values, audit trail, signed file. |

"Autodetect fields" (`SigningEditorState.detectFields`) adds the form fields
and the spots the server finds (`import-fields`) for the first party as one
undo step. Turning pages (`SigningEditorState.rotatePages`, `rotate`) saves
what is pending, adopts the server's answer (the boxes on turned pages have
moved) and starts a fresh undo history.

`signing:updated` (socket) and the poll on phones bump
`controller.signingRevision`; the list and the progress page reload on it.
Tests pass a stand-in for the PDF pages (`SigningPagesBuilder`); see
`test/support/signing_harness.dart`.

## The signer's page (`/app/sign/<token>`)

Opened without an account: `NeoCloudController` reads the token from the
launch address (or `openSigningLink`), keeps a `PublicSigningController` in
`signingFlow` and `NeoCloudRoot` shows `SigningPublicPage` before anything
else. While it is open `boot()` makes no session call; `finishSigning()`
returns to the normal app. The flow lives on the controller so a language
change (which rebuilds the app) loses nothing.

`PublicSigningController` holds the state: step (`intro` consent, `fill`,
`review`), the signer's fields in reading order, one active field, values,
validation (`FieldTypes`), consent, finish and decline, polling (waiting
every 20 s; after finishing until the signed file is ready), and the
autosave. Values are saved after 600 ms of quiet, choices and uploads at
once, one request at a time, latest value wins. A network or server error
keeps the value and retries with growing pauses (1 s up to 30 s); a refused
value (400) is shown at its field; a 409 reloads the page state. Nothing is
stored on the device and the token only lives inside the API object.

Tests swap PDFium for a fake page list through `debugPublicPagesBuilder`.
`test/signing_public_e2e_test.dart` runs the whole flow against a real
(throwaway) server when `SIGNING_E2E_BASE` and `SIGNING_E2E_TOKENS` are set.

## What the signer never sees, and the two downloads

The public view carries `progress: { done, total }` and `turn.waitingCount`;
it has no party names, so the UI says "{n} more people have to sign first"
and "{done} of {total} have signed". The final file is offered with and
without the appended signature pages: `finalPdf(certificate: false)` calls
`GET .../final?certificate=0` (public and creator) and
`cleanPdfForNode(nodeId)` calls `GET /api/signing/nodes/:nodeId/clean` for a
file with `signed: true`. The gold button stays the one primary action; the
other file sits in a menu or an outlined button.

## Fonts in buttons

A `ButtonStyle.textStyle` (and snack bar, tooltip, tab label) *replaces* the
text style, it is not merged. Give every such style a Geist family
(`GoogleFonts.geist(...)`); a style without one falls back to Roboto, which
the web page's CSP stops the browser from fetching, and the label turns
blank. `test/button_fonts_test.dart` guards this.

## Wave 2: mail and accounts (Flutter)

All mail UI is drawn only when `GET /api/signing/capabilities` says `email.configured`; otherwise the app is as before (copy-link flow).

- **Models / API**: `src/signing/models_account.dart` (delivery, send options, profile, claim, inbox), `account_api.dart` (`SigningAccountApi`, reached as `controller.signingAccount`), `backend_client_signing_account.dart`, `fake_signing_account_api.dart`. `SigningApi.send(id, {SendOptions})` carries `message`, `deliverEmail`, `reminderDays`.
- **Send dialog** (`main_signing_send.dart`, `main_signing_delivery.dart`): per-party "invitation goes to this address", "This is me" (marks `isSelf`), mail switch, message (1000 chars), automatic reminders. The result lists links as before plus a delivery chip per party (failed: "copy the link instead" with the server's reason) and "Sign yourself now" (`open-as-owner`).
- **Progress page**: delivery chip, last sent, reminder counter, next automatic reminder, "Send reminder" (countdown on `REMINDER_TOO_SOON`, a sentence for `PARTY_NOT_ACTIVE`, `NO_EMAIL`, `EMAIL_NOT_CONFIGURED`, `REMINDER_LIMIT`), "Remind everyone", failed-mail banner with "Send again", completion note.
- **Signer with an account** (`main_signing_claim.dart`): banner on the first step calls `POST /api/signing/claim {token, apply:true}`, reloads the view, marks values "From your profile"; the consent stays, and the flow jumps to review when everything is filled. After completion the saved copy is found through the inbox (`savedNodeId`). Signed-out visitors get one quiet sign-in line; the link is held only in memory (`controller.signingToken`).
- **Settings > Signing profile** (`main_signing_profile.dart`): details, up to 5 signatures and 5 initials (immediate), saved details, switches and folder; one Save.
- **Sign yourself** (`main_signing_quick.dart`, `src/signing/quick_sign.dart`): "Sign yourself" on the Signing page (pick a file or upload one into the folder shown) and "Sign it yourself…" in a signable file's menu make a draft and open `QuickSignPage` full screen. It reuses the editor's `SigningEditorState`, pages (with the rotate bar) and palette tiles (signature, initials, date, name) with each box showing what it will carry. Once the profile is loaded, an empty draft asks the server for the spots to sign (`detectFields(roles: quickSignRoles)`: signature, initials, date, name) and shows them filled in; any box can be removed, and "Find spots to sign" asks again; the default saved signature and initials of the profile are used, others can be picked, and a new one is drawn with `showSignatureDialog` and then offered for the profile ("Keep it" / "Only this time"). "Sign and save" sends one `quickSign` request and opens the progress page; leaving without signing deletes the draft.
- **Inbox** (`main_signing_inbox.dart`): tabs "My documents / To sign / Signed by me" on the Signing page (when mail is on), badge from `counts.todo` on the sidebar and phone chip, open through `POST /inbox/:partyId/open`.
