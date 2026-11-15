'use strict';

// Mail links and anonymous mail requests under /api/auth: resend the sign-up
// confirmation, "forgot password", the confirmation page and the reset page
// (docs/email.md). Anonymous requests answer the same for every account, and
// the pages never act on a GET: a mail scanner that opens the link must not
// spend it.

const express = require('express');
const { rateLimiter, jsonHandler } = require('./_helpers/http');
const { getEmailConfig } = require('../services/account/service_email');
const { t } = require('../services/account/email_i18n');
const tokens = require('../services/account/email_tokens');
const accountEmail = require('../services/account/account_email');
const { renderConfirmPage, renderResetPage, setPageHeaders } = require('../http/pages/email_pages');
const { createServiceLogger } = require('../utils/logger');

const router = express.Router();
const log = createServiceLogger('Email');

function sendPage(res, status, html) {
  setPageHeaders(res);
  res.status(status).send(html);
}

function pageContext(req) {
  return { lang: accountEmail.languageFor(req), brand: getEmailConfig().brandName };
}

// A visitor who is over the limit gets the page's own "does not work" screen, not JSON.
function pageRateLimiter(max, render) {
  return rateLimiter(max, {
    handler: (req, res) => sendPage(res, 429, render({ ...pageContext(req), state: 'toomany' })),
  });
}

// Per address, per 15 minutes. The per-account limit lives in account_email.js.
const resendLimiter = rateLimiter(10);
const forgotLimiter = rateLimiter(10);
const pageLimiter = pageRateLimiter(60, renderConfirmPage);
const confirmLimiter = pageRateLimiter(8, renderConfirmPage);
const resetPageLimiter = pageRateLimiter(60, renderResetPage);
const resetLimiter = pageRateLimiter(8, renderResetPage);

function accountField(req) {
  return typeof req.body?.account === 'string' ? req.body.account.trim().slice(0, 254) : '';
}

function tokenOf(value) {
  return typeof value === 'string' ? value.trim().slice(0, 200) : '';
}

// Runs an anonymous mail request and answers it identically for every
// account: the work starts, the answer waits out the latency floor.
function uniformMailRequest(messageKey, work) {
  return jsonHandler(async (req) => {
    const startedAt = Date.now();
    const lang = accountEmail.languageFor(req);
    const account = accountField(req);
    try {
      if (account) work(account, lang);
    } catch (err) {
      log.error('Anonymous mail request failed', err);
    }
    await accountEmail.padAnonymousLatency(startedAt);
    return { success: true, message: t(lang, messageKey, { brand: getEmailConfig().brandName }) };
  });
}

router.post('/email/resend', resendLimiter, uniformMailRequest('msg.resendUniform', accountEmail.resendSignupConfirmation));
router.post('/password/forgot', forgotLimiter, uniformMailRequest('msg.forgotUniform', accountEmail.requestPasswordReset));

router.get('/email/confirm', pageLimiter, (req, res) => {
  const token = tokenOf(req.query.token);
  const context = pageContext(req);
  if (!token) return sendPage(res, 400, renderConfirmPage({ ...context, state: 'missing' }));
  if (!tokens.isConfirmationLinkLive(token)) return sendPage(res, 400, renderConfirmPage({ ...context, state: 'failed' }));
  return sendPage(res, 200, renderConfirmPage({ ...context, state: 'ready', token }));
});

router.post('/email/confirm', confirmLimiter, (req, res) => {
  const token = tokenOf(req.body?.token);
  const context = pageContext(req);
  if (!token) return sendPage(res, 400, renderConfirmPage({ ...context, state: 'missing' }));
  try {
    const result = tokens.consumeConfirmationToken(token);
    accountEmail.afterConfirmation(result, context.lang);
    return sendPage(res, 200, renderConfirmPage({ ...context, state: result.type === 'email_changed' ? 'changed' : 'done' }));
  } catch (err) {
    if (err.code === 'EMAIL_TAKEN') return sendPage(res, 409, renderConfirmPage({ ...context, state: 'taken' }));
    if (err.code !== 'LINK_INVALID') log.error('Email confirmation failed', err);
    return sendPage(res, err.code === 'LINK_INVALID' ? 400 : 500, renderConfirmPage({ ...context, state: 'failed' }));
  }
});

router.get('/password/reset', resetPageLimiter, (req, res) => {
  const token = tokenOf(req.query.token);
  const context = pageContext(req);
  if (!token) return sendPage(res, 400, renderResetPage({ ...context, state: 'missing' }));
  if (!tokens.isPasswordResetLinkLive(token)) return sendPage(res, 400, renderResetPage({ ...context, state: 'failed' }));
  return sendPage(res, 200, renderResetPage({ ...context, state: 'form', token }));
});

router.post('/password/reset', resetLimiter, async (req, res) => {
  const token = tokenOf(req.body?.token);
  const context = pageContext(req);
  if (!token) return sendPage(res, 400, renderResetPage({ ...context, state: 'missing' }));
  const password = typeof req.body?.password === 'string' ? req.body.password : '';
  if (password !== (typeof req.body?.confirmPassword === 'string' ? req.body.confirmPassword : password)) {
    return sendPage(res, 400, renderResetPage({ ...context, state: 'form', token, error: t(context.lang, 'page.reset.mismatch') }));
  }
  try {
    const { user } = await tokens.consumePasswordResetToken(token, password);
    accountEmail.noticePasswordChanged(user, context.lang);
    return sendPage(res, 200, renderResetPage({ ...context, state: 'done' }));
  } catch (err) {
    // A password the policy refuses leaves the link usable: the form comes back with the reason.
    if (err.status === 400 && err.code !== 'LINK_INVALID') {
      return sendPage(res, 400, renderResetPage({ ...context, state: 'form', token, error: err.message }));
    }
    if (err.code !== 'LINK_INVALID') log.error('Password reset failed', err);
    return sendPage(res, err.code === 'LINK_INVALID' ? 400 : 500, renderResetPage({ ...context, state: 'failed' }));
  }
});

module.exports = router;
