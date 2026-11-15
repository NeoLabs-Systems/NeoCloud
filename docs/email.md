# Service email

NeoCloud can send mail for its own sake: confirm a new account's address,
reset a forgotten password, confirm a changed address, and tell people about
changes to their account. It is **off until an admin sets up an SMTP server**.
While it is off nothing changes: sign-up needs no confirmation and "forgot
password" is not offered (the apps say to ask whoever runs your NeoCloud).
Every screen about mail appears only when the server says it is configured
(`GET /api/auth/status` → `email.configured`).

## Set it up

Admin › Email (the app writes the settings to the server's `.env`, and they
apply at once, no restart), or put the keys below in `.env` yourself. Send a
test message from the same page before relying on it. A server is configured
when both **host** and **sender** are set.

| Key | Default | Meaning |
| --- | --- | --- |
| `NEOCLOUD_EMAIL_FROM` | | Sender, `cloud@example.com` or `NeoCloud <cloud@example.com>` |
| `NEOCLOUD_EMAIL_SMTP_HOST` | | SMTP server |
| `NEOCLOUD_EMAIL_SMTP_PORT` | `587` | |
| `NEOCLOUD_EMAIL_SMTP_USER` / `_PASS` | | Login, if the server wants one. The password is write-only in the admin page |
| `NEOCLOUD_EMAIL_SMTP_SECURE` | `true` on port 465, else `false` | TLS from the first byte ("SMTPS") |
| `NEOCLOUD_EMAIL_SMTP_REQUIRE_TLS` | `true` unless `SECURE` | Refuse to send when STARTTLS is not offered |
| `NEOCLOUD_EMAIL_SMTP_REJECT_UNAUTHORIZED` | `true` | Check the server's certificate. Turn off only for a test server |
| `NEOCLOUD_EMAIL_REPLY_TO` | | Where replies go |
| `NEOCLOUD_EMAIL_BRAND_NAME` | `NeoCloud` | Name in subjects, headlines and pages |
| `NEOCLOUD_EMAIL_SUPPORT_URL` | | Shown in the footer of every mail |
| `NEOCLOUD_EMAIL_PUBLIC_URL` | `PUBLIC_URL`, else `http://localhost:PORT` | Where links in mails point. **Set `PUBLIC_URL` (or this) to the address people use**; the request's `Host` header is never used |
| `NEOCLOUD_EMAIL_TOKEN_TTL_HOURS` | `24` | How long a link lives (1–8760) |
| `NEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION` | `true` | New accounts must confirm their address before they can sign in |
| `NEOCLOUD_EMAIL_REQUIRE_EMAIL_CHANGE_CONFIRMATION` | `true` | A new address applies only after its link is opened |
| `NEOCLOUD_EMAIL_NOTIFY_NEW_LOGIN` | `true` | Mail a sign-in from an address and browser the account has not used |
| `NEOCLOUD_EMAIL_NOTIFY_ACCOUNT_CHANGES` | `true` | Mail password and address changes |
| `NEOCLOUD_EMAIL_SIGNING_MAILS` | `true` | Kill switch for mail to other people (signing invitations, reminders, completion). Off: signing is link-only, what waits in the queue is dropped; account mail is unaffected |

