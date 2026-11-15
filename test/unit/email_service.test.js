'use strict';

// The mail layer without the server: templates, languages, header safety,
// the settings file and the background sender.

const fs = require('fs');
const os = require('os');
const path = require('path');
const test = require('node:test');
const assert = require('node:assert/strict');
const dotenv = require('dotenv');

const home = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-email-unit-'));
process.env.NEOCLOUD_HOME = home;

const { installFakeMail } = require('../helpers/mail');
const service = require('../../server/services/account/service_email');
const { renderEmail, renderText, escapeHtml } = require('../../server/services/account/email_templates');
const i18n = require('../../server/services/account/email_i18n');
const settings = require('../../server/services/account/service_email_settings');

const mail = installFakeMail();

test.after(() => {
  mail.restore();
  mail.reset();
  fs.rmSync(home, { recursive: true, force: true });
});

test.beforeEach(() => mail.reset());

const EVIL = '<script>alert("x")</script> & \'quotes\'';

test('every value in a mail is escaped, and only http(s) links become buttons', () => {
  const message = {
    title: EVIL,
    intro: EVIL,
    paragraphs: [EVIL],
    quote: { label: EVIL, text: EVIL },
    actionUrl: 'https://cloud.example.com/x?a=1&b="2"',
    actionLabel: EVIL,
    details: [{ label: EVIL, value: EVIL }],
    footer: EVIL,
    brandName: EVIL,
    lang: 'en',
  };
  const html = renderEmail(message);
  assert.equal(html.includes('<script>'), false);
  assert.ok(html.includes('&lt;script&gt;alert(&quot;x&quot;)&lt;/script&gt; &amp; &#39;quotes&#39;'));
  assert.ok(html.includes('href="https://cloud.example.com/x?a=1&amp;b=%222%22"'));
  assert.equal(html.match(/href=/g).length, 2, 'button and fallback link');

  for (const actionUrl of ['javascript:alert(1)', 'data:text/html,x', '//evil.example', 'not a url']) {
    const without = renderEmail({ ...message, actionUrl });
    assert.equal(without.includes('href='), false, actionUrl);
    assert.equal(renderText({ ...message, actionUrl }).includes(actionUrl), false, actionUrl);
  }
});

test('the layout has the dark block, the brand and a plain-text twin', () => {
  const message = {
    title: 'Hello', intro: 'Intro', actionUrl: 'https://cloud.example.com/go', actionLabel: 'Go',
    details: [{ label: 'Account', value: 'kim' }], brandName: 'Acme', lang: 'de',
  };
  const html = renderEmail(message);
  assert.match(html, /prefers-color-scheme: dark/);
  assert.match(html, /max-width:560px/);
  assert.match(html, /<html lang="de">/);
  assert.match(html, /Acme/);
  assert.match(html, /Falls die Schaltfläche nicht funktioniert/);
  assert.doesNotMatch(html, /fonts\.googleapis|<img|<link/, 'opening a mail contacts no other server');
  const text = renderText(message);
  assert.match(text, /^Hello\n\nIntro\n\nGo: https:\/\/cloud\.example\.com\/go\n\nAccount: kim\n\n--\n/);
  assert.doesNotMatch(text, /<\w+/);
});

