'use strict';

// The account's mail flows, built on service_email.js (sending) and
// email_tokens.js (links): the sign-up confirmation, resending it, password
// reset requests, changing the email address, and the notices that tell an
// owner something happened to the account (password changed, address changed,
// sign-in from somewhere new). Routes call these; they hold the rules:
//   - mail that answers an anonymous request never reveals whether an account
//     exists, so those flows return nothing and send in the background;
//   - an account gets at most 3 mails of one kind in 15 minutes;
//   - notices are fire and forget and never break the action they report.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const {
  getEmailConfig,
  requireEmailChangeConfirmation,
  requireSignupEmailConfirmation,
  sendBackground,
  sendServiceEmail,
} = require('./service_email');
const { pickLanguage, t, formatHours } = require('./email_i18n');
const tokens = require('./email_tokens');
const { assertEmailAvailable, findUserForSignIn } = require('./credentials');
const { isAwaitingSignupConfirmation } = require('./status');

const WINDOW_MS = 15 * 60 * 1000;
const MAX_MAILS_PER_WINDOW = 3;
// A visitor who asks for mail waits at least this long, whether or not an
// account matched, so the time taken reveals nothing.
const MIN_ANONYMOUS_LATENCY_MS = 350;
const SIGN_IN_NOTICE_COOLDOWN_MS = 60 * 60 * 1000;
const lastSignInNotice = new Map();

/** The visitor's language for a mail or page: German or English, from Accept-Language. */
function languageFor(req) {
  return pickLanguage(req.get?.('accept-language'));
}

/** What the sign-in screen needs to know about mail (GET /api/auth/status). */
function emailStatus() {
  const config = getEmailConfig();
  return {
    enabled: config.configured,
    configured: config.configured,
    signupConfirmationRequired: requireSignupEmailConfirmation(config),
    emailChangeConfirmationRequired: requireEmailChangeConfirmation(config),
  };
}

/** The 202 answer's text for a new account that must confirm its address. */
function pendingMessage(lang) {
  return t(lang, 'msg.registerPending', { brand: getEmailConfig().brandName });
}

function now() {
  return `${new Date().toISOString().slice(0, 16).replace('T', ' ')} UTC`;
}

function rateLimited() {
  return httpError(429, 'Too many emails were requested for this account. Try again in a few minutes.', 'RATE_LIMITED');
}

function sendFailed() {
  return httpError(503, 'The email could not be sent. Try again later.', 'EMAIL_SEND_FAILED');
}

function tooManyRecent(userId, type) {
  return tokens.recentTokenCount(userId, type, WINDOW_MS) >= MAX_MAILS_PER_WINDOW;
}

// The confirmation mail for an address: kind 'signup', 'emailChange' (a new
// address) or 'verify' (the current one, never confirmed).
function confirmationMessage(kind, { user, email, token, lang }) {
  const config = getEmailConfig();
  const params = { brand: config.brandName, username: user.username };
  const details = [{ label: t(lang, 'mail.label.account'), value: user.username }];
  if (kind === 'emailChange') details.push({ label: t(lang, 'mail.label.newEmail'), value: email });
  details.push({ label: t(lang, 'mail.label.expires'), value: formatHours(lang, config.tokenTtlHours) });
  return {
    lang,
    subject: t(lang, `${kind}.subject`, params),
    title: t(lang, `${kind}.title`, params),
    intro: t(lang, `${kind}.intro`, params),
    actionUrl: tokens.confirmationUrl(token),
    actionLabel: t(lang, `${kind}.button`, params),
    details,
  };
}

/** Sends the sign-up link; throws when SMTP refuses (the caller undoes the account). */
function sendSignupConfirmation(user, token, lang) {
  return sendServiceEmail(user.email, confirmationMessage('signup', { user, email: user.email, token, lang }));
}

/**
 * "Send me the confirmation again" from the sign-in screen. Answers nothing:
 * the caller replies the same way for every account. Only an account that is
 * waiting for its sign-up confirmation gets a mail (so nobody can use this to
 * lock a working account out), and only up to the 15-minute limit.
 */
function resendSignupConfirmation(account, lang) {
  if (!requireSignupEmailConfirmation()) return;
  const user = findUserForSignIn(account);
  if (!user || !user.email || user.email_verified_at || user.disabled_at) return;
  if (!isAwaitingSignupConfirmation(user.id) || tooManyRecent(user.id, tokens.SIGNUP)) return;
  const { token } = tokens.createEmailToken(user.id, tokens.SIGNUP, user.email);
  sendBackground('Sign-up confirmation resend', () => sendSignupConfirmation(user, token, lang));
}