Values the admin page writes are quoted so that `.env` loaders read them back
exactly (a password with `#`, spaces, quotes or backslashes works). A value
holding all of `'`, `"` and `` ` `` at once cannot be stored in a `.env` file
and is refused.

### Examples

Gmail or Google Workspace (create an *app password*; your normal password does
not work, and two-step verification must be on):

```
NEOCLOUD_EMAIL_FROM="NeoCloud <you@gmail.com>"
NEOCLOUD_EMAIL_SMTP_HOST=smtp.gmail.com
NEOCLOUD_EMAIL_SMTP_PORT=587
NEOCLOUD_EMAIL_SMTP_USER=you@gmail.com
NEOCLOUD_EMAIL_SMTP_PASS="abcd efgh ijkl mnop"
```

A transactional relay (Mailgun, Postmark, SES, Brevo, ...): use the SMTP
credentials of the service, port 587, and a sender on a domain you verified
there (SPF and DKIM set up, or mail lands in spam).

```
NEOCLOUD_EMAIL_FROM="NeoCloud <noreply@cloud.example.com>"
NEOCLOUD_EMAIL_SMTP_HOST=smtp.eu.mailgun.org
NEOCLOUD_EMAIL_SMTP_USER=postmaster@cloud.example.com
NEOCLOUD_EMAIL_SMTP_PASS=...
```

A local relay on the same host or network (Postfix, a smarthost) needs no
login: set host and port, and `NEOCLOUD_EMAIL_SMTP_REQUIRE_TLS=false` if it
does not offer STARTTLS.

## What people get

- **Sign-up confirmation.** Registering answers `202` and mails a link. The
  account can't sign in (`403 EMAIL_NOT_CONFIRMED`, only once the password is
  right) until the link is opened and its button pressed. The first account of
  an install is never asked to confirm. If the mail can't be sent the account
  is not created.
- **Forgot password.** "Forgot password?" on the sign-in screen mails a link
  to the reset page. The answer is the same for every name, so the form can't
  be used to find out who has an account. An account whose sign-up is still
  unconfirmed gets nothing.
- **Reset.** The page asks for the new password twice, applies the same
  password rules as sign-up, then ends **every** session, device token and
  WebDAV app password of the account, lifts any sign-in lock, marks the
  address confirmed (reading the mail proved it) and mails a notice.
- **New address.** Settings › Profile asks for the password. The new address
  gets a link, the old one a notice; the address changes when the link is
  opened. Until then the old one still works.
- **Notices** (when switched on): password changed, address change asked for,
  address changed (both addresses), sign-in from a new address or browser
  (a plain comparison of IP and browser with the account's recorded sessions;
  never for the first sign-in, at most one an hour).
- Mails come in German when the browser asked for it first, else English, as an
  HTML and a plain-text part with a dark-mode style. Opening one loads nothing
  from other servers.

A link works once, for `TOKEN_TTL_HOURS`, and a newer link of the same kind
cancels the older. An account gets at most 3 mails of one kind per 15
minutes.

### Accounts made before mail was on

Every account that has an address when mail support is added counts as
confirmed, so switching confirmation on never locks anyone out. An account
made while confirmation was off is not locked out later either; it simply has
no confirmation date (`emailVerifiedAt: null`) and can confirm its address in
Settings › Profile, or an admin can mark it (Admin › Users). If a person never
gets their mail, an admin can do the same for an account that is waiting for
its sign-up link.

## For developers

```js
const { isServiceEmailConfigured, sendServiceEmail, sendBackground } =
  require('../services/account/service_email');

