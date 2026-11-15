'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const forge = require('node-forge');
const { PDFDocument, PDFName } = require('pdf-lib');

const fx = require('../helpers/signing_pdf_fixtures');
const engine = require('../../server/services/signing/pdf');

const { PdfError } = engine;

let identity;
test.before(() => {
  identity = engine.generateSelfSignedP12({ commonName: 'NeoCloud Test Seal', years: 5 });
});

const sealOptions = () => ({ ...identity, reason: 'Test', name: 'NeoCloud', location: 'Test' });

test('generateSelfSignedP12 makes an RSA-2048 SHA-256 certificate with a random passphrase', () => {
  const { p12Buffer, passphrase } = identity;
  assert.ok(Buffer.isBuffer(p12Buffer) && p12Buffer.length > 1000);
  assert.ok(passphrase.length >= 24);
  assert.notEqual(passphrase, engine.generateSelfSignedP12({ commonName: 'Other' }).passphrase);

  const p12 = forge.pkcs12.pkcs12FromAsn1(forge.asn1.fromDer(forge.util.createBuffer(p12Buffer.toString('binary'))), false, passphrase);
  const [bag] = p12.getBags({ bagType: forge.pki.oids.certBag })[forge.pki.oids.certBag];
  assert.equal(bag.cert.subject.getField('CN').value, 'NeoCloud Test Seal');
  assert.equal(bag.cert.publicKey.n.bitLength(), 2048);
  assert.equal(forge.pki.oids[bag.cert.siginfo.algorithmOid], 'sha256WithRSAEncryption');
  const years = (bag.cert.validity.notAfter - bag.cert.validity.notBefore) / (365 * 24 * 3600 * 1000);
  assert.ok(years > 4.9 && years < 5.1);
});

test('signPdf seals a PDF and verifySignedPdf reports it intact', async () => {
  const original = await fx.plainPdf({ pages: 3 });
  assert.deepEqual(await engine.verifySignedPdf(original), { signed: false, intact: false });
  const signed = await engine.signPdf(original, sealOptions());
  assert.deepEqual(await engine.verifySignedPdf(signed), { signed: true, intact: true });
  // Still a normal PDF.
  assert.equal((await engine.probePdf(signed)).pageCount, 3);
});

test('verifySignedPdf detects any change after sealing', async () => {
  const signed = await engine.signPdf(await fx.plainPdf({ pages: 2 }), sealOptions());

  const flipped = Buffer.from(signed);
  const at = flipped.indexOf('/Type /Page') + 3; // inside the signed byte ranges
  flipped[at] = flipped[at] ^ 0x01;
  assert.deepEqual(await engine.verifySignedPdf(flipped), { signed: true, intact: false });

  const appended = Buffer.concat([signed, Buffer.from('\n% incremental update\n')]);
  assert.deepEqual(await engine.verifySignedPdf(appended), { signed: true, intact: false });

  const truncated = signed.subarray(0, signed.length - 10);
  assert.equal((await engine.verifySignedPdf(truncated)).intact, false);

  // Replacing the signature with garbage of the same length must fail too.
  const marker = signed.indexOf('/Contents <') + '/Contents <'.length;
  const forged = Buffer.from(signed);
  forged.write('ab'.repeat(200), marker, 'latin1');
  assert.equal((await engine.verifySignedPdf(forged)).intact, false);
});

test('signPdf with a wrong passphrase or no certificate fails with SIGNING_FAILED', async () => {
  const original = await fx.plainPdf();
  await assert.rejects(
    engine.signPdf(original, { ...sealOptions(), passphrase: 'wrong' }),
    (err) => err instanceof PdfError && err.code === 'SIGNING_FAILED',
  );
  await assert.rejects(engine.signPdf(original, {}), (err) => err instanceof PdfError && err.code === 'SIGNING_FAILED');
});

test('buildFinalPdf with a signer returns a sealed, intact file and its hash', async () => {
  const input = {
    pdfBytes: await fx.plainPdf(),
    parties: [{ id: 'p1', name: 'A', completedAt: '2026-11-14T09:05:00Z' }],
    fields: [{ id: 'f1', partyId: 'p1', type: 'signature', name: 's', areas: [{ page: 0, x: 0.1, y: 0.1, w: 0.3, h: 0.08 }] }],
    values: [{ fieldId: 'f1', bytes: await fx.signaturePng(), method: 'draw' }],
    events: [],
    document: { id: 'd1', title: 'Sealed', finalizedAt: '2026-11-14T10:06:00Z' },
    locale: 'en',
    signer: sealOptions(),
  };
  const { bytes, sha256 } = await engine.buildFinalPdf(input);
  assert.deepEqual(await engine.verifySignedPdf(bytes), { signed: true, intact: true });
  assert.equal(sha256, crypto.createHash('sha256').update(bytes).digest('hex'));

  // RSA PKCS#1 v1.5 is deterministic and the signing time is the fixed finalizedAt.
  const again = await engine.buildFinalPdf(input);
  assert.equal(again.sha256, sha256);

  const tampered = Buffer.from(bytes);
  tampered[tampered.indexOf('%%EOF') - 40] ^= 0x01;
  assert.equal((await engine.verifySignedPdf(tampered)).intact, false);
});

test('sealing a 50 MB PDF works; timing is reported', async () => {
  const doc = await PDFDocument.create();
  doc.addPage();
  const refs = [];
  for (let i = 0; i < 12; i += 1) refs.push(doc.context.register(doc.context.stream(crypto.randomBytes(4 * 1024 * 1024))));
  doc.catalog.set(PDFName.of('NeoBallast'), doc.context.obj(refs));
  const original = Buffer.from(await doc.save({ useObjectStreams: false }));

  const before = process.memoryUsage().rss;
  const started = Date.now();
  const signed = await engine.signPdf(original, sealOptions());
  const signedMs = Date.now() - started;
  const verified = await engine.verifySignedPdf(signed);
  const growth = Math.round((process.memoryUsage().rss - before) / 1024 / 1024);
  console.log(`# seal 48 MB: sign ${signedMs} ms, verify+sign ${Date.now() - started} ms, rss +${growth} MB`);
  assert.deepEqual(verified, { signed: true, intact: true });
  assert.ok(signedMs < 60000);
});
