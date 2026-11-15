'use strict';

// Starting an editing session: the page to put in the iframe and the access
// token the editor presents to the WOPI endpoints (docs/office-editing.md).

const { httpError } = require('../../utils/http_error');
const { editorPageUrl } = require('../../utils/wopi_discovery');
const { currentActions } = require('./discovery');
const { extensionOf, isEditableName, openFile } = require('./file_access');
const { issueAccessToken } = require('./tokens');
const { readOfficeSettings } = require('./settings');

/** The extensions the editor opens right now; empty when it is off or unreachable. */
async function editableExtensions() {
  const actions = await currentActions();
  if (!actions) return [];
  return Object.keys(actions).filter((extension) => isEditableName(`file.${extension}`));
}

/**
 * `baseUrl` is how the caller reached this server; the admin's address for
 * this server, when set, wins (the editor often sits on another network).
 */
async function createEditSession(userId, nodeId, { baseUrl }) {
  const file = openFile(userId, nodeId, { wantWrite: true });
  const actions = await currentActions();
  if (!actions) throw httpError(503, 'Document editing is not available right now.', 'EDITOR_UNAVAILABLE');
  const available = isEditableName(file.node.name) ? actions[extensionOf(file.node.name)] : null;
  if (!available) throw httpError(415, 'This kind of file can’t be edited here.', 'NOT_EDITABLE');
  const urlsrc = file.canWrite ? available.edit || available.view : available.view || available.edit;
  const host = readOfficeSettings().hostUrl || baseUrl;
  const { token, expiresAt } = issueAccessToken({ userId, nodeId: file.node.id, write: file.canWrite });
  return {
    editorUrl: editorPageUrl(urlsrc, `${host}/wopi/files/${file.node.id}`),
    accessToken: token,
    // WOPI semantics: the moment the token ends, in milliseconds since 1970.
    accessTokenTtl: expiresAt,
    canWrite: file.canWrite,
  };
}

module.exports = { createEditSession, editableExtensions };