if (isServiceEmailConfigured()) {
  sendBackground('Signing invitation', () => sendServiceEmail('kim@example.com', {
    lang: 'de',                       // 'en' | 'de'
    subject: 'Bitte unterschreiben',
    title: 'Vertrag unterschreiben',
    intro: 'Anna bittet dich, einen Vertrag zu unterschreiben.',
    paragraphs: ['Optional further text.'],
    quote: { label: 'Nachricht', text: 'A personal note.' },
    actionUrl: 'https://cloud.example.com/app/sign/…', actionLabel: 'Prüfen und unterschreiben',
    details: [{ label: 'Dokument', value: 'Vertrag.pdf' }],
    attachments: [{ filename: 'signed.pdf', content: buffer, contentType: 'application/pdf' }],
  }));
}
```

`sendServiceEmail` resolves `{ skipped: true }` when mail is off, throws the
transport's error otherwise, holds the subject and sender to one line and the
recipient to a bare address, and sends attachments by content only (never a
path or URL). `sendBackground` never throws and logs failures without secrets.
Strings live in `services/account/email_i18n.js` (English and German; a test
keeps them in step); the layout is `email_templates.js`.

Tests install a fake transport (`test/helpers/mail.js`) through
`setTransportFactoryForTests`: nothing touches the network.

## Security model

- Links carry 32 random bytes; only the SHA-256 is stored. A link is spent by
  one atomic write, so two clicks can't both win.
- Opening a link in a browser (or a mail scanner doing so) changes nothing:
  the confirmation and reset pages act only on a POST from their button/form.
- Anonymous requests (resend, forgot) answer identically and in the same time
  for every account, send in the background, and are limited per address and
  per account. A sign-in says "email not confirmed" only after the right
  password.
- Links are built from configuration, never from the request.
- The pages are `no-store`, `noindex`, not framable, with a CSP that allows
  only their own inline script (by hash); the referrer policy is
  `strict-origin` (with `no-referrer` browsers send `Origin: null` on the form
  post, which the origin check refuses).
- Tokens and passwords are never logged: requests are logged by path only and
  errors from the SMTP server are shown to admins with the password removed.
- A reset or an admin password reset ends every credential of the account.

## Abuse limits (signing mail)

Open registration plus "mail whoever I name" would make an instance a mail
relay, so signing mail is limited (constants in
`services/signing/mail/limits.js`, kept in `sign_mail_ledger`, which survives
deleting documents):

- the sender needs a **verified** address (`403 EMAIL_NOT_VERIFIED`; links can
  still be copied by hand);
- 100 request mails (invitation, turn, reminder, expiry warning) per account
  and 24 hours, 1000 for an administrator (`429 MAIL_QUOTA_EXCEEDED`);
- 20 addresses per document;
- 3 invitations per 24 hours to one address from the whole instance, whoever
  asks;
- the personal note is plain text, 1000 characters at most, escaped in the mail;
- every mail to a party says who asked for it, that it can be ignored, and where
  to report it (the support URL, else the reply-to or sender address), and
  carries `Auto-Submitted: auto-generated` and `Precedence: bulk`;
- the admin toggle `NEOCLOUD_EMAIL_SIGNING_MAILS` stops all of it at once.

Failed signing mail shows the owner a short sentence by code (`SMTP_UNREACHABLE`,
`SMTP_REJECTED`, `INVALID_ADDRESS`, `RATE_LIMITED`, `UNKNOWN`); the mail
server's own text is only in the server log and in the admin's test message.

The SMTP host is whatever the admin enters, including a machine on the local
network (the same trust as the other admin-configured hosts); only admins can
set it, and the test message is limited to 5 per 15 minutes.

## Troubleshooting

- *Test message fails with `EAUTH`* — wrong user or password; Gmail needs an
  app password. `ECONNECTION`/`ETIMEDOUT` — host or port unreachable from the
  server (many hosts block outgoing port 25; use 587 or 465). `ESOCKET` with a
  certificate message — the server's certificate does not match the host name;
  fix the host, or (test servers only) turn off certificate checking.
  "Greeting never received" on port 465 — turn on *TLS from the start*
  (`SMTP_SECURE`); on 587 leave it off.
- *Mail arrives in spam or not at all* — the sender domain needs SPF and
  DKIM at your provider; send from an address on a domain you control.
- *Links point to `localhost`* — set `PUBLIC_URL` (or
  `NEOCLOUD_EMAIL_PUBLIC_URL`) to the address people open, with `https://`.
- *The confirmation page says the link does not work* — it was used, has
  expired, or a newer one was requested. Sign in (or use "resend") to get a
  new one.
- *A person can't sign in and has no mail* — Admin › Users › mark the address
  as confirmed.
- *Mail only partly set up* — the admin page shows what is missing (sender
  address, SMTP host); until both are set the apps behave as if there were no
  mail.
