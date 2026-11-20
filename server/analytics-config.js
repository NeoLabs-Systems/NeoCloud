'use strict';

const COUNTLY_ENDPOINT = new URL('https://data.neo.eu.com/i');

const config = Object.freeze({
  endpoint: COUNTLY_ENDPOINT.href,
  origin: COUNTLY_ENDPOINT.origin,
  appKey: 'b2c25a128d98c32f5fcf7e1b06a5f15433d458b4',
});

function analyticsConfig() {
  const enabled = (process.env.ANALYTICS_ENABLED || '').trim().toLowerCase();
  return ['false', '0', 'off', 'no'].includes(enabled) ? null : config;
}

module.exports = { analyticsConfig };
