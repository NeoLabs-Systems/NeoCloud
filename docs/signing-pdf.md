# Signing PDF engine

`internal/signing/signpdf` holds the PDF engine: functions over bytes, with no database and no HTTP. Failures carry a `code`.

| Function | Purpose |
| --- | --- |
| `probePdf(bytes)` | `{ pageCount, pages: [{ width, height, rotate }], hasAcroForm, encrypted }`. Sizes are displayed points (CropBox, swapped for /Rotate 90/270). |
| `importAcroFields(bytes)` | AcroForm widgets as `[{ type, name, required, areas, prefs, options? }]`; radio areas carry `option`. Read-only fields and buttons are skipped. |
| `imageToPdf(bytes, mime)` | One A4 page (landscape for wide images), margins 36 pt. WebP and EXIF orientation are normalised; HEIC only if this sharp build decodes it. |
| `docToPdf(bytes, filename)` / `canConvertDocuments()` (sync) | LibreOffice via `services/preview/office_convert.js` (set `NEOCLOUD_LIBREOFFICE_PATH` to override). One conversion at a time, private temp folder removed afterwards. |
| `buildFinalPdf({ pdfBytes, parties, fields, values, events, document, locale, signer })` | `{ bytes, sha256 }`. Deterministic for equal inputs and `document.finalizedAt`. `signer` (optional) seals the result. |
| `signPdf(bytes, { p12Buffer, passphrase, reason, name, location, signingTime? })` | Detached PKCS#7 seal. |
| `verifySignedPdf(bytes)` | `{ signed, intact }`: byte-range digest, RSA signature, nothing appended. No chain or trust validation. |
| `generateSelfSignedP12({ commonName, years })` (sync) | RSA-2048 / SHA-256, random passphrase. |

## Error codes

`PDF_INVALID`, `PDF_ENCRYPTED`, `PDF_TOO_LARGE` (over 50 MB), `PDF_TOO_MANY_PAGES` (over 300), `IMAGE_INVALID`, `CONVERSION_UNAVAILABLE`, `CONVERSION_FAILED`, `UNSUPPORTED_DOCUMENT`, `HASH_MISMATCH` (snapshot differs from `document.originalSha256`), `SIGNING_FAILED`.

## Inputs to buildFinalPdf

camelCase or snake_case keys are both accepted; `prefs`/`areas` may be JSON strings.

- Area: `{ page (0-based), x, y, w, h }`, fractions 0..1 of the displayed page, origin top-left. Radio/multiple areas may carry `option`; without it, area i maps to option i when the counts match.
- Prefs: `fontSize`, `fontType` (`bold`), `color`/`background` (`#RRGGBB`), `align` (left/center/right), `valign` (top/center/bottom), `format`, `multiline`, `cells` (character boxes; `length` also read).
  Date formats use tokens `YYYY YY MMMM MMM MM M DD D`; number formats are `none comma dot space usd eur gbp`.
- Values: array of `{ fieldId, value, bytes, filename, mime, method, files }`, or an object keyed by field id. Text-like: string/number; checkbox: boolean; multiple: string[]; dates: `YYYY-MM-DD`; signature/initials/stamp/image: `bytes` (PNG/JPEG, others converted) plus `method` (`draw|type|upload`); file: `bytes` + `filename` or `files[]`, attached to the PDF and listed on the certificate.
- Fields without a value fall back to `defaultValue`. Unknown types (payment etc.) are ignored.
- Parties: `{ id, name, position, color, email, signerName, ip, userAgent, timezone, tzOffsetMinutes, openedAt, consentedAt, consentVersion, completedAt, declinedAt, declineReason }`. Events: `{ type, at, partyId, ip, hash }`.

`buildFinalPdf` takes `certificate` (default `true`); `false` draws and flattens everything but appends no audit pages. The seal (`signer`) still applies, over the clean file's own bytes.

## Behaviour and limits

- Existing form fields are flattened, XFA/OpenAction/JavaScript removed. A signed source PDF loses its original signature.
- Fonts: bundled Noto Sans Regular/Bold subsets (SIL OFL, `internal/signing/signpdf/fonts`): Latin, Greek, Cyrillic, punctuation, currency. Other scripts and emoji print as `?`. No italic.
- The seal uses subfilter `adbe.pkcs7.detached`, not `ETSI.CAdES.detached`: the server does not write the signing-certificate attribute CAdES requires.
- This produces a simple electronic signature, not a qualified one.
