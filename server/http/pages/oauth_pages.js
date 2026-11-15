'use strict';

// The pages a person sees while connecting an app (NeoAgent) to NeoCloud:
// sign in, the 2FA code, the consent page and an error page. Plain HTML, no
// script, NeoCloud's palette and logo, English or German (du) by the
// browser's Accept-Language. Every dynamic value is escaped.

const BRAND = 'NeoCloud';

const STRINGS = {
  en: {
    eyebrow: 'Connect an app',
    signInTitle: 'Sign in to connect NeoAgent',
    signInIntro: 'Use your NeoCloud account. Your password stays with NeoCloud; NeoAgent never sees it.',
    account: 'Username or email',
    password: 'Password',
    continue: 'Continue',
    codeTitle: 'Enter your code',
    codeIntro: 'Enter the 6-digit code from your authenticator app, or one of your recovery codes.',
    code: 'Code',
    verify: 'Verify',
    back: 'Back',
    consentTitle: 'Connect NeoAgent?',
    consentIntro: 'NeoAgent at {host} would like to work with your NeoCloud. You can change or remove this at any time in Settings.',
    signedInAs: 'Signed in as {user}',
    files: 'Files',
    filesRead: 'Read only',
    filesReadHint: 'See, open and search your files.',
    filesWrite: 'Read & write',
    filesWriteHint: 'Also create, change, move and put files in the trash. Nothing is deleted for good.',
    signatures: 'Signatures',
    signaturesToggle: 'Create and send signature requests',
    signaturesHint: 'Prepare documents for signing and send signing links in your name.',
    signaturesStatus: 'NeoAgent can see how your signature requests are going.',
    vaults: 'Files in your encrypted vaults stay out of reach.',
    allow: 'Allow',
    deny: 'Don’t allow',
    errorTitle: 'This didn’t work',
    errorIntro: 'The connection request is no longer valid. Start connecting again from NeoAgent.',
    errors: {
      INVALID_CREDENTIALS: 'That username or password isn’t right.',
      MISSING_FIELDS: 'Fill in both fields.',
      LOGIN_LOCKED: 'Too many attempts. Try again in a few minutes.',
      ACCOUNT_DISABLED: 'This account is turned off.',
      EMAIL_NOT_CONFIRMED: 'Confirm your email address first, then try again.',
      INVALID_CODE: 'That code isn’t right.',
      TWO_FACTOR_EXPIRED: 'That took too long. Sign in again.',
      TWO_FACTOR_ATTEMPTS_EXCEEDED: 'Too many wrong codes. Sign in again.',
      generic: 'Something went wrong. Try again.',
    },
  },
  de: {
    eyebrow: 'App verbinden',
    signInTitle: 'Melde dich an, um NeoAgent zu verbinden',
    signInIntro: 'Nutze dein NeoCloud-Konto. Dein Passwort bleibt bei NeoCloud; NeoAgent sieht es nie.',
    account: 'Benutzername oder E-Mail',
    password: 'Passwort',
    continue: 'Weiter',
    codeTitle: 'Gib deinen Code ein',
    codeIntro: 'Gib den 6-stelligen Code aus deiner Authenticator-App oder einen deiner Wiederherstellungscodes ein.',
    code: 'Code',
    verify: 'Bestätigen',
    back: 'Zurück',
    consentTitle: 'NeoAgent verbinden?',
    consentIntro: 'NeoAgent unter {host} möchte mit deiner NeoCloud arbeiten. Du kannst das jederzeit in den Einstellungen ändern oder entfernen.',
    signedInAs: 'Angemeldet als {user}',
    files: 'Dateien',
    filesRead: 'Nur lesen',
    filesReadHint: 'Deine Dateien sehen, öffnen und durchsuchen.',
    filesWrite: 'Lesen & schreiben',
    filesWriteHint: 'Außerdem Dateien anlegen, ändern, verschieben und in den Papierkorb legen. Endgültig gelöscht wird nichts.',
    signatures: 'Signaturen',
    signaturesToggle: 'Signaturanfragen erstellen und versenden',
    signaturesHint: 'Dokumente zum Unterschreiben vorbereiten und Signatur-Links in deinem Namen versenden.',
    signaturesStatus: 'NeoAgent sieht, wie es um deine Signaturanfragen steht.',
    vaults: 'Dateien in deinen verschlüsselten Tresoren bleiben unerreichbar.',
    allow: 'Erlauben',
    deny: 'Nicht erlauben',
    errorTitle: 'Das hat nicht geklappt',
    errorIntro: 'Die Verbindungsanfrage ist nicht mehr gültig. Starte das Verbinden in NeoAgent noch einmal.',
    errors: {
      INVALID_CREDENTIALS: 'Benutzername oder Passwort stimmen nicht.',
      MISSING_FIELDS: 'Fülle beide Felder aus.',
      LOGIN_LOCKED: 'Zu viele Versuche. Versuch es in ein paar Minuten noch einmal.',
      ACCOUNT_DISABLED: 'Dieses Konto ist deaktiviert.',
      EMAIL_NOT_CONFIRMED: 'Bestätige zuerst deine E-Mail-Adresse und versuch es dann noch einmal.',
      INVALID_CODE: 'Der Code stimmt nicht.',
      TWO_FACTOR_EXPIRED: 'Das hat zu lange gedauert. Melde dich noch einmal an.',
      TWO_FACTOR_ATTEMPTS_EXCEEDED: 'Zu viele falsche Codes. Melde dich noch einmal an.',
      generic: 'Etwas ist schiefgelaufen. Versuch es noch einmal.',
    },
  },
};

