'use strict';

// Digital seal: a detached PKCS#7 signature (subfilter adbe.pkcs7.detached,
// SHA-256, RSA) over the whole file, made with a server-held P12.
//
// Why not ETSI.CAdES.detached ("PAdES-B-B"): that subfilter requires the
// ESS signing-certificate-v2 attribute, which @signpdf's P12Signer (and
// node-forge's PKCS#7 writer) cannot add. adbe.pkcs7.detached carries the
// same integrity guarantee and is understood by Acrobat, pdf.js-based and
// EU DSS validators; it is just not labelled PAdES.

const crypto = require('crypto');
const forge = require('node-forge');
const { SignPdf } = require('@signpdf/signpdf');
const { pdflibAddPlaceholder } = require('@signpdf/placeholder-pdf-lib');
const { SUBFILTER_ADOBE_PKCS7_DETACHED } = require('@signpdf/utils');
const { P12Signer } = require('@signpdf/signer-p12');
const { PdfError } = require('./errors');
const { loadPdfDocument } = require('./load');
const { toBytes } = require('./normalize');

const MIN_SIGNATURE_BYTES = 8192;

/**
 * Room to reserve for the signature. The PKCS#7 holds the certificate chain,
 * which is a subset of the P12, plus ~1 KB of attributes and the RSA value;
 * the P12's own size plus slack is therefore a safe upper bound.
 */
function signatureLengthFor(p12Buffer) {
  return Math.max(MIN_SIGNATURE_BYTES, Math.ceil((p12Buffer.length + 2048) / 1024) * 1024);
}

/** Adds an invisible signature field to page 1 of a pdf-lib document. */
function addSignaturePlaceholder(doc, { p12Buffer, reason, name, location, signingTime }) {
  pdflibAddPlaceholder({
    pdfDoc: doc,
    reason: reason || 'Electronically signed with NeoCloud',
    contactInfo: '',
    name: name || 'NeoCloud',
    location: location || '',
    signingTime,
    signatureLength: signatureLengthFor(p12Buffer),
    subFilter: SUBFILTER_ADOBE_PKCS7_DETACHED,
  });
}

/** Signs a saved PDF that already holds a placeholder. */
async function signWithPlaceholder(pdfBuffer, { p12Buffer, passphrase, signingTime }) {
  try {
    const signer = new P12Signer(p12Buffer, { passphrase: passphrase || '' });
    return await new SignPdf().sign(pdfBuffer, signer, signingTime);
  } catch (err) {
    throw new PdfError('SIGNING_FAILED', 'The PDF could not be signed.', { cause: err });
  }
}

/**
 * Seals a PDF: `{ p12Buffer, passphrase, reason, name, location }`
 * (+ optional `signingTime`, default now). The file is rewritten without
 * object streams, as the placeholder needs a classic cross-reference table.
 */
async function signPdf(bytes, options) {
  const p12Buffer = toBytes(options && options.p12Buffer);
  if (!p12Buffer) throw new PdfError('SIGNING_FAILED', 'No signing certificate given.');
  const signingTime = options.signingTime || new Date();
  const doc = await loadPdfDocument(bytes, { maxBytes: Infinity });
  addSignaturePlaceholder(doc, { ...options, p12Buffer, signingTime });
  const saved = Buffer.from(await doc.save({ useObjectStreams: false }));
  return signWithPlaceholder(saved, { p12Buffer, passphrase: options.passphrase, signingTime });
}

const BYTE_RANGE = /\/ByteRange\s*\[\s*(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s*\]/g;

/** The last /ByteRange of the file as numbers, or null. */
function lastByteRange(buffer) {
  const tail = buffer.toString('latin1', Math.max(0, buffer.lastIndexOf('/ByteRange') - 8), Math.min(buffer.length, buffer.lastIndexOf('/ByteRange') + 200));
  const matches = [...tail.matchAll(BYTE_RANGE)];
  const match = matches[matches.length - 1];
  return match ? match.slice(1, 5).map(Number) : null;
}

const HASHES = {
  [forge.pki.oids.sha256]: 'sha256',
  [forge.pki.oids.sha384]: 'sha384',
  [forge.pki.oids.sha512]: 'sha512',
};

/** Checks the CMS blob against the signed bytes; true only if hash AND RSA signature match. */
function cmsMatches(cmsDer, signedDigests) {
  const asn1 = forge.asn1.fromDer(forge.util.createBuffer(cmsDer.toString('binary')), { parseAllBytes: false });
  const message = forge.pkcs7.messageFromAsn1(asn1);
  const capture = message.rawCapture;
  const hashName = HASHES[forge.asn1.derToOid(capture.digestAlgorithm)];
  if (!hashName || !capture.authenticatedAttributes) return false;

  const attributes = capture.authenticatedAttributes;
  const digestAttribute = attributes.find(
    (attr) => forge.asn1.derToOid(attr.value[0].value) === forge.pki.oids.messageDigest,
  );
  if (!digestAttribute) return false;
  const recorded = Buffer.from(digestAttribute.value[1].value[0].value, 'binary');
  if (!recorded.equals(signedDigests[hashName]())) return false;

  // The signature covers the attributes re-encoded as a SET.
  const set = forge.asn1.create(forge.asn1.Class.UNIVERSAL, forge.asn1.Type.SET, true, attributes);
  const md = forge.md[hashName].create();
  md.update(forge.asn1.toDer(set).getBytes());
  return message.certificates.some((certificate) => {
    try {
      return certificate.publicKey.verify(md.digest().getBytes(), capture.signature);
    } catch {
      return false;
    }
  });
}

/**
 * Best-effort integrity check of the latest signature: `signed` says whether
 * the file carries one, `intact` that the signed bytes still hash to the
 * recorded digest, the RSA signature verifies, and nothing was appended after
 * the signature. It does NOT validate the certificate chain or trust.
 */
async function verifySignedPdf(bytes) {
  const buffer = toBytes(bytes);
  if (!buffer || buffer.indexOf('/ByteRange') === -1) return { signed: false, intact: false };
  const range = lastByteRange(buffer);
  if (!range) return { signed: false, intact: false };
  const [start1, length1, start2, length2] = range;
  const covered = start1 === 0 && length1 > 0 && start2 >= length1 && start2 + length2 === buffer.length;
  if (!covered) return { signed: true, intact: false };

  try {
    const hex = buffer.toString('latin1', length1, start2).replace(/^</, '').replace(/>$/, '');
    const cms = Buffer.from(hex, 'hex');
    const digests = {};
    const digestOf = (name) => () => {
      digests[name] ||= crypto
        .createHash(name)
        .update(buffer.subarray(start1, start1 + length1))
        .update(buffer.subarray(start2, start2 + length2))
        .digest();
      return digests[name];
    };
    const intact = cmsMatches(cms, { sha256: digestOf('sha256'), sha384: digestOf('sha384'), sha512: digestOf('sha512') });
    return { signed: true, intact };
  } catch {
    return { signed: true, intact: false };
  }
}

module.exports = { addSignaturePlaceholder, signPdf, signWithPlaceholder, verifySignedPdf };
