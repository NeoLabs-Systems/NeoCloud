'use strict';

// The two small server-rendered pages a mail link opens: "confirm your email"
// and "choose a new password". They are plain HTML (no Flutter, no login):
// the link's token is the credential. NeoCloud's palette and type from
// landing/css/styles.css, light and dark by the visitor's setting, fonts from
// the landing page's own /css/fonts.css. All dynamic text is escaped.
//
// Pages carry a token in their address, so they are never cached or indexed
// and their CSP allows exactly one inline script (by hash) and nothing from
// other hosts. The referrer policy is strict-origin, not no-referrer: with
// no-referrer browsers send "Origin: null" on the form post, which the CSRF
// check (http/middleware.js) refuses.

const crypto = require('crypto');
const { MIN_PASSWORD_LENGTH } = require('../../services/account/password_policy');
const { escapeHtml } = require('../../services/account/email_templates');
const { t } = require('../../services/account/email_i18n');

// Strength meter and show/hide for the reset form. Indicative only: the
// server's policy (password_policy.js) decides.
const RESET_SCRIPT = `
(function () {
  var form = document.getElementById('reset');
  if (!form) return;
  var pw = form.elements.password, again = form.elements.confirmPassword;
  var bar = document.getElementById('meter'), mismatch = document.getElementById('mismatch');
  function score(v) {
    var s = 0;
    if (v.length >= 8) s++;
    if (v.length >= 12) s++;
    var kinds = [/[a-z]/, /[A-Z]/, /[0-9]/, /[^A-Za-z0-9]/].filter(function (r) { return r.test(v); }).length;
    if (kinds >= 3) s++;
    if (kinds === 4 || v.length >= 16) s++;
    return s;
  }
  pw.addEventListener('input', function () { bar.setAttribute('data-score', String(pw.value ? score(pw.value) : 0)); });
  function check() { mismatch.hidden = !again.value || again.value === pw.value; }
  again.addEventListener('input', check);
  pw.addEventListener('input', check);
  form.addEventListener('submit', function (e) { if (again.value !== pw.value) { e.preventDefault(); mismatch.hidden = false; again.focus(); } });
  var toggle = document.getElementById('toggle');
  toggle.hidden = false;
  toggle.addEventListener('click', function () {
    var show = pw.type === 'password';
    pw.type = again.type = show ? 'text' : 'password';
    toggle.textContent = toggle.getAttribute(show ? 'data-hide' : 'data-show');
  });
})();`;

const RESET_SCRIPT_HASH = `'sha256-${crypto.createHash('sha256').update(RESET_SCRIPT).digest('base64')}'`;

const PAGE_CSP = [
  "default-src 'none'",
  "style-src 'self' 'unsafe-inline'",
  `script-src ${RESET_SCRIPT_HASH}`,
  "font-src 'self'",
  "img-src 'self' data:",
  "form-action 'self'",
  "base-uri 'none'",
  "frame-ancestors 'none'",
].join('; ');

