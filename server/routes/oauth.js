'use strict';

// Connecting an app (NeoAgent) with OAuth 2: sign-in and consent pages, the
// token, revocation and userinfo endpoints. Mounted at /oauth. The sign-in
// here is only for the connection: it keeps the account in the browser
// session as `oauthUserId` for a few minutes and does not sign the browser
// in to NeoCloud. An existing NeoCloud browser session is used as it is.

const crypto = require('crypto');
const express = require('express');
const bcrypt = require('bcrypt');
const db = require('../db/database');
const { findUserForSignIn } = require('../services/account/credentials');
const { assertEmailConfirmed, isAccountActive } = require('../services/account/status');
const { getTwoFactorStatus, verifySignInCode } = require('../services/account/two_factor');
const throttle = require('../services/account/login_throttle');
const oauth = require('../services/oauth/connected_apps');
const { bearerToken } = require('../middleware/oauth_bearer');
const pages = require('../http/pages/oauth_pages');
const { rateLimiter } = require('./_helpers/http');

const router = express.Router();

const SIGN_IN_TTL_MS = 15 * 60 * 1000;
const TWO_FACTOR_TTL_MS = 10 * 60 * 1000;
const DUMMY_PASSWORD_HASH = bcrypt.hashSync('neocloud-oauth-timing-equalizer', 10);

const signInLimiter = rateLimiter(30);
const tokenLimiter = rateLimiter(300);

function signInError(code, status = 400) {
  const error = new Error(code);
  error.code = code;
  error.status = status;
  return error;
}

// Only /oauth/authorize?… may be continued to after signing in.
function continuePathOf(value) {
  try {
    const parsed = new URL(String(value || ''), 'http://localhost');
    return parsed.pathname === '/oauth/authorize' ? `${parsed.pathname}${parsed.search}` : '/oauth/authorize';
  } catch {
    return '/oauth/authorize';
  }
}

function authorizePath(params) {
  const keys = ['response_type', 'client_id', 'redirect_uri', 'state', 'scope', 'code_challenge', 'code_challenge_method'];
  const query = new URLSearchParams(keys.filter((key) => params[key] !== undefined).map((key) => [key, String(params[key])]));
  return `/oauth/authorize?${query}`;
}

/** The account approving the connection: a NeoCloud browser session, or this flow's own sign-in. */
function signedInUserId(req) {
  const session = req.session || {};
  if (session.userId && isAccountActive(session.userId)) return session.userId;
  const fresh = Date.now() - Number(session.oauthSignedInAt || 0) < SIGN_IN_TTL_MS;
  if (session.oauthUserId && fresh && isAccountActive(session.oauthUserId)) return session.oauthUserId;
  return null;
}

function pendingTwoFactor(req) {
  const pending = req.session?.oauthPending;
  if (!pending || Date.now() - Number(pending.startedAt || 0) > TWO_FACTOR_TTL_MS) return null;
  return pending;
}

function regenerate(req) {
  return new Promise((resolve, reject) => {
    req.session.regenerate((err) => (err ? reject(err) : resolve()));
  });
}

function save(req) {
  return new Promise((resolve, reject) => {
    req.session.save((err) => (err ? reject(err) : resolve()));
  });
}

function assertMaySignIn(user) {
  if (user.disabled_at) throw signInError('ACCOUNT_DISABLED', 403);
  assertEmailConfirmed(user.id);
}

async function checkPassword(account, password) {
  if (!account || !password) throw signInError('MISSING_FIELDS');
  const user = findUserForSignIn(account);
  const key = throttle.accountKey(user, account);
  throttle.assertSignInAllowed(key);
  throttle.recordFailure(key, { kind: 'password' });
  const matches = await bcrypt.compare(password, user?.password || DUMMY_PASSWORD_HASH);
  if (!user || !matches) throw signInError('INVALID_CREDENTIALS', 401);
  throttle.clearFailures(key, 'password');
  assertMaySignIn(user);
  return user;
}

