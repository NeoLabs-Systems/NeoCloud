'use strict';

// What the NeoRouter routes do: show the connections, connect, follow a
// sign-in, change roles, disconnect. scope 'server' needs an admin.

const { isAdminUser } = require('../../access/admin');
const { validateCloudUrlWithDns } = require('../../../utils/cloud_security');
const { httpError } = require('../../../utils/http_error');
const { ROLES, apiBaseFor, serverConnection, userConnection } = require('./connection');
const { describeOffer } = require('./offer');
const signIn = require('./sign_in');

function checkScope(userId, scope) {
  if (scope !== 'server' && scope !== 'user') throw httpError(400, 'scope must be "server" or "user".', 'INVALID_SCOPE');
  if (scope === 'server' && !isAdminUser(userId)) {
    throw httpError(403, 'Only an admin can connect NeoRouter for the whole server.', 'ADMIN_REQUIRED');
  }
  return scope;
}

// The settings page always asks the router afresh: what a key may use is
// changed in NeoRouter, not here.
async function describe(connection) {
  if (!connection) return { connected: false, address: null, roles: [], offer: null };
  const offer = await describeOffer(connection, { fresh: true }).catch(() => null);
  return { connected: true, address: connection.origin, roles: [...connection.roles], offer, reachable: Boolean(offer) };
}

async function describeConnections(userId) {
  const isAdmin = isAdminUser(userId);
  const server = serverConnection();
  const [serverState, userState] = await Promise.all([
    // Only admins see where the server's NeoRouter lives and what it offers;
    // everyone else only learns that one is connected.
    isAdmin ? describe(server) : { connected: Boolean(server) },
    describe(userConnection(userId)),
  ]);
  return { roles: ROLES, isAdmin, server: serverState, user: userState };
}

async function connect(userId, scope, address) {
  checkScope(userId, scope);
  const cleaned = String(address || '').trim();
  const apiBase = apiBaseFor(cleaned);
  // A person's own address is user input pointed at from the server: it may
  // not reach the server's network, unless it is the admin's own NeoRouter.
  if (scope === 'user' && apiBase && apiBase !== serverConnection()?.apiBase) {
    const check = await validateCloudUrlWithDns(apiBase);
    if (!check.allowed) throw httpError(400, check.reason, 'URL_NOT_ALLOWED');
  }
  return signIn.begin({ scope, userId, address: cleaned });
}

function connectStatus(userId, id) {
  const result = signIn.status(id, userId);
  if (!result) throw httpError(404, 'This sign-in expired. Start again.', 'NOT_FOUND');
  return result;
}

function setRoles(userId, scope, roles) {
  checkScope(userId, scope);
  if (!Array.isArray(roles)) throw httpError(400, 'roles must be a list.', 'INVALID_VALUE');
  return { roles: signIn.updateRoles({ scope, userId, roles }) };
}

function disconnect(userId, scope) {
  checkScope(userId, scope);
  signIn.disconnect({ scope, userId });
  return { success: true };
}

module.exports = { connect, connectStatus, describeConnections, disconnect, setRoles };