const STYLES = `
:root{color-scheme:light dark;--bg:#f4f1e8;--card:#fdfcf8;--ink:#1c2117;--ink-2:#49503f;--ink-3:#7e8470;--line:rgba(28,33,23,.10);--line-2:rgba(28,33,23,.16);--gold:#b07d2b;--gold-ink:#8a5f1c;--gold-muted:rgba(176,125,43,.14);--btn:#1c2117;--btn-ink:#fdfcf8;--btn-hover:#2a3226;--danger:#ae473c;--ok:#527c4f;--shadow:0 18px 48px -24px rgba(28,33,23,.28),0 8px 20px -12px rgba(28,33,23,.12);--glow:rgba(176,125,43,.18)}
@media (prefers-color-scheme:dark){:root{--bg:#0e1511;--card:#171f1a;--ink:#ecefe5;--ink-2:#aeb7a6;--ink-3:#7e8877;--line:rgba(224,240,224,.08);--line-2:rgba(224,240,224,.14);--gold:#e1b052;--gold-ink:#eac272;--gold-muted:rgba(225,176,82,.14);--btn:#e1b052;--btn-ink:#0e1511;--btn-hover:#eac272;--danger:#de8a78;--ok:#74c07c;--shadow:0 24px 60px -28px rgba(0,0,0,.65),0 10px 24px -16px rgba(0,0,0,.35);--glow:rgba(225,176,82,.16)}}
*,*::before,*::after{box-sizing:border-box}
html{-webkit-font-smoothing:antialiased;text-rendering:optimizeLegibility}
body{margin:0;min-height:100vh;background:var(--bg);color:var(--ink);font:440 16px/1.5 Inter,ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;background-image:radial-gradient(ellipse 60% 40% at 50% 0,var(--glow),transparent 70%);background-repeat:no-repeat}
main{min-height:100vh;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:32px 16px}
.brand{display:flex;align-items:center;gap:10px;width:100%;max-width:460px;padding:0 6px 18px;font-weight:700;font-size:17px;letter-spacing:-.03em}
.brand img{width:28px;height:28px;border-radius:8px}
.card{width:100%;max-width:460px;background:var(--card);border:1px solid var(--line);border-radius:26px;padding:34px 32px 32px;box-shadow:var(--shadow)}
.eyebrow{display:flex;align-items:center;gap:9px;font:600 11px/1 "IBM Plex Mono",ui-monospace,SFMono-Regular,Menlo,monospace;letter-spacing:.16em;text-transform:uppercase;color:var(--gold-ink)}
.dot{width:6px;height:6px;border-radius:99px;background:var(--gold);box-shadow:0 0 0 4px var(--gold-muted)}
.bad .dot{background:var(--danger);box-shadow:0 0 0 4px color-mix(in srgb,var(--danger) 16%,transparent)}
.bad .eyebrow{color:var(--danger)}
h1{margin:18px 0 0;font:560 30px/1.1 Fraunces,"Iowan Old Style",Georgia,serif;letter-spacing:-.015em}
p{margin:14px 0 0;color:var(--ink-2);font-size:15.5px;line-height:1.65}
form{margin:26px 0 0}
label{display:block;margin:18px 0 7px;font-size:13px;font-weight:600;color:var(--ink)}
label:first-of-type{margin-top:0}
.field{position:relative}
input[type=password],input[type=text]{width:100%;padding:13px 14px;border:1px solid var(--line-2);border-radius:14px;background:transparent;color:var(--ink);font:inherit;font-size:15px}
input:focus{outline:none;border-color:var(--gold);box-shadow:0 0 0 4px var(--gold-muted)}
.meter{display:flex;gap:5px;margin-top:10px}
.meter i{flex:1;height:4px;border-radius:99px;background:var(--line-2)}
.meter[data-score="1"] i:nth-child(-n+1),.meter[data-score="2"] i:nth-child(-n+2),.meter[data-score="3"] i:nth-child(-n+3),.meter[data-score="4"] i:nth-child(-n+4){background:var(--gold)}
.hint{margin:8px 0 0;font-size:12.5px;color:var(--ink-3);line-height:1.55}
.error{margin:18px 0 0;padding:11px 14px;border:1px solid color-mix(in srgb,var(--danger) 40%,transparent);border-radius:14px;color:var(--danger);font-size:13.5px;line-height:1.5}
.error[hidden]{display:none}
.btn{display:inline-flex;align-items:center;justify-content:center;gap:8px;margin-top:26px;padding:13px 24px;border:0;border-radius:999px;background:var(--btn);color:var(--btn-ink);font:600 14.5px Inter,system-ui,sans-serif;letter-spacing:-.01em;text-decoration:none;cursor:pointer;transition:background .2s}
.btn:hover{background:var(--btn-hover)}
.btn:focus-visible{outline:2px solid var(--gold);outline-offset:3px}
form .btn{width:100%}
.link{margin-top:14px;background:none;border:0;padding:0;color:var(--gold-ink);font:inherit;font-size:13px;font-weight:600;cursor:pointer}
.link[hidden]{display:none}
@media (max-width:420px){.card{padding:28px 22px 26px;border-radius:22px}h1{font-size:26px}}
`;

function shell({ lang, brand, title, tone = 'ok', content }) {
  return `<!doctype html>
<html lang="${escapeHtml(lang)}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark">
<meta name="robots" content="noindex,nofollow">
<title>${escapeHtml(title)} · ${escapeHtml(brand)}</title>
<link rel="icon" href="/assets/favicon.png">
<link rel="stylesheet" href="/css/fonts.css">
<style>${STYLES}</style>
</head>
<body>
<main class="${tone === 'bad' ? 'bad' : ''}">
<div class="brand"><img src="/assets/logo.svg" alt="" width="28" height="28"><span>${escapeHtml(brand)}</span></div>
<section class="card">
${content}
</section>
</main>
</body>
</html>`;
}

function card({ eyebrow, title, intro, extra = '' }) {
  return `<div class="eyebrow"><span class="dot"></span>${escapeHtml(eyebrow)}</div>
<h1>${escapeHtml(title)}</h1>
<p>${escapeHtml(intro)}</p>
${extra}`;
}

function openAppButton(lang, brand) {
  return `<a class="btn" href="/app">${escapeHtml(t(lang, 'page.openApp', { brand }))} &rarr;</a>`;
}

/**
 * state: 'ready' (token still good: shows the confirm button), 'done',
 * 'changed' (an email change went through), 'failed', 'missing', 'taken'.
 */