async function finishSignIn(req, userId) {
  await regenerate(req);
  req.session.oauthUserId = userId;
  req.session.oauthSignedInAt = Date.now();
  await save(req);
}

router.get('/sign-in', async (req, res, next) => {
  try {
    const lang = pages.languageFor(req);
    const continuePath = continuePathOf(req.query.continue);
    if (req.query.restart && req.session) delete req.session.oauthPending;
    if (signedInUserId(req)) return res.redirect(continuePath);
    if (pendingTwoFactor(req)) return pages.sendPage(res, pages.renderTwoFactor({ lang, continuePath }));
    return pages.sendPage(res, pages.renderSignIn({ lang, continuePath }));
  } catch (err) {
    return next(err);
  }
});

router.post('/sign-in', signInLimiter, async (req, res, next) => {
  const lang = pages.languageFor(req);
  const continuePath = continuePathOf(req.body?.continue);
  const pending = pendingTwoFactor(req);
  try {
    if (pending && typeof req.body?.code === 'string') {
      try {
        await verifySignInCode({ userId: pending.userId, challengeId: pending.challengeId, code: req.body.code, restart: 'your password' });
      } catch (err) {
        if (throttle.challengeExhausted(pending.challengeId)) delete req.session.oauthPending;
        throw err;
      }
      const user = db.prepare('SELECT * FROM users WHERE id = ?').get(pending.userId);
      if (!user) throw signInError('INVALID_CREDENTIALS', 401);
      assertMaySignIn(user);
      await finishSignIn(req, user.id);
      return res.redirect(continuePath);
    }
    if (typeof req.body?.code === 'string') throw signInError('TWO_FACTOR_EXPIRED', 401);
    const account = String(req.body?.account || '').trim();
    const password = typeof req.body?.password === 'string' ? req.body.password : '';
    const user = await checkPassword(account, password);
    if (getTwoFactorStatus(user.id).enabled) {
      await regenerate(req);
      req.session.oauthPending = { userId: user.id, challengeId: crypto.randomUUID(), startedAt: Date.now() };
      await save(req);
      return pages.sendPage(res, pages.renderTwoFactor({ lang, continuePath }));
    }
    await finishSignIn(req, user.id);
    return res.redirect(continuePath);
  } catch (err) {
    if (!err?.code) return next(err);
    if (err.retryAfter) res.set('Retry-After', String(err.retryAfter));
    res.status(Number(err.status) || 400);
    const stillPending = pendingTwoFactor(req);
    if (pending && stillPending) return pages.sendPage(res, pages.renderTwoFactor({ lang, continuePath, error: err.code }));
    const code = pending ? 'TWO_FACTOR_ATTEMPTS_EXCEEDED' : err.code;
    return pages.sendPage(res, pages.renderSignIn({ lang, continuePath, account: req.body?.account, error: code }));
  }
});

router.get('/authorize', (req, res) => {
  const lang = pages.languageFor(req);
  let authorize;
  try {
    authorize = oauth.validateAuthorizationRequest(req.query);
  } catch {
    return pages.sendPage(res.status(400), pages.renderError({ lang }));
  }
  const userId = signedInUserId(req);
  if (!userId) return res.redirect(`/oauth/sign-in?continue=${encodeURIComponent(authorizePath(req.query))}`);
  const consentToken = crypto.randomBytes(24).toString('base64url');
  req.session.oauthConsent = consentToken;
  const { username } = db.prepare('SELECT username FROM users WHERE id = ?').get(userId);
  return pages.sendPage(res, pages.renderConsent({ lang, authorize, username, consentToken }), { redirectUri: authorize.redirectUri });
});

function sameToken(a, b) {
  const left = Buffer.from(String(a || ''));
  const right = Buffer.from(String(b || ''));
  return left.length > 0 && left.length === right.length && crypto.timingSafeEqual(left, right);
}

function redirectWith(res, redirectUri, values) {
  const target = new URL(redirectUri);
  for (const [key, value] of Object.entries(values)) target.searchParams.set(key, value);
  return res.redirect(target.toString());
}

