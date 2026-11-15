'use strict';

// E-signature documents as their owner manages them (browser session or the
// phone app). What signers do with their links is in public_signing.js.

const express = require('express');
const { requireSessionOrPhone } = require('../middleware/auth');
const documents = require('../services/signing/documents');
const links = require('../services/signing/links');
const audit = require('../services/signing/audit');
const results = require('../services/signing/results');
const { claimParty } = require('../services/signing/claim');
const { finalContext, listInbox, openInboxParty } = require('../services/signing/inbox');
const { remindAll, remindParty } = require('../services/signing/mail/reminders');
const profile = require('../services/signing/profile');
const images = require('../services/signing/profile_images');
const { openAsOwner } = require('../services/signing/self_sign');
const { sendValueImage } = require('../services/signing/serve');
const { getOwnedDocument } = require('../services/signing/store');
const { signingCapabilities } = require('../services/signing/snapshot');
const { sendCleanPdf, sendSnapshotPdf } = require('../services/signing/serve');
const { sendFile } = require('../services/transfer/downloads');
const { jsonHandler, publicOrigin, rateLimiter, streaming } = require('./_helpers/http');

const router = express.Router();
router.use(requireSessionOrPhone);
router.use(rateLimiter(1500));
// Creating and sending work on whole files: a tighter bound.
const heavyLimiter = rateLimiter(200);

// Mail goes to other people: a tight bound on reminders. Claims are bounded per account.
const remindLimiter = rateLimiter(60);
const claimLimiter = rateLimiter(60, { keyGenerator: (req) => `claim:${req.userId}` });

const ownerId = (req) => req.userId;

router.get('/capabilities', jsonHandler(() => signingCapabilities()));

router.post('/documents', heavyLimiter, jsonHandler((req) => documents.createDocument(ownerId(req), {
  nodeId: req.body?.nodeId,
  title: req.body?.title,
  locale: req.body?.locale,
}, req), { status: 201 }));

router.get('/documents', jsonHandler((req) => documents.listDocuments(ownerId(req), {
  status: typeof req.query.status === 'string' ? req.query.status : undefined,
  limit: req.query.limit,
  cursor: typeof req.query.cursor === 'string' ? req.query.cursor : undefined,
})));

router.get('/documents/:id', jsonHandler((req) => documents.describeDocument(ownerId(req), req.params.id)));

router.put('/documents/:id', jsonHandler((req) => documents.updateSettings(ownerId(req), req.params.id, req.body, req)));

router.delete('/documents/:id', jsonHandler((req) => documents.deleteDocument(ownerId(req), req.params.id)));

router.put('/documents/:id/fields', jsonHandler((req) => documents.saveFields(ownerId(req), req.params.id, req.body?.fields, req)));

router.put('/documents/:id/parties', jsonHandler((req) => documents.saveParties(ownerId(req), req.params.id, req.body?.parties)));

router.post('/documents/:id/import-fields', heavyLimiter, jsonHandler((req) => documents.importAcroFields(ownerId(req), req.params.id)));

router.post('/documents/:id/send', heavyLimiter, jsonHandler((req) => links.sendDocument(ownerId(req), req.params.id, req, publicOrigin(req), req.body)));

router.post('/documents/:id/cancel', jsonHandler((req) => documents.cancelDocument(ownerId(req), req.params.id, req)));

router.post('/documents/:id/duplicate', heavyLimiter, jsonHandler((req) => documents.duplicateDocument(ownerId(req), req.params.id, req), { status: 201 }));

router.post('/documents/:id/finalize', jsonHandler((req) => results.retryFinalization(ownerId(req), req.params.id)));

router.post('/documents/:id/parties/:partyId/link', jsonHandler((req) => links.regenerateLink(
  ownerId(req), req.params.id, req.params.partyId, req, publicOrigin(req),
)));

router.post('/documents/:id/parties/:partyId/revoke', jsonHandler((req) => links.revokeLink(
  ownerId(req), req.params.id, req.params.partyId, req,
)));

router.post('/documents/:id/parties/:partyId/remind', remindLimiter, jsonHandler((req) => ({
  delivery: remindParty(ownerId(req), req.params.id, req.params.partyId, req.body),
}), { status: 202 }));

