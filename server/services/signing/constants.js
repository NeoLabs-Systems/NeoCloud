'use strict';

// Shared vocabulary and limits of the signing feature.

const DOCUMENT_STATUS = Object.freeze({
  DRAFT: 'draft',
  SENT: 'sent',
  IN_PROGRESS: 'in_progress',
  FINALIZING: 'finalizing',
  COMPLETED: 'completed',
  DECLINED: 'declined',
  EXPIRED: 'expired',
  CANCELED: 'canceled',
});

const PARTY_STATUS = Object.freeze({
  PENDING: 'pending',
  WAITING: 'waiting',
  OPENED: 'opened',
  COMPLETED: 'completed',
  DECLINED: 'declined',
});

// Documents that people can still act on.
const LIVE_STATUSES = [DOCUMENT_STATUS.SENT, DOCUMENT_STATUS.IN_PROGRESS];
// Documents that can be deleted: nothing is waiting on a signer.
const DELETABLE_STATUSES = [
  DOCUMENT_STATUS.DRAFT,
  DOCUMENT_STATUS.COMPLETED,
  DOCUMENT_STATUS.DECLINED,
  DOCUMENT_STATUS.EXPIRED,
  DOCUMENT_STATUS.CANCELED,
];

const SIGNING_ORDERS = ['preserved', 'random'];
const LOCALES = ['en', 'de'];
const DEFAULT_LOCALE = 'en';

const EVENT = Object.freeze({
  CREATED: 'created',
  UPDATED: 'updated',
  SENT: 'sent',
  LINK_REGENERATED: 'link_regenerated',
  REVOKED: 'revoked',
  OPENED: 'opened',
  CONSENTED: 'consented',
  PARTY_COMPLETED: 'party_completed',
  PARTY_DECLINED: 'party_declined',
  COMPLETED: 'completed',
  FINALIZED: 'finalized',
  CANCELED: 'canceled',
  EXPIRED: 'expired',
  FINAL_DOWNLOADED: 'final_downloaded',
});

const LIMITS = Object.freeze({
  maxSourceBytes: 50 * 1024 * 1024,
  maxPages: 300,
  maxParties: 20,
  maxFields: 500,
  maxAreasPerField: 20,
  maxTitleLength: 200,
  maxNameLength: 100,
  maxEmailLength: 254,
  maxDeclineReason: 500,
  maxSignerName: 120,
  // Uploads by signers.
  maxImageBytes: 8 * 1024 * 1024,
  maxFileBytes: 10 * 1024 * 1024,
  maxUploadBytesPerDocument: 50 * 1024 * 1024,
});

const DEFAULT_VALIDITY_DAYS = 30;
const MAX_VALIDITY_DAYS = 365;

// The web app's page for a signer; the token is its last path segment.
const SIGN_PAGE_PATH = '/app/sign/';

const PARTY_COLORS = [
  '#D4A017', '#2E86AB', '#C0392B', '#27AE60', '#8E44AD',
  '#E67E22', '#16A085', '#D81B60', '#5C6BC0', '#7F8C8D',
];

module.exports = {
  DEFAULT_LOCALE,
  DEFAULT_VALIDITY_DAYS,
  DELETABLE_STATUSES,
  DOCUMENT_STATUS,
  EVENT,
  LIMITS,
  LIVE_STATUSES,
  LOCALES,
  MAX_VALIDITY_DAYS,
  PARTY_COLORS,
  PARTY_STATUS,
  SIGNING_ORDERS,
  SIGN_PAGE_PATH,
};
