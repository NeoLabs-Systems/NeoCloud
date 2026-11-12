'use strict';

const { getProviderHealthCatalog, getSupportedModels } = require('./models');
const { isAdminUser } = require('../access/admin');

/** The models this account can pick from, every kind. */
async function listModelsFor(userId, { signal } = {}) {
  return { models: await getSupportedModels(userId, { signal }) };
}

/** Every provider, its state for this account and how many models it offers. */
async function listProvidersFor(userId, { signal } = {}) {
  const providers = await getProviderHealthCatalog(userId, { signal });
  const models = await getSupportedModels(userId, { providerCatalog: providers, signal });
  const counts = {};
  for (const model of models) {
    counts[model.provider] ??= { total: 0, available: 0 };
    counts[model.provider].total += 1;
    if (model.available !== false) counts[model.provider].available += 1;
  }
  // A base URL from the server's environment (a NeoRouter, a LAN Ollama, …)
  // is the operator's business: people see only addresses they set up.
  const isAdmin = isAdminUser(userId);
  return {
    providers: providers.map((provider) => {
      const hideBaseUrl = !isAdmin && !provider.isByok && Boolean(provider.baseUrl);
      return {
        ...provider,
        baseUrl: hideBaseUrl ? '' : provider.baseUrl,
        availabilityReason: hideBaseUrl
          ? String(provider.availabilityReason || '').split(provider.baseUrl).join('the configured address')
          : provider.availabilityReason,
        modelCount: counts[provider.id]?.total || 0,
        availableModelCount: counts[provider.id]?.available || 0,
      };
    }),
  };
}

module.exports = { listModelsFor, listProvidersFor };