/**
 * "Forgot password" for a username or email. Answers nothing (uniform reply
 * upstream). A link goes out only for an enabled account with an address that
 * is not still waiting for its sign-up confirmation.
 */
function requestPasswordReset(account, lang) {
  if (!getEmailConfig().configured) return;
  const user = findUserForSignIn(account);
  if (!user || !user.email || user.disabled_at) return;
  if (requireSignupEmailConfirmation() && isAwaitingSignupConfirmation(user.id)) return;
  if (tooManyRecent(user.id, tokens.PASSWORD_RESET)) return;
  const { token } = tokens.createEmailToken(user.id, tokens.PASSWORD_RESET, user.email);
  const config = getEmailConfig();
  const params = { brand: config.brandName, username: user.username };
  sendBackground('Password reset', () => sendServiceEmail(user.email, {
    lang,
    subject: t(lang, 'reset.subject', params),
    title: t(lang, 'reset.title', params),
    intro: t(lang, 'reset.intro', params),
    actionUrl: tokens.passwordResetUrl(token),
    actionLabel: t(lang, 'reset.button', params),
    details: [
      { label: t(lang, 'mail.label.account'), value: user.username },
      { label: t(lang, 'mail.label.expires'), value: formatHours(lang, config.tokenTtlHours) },
    ],
  }));
}

/** Waits until `minimum` ms passed since `startedAt`; a visitor's reply is never faster. */
async function padAnonymousLatency(startedAt, minimum = MIN_ANONYMOUS_LATENCY_MS) {
  const remaining = minimum - (Date.now() - startedAt);
  if (remaining > 0) await new Promise((resolve) => setTimeout(resolve, remaining));
}

// --- Notices ---------------------------------------------------------------

function notice(user, kind, { lang, extra = [], address = user.email }) {
  const config = getEmailConfig();
  if (!config.configured || !config.notifyAccountChanges || !address) return Promise.resolve();
  const params = { brand: config.brandName, username: user.username };
  return sendBackground(`Notice ${kind}`, () => sendServiceEmail(address, {
    lang,
    subject: t(lang, `${kind}.subject`, params),
    title: t(lang, `${kind}.title`, params),
    intro: t(lang, `${kind}.intro`, params),
    details: [{ label: t(lang, 'mail.label.account'), value: user.username }, ...extra, { label: t(lang, 'mail.label.time'), value: now() }],
  }));
}

function noticePasswordChanged(user, lang) {
  return notice(user, 'passwordChanged', { lang });
}

function noticeEmailChangeRequested(user, requestedEmail, lang) {
  return notice(user, 'emailChangeRequested', {
    lang,
    extra: [{ label: t(lang, 'mail.label.requestedEmail'), value: requestedEmail }],
  });
}

/** After an address changed: the old address is told, and so is the new one. */
function noticeEmailChanged(user, previousEmail, newEmail, lang) {
  const extra = [{ label: t(lang, 'mail.label.newEmail'), value: newEmail }];
  if (previousEmail && previousEmail.toLowerCase() !== newEmail.toLowerCase()) {
    notice(user, 'emailChangedOld', { lang, extra, address: previousEmail });
  }
  return notice(user, 'emailChangedNew', { lang, extra, address: newEmail });
}

/**
 * Tells the owner about a sign-in from an address and browser none of their
 * recorded sessions used (a plain IP + User-Agent comparison). The first
 * sign-in of an account, and a repeat within the hour, send nothing. Call it
 * before the new session is recorded.
 */
function noticeIfNewSignIn(req, user) {
  const config = getEmailConfig();
  if (!config.configured || !config.notifyNewLogin || !user.email) return;
  const ip = String(req.ip || '');
  const userAgent = String(req.get('user-agent') || '').slice(0, 500);
  const seen = db.prepare('SELECT ip_address, user_agent FROM user_sessions WHERE user_id = ?').all(user.id);
  if (seen.length === 0) return;
  if (seen.some((row) => (row.ip_address || '') === ip && (row.user_agent || '') === userAgent)) return;
  const last = lastSignInNotice.get(user.id) || 0;
  if (Date.now() - last < SIGN_IN_NOTICE_COOLDOWN_MS) return;
  lastSignInNotice.set(user.id, Date.now());
  const lang = languageFor(req);
  const params = { brand: config.brandName, username: user.username };
  sendBackground('Notice new sign-in', () => sendServiceEmail(user.email, {
    lang,
    subject: t(lang, 'signIn.subject', params),
    title: t(lang, 'signIn.title', params),
    intro: t(lang, 'signIn.intro', params),
    details: [
      { label: t(lang, 'mail.label.account'), value: user.username },
      { label: t(lang, 'mail.label.ip'), value: ip || '-' },
      { label: t(lang, 'mail.label.device'), value: userAgent.slice(0, 120) || '-' },
      { label: t(lang, 'mail.label.time'), value: now() },
    ],
  }));
}

