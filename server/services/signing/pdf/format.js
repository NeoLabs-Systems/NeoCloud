'use strict';

// Locale-aware formatting of date and number values for display in a field.

const MONTHS = {
  en: ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'],
  de: ['Januar', 'Februar', 'März', 'April', 'Mai', 'Juni', 'Juli', 'August', 'September', 'Oktober', 'November', 'Dezember'],
};

const DEFAULT_DATE_FORMAT = { en: 'YYYY-MM-DD', de: 'DD.MM.YYYY' };

const pad2 = (n) => String(n).padStart(2, '0');

/**
 * Formats an ISO date ("2026-11-15", optionally with a time part, which is
 * ignored: a date field holds a calendar date, not an instant) with tokens
 * YYYY YY MMMM MMM MM M DD D. Unparseable input is returned unchanged.
 */
function formatDate(value, format, locale) {
  const match = /^(\d{4})-(\d{2})-(\d{2})/.exec(String(value));
  if (!match) return String(value);
  const [, year, month, day] = match;
  const monthName = (MONTHS[locale] || MONTHS.en)[Number(month) - 1] || '';
  const tokens = {
    YYYY: year,
    YY: year.slice(2),
    MMMM: monthName,
    MMM: monthName.slice(0, 3),
    MM: pad2(month),
    M: String(Number(month)),
    DD: pad2(day),
    D: String(Number(day)),
  };
  const pattern = typeof format === 'string' && format ? format : DEFAULT_DATE_FORMAT[locale] || DEFAULT_DATE_FORMAT.en;
  return pattern.replace(/YYYY|YY|MMMM|MMM|MM|M|DD|D/g, (token) => tokens[token]);
}

function groupDigits(integer, separator) {
  return integer.replace(/\B(?=(\d{3})+(?!\d))/g, separator);
}

const NUMBER_STYLES = {
  comma: { group: ',', decimal: '.' },
  dot: { group: '.', decimal: ',' },
  space: { group: ' ', decimal: ',' },
  usd: { group: ',', decimal: '.', prefix: '$' },
  eur: { group: '.', decimal: ',', suffix: ' €' },
  gbp: { group: ',', decimal: '.', prefix: '£' },
};

/** Formats a number per `format` (none, comma, dot, space, usd, eur, gbp). */
function formatNumber(value, format) {
  const text = String(value).trim();
  const style = NUMBER_STYLES[format];
  if (!style || !/^-?\d+(\.\d+)?$/.test(text)) return text;
  const negative = text.startsWith('-');
  const [integer, fraction] = text.replace('-', '').split('.');
  let out = groupDigits(integer, style.group);
  if (fraction !== undefined) out += style.decimal + fraction;
  return `${negative ? '-' : ''}${style.prefix || ''}${out}${style.suffix || ''}`;
}

module.exports = { formatDate, formatNumber };
