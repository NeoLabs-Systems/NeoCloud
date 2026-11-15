'use strict';

// A readable name for a browser, from its User-Agent ("Chrome on macOS").
// Browsers have no name of their own; apps are named by their owner.

const BROWSERS = [[/Edg\//, 'Edge'], [/OPR\/|Opera/, 'Opera'], [/Firefox\//, 'Firefox'], [/Chrome\//, 'Chrome'], [/Safari\//, 'Safari']];
const SYSTEMS = [[/Windows/, 'Windows'], [/Android/, 'Android'], [/iPhone|iPad|iOS/, 'iOS'], [/Mac OS X|Macintosh/, 'macOS'], [/Linux|X11/, 'Linux']];

function firstMatch(table, text, fallback) {
  const hit = table.find(([pattern]) => pattern.test(text));
  return hit ? hit[1] : fallback;
}

function browserLabel(userAgent) {
  const text = String(userAgent || '');
  return `${firstMatch(BROWSERS, text, 'Browser')} on ${firstMatch(SYSTEMS, text, 'this device')}`;
}

/** The platform name a browser's system maps to (a device token's platform values). */
function browserPlatform(userAgent) {
  const system = firstMatch(SYSTEMS, String(userAgent || ''), '');
  return system.toLowerCase();
}

module.exports = { browserLabel, browserPlatform };
