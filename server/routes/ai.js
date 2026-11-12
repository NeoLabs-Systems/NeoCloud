'use strict';

// AI for the signed-in account: what can run, which folders AI must never
// read, the review queue of suggestions, the jobs behind it, sorting rules
// and summaries. A browser session or the phone app; desktop tokens don't
// reach these routes.

const express = require('express');
const { requireSessionOrPhone } = require('../middleware/auth');
const { getAiStatus } = require('../services/ai/status');
const { setFolderAiExcluded } = require('../services/ai/exclusions');
const suggestions = require('../services/ai/organize/suggestions');
const review = require('../services/ai/organize/review');
const jobs = require('../services/ai/organize/jobs');
const rules = require('../services/ai/organize/rules');
const { summarizeFile } = require('../services/ai/organize/summaries');
const { jsonHandler, rateLimiter } = require('./_helpers/http');

const router = express.Router();
router.use(requireSessionOrPhone);

// Starting model work costs tokens; deciding on suggestions doesn't.
const workLimiter = rateLimiter(300);

function queryText(value) {
  return typeof value === 'string' ? value : undefined;
}

router.get('/status', jsonHandler((req) => getAiStatus(req.userId)));
router.put('/folders/:id/excluded', jsonHandler((req) => setFolderAiExcluded(req.userId, req.params.id, req.body?.excluded)));

router.get('/suggestions', jsonHandler((req) => suggestions.listSuggestions(req.userId, {
  kind: queryText(req.query.kind),
  tier: queryText(req.query.tier),
  status: queryText(req.query.status),
  nodeId: queryText(req.query.nodeId),
  limit: queryText(req.query.limit),
  offset: queryText(req.query.offset),
})));
router.get('/suggestions/counts', jsonHandler((req) => suggestions.suggestionCounts(req.userId)));
router.post('/suggestions/accept-high', jsonHandler((req) => review.acceptAllHigh(req.userId, { kind: req.body?.kind })));
router.post('/suggestions/:id/accept', jsonHandler((req) => review.acceptSuggestion(req.userId, req.params.id, { value: req.body?.value })));
router.post('/suggestions/:id/reject', jsonHandler((req) => review.rejectSuggestion(req.userId, req.params.id)));
router.post('/suggestions/:id/undo', jsonHandler((req) => review.undoSuggestion(req.userId, req.params.id)));

router.get('/jobs', jsonHandler((req) => jobs.jobStatus(req.userId, { batchId: queryText(req.query.batchId) })));
router.post('/jobs', workLimiter, jsonHandler((req) => jobs.requestSuggestions(req.userId, {
  nodeIds: req.body?.nodeIds,
  folderId: req.body?.folderId,
  kinds: req.body?.kinds,
}), { status: 202 }));

router.get('/rules', jsonHandler((req) => rules.listRules(req.userId)));
router.post('/rules', jsonHandler((req) => rules.createRule(req.userId, req.body), { status: 201 }));
router.get('/rules/proposals', jsonHandler((req) => rules.listProposals(req.userId)));
router.post('/rules/proposals/:id', jsonHandler((req) => rules.acceptProposal(req.userId, req.params.id, { name: req.body?.name }), { status: 201 }));
router.delete('/rules/proposals/:id', jsonHandler((req) => rules.dismissProposal(req.userId, req.params.id)));
router.patch('/rules/:id', jsonHandler((req) => rules.updateRule(req.userId, req.params.id, req.body)));
router.delete('/rules/:id', jsonHandler((req) => rules.deleteRule(req.userId, req.params.id)));

router.post('/files/:id/summary', workLimiter, jsonHandler((req) => summarizeFile(req.userId, req.params.id, {
  refresh: req.body?.refresh === true,
})));

module.exports = router;
