'use strict';

// Admin use cases for service mail: reading and changing the SMTP settings,
// sending a test message, and vouching for a user's address.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { recordAccessEvent } = require('../access/audit');
const { getAdminEmailSettings, updateAdminEmailSettings } = require('../account/service_email_settings');
const { describeSendError, getEmailConfig, sendServiceEmail } = require('../account/service_email');
const { markEmailVerified } = require('../account/email_tokens');
const { t } = require('../account/email_i18n');
const { isValidEmail, normalizeEmail } = require('../account/credentials');

function getEmailSettings() {
  return getAdminEmailSettings();
}

function updateEmailSettings(body, { actorUserId }) {
  const result = updateAdminEmailSettings(body);
  recordAccessEvent('email.config', { actorUserId, detail: { configured: result.configured } });
  return result;
}

/**
 * Sends a test message to `to`, or to the admin's own address. Refuses with
 * 409 when mail is not configured and 422 EMAIL_TEST_FAILED, carrying the
 * transport's error text (never the password), when SMTP says no. The audit
 * entry records only whether it worked.
 */
async function sendTestEmail({ actorUserId, to, lang }) {
  const config = getEmailConfig();
  if (!config.configured) {
    throw httpError(409, 'Set the sender address and SMTP host first.', 'EMAIL_NOT_CONFIGURED');
  }
  const own = db.prepare('SELECT email FROM users WHERE id = ?').get(actorUserId)?.email || '';
  const address = typeof to === 'string' && to.trim() ? normalizeEmail(to) : own;
  if (!address) throw httpError(400, 'Your account has no email address: enter one to send the test to.', 'EMAIL_REQUIRED');
  if (!isValidEmail(address)) throw httpError(400, 'A valid email is required', 'INVALID_EMAIL');
  const params = { brand: config.brandName };
  try {
    await sendServiceEmail(address, {
      lang,
      subject: t(lang, 'test.subject', params),
      title: t(lang, 'test.title', params),
      intro: t(lang, 'test.intro', params),
      details: [
        { label: t(lang, 'mail.label.account'), value: address },
        { label: 'SMTP', value: `${config.host}:${config.port}` },
      ],
    });
  } catch (err) {
    recordAccessEvent('email.test', { actorUserId, detail: { ok: false } });
    throw httpError(422, describeSendError(err, config), 'EMAIL_TEST_FAILED');
  }
  recordAccessEvent('email.test', { actorUserId, detail: { ok: true } });
  return { ok: true, message: `Test email sent to ${address}.` };
}

/** Marks a user's address verified (and lifts a pending sign-up confirmation). */
function verifyUserEmail(userId, { actorUserId }) {
  const user = db.prepare('SELECT id, email FROM users WHERE id = ?').get(userId);
  if (!user) throw httpError(404, 'User not found', 'NOT_FOUND');
  if (!user.email) throw httpError(409, 'This account has no email address.', 'EMAIL_REQUIRED');
  markEmailVerified(userId);
  recordAccessEvent('account.email_verify', { actorUserId, subjectUserId: userId });
  return { success: true };
}

module.exports = { getEmailSettings, sendTestEmail, updateEmailSettings, verifyUserEmail };
