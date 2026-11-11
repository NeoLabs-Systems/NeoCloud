'use strict';

const { jsonHandler } = require('../routes/_helpers/http');
const { getServerVersion } = require('../utils/version');
const { checkHealth } = require('../services/health');

// Mount point → router module. Each router applies its own auth guard.
const ROUTE_REGISTRY = [
  { basePath: '/api/auth', modulePath: '../routes/auth' },
  { basePath: '/api/account', modulePath: '../routes/account' },
  { basePath: '/api/settings', modulePath: '../routes/settings' },
  { basePath: '/api/admin', modulePath: '../routes/admin' },
  { basePath: '/api', modulePath: '../routes/files' },
  { basePath: '/api', modulePath: '../routes/transfers' },
  { basePath: '/api/sync', modulePath: '../routes/sync' },
  { basePath: '/api/devices', modulePath: '../routes/devices' },
  { basePath: '/api/storage', modulePath: '../routes/storage' },
  { basePath: '/api/shares', modulePath: '../routes/shares' },
  { basePath: '/api/public/shares', modulePath: '../routes/public_shares' },
  { basePath: '/api/vaults', modulePath: '../routes/vaults' },
  { basePath: '/api/imports', modulePath: '../routes/imports' },
];

function registerApiRoutes(app) {
  app.get('/api/health', (req, res) => {
    const health = checkHealth();
    res.status(health.status === 'ok' ? 200 : 503).json(health);
  });
  app.get('/api/version', jsonHandler(() => getServerVersion()));
  for (const { basePath, modulePath } of ROUTE_REGISTRY) {
    app.use(basePath, require(modulePath));
  }
}

module.exports = { registerApiRoutes };