// --- Changing the address ---------------------------------------------------

function applyEmailNow(user, email) {
  try {
    db.transaction(() => {
      // The new address has not been proven; links sent to the old one are void.
      db.prepare('UPDATE users SET email = ?, email_verified_at = NULL WHERE id = ?').run(email, user.id);
      tokens.spendTokens(user.id, [tokens.SIGNUP, tokens.EMAIL_CHANGE, tokens.PASSWORD_RESET]);
    })();
  } catch (err) {
    // The unique index is the last line against two accounts claiming one address.
    if (String(err?.code || '').startsWith('SQLITE_CONSTRAINT')) throw httpError(409, 'Email is already in use', 'EMAIL_TAKEN');
    throw err;
  }
}

/**
 * Asks for a new address for `user` (a users row; the caller has checked the
 * password). With confirmation required the address changes only when the
 * link sent to it is opened, and the current address is told; otherwise it
 * changes now. Returns { changed } or { pendingEmail }.
 */
async function requestEmailChange(user, newEmail, lang) {
  if ((user.email || '').toLowerCase() === newEmail) return { changed: false };
  assertEmailAvailable(newEmail);
  if (!requireEmailChangeConfirmation()) {
    applyEmailNow(user, newEmail);
    noticeEmailChanged(user, user.email, newEmail, lang);
    return { changed: true };
  }
  if (tooManyRecent(user.id, tokens.EMAIL_CHANGE)) throw rateLimited();
  const { token } = tokens.createEmailToken(user.id, tokens.EMAIL_CHANGE, newEmail);
  try {
    await sendServiceEmail(newEmail, confirmationMessage('emailChange', { user, email: newEmail, token, lang }));
  } catch (err) {
    tokens.spendTokens(user.id, [tokens.EMAIL_CHANGE]);
    throw Object.assign(sendFailed(), { cause: err });
  }
  noticeEmailChangeRequested(user, newEmail, lang);
  return { pendingEmail: newEmail };
}

/**
 * "Send the confirmation again" for the signed-in account: to the pending
 * address if a change is waiting, else to the current address if it was
 * never confirmed. Returns { pendingEmail }.
 */
async function resendOwnConfirmation(user, lang) {
  if (!getEmailConfig().configured) {
    throw httpError(409, 'Email is not set up on this server.', 'EMAIL_NOT_CONFIGURED');
  }
  const pending = tokens.pendingEmailFor(user.id);
  if (!pending && (!user.email || user.email_verified_at)) {
    throw httpError(409, 'There is nothing to confirm.', 'NOTHING_TO_CONFIRM');
  }
  if (tooManyRecent(user.id, tokens.EMAIL_CHANGE)) throw rateLimited();
  const address = pending || user.email;
  const { token } = tokens.createEmailToken(user.id, tokens.EMAIL_CHANGE, address);
  try {
    await sendServiceEmail(address, confirmationMessage(pending ? 'emailChange' : 'verify', { user, email: address, token, lang }));
  } catch (err) {
    tokens.spendTokens(user.id, [tokens.EMAIL_CHANGE]);
    throw Object.assign(sendFailed(), { cause: err });
  }
  return { pendingEmail: pending };
}

/** After a confirmation link was spent: tells both addresses when the address changed. */
function afterConfirmation(result, lang) {
  if (result.type !== 'email_changed') return;
  const user = db.prepare('SELECT id, username FROM users WHERE id = ?').get(result.user.id);
  if (user) noticeEmailChanged(user, result.previousEmail, result.email, lang);
}

module.exports = {
  afterConfirmation,
  emailStatus,
  languageFor,
  noticeIfNewSignIn,
  noticePasswordChanged,
  padAnonymousLatency,
  pendingMessage,
  requestEmailChange,
  requestPasswordReset,
  resendOwnConfirmation,
  resendSignupConfirmation,
  sendFailed,
  sendSignupConfirmation,
};
