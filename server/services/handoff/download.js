'use strict';

// Serving a send to its receiver: one file with HTTP Range (so a long
// download over a flaky link resumes), or all files as a ZIP. Only the device
// the send is addressed to, and only after it accepted: the offer row is the
// whole authorisation, there is no separate link to leak.

const { pipeline } = require('stream/promises');
const { ZipArchive } = require('archiver');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { contentDisposition } = require('../../utils/file_names');
const { parseRange } = require('../../utils/http_range');
const { openBlob, openBlobChecked } = require('../storage/blob_store');
const { deliverWhenFetched } = require('./delivery');
const { receiverRow } = require('./offers');

const SANDBOX_CSP = "sandbox; default-src 'none'";

function acceptedOffer(userId, key, id) {
  const row = receiverRow(userId, key, id);
  if (row.status !== 'accepted') throw httpError(409, 'Accept the send first.', 'NOT_ACCEPTED');
  if (new Date(`${row.expires_at.replace(' ', 'T')}Z`).getTime() <= Date.now()) {
    throw httpError(410, 'This send has expired.', 'OFFER_EXPIRED');
  }
  return row;
}

function stagedFile(offerId, fileId) {
  const file = db.prepare('SELECT * FROM handoff_files WHERE id = ? AND offer_id = ?').get(Number(fileId), offerId);
  if (!file || !file.blob_id) throw httpError(404, 'File not found.', 'NOT_FOUND');
  return file;
}

// Always an attachment with no sniffing: a sent file is never rendered by the app.
async function sendFile(req, res, userId, key, offerId, fileId) {
  const offer = acceptedOffer(userId, key, offerId);
  const file = stagedFile(offer.id, fileId);
  const range = parseRange(req.get('range'), file.size);
  const start = range && !range.unsatisfiable ? range.start : 0;
  const end = range && !range.unsatisfiable ? range.end : file.size - 1;
  const content = range?.unsatisfiable || req.method === 'HEAD' || file.size === 0
    ? null
    : await openBlobChecked(userId, file.blob_id, file.size, { start, end });
  res.set({
    'Content-Type': 'application/octet-stream',
    'Content-Disposition': contentDisposition('attachment', file.name),
    'Accept-Ranges': 'bytes',
    ETag: `"${file.blob_id}"`,
    'Cache-Control': 'no-store',
    'X-Content-Type-Options': 'nosniff',
    'Content-Security-Policy': SANDBOX_CSP,
  });
  if (range?.unsatisfiable) {
    res.status(416).set('Content-Range', `bytes */${file.size}`).end();
    return;
  }
  if (range) res.status(206).set('Content-Range', `bytes ${start}-${end}/${file.size}`);
  res.set('Content-Length', String(Math.max(0, end - start + 1)));
  if (!content) {
    res.end();
    if (file.size === 0 && req.method !== 'HEAD') await deliverWhenFetched(userId, offer.id, [file.id]);
    return;
  }
  await pipeline(content, res);
  if (end === file.size - 1) await deliverWhenFetched(userId, offer.id, [file.id]);
}

/** Every file of the send as one stored (not recompressed) ZIP, for browsers that block several downloads. */
async function sendZip(req, res, userId, key, offerId) {
  const offer = acceptedOffer(userId, key, offerId);
  const files = db.prepare('SELECT * FROM handoff_files WHERE offer_id = ? AND blob_id IS NOT NULL ORDER BY position').all(offer.id);
  res.set({
    'Content-Type': 'application/zip',
    'Content-Disposition': contentDisposition('attachment', 'NeoCloud files.zip'),
    'Cache-Control': 'no-store',
    'X-Content-Type-Options': 'nosniff',
  });
  if (req.method === 'HEAD') {
    res.end();
    return;
  }
  const archive = new ZipArchive({ store: true });
  for (const file of files) archive.append(openBlob(userId, file.blob_id, file.size), { name: file.name });
  const done = pipeline(archive, res);
  archive.finalize();
  await done;
  await deliverWhenFetched(userId, offer.id, files.map((file) => file.id));
}

module.exports = { sendFile, sendZip };