router.post('/documents/:id/remind-all', remindLimiter, jsonHandler((req) => remindAll(ownerId(req), req.params.id, req.body), { status: 202 }));

router.post('/documents/:id/parties/:partyId/open-as-owner', jsonHandler((req) => openAsOwner(
  ownerId(req), req.params.id, req.params.partyId,
)));

router.get('/documents/:id/pdf', streaming(async (req, res) => {
  await sendSnapshotPdf(req, res, getOwnedDocument(ownerId(req), req.params.id));
}));

router.get('/documents/:id/preview-values', jsonHandler((req) => results.previewValues(ownerId(req), req.params.id)));

router.get('/documents/:id/values/:fieldId/image', streaming(async (req, res) => {
  const { row, size, ownerId: owner } = results.ownerImage(ownerId(req), req.params.id, req.params.fieldId);
  await sendValueImage(res, owner, row, size);
}));

router.get('/documents/:id/events', jsonHandler((req) => audit.eventsOf(ownerId(req), req.params.id)));

router.get('/documents/:id/audit.json', jsonHandler((req, res) => {
  res.set('Content-Disposition', `attachment; filename="signing-audit-${req.params.id}.json"`);
  return audit.auditRecord(ownerId(req), req.params.id);
}));

// ?certificate=0 gives the signed file without the audit pages (built on first use, then cached).
router.get('/documents/:id/final', streaming(async (req, res) => {
  if (!results.wantsCertificate(req.query)) {
    const { document, fileName } = await results.cleanForOwner(ownerId(req), req.params.id, req);
    await sendCleanPdf(req, res, document, fileName);
    return;
  }
  const { ownerId: owner, nodeId } = results.finalForOwner(ownerId(req), req.params.id, req);
  await sendFile(req, res, owner, nodeId, { download: true });
}));

// The same clean variant, found through the signed file in the library.
router.get('/nodes/:nodeId/clean', streaming(async (req, res) => {
  const { document, fileName } = await results.cleanForNode(ownerId(req), req.params.nodeId, req);
  await sendCleanPdf(req, res, document, fileName);
}));

// The signer's own side: profile, saved signatures, claiming a link, the inbox.
router.get('/profile', jsonHandler((req) => profile.readProfile(ownerId(req))));

router.put('/profile', jsonHandler((req) => profile.saveProfile(ownerId(req), req.body)));

// The body is the raw PNG, like a signer's upload.
router.post('/profile/images', heavyLimiter, jsonHandler((req) => images.addImage(ownerId(req), req), { status: 201 }));

router.get('/profile/images/:id', streaming(async (req, res) => {
  const row = images.imageRow(ownerId(req), req.params.id);
  await sendValueImage(res, row.user_id, { blob_ref: row.blob_id }, row.size);
}));

router.put('/profile/images/:id', jsonHandler((req) => images.updateImage(ownerId(req), req.params.id, req.body)));

router.delete('/profile/images/:id', jsonHandler((req) => images.deleteImage(ownerId(req), req.params.id)));

router.post('/claim', claimLimiter, jsonHandler((req) => claimParty(ownerId(req), req.body, req)));

router.get('/inbox', jsonHandler((req) => listInbox(ownerId(req), typeof req.query.status === 'string' ? req.query.status : undefined)));

router.post('/inbox/:partyId/open', claimLimiter, jsonHandler((req) => openInboxParty(ownerId(req), req.params.partyId)));

// The signed file for a party the account holds, once the document is completed
// (the grant the party signed through is gone by then). Same variants as the public one.
router.get('/inbox/:partyId/final', streaming(async (req, res) => {
  const context = finalContext(ownerId(req), req.params.partyId);
  if (!results.wantsCertificate(req.query)) {
    const { document, fileName } = await results.cleanForSigner(context, req);
    await sendCleanPdf(req, res, document, fileName);
    return;
  }
  const { ownerId: owner, nodeId } = results.finalForSigner(context, req);
  await sendFile(req, res, owner, nodeId, { download: true });
}));

module.exports = router;
