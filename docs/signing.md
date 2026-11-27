# Signing (e-signature)

Turn a file of the library into a document that one or more people sign
through personal links, without an account. The result is a new file next to
the original. Routes: [api.md](api.md#signing--apisigning-session-or-phone).
The PDF engine is described in [signing-pdf.md](signing-pdf.md), the app in
[signing-app.md](signing-app.md).

## Flow

1. **Create.** `POST /api/signing/documents` with a file id. A PDF is used as it is, an image becomes an A4 page, an office document is converted when LibreOffice is installed (`GET /capabilities`). The bytes are frozen at once: the document keeps its own reference on the blob, so editing, moving or deleting the original changes nothing and the field positions keep matching the pages. Not accepted: vault and trashed files, other accounts' files, encrypted PDFs, over 50 MB or 300 pages.
2. **Prepare** (draft). Parties (1–20, each with a colour and an optional email), fields (types in `services/signing/field_types.js`, positions as page fractions), signing order `preserved` (one after another) or `random`, expiry (30 days by default), language. Pages can be turned and fields found automatically (see [Preparing the pages](#preparing-the-pages)).
3. **Send.** One link per party, shown once (`<origin>/app/sign/<token>`), and, when an admin configured service mail, an invitation to every party that has an address (see [Mail](#mail)). A link can be replaced or turned off at any time. Fields can still change until a party opens its link; then fields and parties are locked.
4. **Sign.** The signer opens the link, agrees to sign electronically, fills in its own fields (each saved as it goes, validated on the server per type), and finishes. With `preserved` order the next party's link stays on "waiting" until the one before has finished. Any party can decline with a reason; the document then ends as `declined`.
5. **Finalize.** After the last party finished, a background step draws all values onto the frozen PDF, appends the audit pages, applies the server's digital seal and saves `<name> (signed).pdf` (`<name> (signiert).pdf` for German documents) in the original's folder (root if that is gone, never into a vault), under the normal quota. Failures (no space left, engine errors) are recorded on the document and retried with growing pauses (30 s, up to 1 h); the owner can retry at once. One document is built at a time. The final file is built from the same inputs and times, so a rebuild is identical and saving it again finds the file it already made.
6. **After.** Signers can download the signed file from their link while the owner's file is unchanged. Every download is offered with the certificate pages (the audit record) or without them (`?certificate=0`, also from the signed file in the library through `/api/signing/nodes/:nodeId/clean`). The clean file is the same document with the values drawn in and its own digital seal (its SHA-256 differs from the full file's); it is built on the first request, cached as a blob of the owner and deleted with the document. It counts against the owner's quota like any blob (`used_bytes` changes only in `blob_refs`), is never refused for lack of space and one build runs at a time per document. Attachments of `file` fields stay in both. The owner sees status, per-party times and the audit trail, and may delete the document (the signed file stays).

States of a document: `draft → sent → in_progress → finalizing → completed`, or `declined`, `expired` (checked on every link visit and by a 30-second background pass), `canceled`.

## Preparing the pages

- **Turning pages.** `POST /documents/:id/rotate` turns one page, some or all
  of them by quarter turns while the document can still change (a draft, or
  sent and unopened). The frozen PDF is written again with each turned page's
  new `/Rotate`; nothing on the page is redrawn, so the content stays as it
  was and pages that already carried a `/Rotate` keep it added up. The pages
  clients show, the final file and its seal all work on the turned file. A box
  on a turned page keeps its size in points, stays upright and moves with the
  spot it sat on. The document keeps the new file's SHA-256 as its original
  hash; the audit trail records `pages_rotated` with the pages, the angle and
  the hashes before and after, so the hash in the `created` entry still leads
  to the file that was signed. The old file's blob is released (the library
  file it came from stays). Signers cannot turn pages.
- **Finding fields.** `POST /documents/:id/import-fields` adds what detection
  finds to the first party (draft only): the PDF's form fields (signature
  widgets included; a text field whose name or label says "Signature",
  "Datum", "Initials" … becomes that kind of field) and the places the page
  layout marks: labels such as Signature/Unterschrift, Date/Datum,
  Name/Vor- und Nachname, Initials/Kürzel, Place/Ort beside, under or above a
  drawn line, a run of underscores or dots, or an empty box; a label naming
  several ("Ort, Datum", "Datum / Unterschrift") splits its line; a label with
  a colon and nothing after it; drawn or typed check boxes (☐, ❏, `[ ]`)
  named by the text beside them. Dates of birth, company names and the like
  stay plain text fields. Every find has a role and a confidence (form fields
  1); overlapping finds keep the most certain one, form fields always win, and
  a find that would cover a field already there is skipped, so asking twice
  adds nothing. It reads the text layer only: no AI, no OCR, so a scanned
  page without text gives nothing, and text that does not read upright on the
  displayed page is ignored (turn the page first). The same file always gives
  the same fields. Quick sign asks for the roles signature, initials, date and
  name and fills them at once with the saved pictures, today and the name.

## Data model (migrations 21, 23 and 25)

`sign_documents` (frozen blob and SHA-256, pages, status, order, locale, expiry, final node and SHA-256, finalization retry state), `sign_parties` (position, colour, email, `token_hash`, status, times, signer name, IP, user agent, time zone), `sign_fields` (type, flags, default, preferences, areas), `sign_values` (JSON or an encrypted blob of the owner), `sign_events` (hash-chained trail), `sign_seal_keys` (the server's certificate, encrypted with the master key). Migration 23 adds `clean_blob_id`, `clean_sha256`, `clean_size` to `sign_documents` for the cached clean file. Migration 25 adds `message`, `reminder_days`, `deliver_email` to documents, `token_enc`, `user_id`, `is_self` to parties and the tables `sign_mail_outbox`, `sign_party_grants`, `sign_profiles` and `sign_profile_images`. Everything cascades with the document; the account's deletion removes it all and the blobs; the account export lists documents, parties and events (no tokens, no uploads) and the account's signing profile (details, presets, switches, which pictures are saved, which documents it signed); erasing an account removes its profile, saved pictures and their blobs, unbinds the parties it had claimed and drops its grants.

## Security model

- **Links.** 256 random bits, base64url; only the SHA-256 is stored (compared in constant time); a malformed token never reaches the database. Unknown, revoked, replaced and malformed tokens give the same `404`. The link is the only credential: no cookie is read or set, answers are `no-store`, `noindex`, `no-referrer`, and the token path is redacted in logs (`/api/public/sign/[redacted]`, `/app/sign/[redacted]`). The page `/app/sign/<token>` carries the same headers plus a strict CSP (this origin only, no framing): it loads no Google fonts and no CDN, so the release builds the web app with `--no-web-resources-cdn` and the fonts come from `assets/fonts/geist/` with `GoogleFonts.config.allowRuntimeFetching = false`. A replaced link starts over at consent (what was filled in stays).
- **Limits.** Per address (reads, writes, and answers of `404`), and per token. Request bodies are capped (JSON 1 MB, images 8 MB, files 10 MB, 50 MB of uploads per document).
- **Other parties stay unknown to a signer.** A link's page carries the caller's own party and only counts for the rest (`progress: { done, total }`, `turn.waitingCount`): never another party's name, email or colour. What other parties filled in (their signature images, typed text) stays visible so the preview matches the final file; the certificate pages of the final PDF list everybody, as the audit record that all parties receive.
- **Server-side authority.** Every value is validated against its field type; a party writes only its own, non-read-only fields, only while its turn is `ready` and after consent; geometry and ownership sent by a client are never trusted. Completion is one transaction that re-checks the required fields; afterwards values cannot change.
- **Names and text.** Direction overrides, line and paragraph separators, zero-width characters and line breaks are removed from names, titles, field labels and signed text before they are stored (`plain_text.js`), so a name cannot read reversed or fake a line on the certificate.
- **PDFs.** Object and xref streams may inflate to 64 MB at most while a file loads (a 1.5 MB bomb used to take 3.8 GB); broken, nested and oversized files answer a plain `400`. The engine runs in the server process, one finalization at a time: a 44 MB / 300-page document takes about 1 s and 750 MB peak.
- **Uploads.** Images are decoded and re-encoded as PNG with sharp (metadata dropped, pixel limit, size cap, SVG refused); attachments are stored under a cleaned name and only ever attached to the final PDF. Everything is encrypted with the owner's key and counts against the owner's quota.
- **Audit trail.** Created, updated (after sending), pages rotated (with the file's hash before and after), sent, link replaced or turned off, opened, consented (with name, time zone, consent version), party completed or declined, completed, finalized, canceled, expired, final downloaded, claimed by an account, grant used, mail queued, sent or failed and reminder sent (masked address), saved to an account's cloud or not; each with time, address (`req.ip`, so set `TRUST_PROXY` behind a proxy) and a shortened user agent. Each entry's hash covers the previous one: `GET …/events` re-computes the chain and reports `chainValid`. Saving a value is not an event; downloads of the signed file are logged once per person and minute.
- **Seal.** The final PDF carries a detached PKCS#7 signature made with a self-signed certificate generated on first use (`NEOCLOUD_SIGNING_SEAL=false` turns it off). Viewers show an unknown signer; the seal shows that the file has not changed since NeoCloud made it. It is a digital seal, not a qualified or advanced signature.

## Mail

Only when an admin configured service mail ([email.md](email.md);
`GET /capabilities` says so in `email.configured`). Without it nothing changes:
links are copied by hand as before, no mail row is ever written.

**Which mail.** *Invitation* (the creator's name, the title, the optional
personal note as a quote, the expiry, a "Review and sign" button to
`/app/sign/<token>` built from the configured public URL, the plain link under
it), *reminder*, *your turn* (sequential documents: the next party is invited
when the one before has signed, not earlier), *expiry warning* (24 hours before
the end, once, only for a request that lasted longer than that), *completed* (to
every party with an address and to the creator: the signed file with its
certificate pages attached when it is at most 10 MB, otherwise a link to the
party's signing page or, for the creator, to `/app/signing/<id>`; both mention
that the page also offers the file without the signature pages) and notices to
the creator (*a party signed*, only when the profile's `notifyOnEachSignature` is
on and not for the last signature, which the completion mail covers; *a party
declined*, with the reason; *expired*; nothing when the owner cancels the request themselves). Texts are English or German
by the document's language, built with the shared layout (escaped, plain-text
twin, subject on one line, dark mode). A mail to a party carries only that
party's own link; no template ever receives another party's token.

**Who gets mail.** Parties only when the creator sent with `deliverEmail` (default
on) while mail was configured (`sign_documents.deliver_email`); the owner's own
party (`isSelf`) never does. The creator's notices need a **verified** address on
the account and are skipped silently otherwise. Mail never goes to an address
that is not a plain one (no names, lists or comments).

**Limits.** Mail needs a verified sender address (`EMAIL_NOT_VERIFIED`), at most
100 request mails per account and day (1000 for an admin), 20 addresses per
document and 3 invitations a day to one address from the whole instance
(`MAIL_QUOTA_EXCEEDED`); see [email.md](email.md#abuse-limits-signing-mail).
A mail the limits refuse later (a turn, an automatic reminder, a renewed link)
shows as failed with `RATE_LIMITED`. `delivery` carries `errorCode` and a short
`lastError` sentence in the document's language, never the mail server's text.

**Reminders.** The creator reminds one party or all at once; at least 24 hours
between two reminders to a party and at most 5 (automatic ones count). A document
can also remind by itself every 2, 3, 5, 7 or 14 days (`reminderDays`, counted
from the last mail asking that party to sign; the expiry warning restarts the
clock). Both stop when the party signs, declines or the document ends.

**Outbox.** A mail is first a row in `sign_mail_outbox` with a status (`queued`,
`sending`, `sent`, `failed`, `canceled`) and sent by a worker started and
stopped with the server (two at a time, timers `unref`'d). A failed attempt is
tried again after 1, 5, 30, 120 and 120 minutes; the sixth failure is final.
Rows left in `sending` by a stop are queued again on the next start. A mail that
is no longer wanted (the party signed, the document ended) is canceled while it
is still queued. The row holds the parameters of a template, never a link: the
mail is rendered when it is sent, from the party's current link and the
document's current title. The owner sees each party's state in `delivery`
(`failed`, `lastError` without addresses or secrets, so the screen can offer
"copy the link instead"). The audit trail records `mail_queued`, `mail_sent`,
`mail_failed` and `reminder_sent` with a masked address (`a***@example.com`),
never the address.

**The token copy.** A later mail has to link to the party's page, but a link's
token is shown once and only its hash is stored. For a party **with an email
address** the token is also kept encrypted with the server's secret key
(`token_enc`, the master-key derived `neocloud:secrets` key, like 2FA seeds) and
decrypted only to put it in that party's own mail. The trade-off: somebody who
holds both the database and the master key can open these parties' pages, which
the hash alone never allowed; a party without an address has no copy, the copy
is removed when the link is turned off and replaced when it is renewed, and
verification stays hash-based. Leave `deliverEmail` off and the copy still
exists only for parties that have an address; clear the address and send again
if that is not acceptable.

## Signers with an account

A person with an account on the instance has a smoother path. None of it is
needed to sign: the public routes stay cookie-free and unchanged.

- **Signing profile.** Name, email, phone, named presets (Address, Company,
  Title, …, up to 30), saved signatures and initials (up to 5 each, PNG, in the
  account's own encrypted storage) and the switches *fill in*, *save signed
  documents to my cloud* (with a folder, default the automatic "Signed
  documents" folder in the root, never a vault) and *mail me for each
  signature*. The personal details and presets are one text encrypted with the
  server's secret key.
- **Claim and prefill.** `POST /claim` with the link's token binds the party to
  the account and returns what the profile can fill in by field type (signature
  and initials from the default saved picture, a self-filling date) and name
  (full/first/last name, email, phone and the presets, English and German
  synonyms). With `apply` it writes those values into the party's own, empty,
  writable fields without a sender default, each checked like a typed value; it
  never overwrites, works only while the party's turn is `ready` and leaves
  consent to the signer.
- **Saved to the cloud.** When a document completes, every party bound to an
  account whose profile says so gets a copy of the signed file
  (`<title> (signed).pdf`, certificate pages included, name conflicts renamed)
  into its cloud, owned by it and counted against its quota; the owner already
  has the file beside the original and is not copied. A failure (no space left,
  the file gone) never touches the document: it is recorded as `autosave_failed`
  and tried once more five minutes later by the background pass; success is
  `autosaved` with the new file's id. A party that was never tried (a stop in
  between, or it claimed after completion) is picked up within a day.
- **Inbox.** The requests addressed to the account: parties bound to it and
  parties whose address is the account's **verified** email (an address nobody
  confirmed entitles to nothing). A party another account has claimed is not
  shown to a second account with the same address.
- **Sign yourself.** One party of a document can be the owner (`isSelf`): its
  email defaults to the owner's, nothing is mailed for it, and
  `open-as-owner` opens it in the editor flow through a grant.

- **Quick sign.** `POST /documents/:id/quick-sign` signs a draft of the owner
  in one request (before it, the app finds the places to sign with
  `import-fields` and may turn pages with `rotate`): the owner becomes its only party (`isSelf`), the given
  signature, initials, date and text boxes become its fields, and the server
  runs the normal steps on it (sent without mail, opened, consented with the
  given name, values saved, completed). The signature and initials come from
  the signing profile's saved pictures or are sent along for this document
  only. Everything after (finalizing, seal, `<name> (signed).pdf` beside the
  original, audit trail, the owner's completion mail) is the same as for any
  request. Input is checked before anything changes; a failure after sending
  cancels and deletes the document so no open request is left behind.

**Grants.** Opening an inbox request or the owner's own party returns
`/app/sign/<grant>`. A grant is a short-lived stand-in for the party's token that
the public routes accept with identical permissions: 256 random bits, only the
SHA-256 stored, valid 60 minutes, bound to one party and one account (a party
another account holds does not open), audited on first use (`grant_used`; the
first use also binds the party, `claimed`), and removed when the party completes,
the document is declined, canceled or expires, or the party's link is replaced
or turned off. The party's real token is never returned to anyone. The cost of
dying on completion: a page opened with a grant cannot ask for the result
afterwards; the finished request's file comes from `GET /api/signing/inbox/:partyId/final` (the signer's page falls back to it when the link no longer answers).

**Privacy.** Before applying, the page lists which saved details go into which field (`POST /claim` with `preview: true` binds and writes nothing), because the sender names the fields.

A signer still never learns another party's name, email or colour;
the public payload is unchanged (no creator name). The inbox shows the creator's
name only to the account the request was addressed to. Logs hold no tokens (the
`/app/sign/` and `/api/public/sign/` paths are redacted for grants too) and no
addresses; errors shown to people are masked.

## Legal notes

This is a simple electronic signature: it records who opened a personal link, their agreement to sign electronically, the typed, drawn or uploaded signature, times, addresses and a tamper-evident trail. It is not a qualified electronic signature and does not verify identity beyond possession of the link. Some documents need written form or a qualified signature by law, depending on country and subject. This page and the app are not legal advice; ask a lawyer when it matters.