function renderConfirmPage({ state, token = '', lang = 'en', brand = 'NeoCloud' }) {
  const eyebrow = t(lang, 'mail.eyebrow');
  const p = { brand };
  let body;
  let tone = 'ok';
  let pageTitle = t(lang, 'page.confirm.title');
  if (state === 'ready') {
    const form = `<form method="post" action="/api/auth/email/confirm">
<input type="hidden" name="token" value="${escapeHtml(token)}">
<button class="btn" type="submit">${escapeHtml(t(lang, 'page.confirm.button'))} &rarr;</button>
</form>`;
    body = card({ eyebrow, title: t(lang, 'page.confirm.title'), intro: t(lang, 'page.confirm.intro', p), extra: form });
  } else if (state === 'done' || state === 'changed') {
    const intro = t(lang, state === 'changed' ? 'page.confirmedChange.intro' : 'page.confirmed.intro', p);
    body = card({ eyebrow, title: t(lang, 'page.confirmed.title'), intro, extra: openAppButton(lang, brand) });
  } else {
    tone = 'bad';
    pageTitle = t(lang, state === 'missing' ? 'page.confirmMissing.title' : 'page.confirmFailed.title');
    const key = {
      taken: 'page.emailTaken.intro',
      missing: 'page.confirmMissing.intro',
      toomany: 'page.resetTooMany.intro',
    }[state] || 'page.confirmFailed.intro';
    const title = state === 'missing' ? 'page.confirmMissing.title' : 'page.confirmFailed.title';
    body = card({ eyebrow, title: t(lang, title), intro: t(lang, key, p), extra: openAppButton(lang, brand) });
  }
  return shell({ lang, brand, title: pageTitle, tone, content: body });
}

/** state: 'form' (optionally with `error`), 'done', 'failed', 'missing', 'toomany'. */
function renderResetPage({ state, token = '', error = '', lang = 'en', brand = 'NeoCloud' }) {
  const eyebrow = t(lang, 'mail.eyebrow');
  const p = { brand };
  let body;
  let tone = 'ok';
  let pageTitle = t(lang, 'page.reset.title');
  if (state === 'form') {
    const form = `<form id="reset" method="post" action="/api/auth/password/reset" autocomplete="off">
<input type="hidden" name="token" value="${escapeHtml(token)}">
<label for="password">${escapeHtml(t(lang, 'page.reset.password'))}</label>
<div class="field"><input id="password" name="password" type="password" autocomplete="new-password" minlength="${MIN_PASSWORD_LENGTH}" required autofocus></div>
<div class="meter" id="meter" data-score="0"><i></i><i></i><i></i><i></i></div>
<div class="hint">${escapeHtml(t(lang, 'page.reset.hintMin', { min: MIN_PASSWORD_LENGTH }))}</div>
<label for="confirmPassword">${escapeHtml(t(lang, 'page.reset.confirm'))}</label>
<div class="field"><input id="confirmPassword" name="confirmPassword" type="password" autocomplete="new-password" minlength="${MIN_PASSWORD_LENGTH}" required></div>
<div class="error" id="mismatch" role="alert" hidden>${escapeHtml(t(lang, 'page.reset.mismatch'))}</div>
${error ? `<div class="error" role="alert">${escapeHtml(error)}</div>` : ''}
<button class="btn" type="submit">${escapeHtml(t(lang, 'page.reset.submit'))} &rarr;</button>
<button class="link" id="toggle" type="button" hidden data-show="${escapeHtml(t(lang, 'page.reset.show'))}" data-hide="${escapeHtml(t(lang, 'page.reset.hide'))}">${escapeHtml(t(lang, 'page.reset.show'))}</button>
</form>
<script>${RESET_SCRIPT}</script>`;
    body = card({ eyebrow, title: t(lang, 'page.reset.title'), intro: t(lang, 'page.reset.intro', p), extra: form });
  } else if (state === 'done') {
    body = card({ eyebrow, title: t(lang, 'page.resetDone.title'), intro: t(lang, 'page.resetDone.intro', p), extra: openAppButton(lang, brand) });
  } else {
    tone = 'bad';
    pageTitle = t(lang, 'page.resetFailed.title');
    const key = { missing: 'page.resetMissing.intro', toomany: 'page.resetTooMany.intro' }[state] || 'page.resetFailed.intro';
    body = card({ eyebrow, title: t(lang, 'page.resetFailed.title'), intro: t(lang, key, p), extra: openAppButton(lang, brand) });
  }
  return shell({ lang, brand, title: pageTitle, tone, content: body });
}

/** Headers for a page whose address carries a token. */
function setPageHeaders(res) {
  res.set({
    'Content-Type': 'text/html; charset=utf-8',
    'Cache-Control': 'no-store, max-age=0',
    'Referrer-Policy': 'strict-origin',
    'X-Robots-Tag': 'noindex, nofollow',
    'Content-Security-Policy': PAGE_CSP,
  });
}

module.exports = { renderConfirmPage, renderResetPage, setPageHeaders };
