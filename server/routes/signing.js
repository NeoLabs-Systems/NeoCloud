'use strict';

// E-signature documents as their owner manages them (browser session or the
// phone app). What signers do with their links is in public_signing.js.

const express = require('express');
const { requireSessionOrPhone } = require('../middleware/auth');
const documents = require('../services/signing/documents');
const links = require('../services/signing/links');
const audit = require('../services/signing/audit');
const results = require('../services/signing/results');
const { getOwnedDocument } = require('../services/signing/store');
const { signingCapabilities } = require('../services/signing/snapshot');
const { sendCleanPdf, sendSnapshotPdf, sendValueImage } = require('../services/signing/serve');
const { sendFile } = require('../services/transfer/downloads');
const { jsonHandler, publicOrigin, rateLimiter, streaming } = require('./_helpers/http');

const router = express.Router();
router.use(requireSessionOrPhone);
router.use(rateLimiter(1500));
// Creating and sending work on whole files: a tighter bound.
const heavyLimiter = rateLimiter(200);

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

router.post('/documents/:id/send', heavyLimiter, jsonHandler((req) => links.sendDocument(ownerId(req), req.params.id, req, publicOrigin(req))));

router.post('/documents/:id/cancel', jsonHandler((req) => documents.cancelDocument(ownerId(req), req.params.id, req)));

router.post('/documents/:id/duplicate', heavyLimiter, jsonHandler((req) => documents.duplicateDocument(ownerId(req), req.params.id, req), { status: 201 }));

router.post('/documents/:id/finalize', jsonHandler((req) => results.retryFinalization(ownerId(req), req.params.id)));

router.post('/documents/:id/parties/:partyId/link', jsonHandler((req) => links.regenerateLink(
  ownerId(req), req.params.id, req.params.partyId, req, publicOrigin(req),
)));

router.post('/documents/:id/parties/:partyId/revoke', jsonHandler((req) => links.revokeLink(
  ownerId(req), req.params.id, req.params.partyId, req,
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

module.exports = router;
