'use strict';

// Dates a client sends with an upload: when the photo was taken or the file
// created (X-File-Created) and when it last changed on the device
// (X-File-Modified). Either an ISO 8601 time, ideally with the device's
// offset ("2026-03-12T10:10:10+01:00"), or epoch milliseconds. Kept as
// wall-clock "YYYY-MM-DDTHH:MM:SS" like EXIF dates: an ISO time as written,
// epoch milliseconds in the server's time zone. Anything unreadable, from
// before 2 January 1970 or more than a day ahead is dropped, never an error: the date is a
// hint, the upload matters more.

const ISO_PATTERN = /^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?$/;
const EPOCH_PATTERN = /^-?\d{1,16}$/;
const DAY_MS = 24 * 60 * 60 * 1000;

const pad = (value) => String(value).padStart(2, '0');

function wallClock(date) {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

function inRange(instantMs) {
  // The first day of 1970 too: epoch 0 is how many devices say "unknown".
  return Number.isFinite(instantMs) && instantMs > DAY_MS && instantMs <= Date.now() + DAY_MS;
}

/** "YYYY-MM-DDTHH:MM:SS", or null when the value is missing or not plausible. */
function parseClientDate(value) {
  if (value === undefined || value === null) return null;
  const text = String(value).trim();
  if (!text || text.length > 40) return null;
  if (EPOCH_PATTERN.test(text)) {
    const ms = Number(text);
    return inRange(ms) ? wallClock(new Date(ms)) : null;
  }
  const match = ISO_PATTERN.exec(text);
  if (!match) return null;
  const [, year, month, day, hour, minute, second = '00', zone] = match;
  const fields = [Number(year), Number(month) - 1, Number(day), Number(hour), Number(minute), Number(second)];
  const utc = new Date(Date.UTC(...fields));
  if (utc.getUTCMonth() !== fields[1] || utc.getUTCDate() !== fields[2] || fields[3] > 23 || fields[4] > 59 || fields[5] > 59) return null;
  let instant;
  if (!zone) instant = new Date(...fields).getTime();
  else if (zone === 'Z') instant = utc.getTime();
  else {
    const digits = zone.replace(':', '');
    const offset = (Number(digits.slice(1, 3)) * 60 + Number(digits.slice(3, 5))) * (digits[0] === '-' ? -1 : 1);
    instant = utc.getTime() - offset * 60 * 1000;
  }
  if (!inRange(instant)) return null;
  return `${year}-${month}-${day}T${hour}:${minute}:${second}`;
}

/** Both dates of an upload, from its request headers or body fields. */
function clientDates({ created, modified } = {}) {
  return { createdAt: parseClientDate(created), modifiedAt: parseClientDate(modified) };
}

module.exports = { clientDates, parseClientDate };