router.post('/authorize', (req, res) => {
  const lang = pages.languageFor(req);
  let authorize;
  try {
    authorize = oauth.validateAuthorizationRequest(req.body || {});
  } catch {
    return pages.sendPage(res.status(400), pages.renderError({ lang }));
  }
  const userId = signedInUserId(req);
  if (!userId) return res.redirect(`/oauth/sign-in?continue=${encodeURIComponent(authorizePath(req.body))}`);
  const expected = req.session.oauthConsent;
  delete req.session.oauthConsent;
  if (!sameToken(req.body?.consent, expected)) return pages.sendPage(res.status(400), pages.renderError({ lang }));
  // The connection's own sign-in is used once; a NeoCloud browser session stays.
  delete req.session.oauthUserId;
  delete req.session.oauthSignedInAt;
  const scopes = oauth.grantedScopes(authorize.scopes, {
    files: req.body?.files === 'write' ? 'write' : 'read',
    signing: req.body?.signing === 'on',
  });
  if (req.body?.decision !== 'approve' || scopes.length === 0) {
    return redirectWith(res, authorize.redirectUri, { error: 'access_denied', state: authorize.state });
  }
  const code = oauth.createAuthorizationCode({
    clientId: authorize.client.id,
    userId,
    redirectUri: authorize.redirectUri,
    scopes,
    codeChallenge: authorize.codeChallenge,
  });
  return redirectWith(res, authorize.redirectUri, { code, state: authorize.state });
});

function sendOauthError(res, err) {
  if (err?.code === 'INVALID_CLIENT') return res.status(401).json({ error: 'invalid_client', error_description: err.message });
  if (err?.code === 'INVALID_GRANT') return res.status(400).json({ error: 'invalid_grant', error_description: err.message });
  if (err?.code === 'INVALID_REQUEST') return res.status(400).json({ error: 'invalid_request', error_description: err.message });
  throw err;
}

router.post('/token', tokenLimiter, (req, res, next) => {
  res.set({ 'Cache-Control': 'no-store', Pragma: 'no-cache' });
  try {
    const grantType = String(req.body?.grant_type || '');
    const clientId = String(req.body?.client_id || '');
    let tokens;
    if (grantType === 'authorization_code') {
      tokens = oauth.exchangeAuthorizationCode({
        clientId, code: req.body?.code, redirectUri: req.body?.redirect_uri, codeVerifier: req.body?.code_verifier,
      });
    } else if (grantType === 'refresh_token') {
      tokens = oauth.refreshTokens({ clientId, refreshToken: req.body?.refresh_token });
    } else {
      return res.status(400).json({ error: 'unsupported_grant_type' });
    }
    return res.json({
      access_token: tokens.accessToken,
      token_type: 'Bearer',
      expires_in: tokens.expiresIn,
      refresh_token: tokens.refreshToken,
      scope: tokens.scope,
    });
  } catch (err) {
    try {
      return sendOauthError(res, err);
    } catch (unexpected) {
      return next(unexpected);
    }
  }
});

router.post('/revoke', tokenLimiter, (req, res, next) => {
  try {
    oauth.revokeToken({ clientId: req.body?.client_id, token: req.body?.token });
    return res.status(200).json({});
  } catch (err) {
    try {
      return sendOauthError(res, err);
    } catch (unexpected) {
      return next(unexpected);
    }
  }
});

router.get('/userinfo', (req, res) => {
  const grant = oauth.resolveAccessToken(bearerToken(req));
  if (!grant) return res.status(401).set('WWW-Authenticate', 'Bearer').json({ error: 'Unauthorized', code: 'UNAUTHORIZED' });
  res.set('Cache-Control', 'no-store');
  return res.json({
    sub: String(grant.userId),
    preferred_username: grant.username,
    email: grant.email || null,
    scope: grant.scopes.join(' '),
  });
});

module.exports = router;
