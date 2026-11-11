'use strict';

// Reference NCV1 implementation on Node's crypto, used by vault_crypto_test.dart
// to prove the Dart format is interoperable. Reads one JSON request on stdin,
// writes one JSON answer on stdout.
//
//   { op: "create", passphrase, iterations, name, content }  -> vault + encrypted name/content
//   { op: "open", passphrase, salt, iterations, wrappedKey, name, content } -> plaintext name/content
// `content` is base64 everywhere.

const crypto = require('crypto');

const SEGMENT = 65536;

function kek(passphrase, salt, iterations) {
  return crypto.pbkdf2Sync(Buffer.from(passphrase, 'utf8'), salt, iterations, 32, 'sha256');
}

function seal(key, nonce, plaintext, aad) {
  const cipher = crypto.createCipheriv('aes-256-gcm', key, nonce);
  if (aad) cipher.setAAD(aad);
  return Buffer.concat([cipher.update(plaintext), cipher.final(), cipher.getAuthTag()]);
}

function open(key, nonce, sealed, aad) {
  const decipher = crypto.createDecipheriv('aes-256-gcm', key, nonce);
  if (aad) decipher.setAAD(aad);
  decipher.setAuthTag(sealed.subarray(sealed.length - 16));
  return Buffer.concat([decipher.update(sealed.subarray(0, sealed.length - 16)), decipher.final()]);
}

function hkdf(vaultKey, salt, info) {
  return Buffer.from(crypto.hkdfSync('sha256', vaultKey, salt, Buffer.from(info, 'utf8'), 32));
}

function segmentNonce(prefix, index) {
  const nonce = Buffer.alloc(12);
  prefix.copy(nonce, 0);
  nonce.writeUInt32BE(index, 8);
  return nonce;
}

function encryptName(vaultKey, name) {
  const nonce = crypto.randomBytes(12);
  const sealed = seal(hkdf(vaultKey, Buffer.alloc(0), 'ncv1-name'), nonce, Buffer.from(name, 'utf8'));
  return Buffer.concat([nonce, sealed]).toString('base64url');
}

function decryptName(vaultKey, stored) {
  const blob = Buffer.from(stored, 'base64url');
  return open(hkdf(vaultKey, Buffer.alloc(0), 'ncv1-name'), blob.subarray(0, 12), blob.subarray(12)).toString('utf8');
}

function encryptContent(vaultKey, plaintext) {
  const prefix = crypto.randomBytes(8);
  const fileKey = hkdf(vaultKey, prefix, 'ncv1-file');
  const count = Math.max(1, Math.ceil(plaintext.length / SEGMENT));
  const parts = [Buffer.from('NCV1', 'ascii'), prefix];
  for (let i = 0; i < count; i += 1) {
    const last = i === count - 1;
    parts.push(seal(fileKey, segmentNonce(prefix, i), plaintext.subarray(i * SEGMENT, (i + 1) * SEGMENT), Buffer.from([last ? 1 : 0])));
  }
  return Buffer.concat(parts);
}

function decryptContent(vaultKey, data) {
  if (data.subarray(0, 4).toString('ascii') !== 'NCV1') throw new Error('bad magic');
  const prefix = data.subarray(4, 12);
  const fileKey = hkdf(vaultKey, prefix, 'ncv1-file');
  const body = data.subarray(12);
  const sealedSize = SEGMENT + 16;
  const count = Math.ceil(body.length / sealedSize);
  const parts = [];
  for (let i = 0; i < count; i += 1) {
    const last = i === count - 1;
    parts.push(open(fileKey, segmentNonce(prefix, i), body.subarray(i * sealedSize, (i + 1) * sealedSize), Buffer.from([last ? 1 : 0])));
  }
  return Buffer.concat(parts);
}

function main(request) {
  if (request.op === 'create') {
    const salt = crypto.randomBytes(16);
    const vaultKey = crypto.randomBytes(32);
    const nonce = crypto.randomBytes(12);
    const wrapped = Buffer.concat([nonce, seal(kek(request.passphrase, salt, request.iterations), nonce, vaultKey)]);
    return {
      salt: salt.toString('base64'),
      iterations: request.iterations,
      wrappedKey: wrapped.toString('base64'),
      name: encryptName(vaultKey, request.name),
      content: encryptContent(vaultKey, Buffer.from(request.content, 'base64')).toString('base64'),
    };
  }
  if (request.op === 'open') {
    const wrapped = Buffer.from(request.wrappedKey, 'base64');
    const key = kek(request.passphrase, Buffer.from(request.salt, 'base64'), request.iterations);
    const vaultKey = open(key, wrapped.subarray(0, 12), wrapped.subarray(12));
    return {
      name: decryptName(vaultKey, request.name),
      content: decryptContent(vaultKey, Buffer.from(request.content, 'base64')).toString('base64'),
    };
  }
  throw new Error(`unknown op ${request.op}`);
}

let input = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', (chunk) => { input += chunk; });
process.stdin.on('end', () => {
  process.stdout.write(JSON.stringify(main(JSON.parse(input))));
});
