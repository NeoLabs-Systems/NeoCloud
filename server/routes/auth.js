'use strict';

const express = require('express');
const crypto = require('crypto');
const bcrypt = require('bcrypt');
const rateLimit = require('express-rate-limit');
const db = require('../db/database');
const {
  assertEmailAvailable,
  findUserForSignIn,
  hashPassword,
  isValidEmail,
  normalizeEmail,
  validateNewPassword,
  validateNewUsername,
} = require('../services/account/credentials');
const { getTwoFactorStatus, verifyLoginCode } = require('../services/account/two_factor');
const { endCurrentSession, recordCurrentSession } = require('../services/account/sessions');
const throttle = require('../services/account/login_throttle');
const { isAdminUser } = require('../services/access/admin');
const webauthn = require('../services/account/webauthn');
const { getAllowSignup } = require('../config/access');
const { getUsage } = require('../services/storage/quota');
const { requireSessionTransport } = require('../middleware/auth');
const { SESSION_COOKIE_NAME } = require('../http/middleware');
const { sendJsonError } = require('../http/errors');
const { jsonHandler, publicOrigin } = require('./_helpers/http');
const { httpError } = require('../utils/http_error');
const qrLogin = require('../services/account/qr_login');

const router = express.Router();

const authLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 20,
  message: { error: 'Too many attempts, try again later', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

const webauthnLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 40,
  message: { error: 'Too many security key attempts, try again later', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

// Showing a sign-in QR code is not a credential attempt, so it gets its own
// budget instead of eating into authLimiter. A visible code rotates every two
// minutes; its status checks are held open up to 25 seconds each.
const qrLoginChallengeLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 60,
  message: { error: 'Too many QR sign-in requests, try again shortly', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

const qrLoginPollLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 400,
  message: { error: 'Too many QR sign-in status checks, try again shortly', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

const qrLoginClaimLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 40,
  message: { error: 'Too many QR sign-in attempts, try again shortly', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

const TWO_FACTOR_WINDOW_MS = 10 * 60 * 1000;
// Compared against when the account doesn't exist, so a failed sign-in takes
// as long either way and doesn't reveal which usernames are registered.
const DUMMY_PASSWORD_HASH = bcrypt.hashSync('neocloud-timing-equalizer', 12);

// A throttled account answers 429 LOGIN_LOCKED with Retry-After and
// `retryAfter` (seconds) in the body.
function sendSignInError(res, err) {
  if (err?.retryAfter) {
    res.set('Retry-After', String(err.retryAfter));
    return res.status(err.status).json({ error: err.message, code: err.code, retryAfter: err.retryAfter });
  }
  return sendJsonError(res, err);
}

function toUserPayload(user) {
  return {
    id: user.id,
    username: user.username,
    displayName: user.display_name || null,
    email: user.email || null,
    createdAt: user.created_at,
    lastLogin: user.last_login,
    isAdmin: isAdminUser(user.id),
    storage: getUsage(user.id),
  };
}

function readAuthenticatedUser(req) {
  if (!req.session?.userId) return null;
  const user = db.prepare('SELECT * FROM users WHERE id = ?').get(req.session.userId);
  if (!user) req.session.destroy(() => {});
  return user || null;
}

function establishSession(req, res, user) {
  req.session.regenerate((regenerateError) => {
    if (regenerateError) return sendJsonError(res, regenerateError);
    req.session.userId = user.id;
    req.session.username = user.username;
    return req.session.save((saveError) => {
      if (saveError) return sendJsonError(res, saveError);
      recordCurrentSession(req, user.id);
      db.prepare("UPDATE users SET last_login = datetime('now') WHERE id = ?").run(user.id);
      return res.json({ success: true, redirect: '/app', user: toUserPayload(user) });
    });
  });
}

function establishPendingTwoFactorSession(req, res, user) {
  req.session.regenerate((regenerateError) => {
    if (regenerateError) return sendJsonError(res, regenerateError);
    req.session.pendingTwoFactorUserId = user.id;
    req.session.pendingTwoFactorStartedAt = Date.now();
    req.session.pendingTwoFactorChallenge = crypto.randomUUID();
    return req.session.save((saveError) => {
      if (saveError) return sendJsonError(res, saveError);
      // Only the name: nothing else about the account before the second factor.
      return res.json({ success: false, requiresTwoFactor: true, user: { username: user.username } });
    });
  });
}

// The first account can always register; after that only while sign-up is on.
function isRegistrationOpen() {
  const { count } = db.prepare('SELECT COUNT(*) AS count FROM users').get();
  return count === 0 || getAllowSignup();
}

function assertRegistrationOpen() {
  if (!isRegistrationOpen()) {
    throw httpError(403, 'Registration is closed on this server. Ask an admin for an account.', 'SIGNUP_DISABLED');
  }
}

router.get('/status', (req, res) => {
  const { count } = db.prepare('SELECT COUNT(*) AS count FROM users').get();
  const user = readAuthenticatedUser(req);
  res.json({
    hasUser: count > 0,
    registrationOpen: isRegistrationOpen(),
    authenticated: Boolean(user),
    user: user ? toUserPayload(user) : null,
  });
});

router.post('/register', authLimiter, requireSessionTransport, async (req, res) => {
  try {
    const username = String(req.body?.username || '').trim();
    const password = String(req.body?.password || '');
    const email = normalizeEmail(req.body?.email);
    if (!username || !password || !email) {
      throw httpError(400, 'Username, email, and password required', 'MISSING_FIELDS');
    }
    assertRegistrationOpen();
    if (!isValidEmail(email)) throw httpError(400, 'A valid email is required', 'INVALID_EMAIL');
    validateNewUsername(username);
    validateNewPassword(password, { username, email });

    const hash = await hashPassword(password);
    const userId = db.transaction(() => {
      // Checked again inside the transaction: hashing takes long enough for
      // a concurrent sign-up to claim either name, or the first account.
      assertRegistrationOpen();
      validateNewUsername(username);
      assertEmailAvailable(email);
      const inserted = db.prepare('INSERT INTO users (username, email, password) VALUES (?, ?, ?)')
        .run(username, email, hash);
      return Number(inserted.lastInsertRowid);
    })();
    establishSession(req, res, db.prepare('SELECT * FROM users WHERE id = ?').get(userId));
  } catch (err) {
    if (String(err?.code || '').startsWith('SQLITE_CONSTRAINT')) {
      const emailClash = String(err.message || '').toLowerCase().includes('email');
      return res.status(409).json(emailClash
        ? { error: 'Email is already in use', code: 'EMAIL_TAKEN' }
        : { error: 'Username is already taken', code: 'USERNAME_TAKEN' });
    }
    return sendJsonError(res, err);
  }
});

router.post('/login', authLimiter, requireSessionTransport, async (req, res) => {
  try {
    // `username` also accepts the account email; both ignore case.
    const account = String(req.body?.username || '').trim();
    const password = String(req.body?.password || '');
    if (!account || !password) {
      return res.status(400).json({ error: 'Username and password required', code: 'MISSING_FIELDS' });
    }
    const user = findUserForSignIn(account);
    const key = throttle.accountKey(user, account);
    throttle.assertSignInAllowed(key);
    throttle.recordFailure(key, { kind: 'password' });
    const matches = await bcrypt.compare(password, user?.password || DUMMY_PASSWORD_HASH);
    if (!user || !matches) {
      return res.status(401).json({ error: 'Invalid credentials', code: 'INVALID_CREDENTIALS' });
    }
    throttle.clearFailures(key, 'password');
    if (getTwoFactorStatus(user.id).enabled) return establishPendingTwoFactorSession(req, res, user);
    return establishSession(req, res, user);
  } catch (err) {
    return sendSignInError(res, err);
  }
});

function clearPendingTwoFactor(session) {
  delete session.pendingTwoFactorUserId;
  delete session.pendingTwoFactorStartedAt;
  delete session.pendingTwoFactorChallenge;
}

// At most 5 codes per password sign-in, plus the account-wide throttle.
router.post('/login/2fa', authLimiter, requireSessionTransport, async (req, res) => {
  try {
    const pendingUserId = req.session?.pendingTwoFactorUserId;
    const startedAt = Number(req.session?.pendingTwoFactorStartedAt || 0);
    const challengeId = req.session?.pendingTwoFactorChallenge;
    if (!pendingUserId || !challengeId || Date.now() - startedAt > TWO_FACTOR_WINDOW_MS) {
      return res.status(401).json({ error: 'Two-factor challenge expired', code: 'TWO_FACTOR_EXPIRED' });
    }
    if (!req.body?.code) {
      return res.status(400).json({ error: 'Two-factor code required', code: 'MISSING_FIELDS' });
    }
    if (throttle.challengeExhausted(challengeId)) {
      clearPendingTwoFactor(req.session);
      return res.status(401).json({
        error: 'Too many wrong codes. Sign in with your password again.',
        code: 'TWO_FACTOR_ATTEMPTS_EXCEEDED',
      });
    }
    const key = throttle.accountKey({ id: pendingUserId });
    throttle.assertSignInAllowed(key);
    throttle.recordFailure(key, { kind: 'second_factor', challengeId });
    if (!(await verifyLoginCode(pendingUserId, req.body.code))) {
      if (throttle.challengeExhausted(challengeId)) clearPendingTwoFactor(req.session);
      return res.status(401).json({ error: 'Invalid 2FA code', code: 'INVALID_CODE' });
    }
    throttle.clearFailures(key);
    const user = db.prepare('SELECT * FROM users WHERE id = ?').get(pendingUserId);
    if (!user) return res.status(401).json({ error: 'User not found', code: 'UNAUTHORIZED' });
    return establishSession(req, res, user);
  } catch (err) {
    return sendSignInError(res, err);
  }
});

// Security key sign-in. The challenge is spent by the first verify attempt,
// right or wrong. A key that verified the user (PIN or biometric) stands in
// for the second factor; a key that only confirmed presence still needs the
// authenticator code, as in NeoAgent.
router.post('/webauthn/login/options', webauthnLimiter, requireSessionTransport, jsonHandler(async (req) => {
  const { options, challenge } = await webauthn.beginLogin({ req, username: req.body?.username });
  req.session.pendingWebAuthnLogin = challenge;
  return { options };
}));

router.post('/webauthn/login/verify', webauthnLimiter, requireSessionTransport, async (req, res) => {
  try {
    const pending = req.session?.pendingWebAuthnLogin;
    delete req.session.pendingWebAuthnLogin;
    const result = await webauthn.completeLogin({ req, response: req.body?.response, pending });
    const user = db.prepare('SELECT * FROM users WHERE id = ?').get(result.userId);
    if (!user) return res.status(401).json({ error: 'User not found', code: 'UNAUTHORIZED' });
    if (!result.userVerified && getTwoFactorStatus(user.id).enabled) {
      return establishPendingTwoFactorSession(req, res, user);
    }
    return establishSession(req, res, user);
  } catch (err) {
    return sendSignInError(res, err);
  }
});

// QR sign-in (services/account/qr_login.js): this signed-out screen shows a
// code, the signed-in phone approves it at /api/account/qr-login/*, and this
// screen claims a session.
function pollToken(req) {
  const token = String(req.body?.token || '').trim();
  if (!token) throw httpError(400, 'QR sign-in poll token is required', 'MISSING_FIELDS');
  return token;
}

router.post('/qr-login/challenge', qrLoginChallengeLimiter, jsonHandler((req) => {
  const challenge = qrLogin.createChallenge({
    userAgent: req.get('user-agent'),
    ipAddress: req.ip,
    requestMetadata: req.body?.requestMetadata,
  });
  const server = publicOrigin(req);
  const query = new URLSearchParams({ v: '1', backend: server, challenge: challenge.challengeId, secret: challenge.approveSecret });
  return {
    challengeId: challenge.challengeId,
    pollToken: challenge.pollToken,
    expiresAt: challenge.expiresAt,
    status: challenge.status,
    qrPayload: `neocloud://qr-login?${query}`,
    backendUrl: server,
  };
}));

// With `knownStatus`, the request is held open until the status differs from
// it, so the signing-in screen hears about a scan or approval at once.
router.post('/qr-login/challenge/:id/status', qrLoginPollLimiter, async (req, res) => {
  try {
    const token = pollToken(req);
    const knownStatus = String(req.body?.knownStatus || '').trim();
    if (!knownStatus) return res.json(qrLogin.getChallengeStatusForPoll({ challengeId: req.params.id, pollToken: token }));
    const clientGone = new AbortController();
    res.on('close', () => clientGone.abort());
    const status = await qrLogin.waitForChallengeStatusChange({
      challengeId: req.params.id,
      pollToken: token,
      knownStatus,
      signal: clientGone.signal,
    });
    if (!res.writableEnded && !clientGone.signal.aborted) res.json(status);
  } catch (err) {
    sendJsonError(res, err);
  }
});

router.post('/qr-login/challenge/:id/cancel', qrLoginPollLimiter, jsonHandler((req) => {
  qrLogin.cancelChallenge({ challengeId: req.params.id, pollToken: pollToken(req) });
  return { success: true };
}));

router.post('/qr-login/challenge/:id/claim', qrLoginClaimLimiter, requireSessionTransport, (req, res) => {
  try {
    const user = qrLogin.claimApprovedChallenge({ challengeId: req.params.id, pollToken: pollToken(req) });
    return establishSession(req, res, user);
  } catch (err) {
    return sendJsonError(res, err);
  }
});

router.post('/logout', (req, res) => {
  endCurrentSession(req);
  req.session.destroy((err) => {
    if (err) return sendJsonError(res, err);
    res.clearCookie(SESSION_COOKIE_NAME, {
      path: '/',
      httpOnly: true,
      sameSite: 'lax',
      secure: req.app?.locals?.httpRuntimeConfig?.secureCookies === true,
    });
    return res.json({ success: true });
  });
});

router.get('/me', (req, res) => {
  const user = readAuthenticatedUser(req);
  if (!user) return res.status(401).json({ error: 'Not authenticated', code: 'UNAUTHORIZED' });
  return res.json({ user: toUserPayload(user) });
});

module.exports = router;
module.exports.toUserPayload = toUserPayload;
