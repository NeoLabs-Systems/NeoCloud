'use strict';

// NeoCloud's service mail layout: a 560px column with the brand wordmark, a
// card with a gold dot and mono eyebrow, a serif headline, a pill button, the
// plain link underneath, a details table and a footer. NeoAgent's mail layout
// in the NeoCloud palette (landing/css/styles.css). Mail clients drop CSS
// variables and most <style> rules, so the light palette is inlined and the
// dark block only applies where prefers-color-scheme works. No web fonts and
// no images are loaded: opening a mail never contacts another server.
//
// A message is plain data; every value is escaped here:
//   { title, intro, paragraphs?, quote?: { label, text }, actionUrl?, actionLabel?,
//     details?: [{ label, value }], footer?, eyebrow?, lang?, brandName? }

const { t } = require('./email_i18n');

const FONTS = {
  sans: "Inter,-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Arial,sans-serif",
  serif: "Fraunces,'Iowan Old Style',Georgia,'Times New Roman',serif",
  mono: "'IBM Plex Mono',SFMono-Regular,Menlo,Consolas,monospace",
};

const DARK_STYLES = `
  :root { color-scheme: light dark; supported-color-schemes: light dark; }
  @media (prefers-color-scheme: dark) {
    .nc-bg { background:#0e1511 !important; }
    .nc-card { background:#171f1a !important; border-color:#263129 !important; }
    .nc-ink { color:#ecefe5 !important; }
    .nc-ink-2 { color:#aeb7a6 !important; }
    .nc-ink-3 { color:#7e8877 !important; }
    .nc-gold { color:#eac272 !important; }
    .nc-dot { background:#e1b052 !important; }
    .nc-line { border-color:#263129 !important; }
    .nc-quote { background:#1c261f !important; border-color:#e1b052 !important; }
    .nc-btn { background:#e1b052 !important; color:#0e1511 !important; }
    .nc-btn a { color:#0e1511 !important; }
  }`;

// C0 controls, DEL, and the Unicode line and paragraph separators.
const CONTROL_CHARACTERS = new RegExp('[\\u0000-\\u001f\\u007f\\u2028\\u2029]+', 'g');

