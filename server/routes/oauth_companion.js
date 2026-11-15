'use strict';

// NeoAgent's first step when the user adds NeoCloud: register (or find) the
// OAuth client for its callback URL. Public: it hands out a client id, which
// is no secret; access needs the user's sign-in and consent (routes/oauth.js).

const express = require('express');
const { SCOPES, bootstrapCompanion } = require('../services/oauth/connected_apps');
const { jsonHandler, publicOrigin, rateLimiter } = require('./_helpers/http');

const router = express.Router();

router.post('/companion/neoagent/bootstrap', rateLimiter(60), jsonHandler((req) => {
  const { client, created } = bootstrapCompanion({ redirectUri: req.body?.redirectUri, appName: req.body?.appName });
  const root = publicOrigin(req);
  return {
    companion: 'neoagent',
    created,
    clientId: client.id,
    redirectUri: client.redirect_uri,
    scopes: SCOPES,
    authorizationEndpoint: `${root}/oauth/authorize`,
    tokenEndpoint: `${root}/oauth/token`,
    userinfoEndpoint: `${root}/oauth/userinfo`,
    revocationEndpoint: `${root}/oauth/revoke`,
  };
}));

module.exports = router;
