'use strict';

// An account's signing profile: the details used to fill in signing pages
// (name, email, phone, named presets such as Address or Company), the switches
// (fill in, save to the cloud, notify on each signature) and the folder signed
// documents are saved to. The personal details and presets are one JSON text
// encrypted with the server's secret key; the switches are plain columns the
// server reads without decrypting. Saved signatures: profile_images.js.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { decryptValue, encryptValue } = require('../crypto/keys');
const { usableFolder } = require('./folders');
const { signaturesOf } = require('./profile_images');
const { singleLine, withoutInvisible } = require('./plain_text');
const { UUID_PATTERN, nowIso, parseJson } = require('./store');

const LIMITS = Object.freeze({
  maxPresets: 30,
  maxLabel: 60,
  maxValue: 500,
  maxName: 120,
  maxPhone: 40,
  maxEmail: 254,
});

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function invalid(message) {
  return httpError(400, message, 'INVALID_PROFILE');
}

const rowOf = (userId) => db.prepare('SELECT * FROM sign_profiles WHERE user_id = ?').get(userId);

// What the account itself says, until the person saves a profile of their own.
function accountDefaults(userId) {
  const user = db.prepare('SELECT username, display_name, email FROM users WHERE id = ?').get(userId);
  return { displayName: user?.display_name || user?.username || '', email: user?.email || '', phone: '', presets: [] };
}

function detailsOf(userId, row) {
  if (!row?.data_enc) return accountDefaults(userId);
  const data = parseJson(decryptValue(row.data_enc), {});
  return {
    displayName: data.displayName || '',
    email: data.email || '',
    phone: data.phone || '',
    presets: Array.isArray(data.presets) ? data.presets : [],
  };
}

/** The profile as the API shows it. */
function readProfile(userId) {
  const row = rowOf(userId);
  const details = detailsOf(userId, row);
  return {
    ...details,
    autoFill: row ? row.auto_fill === 1 : true,
    autoSave: row ? row.auto_save === 1 : true,
    saveFolderId: row?.save_folder_id ?? null,
    notifyOnEachSignature: row ? row.notify_each_signature === 1 : false,
    signatures: signaturesOf(userId),
  };
}

/** Whether the owner wants a mail for every signature (off until they say so). */
function notifiesOnEachSignature(userId) {
  return rowOf(userId)?.notify_each_signature === 1;
}

/** Whether signed documents are saved to the account's cloud, and where (null: the automatic folder). */
function autoSaveSettings(userId) {
  const row = rowOf(userId);
  return { enabled: row ? row.auto_save === 1 : true, folderId: row?.save_folder_id ?? null };
}

const comparable = (label) => label.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase();

function cleanText(value, what, max) {
  const text = typeof value === 'string' ? singleLine(value) : null;
  if (text === null || text.length > max) throw invalid(`${what} must be text of at most ${max} characters.`);
  return text;
}

function cleanEmail(value) {
  const email = cleanText(value, 'The email', LIMITS.maxEmail);
  if (email && !EMAIL_PATTERN.test(email)) throw invalid('That email address is not valid.');
  return email;
}

function cleanPresetValue(value) {
  if (typeof value !== 'string') throw invalid('A preset value must be text.');
  const text = withoutInvisible(value).replace(/\r\n?/g, '\n').replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/g, '').trim();
  if (text.length > LIMITS.maxValue) throw invalid(`A preset value has at most ${LIMITS.maxValue} characters.`);
  return text;
}

function cleanPresets(input) {
  if (!Array.isArray(input) || input.length > LIMITS.maxPresets) throw invalid(`Up to ${LIMITS.maxPresets} presets are possible.`);
  const seen = new Set();
  return input.map((entry) => {
    if (!entry || typeof entry !== 'object') throw invalid('Each preset must be an object.');
    const label = cleanText(entry.label, 'A preset label', LIMITS.maxLabel);
    if (!label) throw invalid('A preset needs a label.');
    if (seen.has(comparable(label))) throw invalid('Preset labels must be different.');
    seen.add(comparable(label));
    const id = typeof entry.id === 'string' && UUID_PATTERN.test(entry.id) ? entry.id : crypto.randomUUID();
    return { id, label, value: cleanPresetValue(entry.value) };
  });
}

function cleanSwitch(value, name) {
  if (typeof value !== 'boolean') throw invalid(`${name} must be true or false.`);
  return value;
}

function cleanFolder(userId, value) {
  if (value === null) return null;
  const folder = typeof value === 'string' ? usableFolder(userId, value) : null;
  if (!folder) throw httpError(400, 'Choose a folder of your own that is not a vault.', 'INVALID_FOLDER');
  return folder.id;
}

/** Changes the profile: what the body names is replaced, the rest stays. Resolves with the profile. */
function saveProfile(userId, body) {
  const input = body && typeof body === 'object' ? body : {};
  const row = rowOf(userId);
  const details = detailsOf(userId, row);
  if (input.displayName !== undefined) details.displayName = cleanText(input.displayName, 'The name', LIMITS.maxName);
  if (input.email !== undefined) details.email = cleanEmail(input.email);
  if (input.phone !== undefined) details.phone = cleanText(input.phone, 'The phone number', LIMITS.maxPhone);
  if (input.presets !== undefined) details.presets = cleanPresets(input.presets);
  const next = {
    autoFill: input.autoFill === undefined ? (row ? row.auto_fill === 1 : true) : cleanSwitch(input.autoFill, 'autoFill'),
    autoSave: input.autoSave === undefined ? (row ? row.auto_save === 1 : true) : cleanSwitch(input.autoSave, 'autoSave'),
    notify: input.notifyOnEachSignature === undefined
      ? (row ? row.notify_each_signature === 1 : false)
      : cleanSwitch(input.notifyOnEachSignature, 'notifyOnEachSignature'),
    folder: input.saveFolderId === undefined ? (row?.save_folder_id ?? null) : cleanFolder(userId, input.saveFolderId),
  };
  db.prepare(`
    INSERT INTO sign_profiles (user_id, data_enc, auto_fill, auto_save, save_folder_id, notify_each_signature, updated_at)
    VALUES (@userId, @data, @autoFill, @autoSave, @folder, @notify, @at)
    ON CONFLICT(user_id) DO UPDATE SET data_enc = excluded.data_enc, auto_fill = excluded.auto_fill, auto_save = excluded.auto_save,
      save_folder_id = excluded.save_folder_id, notify_each_signature = excluded.notify_each_signature, updated_at = excluded.updated_at
  `).run({
    userId,
    data: encryptValue(JSON.stringify(details)),
    autoFill: next.autoFill ? 1 : 0,
    autoSave: next.autoSave ? 1 : 0,
    folder: next.folder,
    notify: next.notify ? 1 : 0,
    at: nowIso(),
  });
  return readProfile(userId);
}

/** The details the prefill uses, with the switch: { details, autoFill }. */
function prefillSource(userId) {
  const row = rowOf(userId);
  return { details: detailsOf(userId, row), autoFill: row ? row.auto_fill === 1 : true };
}

module.exports = { LIMITS, autoSaveSettings, notifiesOnEachSignature, prefillSource, readProfile, saveProfile };
