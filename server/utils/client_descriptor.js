'use strict';

// Turns a User-Agent string, plus the labels a client says about itself,
// into what a person recognises: "macOS · Safari", "Android · App".

function trimmedString(value, maxLength = 160) {
  return String(value || '').trim().slice(0, maxLength);
}

const DEVICE_CLASSES = ['mobile', 'tablet', 'desktop', 'server', 'unknown'];

/** The labels a client may send about itself, trimmed and whitelisted. */
function normalizeClientMetadata(value) {
  const raw = value && typeof value === 'object' && !Array.isArray(value) ? value : {};
  const metadata = {
    deviceLabel: trimmedString(raw.deviceLabel, 120),
    platformLabel: trimmedString(raw.platformLabel, 60),
    browserLabel: trimmedString(raw.browserLabel, 60),
    deviceClass: trimmedString(raw.deviceClass, 24).toLowerCase(),
    appMode: trimmedString(raw.appMode, 24).toLowerCase(),
    platform: trimmedString(raw.platform, 32).toLowerCase(),
  };
  if (!DEVICE_CLASSES.includes(metadata.deviceClass)) metadata.deviceClass = '';
  return metadata;
}

function platformFromAgent(lower) {
  if (lower.includes('iphone')) return 'iPhone';
  if (lower.includes('ipad')) return 'iPad';
  if (lower.includes('android')) return 'Android';
  if (lower.includes('mac os x') || lower.includes('macintosh')) return 'macOS';
  if (lower.includes('windows nt')) return 'Windows';
  if (lower.includes('linux') || lower.includes('x11')) return 'Linux';
  if (lower.includes('curl/') || lower.includes('wget/') || lower.includes('httpie/')) return 'CLI session';
  return 'Unknown device';
}

function browserFromAgent(lower) {
  if (lower.includes('edg/')) return 'Edge';
  if (lower.includes('opr/') || lower.includes('opera/')) return 'Opera';
  if (lower.includes('brave/')) return 'Brave';
  if (lower.includes('firefox/')) return 'Firefox';
  if (lower.includes('chrome/') || lower.includes('crios/') || lower.includes('chromium/')) return 'Chrome';
  if (lower.includes('safari/') && lower.includes('version/')) return 'Safari';
  if (lower.includes('dart/')) return 'Flutter app';
  if (lower.includes('curl/')) return 'curl';
  if (lower.includes('wget/')) return 'wget';
  if (lower.includes('httpie/')) return 'HTTPie';
  return 'Unknown browser';
}

function deviceClassFor(lower, platformLabel) {
  const isTablet = lower.includes('ipad') || lower.includes('tablet');
  const isMobile = !isTablet && (lower.includes('iphone') || (lower.includes('android') && lower.includes('mobile')));
  if (platformLabel === 'CLI session') return 'server';
  if (isTablet) return 'tablet';
  if (isMobile) return 'mobile';
  if (['macOS', 'Windows', 'Linux'].includes(platformLabel)) return 'desktop';
  return 'unknown';
}

function describeClient(userAgent, metadata = {}) {
  const lower = String(userAgent || '').toLowerCase();
  const platformLabel = metadata.platformLabel || platformFromAgent(lower);
  const browserLabel = metadata.browserLabel || browserFromAgent(lower);
  const deviceClass = metadata.deviceClass || deviceClassFor(lower, platformLabel);
  let label = metadata.deviceLabel;
  if (!label) {
    const parts = [platformLabel];
    if (browserLabel === 'Flutter app') parts.push('App');
    else if (browserLabel !== 'Unknown browser') parts.push(browserLabel);
    label = parts.join(' · ');
  }
  return { label, platformLabel, browserLabel, deviceClass };
}

module.exports = { describeClient, normalizeClientMetadata, trimmedString };
