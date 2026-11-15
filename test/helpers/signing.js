'use strict';

// Shared set-up for the signing tests: a real PDF, a document with fields,
// and the calls a signer makes with a link.

const { PDFDocument } = require('pdf-lib');
const sharp = require('sharp');
const { app, request, upload } = require('./app');

async function makePdf(pages = 2) {
  const doc = await PDFDocument.create();
  for (let index = 0; index < pages; index += 1) {
    const page = doc.addPage([595, 842]);
    page.drawText(`Contract page ${index + 1}`, { x: 60, y: 760, size: 18 });
  }
  return Buffer.from(await doc.save());
}

async function makePng(width = 300, height = 100) {
  return sharp({ create: { width, height, channels: 4, background: { r: 20, g: 20, b: 90, alpha: 1 } } }).png().toBuffer();
}

const area = (x = 0.1, y = 0.1, w = 0.3, h = 0.05, page = 0) => ({ page, x, y, w, h });

/** Uploads a PDF to the account's library and makes a draft of it. */
async function createDraft(agent, { name = 'contract.pdf', pdf, parentId = 'root' } = {}) {
  const bytes = pdf || await makePdf();
  const file = await upload(agent, name, bytes, { parentId, conflict: 'rename' }).expect(201);
  const created = await agent.post('/api/signing/documents').send({ nodeId: file.body.id }).expect(201);
  return { nodeId: file.body.id, ...created.body };
}

/** Replaces the parties and fields of a draft; returns { parties, fields } as stored. */
async function setUp(agent, documentId, { parties, fields }) {
  const saved = await agent.put(`/api/signing/documents/${documentId}/parties`).send({ parties }).expect(200);
  const stored = saved.body.parties;
  const mapped = fields.map((field) => ({ ...field, partyId: stored[field.party ?? 0].id }));
  const result = await agent.put(`/api/signing/documents/${documentId}/fields`).send({ fields: mapped }).expect(200);
  return { parties: stored, fields: result.body.fields };
}

/** Sends the draft; returns the response body and a token for each party (by position). */
async function send(agent, documentId) {
  const res = await agent.post(`/api/signing/documents/${documentId}/send`).send({}).expect(200);
  const tokens = res.body.links.map((link) => link.url.split('/').pop());
  return { ...res.body, tokens };
}

const sign = (token, path = '') => `/api/public/sign/${token}${path}`;
const publicGet = (token, path = '') => request(app).get(sign(token, path));
const publicPut = (token, path, body) => request(app).put(sign(token, path)).send(body);
const publicPost = (token, path, body = {}) => request(app).post(sign(token, path)).send(body);

function consent(token, signerName = 'Alex Signer') {
  return publicPost(token, '/consent', { accepted: true, signerName }).expect(200);
}

function uploadImage(token, fieldId, png, query = 'method=draw') {
  return request(app)
    .post(`${sign(token, `/fields/${fieldId}/upload`)}?${query}`)
    .set('Content-Type', 'image/png')
    .send(png);
}

/** Polls until `check` returns a truthy value (finalization runs in the background). */
async function until(check, { timeoutMs = 15000, stepMs = 50 } = {}) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const value = await check();
    if (value) return value;
    if (Date.now() > deadline) throw new Error('timed out waiting');
    await new Promise((resolve) => setTimeout(resolve, stepMs));
  }
}

module.exports = {
  area, consent, createDraft, makePdf, makePng, publicGet, publicPost, publicPut, send, setUp, sign, uploadImage, until,
};