test('English and German have the same strings with the same placeholders', () => {
  const source = fs.readFileSync(path.join(__dirname, '../../server/services/account/email_i18n.js'), 'utf8');
  const sections = source.split(/\n  de: \{/);
  const keys = (text) => [...text.matchAll(/^ {4}'([\w.]+)': (.*),$/gm)].map(([, key, value]) => [key, value]);
  const english = new Map(keys(sections[0]));
  const german = new Map(keys(sections[1]));
  assert.deepEqual([...english.keys()].sort(), [...german.keys()].sort());
  const names = (value) => [...value.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort().join(',');
  for (const [key, value] of english) assert.equal(names(german.get(key)), names(value), key);
});

test('languages: German only when asked first, English otherwise, English for unknown keys', () => {
  assert.equal(i18n.pickLanguage('de-DE,de;q=0.9,en;q=0.8'), 'de');
  assert.equal(i18n.pickLanguage('en-US,de;q=0.8'), 'en');
  assert.equal(i18n.pickLanguage('fr'), 'en');
  assert.equal(i18n.pickLanguage(undefined), 'en');
  assert.equal(i18n.t('de', 'signup.button'), 'E-Mail bestätigen');
  assert.equal(i18n.t('xx', 'signup.button'), 'Confirm email');
  assert.equal(i18n.t('en', 'no.such.key'), 'no.such.key');
  assert.equal(i18n.t('en', 'signup.intro', { brand: 'B', username: 'u' }), 'Confirm this email address to finish creating the B account u.');
  assert.equal(i18n.formatHours('de', 1), '1 Stunde');
  assert.equal(i18n.formatHours('en', 24), '24 hours');
});

test('without a host and a sender nothing is configured and nothing is sent', async () => {
  assert.equal(service.isServiceEmailConfigured(), false);
  assert.deepEqual(service.getEmailConfig().missing, ['NEOCLOUD_EMAIL_SMTP_HOST', 'NEOCLOUD_EMAIL_FROM']);
  assert.deepEqual(await service.sendServiceEmail('a@example.com', { subject: 's', title: 't', intro: 'i' }), { skipped: true, reason: 'disabled' });
  assert.equal(mail.sent.length, 0);
  assert.equal(service.requireSignupEmailConfirmation(), false, 'confirmation needs mail');
  assert.equal(service.requireEmailChangeConfirmation(), false);
  process.env.NEOCLOUD_EMAIL_SMTP_HOST = 'smtp.example.com';
  assert.equal(service.isServiceEmailConfigured(), false, 'a host alone is not enough');
  process.env.NEOCLOUD_EMAIL_FROM = 'cloud@example.com';
  assert.equal(service.isServiceEmailConfigured(), true);
});

test('settings are read on every send and carry sensible defaults', () => {
  mail.configure();
  let config = service.getEmailConfig();
  assert.equal(config.port, 587);
  assert.equal(config.secure, false);
  assert.equal(config.requireTls, true);
  assert.equal(config.rejectUnauthorized, true);
  assert.equal(config.brandName, 'NeoCloud');
  assert.equal(config.tokenTtlHours, 24);
  assert.equal(config.requireSignupConfirmation, true);
  assert.equal(config.notifyNewLogin, true);
  process.env.NEOCLOUD_EMAIL_SMTP_PORT = '465';
  config = service.getEmailConfig();
  assert.equal(config.secure, true);
  assert.equal(config.requireTls, false);
  process.env.NEOCLOUD_EMAIL_SMTP_PORT = 'nonsense';
  assert.equal(service.getEmailConfig().port, 587);
});

test('the link base comes from configuration, never a request', () => {
  const saved = { PUBLIC_URL: process.env.PUBLIC_URL, PORT: process.env.PORT };
  try {
    delete process.env.PUBLIC_URL;
    process.env.PORT = '4000';
    assert.equal(service.getEmailConfig().publicUrl, 'http://localhost:4000');
    process.env.PUBLIC_URL = 'https://main.example.com/';
    assert.equal(service.getEmailConfig().publicUrl, 'https://main.example.com');
    process.env.NEOCLOUD_EMAIL_PUBLIC_URL = 'https://mail.example.com';
    assert.equal(service.getEmailConfig().publicUrl, 'https://mail.example.com');
  } finally {
    for (const [key, value] of Object.entries(saved)) {
      if (value === undefined) delete process.env[key]; else process.env[key] = value;
    }
  }
});

test('a subject cannot add headers, and a recipient must be one plain address', async () => {
  mail.configure();
  await service.sendServiceEmail('kim@example.com', {
    subject: 'Hello\r\nBcc: spy@example.com\r\nX-Evil: 1', title: 'T', intro: 'I',
  });
  const [message] = mail.sent;
  const headers = message.raw.split('\n\n')[0];
  assert.doesNotMatch(headers, /^Bcc:/m);
  assert.doesNotMatch(headers, /^X-Evil:/m);
  assert.match(headers, /^Subject: Hello Bcc: spy@example\.com X-Evil: 1$/m);
  assert.match(headers, /^Auto-Submitted: auto-generated$/m);
  assert.match(headers, /^From: NeoCloud <cloud@example\.com>$/m);

  for (const to of ['a@b.example\r\nBcc: x@y.example', 'a@b.example, c@d.example', 'Name <a@b.example>', '"a b"@c.example', 'a@b.example;c@d.example', '', undefined, 5]) {
    await assert.rejects(() => service.sendServiceEmail(to, { subject: 's', title: 't', intro: 'i' }), (err) => err.code === 'INVALID_EMAIL', JSON.stringify(to));
  }
  assert.equal(mail.sent.length, 1);

  process.env.NEOCLOUD_EMAIL_FROM = 'cloud@example.com\r\nBcc: x@y.example';
  await assert.rejects(() => service.sendServiceEmail('kim@example.com', { subject: 's', title: 't', intro: 'i' }), (err) => err.code === 'EMAIL_CONFIG_INVALID');
  assert.equal(mail.sent.length, 1);
});

test('attachments pass through, and the footer names the support page when set', async () => {
  mail.configure({ NEOCLOUD_EMAIL_REPLY_TO: 'help@example.com' });
  process.env.NEOCLOUD_EMAIL_SUPPORT_URL = 'https://example.com/help';
  await service.sendServiceEmail(['a@example.com', 'b@example.com'], {
    subject: 'Signed', title: 'Done', intro: 'Attached.', lang: 'de',
    attachments: [{ filename: 'signed.pdf', content: Buffer.from('%PDF-1.7 test'), contentType: 'application/pdf' }],
  });
  const [message] = mail.sent;
  assert.deepEqual(message.to, ['a@example.com', 'b@example.com']);
  assert.match(message.raw, /filename="?signed\.pdf/);
  assert.match(message.raw, /Reply-To: help@example\.com/);
  assert.match(message.text, /Hilfe: https:\/\/example\.com\/help/);
});

test('a background send never throws and logs no password', async () => {
  mail.configure({ NEOCLOUD_EMAIL_SMTP_PASS: 'hunter2-hunter2' });
  const logged = [];
  const original = console.warn;
  console.warn = (...args) => logged.push(args.join(' '));
  try {
    mail.failNext(Object.assign(new Error('535 bad login hunter2-hunter2 https://x.example/?token=abc123'), { code: 'EAUTH' }));
    const result = await service.sendBackground('Test mail', () => service.sendServiceEmail('kim@example.com', { subject: 's', title: 't', intro: 'i' }));
    assert.equal(result, undefined);
    await service.sendBackground('Sync failure', () => { throw new Error('boom'); });
  } finally {
    console.warn = original;
  }
  const text = logged.join('\n');
  assert.match(text, /Test mail failed: 535 bad login/);
  assert.match(text, /Sync failure failed: boom/);
  assert.equal(text.includes('hunter2'), false);
  assert.equal(text.includes('abc123'), false);
});

test('values are written to .env in a form dotenv reads back exactly', () => {
  const file = path.join(home, 'roundtrip.env');
  const values = ['plain', 'a#b c', 'with "double"', "it's", 'back\\slash', 'tail\\', 'a\\nb', '$HOME ${X}', '  inner  space', '=equals=', '`tick`', "it's \"both\""];
  for (const value of values) {
    const env = {};
    settings.updateAdminEmailSettings({ smtpPassword: value }, { env, envFile: file });
    assert.equal(dotenv.parse(fs.readFileSync(file, 'utf8')).NEOCLOUD_EMAIL_SMTP_PASS, value.trim(), JSON.stringify(value));
    assert.equal(env.NEOCLOUD_EMAIL_SMTP_PASS, value.trim());
  }
  assert.throws(() => settings.encodeEnvValue('a\'b"c`d'), (err) => err.code === 'INVALID_VALUE');
  assert.equal(settings.encodeEnvValue('plain'), '"plain"');
  // Other lines of the file survive an update.
  fs.writeFileSync(file, 'KEEP=me\n');
  settings.updateAdminEmailSettings({ brandName: 'Acme' }, { env: {}, envFile: file });
  const parsed = dotenv.parse(fs.readFileSync(file, 'utf8'));
  assert.equal(parsed.KEEP, 'me');
  assert.equal(parsed.NEOCLOUD_EMAIL_BRAND_NAME, 'Acme');
});

test('the settings view and the sender agree on what counts as configured', () => {
  const cases = [
    {},
    { NEOCLOUD_EMAIL_FROM: 'a@b.example' },
    { NEOCLOUD_EMAIL_SMTP_HOST: 'smtp.example.com' },
    { NEOCLOUD_EMAIL_FROM: 'a@b.example', NEOCLOUD_EMAIL_SMTP_HOST: 'smtp.example.com' },
  ];
  for (const env of cases) {
    mail.reset();
    Object.assign(process.env, env);
    assert.equal(settings.getAdminEmailSettings({ env: process.env }).configured, service.isServiceEmailConfigured(), JSON.stringify(env));
  }
});

test('escapeHtml leaves nothing active', () => {
  assert.equal(escapeHtml(`<>&"'`), '&lt;&gt;&amp;&quot;&#39;');
  assert.equal(escapeHtml(null), '');
});
