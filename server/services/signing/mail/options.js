'use strict';

// What the owner may choose when sending or reminding: a personal note, mail
// on or off and the automatic reminder interval. Checked here, once.

const { httpError } = require('../../../utils/http_error');
const { LIMITS } = require('../constants');
const { withoutInvisible } = require('../plain_text');

const REMINDER_DAYS = [0, 2, 3, 5, 7, 14];

/** A plain-text note: line breaks stay, other control characters go; null when empty. */
function cleanMessage(value) {
  if (value === undefined || value === null) return null;
  if (typeof value !== 'string') throw httpError(400, 'The message must be text.', 'INVALID_REQUEST');
  const text = withoutInvisible(value).replace(/\r\n?/g, '\n').replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/g, '').trim();
  if (text.length > LIMITS.maxMailMessage) {
    throw httpError(400, `A message has at most ${LIMITS.maxMailMessage} characters.`, 'INVALID_REQUEST');
  }
  return text || null;
}

/** { message, deliverEmail, reminderDays } of a send request; deliverEmail is true unless the body says false. */
function parseSendOptions(body) {
  const input = body && typeof body === 'object' ? body : {};
  if (input.deliverEmail !== undefined && typeof input.deliverEmail !== 'boolean') {
    throw httpError(400, 'deliverEmail must be true or false.', 'INVALID_REQUEST');
  }
  if (input.reminderDays !== undefined && !REMINDER_DAYS.includes(input.reminderDays)) {
    throw httpError(400, `reminderDays must be one of: ${REMINDER_DAYS.join(', ')}.`, 'INVALID_REQUEST');
  }
  return {
    message: cleanMessage(input.message),
    deliverEmail: input.deliverEmail !== false,
    reminderDays: input.reminderDays ?? 0,
  };
}

module.exports = { cleanMessage, parseSendOptions };