function escapeHtml(value) {
  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/** One line of text: control characters (CR and LF above all) become spaces. */
function singleLine(value) {
  return String(value ?? '').replace(CONTROL_CHARACTERS, ' ').trim();
}

// Only http(s) links become a button; anything else (javascript:, data:) is dropped.
function safeUrl(value) {
  try {
    const url = new URL(String(value || ''));
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.toString() : '';
  } catch {
    return '';
  }
}

function renderDetails(details) {
  if (!details || details.length === 0) return '';
  const rows = details.map((detail, index) => {
    const border = index === 0 ? '' : 'border-top:1px solid #e6e2d6;';
    return `
    <tr>
      <td class="nc-line nc-ink-3" style="${border}padding:12px 12px 12px 0;color:#7e8470;font-family:${FONTS.mono};font-size:11px;font-weight:500;letter-spacing:.12em;text-transform:uppercase;vertical-align:top;white-space:nowrap;">${escapeHtml(detail.label)}</td>
      <td class="nc-line nc-ink" style="${border}padding:12px 0;color:#1c2117;font-size:13.5px;font-weight:600;text-align:right;word-break:break-word;">${escapeHtml(detail.value)}</td>
    </tr>`;
  }).join('');
  return `
    <table role="presentation" class="nc-line" width="100%" cellspacing="0" cellpadding="0" style="margin-top:28px;border-top:1px solid #e6e2d6;border-bottom:1px solid #e6e2d6;">
      ${rows}
    </table>`;
}

function renderAction(url, label, lang) {
  if (!url || !label) return '';
  const href = escapeHtml(url);
  return `
    <table role="presentation" cellspacing="0" cellpadding="0" style="margin-top:28px;">
      <tr>
        <td class="nc-btn" style="border-radius:999px;background:#1c2117;">
          <a href="${href}" class="nc-btn" style="display:inline-block;padding:13px 22px;border-radius:999px;background:#1c2117;color:#fdfcf8;font-family:${FONTS.sans};font-size:14.5px;font-weight:600;letter-spacing:-0.01em;text-decoration:none;">${escapeHtml(label)} &rarr;</a>
        </td>
      </tr>
    </table>
    <p class="nc-ink-3" style="margin:18px 0 0;color:#7e8470;font-size:12px;line-height:1.6;">${escapeHtml(t(lang, 'mail.fallback'))}<br><a href="${href}" class="nc-gold" style="color:#8a5f1c;word-break:break-all;">${href}</a></p>`;
}

// A personal message (a sender's note), set apart from NeoCloud's own words.
function renderQuote(quote) {
  if (!quote || !quote.text) return '';
  const label = quote.label
    ? `<div class="nc-ink-3" style="margin-bottom:6px;color:#7e8470;font-family:${FONTS.mono};font-size:11px;font-weight:500;letter-spacing:.12em;text-transform:uppercase;">${escapeHtml(quote.label)}</div>`
    : '';
  return `
    <div class="nc-quote" style="margin-top:22px;padding:14px 16px;border-left:3px solid #b07d2b;border-radius:6px;background:#f4f1e8;">
      ${label}
      <div class="nc-ink" style="color:#1c2117;font-size:14.5px;line-height:1.6;white-space:pre-wrap;word-break:break-word;">${escapeHtml(quote.text)}</div>
    </div>`;
}

function renderParagraphs(paragraphs) {
  return (paragraphs || []).map((text) => `
                <p class="nc-ink-2" style="margin:14px 0 0;color:#49503f;font-size:15.5px;line-height:1.65;">${escapeHtml(text)}</p>`).join('');
}

function renderEmail(message) {
  const lang = message.lang || 'en';
  const brand = message.brandName || 'NeoCloud';
  const title = escapeHtml(message.title);
  const eyebrow = escapeHtml(message.eyebrow || t(lang, 'mail.eyebrow'));
  const footer = message.footer || t(lang, 'mail.footer', { brand });
  return `<!doctype html>
<html lang="${escapeHtml(lang)}">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width,initial-scale=1">
    <meta name="color-scheme" content="light dark">
    <meta name="supported-color-schemes" content="light dark">
    <title>${title}</title>
    <style>${DARK_STYLES}
    </style>
  </head>
  <body class="nc-bg" style="margin:0;padding:0;background:#f4f1e8;font-family:${FONTS.sans};color:#1c2117;-webkit-font-smoothing:antialiased;">
    <div style="display:none;max-height:0;overflow:hidden;color:transparent;">${title}</div>
    <table role="presentation" class="nc-bg" width="100%" cellspacing="0" cellpadding="0" style="background:#f4f1e8;padding:40px 16px;">
      <tr>
        <td align="center">
          <table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:560px;">
            <tr>
              <td style="padding:0 6px 18px;">
                <span class="nc-ink" style="color:#1c2117;font-family:${FONTS.sans};font-size:17px;font-weight:700;letter-spacing:-0.03em;">${escapeHtml(brand)}</span>
              </td>
            </tr>
          </table>
          <table role="presentation" class="nc-card" width="100%" cellspacing="0" cellpadding="0" style="max-width:560px;background:#fdfcf8;border:1px solid #e6e2d6;border-radius:26px;box-shadow:0 18px 48px -24px rgba(28,33,23,0.28),0 8px 20px -12px rgba(28,33,23,0.12);">
            <tr>
              <td style="padding:34px 32px 30px;">
                <table role="presentation" cellspacing="0" cellpadding="0">
                  <tr>
                    <td style="padding-right:9px;vertical-align:middle;"><div class="nc-dot" style="width:6px;height:6px;border-radius:999px;background:#b07d2b;box-shadow:0 0 0 4px rgba(176,125,43,0.14);"></div></td>
                    <td class="nc-gold" style="vertical-align:middle;color:#8a5f1c;font-family:${FONTS.mono};font-size:11px;font-weight:600;letter-spacing:.16em;text-transform:uppercase;">${eyebrow}</td>
                  </tr>
                </table>
                <h1 class="nc-ink" style="margin:18px 0 0;color:#1c2117;font-family:${FONTS.serif};font-size:30px;font-weight:560;line-height:1.1;letter-spacing:-0.015em;">${title}</h1>
                <p class="nc-ink-2" style="margin:14px 0 0;color:#49503f;font-size:15.5px;line-height:1.65;">${escapeHtml(message.intro)}</p>${renderParagraphs(message.paragraphs)}${renderQuote(message.quote)}${renderAction(safeUrl(message.actionUrl), message.actionLabel, lang)}${renderDetails(message.details)}
              </td>
            </tr>
          </table>
          <table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:560px;">
            <tr>
              <td style="padding:20px 6px 0;">
                <p class="nc-ink-3" style="margin:0;color:#7e8470;font-size:12px;line-height:1.6;">${escapeHtml(footer)}</p>
              </td>
            </tr>
          </table>
        </td>
      </tr>
    </table>
  </body>
</html>`;
}

/** The plain-text twin of renderEmail, for clients that show no HTML. */
function renderText(message) {
  const lang = message.lang || 'en';
  const brand = message.brandName || 'NeoCloud';
  const lines = [message.title, '', message.intro];
  for (const text of message.paragraphs || []) lines.push('', text);
  if (message.quote?.text) {
    lines.push('', ...(message.quote.label ? [`${message.quote.label}:`] : []), ...String(message.quote.text).split('\n').map((line) => `> ${line}`));
  }
  const url = safeUrl(message.actionUrl);
  if (url && message.actionLabel) lines.push('', `${message.actionLabel}: ${url}`);
  if (message.details?.length) {
    lines.push('');
    for (const detail of message.details) lines.push(`${detail.label}: ${detail.value}`);
  }
  lines.push('', '--', message.footer || t(lang, 'mail.footer', { brand }));
  return lines.join('\n');
}

module.exports = { escapeHtml, renderEmail, renderText, singleLine };