const STYLES = `
:root{color-scheme:light dark;--bg:#f4f1e8;--card:#fdfcf8;--ink:#1c2117;--ink-2:#49503f;--ink-3:#7e8470;--line:rgba(28,33,23,.10);--line-2:rgba(28,33,23,.16);--gold:#b07d2b;--gold-ink:#8a5f1c;--gold-muted:rgba(176,125,43,.14);--btn:#1c2117;--btn-ink:#fdfcf8;--btn-hover:#2a3226;--danger:#ae473c;--shadow:0 18px 48px -24px rgba(28,33,23,.28),0 8px 20px -12px rgba(28,33,23,.12);--glow:rgba(176,125,43,.18)}
@media (prefers-color-scheme:dark){:root{--bg:#0e1511;--card:#171f1a;--ink:#ecefe5;--ink-2:#aeb7a6;--ink-3:#7e8877;--line:rgba(224,240,224,.08);--line-2:rgba(224,240,224,.14);--gold:#e1b052;--gold-ink:#eac272;--gold-muted:rgba(225,176,82,.14);--btn:#e1b052;--btn-ink:#0e1511;--btn-hover:#eac272;--danger:#de8a78;--shadow:0 24px 60px -28px rgba(0,0,0,.65),0 10px 24px -16px rgba(0,0,0,.35);--glow:rgba(225,176,82,.16)}}
*,*::before,*::after{box-sizing:border-box}
html{-webkit-font-smoothing:antialiased}
body{margin:0;min-height:100vh;background:var(--bg);color:var(--ink);font:440 16px/1.5 Inter,ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;background-image:radial-gradient(ellipse 60% 40% at 50% 0,var(--glow),transparent 70%);background-repeat:no-repeat}
main{min-height:100vh;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:32px 16px}
.brand{display:flex;align-items:center;gap:10px;width:100%;max-width:480px;padding:0 6px 18px;font-weight:700;font-size:17px;letter-spacing:-.03em}
.brand img{width:28px;height:28px}
.card{width:100%;max-width:480px;background:var(--card);border:1px solid var(--line);border-radius:26px;padding:34px 32px 32px;box-shadow:var(--shadow)}
.eyebrow{display:flex;align-items:center;gap:9px;font:600 11px/1 "IBM Plex Mono",ui-monospace,Menlo,monospace;letter-spacing:.16em;text-transform:uppercase;color:var(--gold-ink)}
.dot{width:6px;height:6px;border-radius:99px;background:var(--gold);box-shadow:0 0 0 4px var(--gold-muted)}
h1{margin:18px 0 0;font:560 28px/1.15 Fraunces,"Iowan Old Style",Georgia,serif;letter-spacing:-.015em}
p{margin:12px 0 0;color:var(--ink-2);font-size:15px;line-height:1.6}
.muted{color:var(--ink-3);font-size:13px}
form{margin:24px 0 0}
label.input{display:block;margin:16px 0 7px;font-size:13px;font-weight:600}
input[type=password],input[type=text]{width:100%;padding:13px 14px;border:1px solid var(--line-2);border-radius:14px;background:transparent;color:var(--ink);font:inherit;font-size:15px}
input:focus{outline:none;border-color:var(--gold);box-shadow:0 0 0 4px var(--gold-muted)}
fieldset{margin:20px 0 0;padding:0;border:0}
legend{padding:0;font-size:13px;font-weight:600}
.choice{display:flex;gap:12px;align-items:flex-start;margin-top:10px;padding:13px 14px;border:1px solid var(--line-2);border-radius:14px;cursor:pointer}
.choice input{margin:4px 0 0;accent-color:var(--gold)}
.choice b{display:block;font-size:14.5px;font-weight:600}
.choice span{display:block;color:var(--ink-3);font-size:13px;line-height:1.5;margin-top:2px}
.choice:has(input:checked){border-color:var(--gold);box-shadow:0 0 0 3px var(--gold-muted)}
.error{margin:18px 0 0;padding:11px 14px;border:1px solid color-mix(in srgb,var(--danger) 40%,transparent);border-radius:14px;color:var(--danger);font-size:13.5px}
.actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:26px}
.btn{flex:1;display:inline-flex;align-items:center;justify-content:center;padding:13px 22px;border:1px solid transparent;border-radius:999px;background:var(--btn);color:var(--btn-ink);font:600 14.5px Inter,system-ui,sans-serif;cursor:pointer;text-decoration:none}
.btn:hover{background:var(--btn-hover)}
.btn.secondary{background:transparent;color:var(--ink);border-color:var(--line-2)}
.btn:focus-visible{outline:2px solid var(--gold);outline-offset:3px}
a.back{display:inline-block;margin-top:14px;color:var(--gold-ink);font-size:13px;font-weight:600;text-decoration:none}
@media (max-width:420px){.card{padding:28px 22px 26px;border-radius:22px}h1{font-size:24px}}
`;

