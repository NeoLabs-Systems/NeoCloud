'use strict';

const express = require('express');
const rateLimit = require('express-rate-limit');
const { requireAdmin } = require('../middleware/auth');
const users = require('../services/admin/users');
const serverConfig = require('../services/admin/server_config');
const { listAccessEvents } = require('../services/access/audit');
const { getServerVersion } = require('../utils/version');
const backupTargets = require('../services/backup/targets');
const backupRunner = require('../services/backup/runner');
const { verifyTarget } = require('../services/backup/verify');
const { getIndexStatus, rebuildIndex } = require('../services/search_index/indexer');
const { ownSessionId } = require('../services/account/sessions');
const { listProviders, updateProvider } = require('../services/admin/ai_providers');
const { getUserModelGrants, setUserModelGrants } = require('../services/admin/model_grants');
const aiAdmin = require('../services/admin/ai');
const { getSupportedModels } = require('../services/ai/models');
const modelVisibility = require('../services/ai/model_visibility');
const { usageSummary } = require('../services/ai/usage');
const { jsonHandler, intParam } = require('./_helpers/http');
const { httpError } = require('../utils/http_error');

const router = express.Router();
router.use(requireAdmin);

const settingsLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 60,
  message: { error: 'Too many changes, try again shortly', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

const keyExportLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 5,
  message: { error: 'Too many attempts, try again later', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

router.get('/version', jsonHandler(() => getServerVersion()));
router.get('/health', jsonHandler(() => serverConfig.getHealth()));
router.get('/logs', jsonHandler((req) => ({ logs: req.app.locals.logHistory || [] })));
router.get('/audit', jsonHandler(() => listAccessEvents({ limit: 200 })));

router.get('/users', jsonHandler((req) => users.listUsers(req.query.q ? String(req.query.q) : null)));
router.put('/users/:id/quota', settingsLimiter, jsonHandler((req) => users.setUserQuota(
  intParam(req.params.id, 'user id'),
  { mode: req.body?.mode, bytes: req.body?.bytes },
  { actorUserId: req.userId },
)));
router.put('/users/:id/admin', settingsLimiter, jsonHandler((req) => users.setUserAdmin(
  intParam(req.params.id, 'user id'),
  req.body?.isAdmin,
  { actorUserId: req.userId },
)));
router.post('/users/:id/password', settingsLimiter, jsonHandler((req) => users.resetUserPassword(
  intParam(req.params.id, 'user id'),
  req.body?.password,
  { actorUserId: req.userId },
)));
router.put('/users/:id/disabled', settingsLimiter, jsonHandler((req) => users.setUserDisabled(
  intParam(req.params.id, 'user id'),
  req.body?.disabled,
  { actorUserId: req.userId },
)));
router.delete('/users/:id/sessions', settingsLimiter, jsonHandler((req) => users.revokeUserSessions(
  intParam(req.params.id, 'user id'),
  { actorUserId: req.userId, sessionId: ownSessionId(req), deviceId: req.deviceId || null },
)));
router.post('/users/:id/usage/recalculate', jsonHandler((req) => users.recalculateUserUsage(intParam(req.params.id, 'user id'))));
router.delete('/users/:id', settingsLimiter, jsonHandler((req) => users.deleteUser(
  intParam(req.params.id, 'user id'),
  { actorUserId: req.userId },
)));

router.get('/config/storage', jsonHandler(() => serverConfig.getStorageSettings()));
router.put('/config/storage', settingsLimiter, jsonHandler((req) => serverConfig.updateStorageSettings(req.body)));
router.post('/storage/recalculate', settingsLimiter, jsonHandler(() => serverConfig.recalculateAllUsage()));

router.get('/access', jsonHandler(() => serverConfig.getAccessSettings()));
router.put('/access/signup', settingsLimiter, jsonHandler((req) => serverConfig.setAllowSignup(req.body?.allowSignup)));

router.get('/backups', jsonHandler(() => ({ targets: backupTargets.listTargets(), runs: backupTargets.listRuns() })));
router.post('/backups/targets', settingsLimiter, jsonHandler((req) => backupTargets.createTarget(req.body), { status: 201 }));
router.put('/backups/targets/:id', settingsLimiter, jsonHandler((req) => backupTargets.updateTarget(intParam(req.params.id, 'target id'), req.body)));
router.delete('/backups/targets/:id', jsonHandler((req) => backupTargets.deleteTarget(intParam(req.params.id, 'target id'))));
router.post('/backups/targets/:id/test', settingsLimiter, jsonHandler((req) => backupRunner.testTarget(intParam(req.params.id, 'target id'))));
router.post('/backups/targets/:id/run', jsonHandler((req) => backupRunner.startBackup(intParam(req.params.id, 'target id')), { status: 202 }));
router.post('/backups/targets/:id/verify', settingsLimiter, jsonHandler((req) => verifyTarget(intParam(req.params.id, 'target id'))));
router.get('/backups/targets/:id/snapshots', jsonHandler((req) => backupRunner.snapshotsOf(intParam(req.params.id, 'target id'))));

router.get('/config', jsonHandler(() => serverConfig.getServerConfig()));
router.put('/config', settingsLimiter, jsonHandler((req) => serverConfig.updateServerConfig(req.body)));
router.get('/search-index', jsonHandler(() => getIndexStatus()));
router.post('/search-index/rebuild', settingsLimiter, jsonHandler(() => rebuildIndex(), { status: 202 }));

// --- AI: providers, models, grants, switches and usage (docs/ai.md) ---

router.get('/providers', jsonHandler(() => listProviders()));
router.put('/providers', settingsLimiter, jsonHandler((req) => updateProvider(req.body?.key, req.body?.value)));

// Records newly discovered models and applies the auto-enable policy to them.
router.get('/models', jsonHandler(async () => {
  const models = await getSupportedModels(null);
  const disabledModels = modelVisibility.reconcileModelVisibility(models.map((model) => model.id));
  return { models, disabledModels, autoEnableNewModels: modelVisibility.getAutoEnableNewModels() };
}));
router.put('/models/config', settingsLimiter, jsonHandler((req) => {
  const disabledModels = req.body?.disabledModels;
  if (!Array.isArray(disabledModels)) throw httpError(400, 'disabledModels must be an array', 'INVALID_VALUE');
  if (typeof req.body?.autoEnableNewModels === 'boolean') {
    modelVisibility.setAutoEnableNewModels(req.body.autoEnableNewModels);
  }
  return {
    ok: true,
    disabledModels: modelVisibility.setDisabledModelIds(disabledModels),
    autoEnableNewModels: modelVisibility.getAutoEnableNewModels(),
  };
}));

router.get('/users/:id/allowed-models', jsonHandler((req) => getUserModelGrants(intParam(req.params.id, 'user id'))));
router.put('/users/:id/allowed-models', settingsLimiter, jsonHandler(async (req) => {
  const userId = intParam(req.params.id, 'user id');
  const catalog = await getSupportedModels(null);
  return setUserModelGrants(userId, req.body?.modelIds ?? null, {
    actorUserId: req.userId,
    catalogIds: catalog.map((model) => model.id),
  });
}));
router.put('/users/:id/ai-budget', settingsLimiter, jsonHandler((req) => aiAdmin.setUserAiBudget(
  intParam(req.params.id, 'user id'),
  req.body?.monthlyTokens,
  { actorUserId: req.userId },
)));

router.get('/ai', jsonHandler(() => aiAdmin.getAiConfig()));
router.put('/ai', settingsLimiter, jsonHandler((req) => aiAdmin.updateAiConfig(req.body)));
router.get('/ai/usage', jsonHandler(() => usageSummary()));

router.post('/keys/master', keyExportLimiter, jsonHandler((req) => serverConfig.exportMasterKey(req.userId, {
  currentPassword: req.body?.password,
  code: req.body?.code,
})));

module.exports = router;
