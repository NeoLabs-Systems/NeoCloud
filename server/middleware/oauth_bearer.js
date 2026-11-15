'use strict';

// Access tokens of connected apps (services/oauth/connected_apps.js), sent as
// bearer tokens to the agent API (/api/v1). Device tokens and browser
// sessions are not accepted there, and these tokens reach nothing else.

const { resolveAccessToken } = require('../services/oauth/connected_apps');

function bearerToken(req) {
  const match = String(req.get('authorization') || '').match(/^Bearer\s+(\S+)$/i);
  return match ? match[1] : '';
}

function requireOauthToken(req, res, next) {
  const grant = resolveAccessToken(bearerToken(req));
  if (!grant) {
    res.set('WWW-Authenticate', 'Bearer');
    return res.status(401).json({ error: 'Unauthorized', code: 'UNAUTHORIZED' });
  }
  req.userId = grant.userId;
  req.oauth = { clientId: grant.clientId, scopes: grant.scopes };
  return next();
}

function hasScope(req, scope) {
  return Boolean(req.oauth?.scopes.includes(scope));
}

function insufficientScope(res, scope) {
  res.set('WWW-Authenticate', `Bearer error="insufficient_scope", scope="${scope}"`);
  return res.status(403).json({ error: `This connection was not allowed to do that (${scope}).`, code: 'INSUFFICIENT_SCOPE' });
}

/** Lets the request through only when the token was granted every one of `scopes`. */
function requireScope(...scopes) {
  return (req, res, next) => {
    const missing = scopes.find((scope) => !hasScope(req, scope));
    return missing ? insufficientScope(res, missing) : next();
  };
}

module.exports = { bearerToken, hasScope, requireOauthToken, requireScope };