function escapeHtml(value) {
  return String(value ?? '').replace(/[&<>"']/g, (char) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  })[char]);
}

/** 'de' when the browser prefers German, else 'en'. */
function languageFor(req) {
  const first = String(req.get('accept-language') || '').split(',')[0].trim().toLowerCase();
  return first.startsWith('de') ? 'de' : 'en';
}

function fill(text, values = {}) {
  return text.replace(/\{(\w+)\}/g, (_, key) => escapeHtml(values[key]));
}

function errorText(lang, code) {
  const errors = STRINGS[lang].errors;
  return errors[code] || errors.generic;
}

// Browsers apply form-action to the whole redirect chain, so the consent
// form's redirect to the app's callback must be allowed too.
function formAction(redirectUri) {
  try {
    return redirectUri ? `'self' ${new URL(redirectUri).origin}` : "'self'";
  } catch {
    return "'self'";
  }
}

function sendPage(res, html, { redirectUri = '' } = {}) {
  res.set({
    'Cache-Control': 'no-store, max-age=0',
    'Content-Security-Policy': `default-src 'none'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; form-action ${formAction(redirectUri)}; base-uri 'none'; frame-ancestors 'none'`,
    // Not no-referrer: browsers then send `Origin: null` on form posts, which the
    // origin check rightly refuses. same-origin keeps the URL from other sites.
    'Referrer-Policy': 'same-origin',
    'X-Robots-Tag': 'noindex, nofollow',
  });
  return res.type('html').send(html);
}

function shell(lang, title, content) {
  return `<!doctype html>
<html lang="${lang}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark">
<meta name="robots" content="noindex,nofollow">
<title>${escapeHtml(title)} · ${BRAND}</title>
<link rel="icon" href="/assets/favicon.png">
<link rel="stylesheet" href="/css/fonts.css">
<style>${STYLES}</style>
</head>
<body>
<main>
<div class="brand"><img src="/assets/logo.svg" alt="" width="28" height="28"><span>${BRAND}</span></div>
<section class="card">
<div class="eyebrow"><span class="dot"></span>${escapeHtml(STRINGS[lang].eyebrow)}</div>
${content}
</section>
</main>
</body>
</html>`;
}

const hidden = (name, value) => `<input type="hidden" name="${name}" value="${escapeHtml(value)}">`;
const errorBox = (lang, code) => (code ? `<div class="error" role="alert">${escapeHtml(errorText(lang, code))}</div>` : '');

function renderSignIn({ lang, continuePath, account = '', error = null }) {
  const s = STRINGS[lang];
  return shell(lang, s.signInTitle, `<h1>${escapeHtml(s.signInTitle)}</h1>
<p>${escapeHtml(s.signInIntro)}</p>
${errorBox(lang, error)}
<form method="post" action="/oauth/sign-in">
${hidden('continue', continuePath)}
<label class="input" for="account">${escapeHtml(s.account)}</label>
<input id="account" name="account" type="text" autocomplete="username" value="${escapeHtml(account)}" required${account ? '' : ' autofocus'}>
<label class="input" for="password">${escapeHtml(s.password)}</label>
<input id="password" name="password" type="password" autocomplete="current-password" required${account ? ' autofocus' : ''}>
<div class="actions"><button class="btn" type="submit">${escapeHtml(s.continue)}</button></div>
</form>`);
}

function renderTwoFactor({ lang, continuePath, error = null }) {
  const s = STRINGS[lang];
  return shell(lang, s.codeTitle, `<h1>${escapeHtml(s.codeTitle)}</h1>
<p>${escapeHtml(s.codeIntro)}</p>
${errorBox(lang, error)}
<form method="post" action="/oauth/sign-in">
${hidden('continue', continuePath)}
<label class="input" for="code">${escapeHtml(s.code)}</label>
<input id="code" name="code" type="text" inputmode="text" autocomplete="one-time-code" maxlength="32" spellcheck="false" autocapitalize="none" required autofocus>
<div class="actions"><button class="btn" type="submit">${escapeHtml(s.verify)}</button></div>
</form>
<a class="back" href="/oauth/sign-in?restart=1&amp;continue=${encodeURIComponent(continuePath)}">&larr; ${escapeHtml(s.back)}</a>`);
}

function renderConsent({ lang, authorize, username, consentToken }) {
  const s = STRINGS[lang];
  const { scopes } = authorize;
  const offersFiles = scopes.includes('files:read') || scopes.includes('files:write');
  const offersWrite = scopes.includes('files:write');
  const offersSigning = scopes.includes('signing:write');
  const request = {
    response_type: 'code',
    client_id: authorize.client.id,
    redirect_uri: authorize.redirectUri,
    state: authorize.state,
    scope: scopes.join(' '),
    code_challenge: authorize.codeChallenge,
    code_challenge_method: 'S256',
  };
  const files = offersFiles ? `<fieldset><legend>${escapeHtml(s.files)}</legend>
<label class="choice"><input type="radio" name="files" value="read" checked><div><b>${escapeHtml(s.filesRead)}</b><span>${escapeHtml(s.filesReadHint)}</span></div></label>
${offersWrite ? `<label class="choice"><input type="radio" name="files" value="write"><div><b>${escapeHtml(s.filesWrite)}</b><span>${escapeHtml(s.filesWriteHint)}</span></div></label>` : ''}
</fieldset>` : '';
  const signing = offersSigning ? `<fieldset><legend>${escapeHtml(s.signatures)}</legend>
<label class="choice"><input type="checkbox" name="signing" value="on"><div><b>${escapeHtml(s.signaturesToggle)}</b><span>${escapeHtml(s.signaturesHint)}</span></div></label>
</fieldset>` : '';
  const signingStatus = scopes.includes('signing:read') ? `<p class="muted">${escapeHtml(s.signaturesStatus)}</p>` : '';
  const host = new URL(authorize.redirectUri).host;
  return shell(lang, s.consentTitle, `<h1>${escapeHtml(s.consentTitle)}</h1>
<p>${fill(s.consentIntro, { host })}</p>
<p class="muted">${fill(s.signedInAs, { user: username })}</p>
<form method="post" action="/oauth/authorize">
${Object.entries(request).map(([name, value]) => hidden(name, value)).join('\n')}
${hidden('consent', consentToken)}
${files}
${signing}
${signingStatus}
${offersFiles ? `<p class="muted">${escapeHtml(s.vaults)}</p>` : ''}
<div class="actions">
<button class="btn" type="submit" name="decision" value="approve">${escapeHtml(s.allow)}</button>
<button class="btn secondary" type="submit" name="decision" value="deny">${escapeHtml(s.deny)}</button>
</div>
</form>`);
}

function renderError({ lang }) {
  const s = STRINGS[lang];
  return shell(lang, s.errorTitle, `<h1>${escapeHtml(s.errorTitle)}</h1>
<p>${escapeHtml(s.errorIntro)}</p>`);
}

module.exports = { languageFor, renderConsent, renderError, renderSignIn, renderTwoFactor, sendPage };
