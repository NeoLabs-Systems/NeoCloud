'use strict';

const crypto = require('crypto');
const forge = require('node-forge');

/**
 * A fresh self-signed RSA-2048 / SHA-256 certificate in a PKCS#12 bundle for
 * the server's own seal. `{ commonName, years }` -> `{ p12Buffer, passphrase }`;
 * the passphrase is random and must be stored (encrypted) with the bundle.
 * Self-signed means readers show "unknown signer" but still prove integrity.
 */
function generateSelfSignedP12({ commonName = 'NeoCloud', years = 10 } = {}) {
  // Node's native RSA is orders of magnitude faster than forge's JS keygen.
  const { privateKey: privatePem, publicKey: publicPem } = crypto.generateKeyPairSync('rsa', {
    modulusLength: 2048,
    publicKeyEncoding: { type: 'spki', format: 'pem' },
    privateKeyEncoding: { type: 'pkcs8', format: 'pem' },
  });
  const privateKey = forge.pki.privateKeyFromPem(privatePem);

  const cert = forge.pki.createCertificate();
  cert.publicKey = forge.pki.publicKeyFromPem(publicPem);
  // Positive serial: top bit cleared.
  cert.serialNumber = `0${(crypto.randomBytes(1)[0] & 0x7f).toString(16).padStart(2, '0')}${crypto.randomBytes(15).toString('hex')}`;
  const now = Date.now();
  cert.validity.notBefore = new Date(now - 24 * 3600 * 1000);
  cert.validity.notAfter = new Date(now + Math.max(1, years) * 365 * 24 * 3600 * 1000);
  const subject = [
    { name: 'commonName', value: commonName },
    { name: 'organizationName', value: 'NeoCloud' },
  ];
  cert.setSubject(subject);
  cert.setIssuer(subject);
  cert.setExtensions([
    { name: 'basicConstraints', cA: false },
    { name: 'keyUsage', digitalSignature: true, nonRepudiation: true },
    { name: 'subjectKeyIdentifier' },
  ]);
  cert.sign(privateKey, forge.md.sha256.create());

  const passphrase = crypto.randomBytes(24).toString('base64url');
  const asn1 = forge.pkcs12.toPkcs12Asn1(privateKey, [cert], passphrase, {
    algorithm: '3des', // the widest reader compatibility
    friendlyName: commonName,
  });
  return { p12Buffer: Buffer.from(forge.asn1.toDer(asn1).getBytes(), 'binary'), passphrase };
}

module.exports = { generateSelfSignedP12 };
