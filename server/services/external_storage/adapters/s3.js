'use strict';

// S3-compatible storage (AWS, Backblaze B2, Wasabi, MinIO, ...) as folders:
// "/" in a key is the folder separator and a folder with nothing in it is a
// zero-byte "name/" object. Everything lives below the optional prefix.
// Renames and folder moves are copy + delete, as S3 has no rename.

const http = require('http');
const https = require('https');
const {
  CopyObjectCommand, DeleteObjectsCommand, GetObjectCommand, HeadObjectCommand,
  ListObjectsV2Command, PutObjectCommand, S3Client,
} = require('@aws-sdk/client-s3');
const { Upload } = require('@aws-sdk/lib-storage');
const { httpError } = require('../../../utils/http_error');
const { assertHttpUrl } = require('../config_checks');
const { guardedLookup } = require('../network');
const { baseName, isSafeListedName } = require('../paths');

// Folder moves and deletes are bounded: one folder holds at most this many objects.
const MAX_TREE_OBJECTS = 5000;
const REQUEST_TIMEOUT_MS = 60 * 1000;
const COPY_LIMIT = 5 * 1024 ** 3;

function notFound() {
  return httpError(404, 'Not found at the provider.', 'EXTERNAL_NOT_FOUND');
}

function isMissing(err) {
  return err?.name === 'NotFound' || err?.name === 'NoSuchKey' || err?.$metadata?.httpStatusCode === 404;
}

function translate(err) {
  if (err.status) return err;
  const status = err.$metadata?.httpStatusCode;
  if (status === 403) return httpError(502, 'The provider denied access. Check the keys and permissions.', 'EXTERNAL_FORBIDDEN');
  if (status === 404 || isMissing(err)) return notFound();
  if (err.name === 'NoSuchBucket') return httpError(502, 'That bucket does not exist.', 'EXTERNAL_NOT_FOUND');
  if (err.code === 'EXTERNAL_HOST_BLOCKED' || err.cause?.code === 'EXTERNAL_HOST_BLOCKED') return err.cause || err;
  return httpError(502, 'The provider could not be reached or refused the request.', 'EXTERNAL_UNREACHABLE');
}

function create(config, { userId }) {
  const { bucket, accessKeyId, secretAccessKey } = config;
  const base = (config.prefix || '').split('/').filter(Boolean).join('/');
  const lookup = guardedLookup(userId);
  const client = new S3Client({
    region: config.region || 'us-east-1',
    endpoint: config.endpoint || undefined,
    forcePathStyle: config.forcePathStyle === true || Boolean(config.endpoint),
    credentials: { accessKeyId, secretAccessKey },
    maxAttempts: 2,
    requestHandler: {
      httpAgent: new http.Agent({ lookup }),
      httpsAgent: new https.Agent({ lookup }),
      connectionTimeout: 20000,
      requestTimeout: REQUEST_TIMEOUT_MS,
    },
  });
  const keyOf = (path) => (base ? (path ? `${base}/${path}` : base) : path);
  const prefixOf = (path) => (keyOf(path) ? `${keyOf(path)}/` : '');
  const relative = (key) => key.slice(base ? base.length + 1 : 0);

  async function send(command) {
    try {
      return await client.send(command);
    } catch (err) {
      throw translate(err);
    }
  }

  // Every object below a folder (the "name/" marker included), bounded.
  async function keysBelow(path) {
    const keys = [];
    let token;
    do {
      const page = await send(new ListObjectsV2Command({ Bucket: bucket, Prefix: prefixOf(path), ContinuationToken: token }));
      for (const item of page.Contents || []) keys.push(item.Key);
      if (keys.length > MAX_TREE_OBJECTS) throw httpError(413, 'This folder holds too many items to change in one go.', 'EXTERNAL_TOO_LARGE');
      token = page.IsTruncated ? page.NextContinuationToken : undefined;
    } while (token);
    return keys;
  }

  async function removeKeys(keys) {
    for (let index = 0; index < keys.length; index += 1000) {
      const Objects = keys.slice(index, index + 1000).map((Key) => ({ Key }));
      await send(new DeleteObjectsCommand({ Bucket: bucket, Delete: { Objects, Quiet: true } }));
    }
  }

  async function copyKey(from, to) {
    const source = `${bucket}/${from}`.split('/').map(encodeURIComponent).join('/');
    await send(new CopyObjectCommand({ Bucket: bucket, Key: to, CopySource: source }));
  }

  async function head(path) {
    try {
      return await client.send(new HeadObjectCommand({ Bucket: bucket, Key: keyOf(path) }));
    } catch (err) {
      if (isMissing(err)) return null;
      throw translate(err);
    }
  }

  return {
    async open() {
      await send(new ListObjectsV2Command({ Bucket: bucket, Prefix: prefixOf(''), MaxKeys: 1 }));
    },
    async close() {
      client.destroy();
    },
    async list(path, { limit }) {
      const entries = [];
      let token;
      let truncated = false;
      do {
        const page = await send(new ListObjectsV2Command({
          Bucket: bucket, Prefix: prefixOf(path), Delimiter: '/', ContinuationToken: token,
        }));
        for (const item of page.CommonPrefixes || []) {
          const name = baseName(relative(item.Prefix).replace(/\/$/, ''));
          if (isSafeListedName(name)) entries.push({ name, type: 'folder', size: 0, mtime: null });
        }
        for (const item of page.Contents || []) {
          const name = baseName(relative(item.Key));
          if (item.Key.endsWith('/') || !isSafeListedName(name)) continue;
          entries.push({ name, type: 'file', size: item.Size || 0, mtime: item.LastModified ? item.LastModified.toISOString() : null });
        }
        token = page.IsTruncated ? page.NextContinuationToken : undefined;
        if (entries.length >= limit) {
          truncated = Boolean(token) || entries.length > limit;
          break;
        }
      } while (token);
      return { entries: entries.slice(0, limit), truncated };
    },
    async stat(path) {
      if (!path) return { type: 'folder', size: 0, mtime: null };
      const object = await head(path);
      if (object) return { type: 'file', size: object.ContentLength || 0, mtime: object.LastModified?.toISOString() || null };
      const below = await send(new ListObjectsV2Command({ Bucket: bucket, Prefix: prefixOf(path), MaxKeys: 1 }));
      return (below.Contents || []).length > 0 ? { type: 'folder', size: 0, mtime: null } : null;
    },
    async read(path, range) {
      const response = await send(new GetObjectCommand({
        Bucket: bucket,
        Key: keyOf(path),
        Range: range ? `bytes=${range.start}-${range.end}` : undefined,
      }));
      return response.Body;
    },
    async write(path, stream, { size }) {
      const upload = new Upload({
        client,
        params: { Bucket: bucket, Key: keyOf(path), Body: stream, ContentLength: size ?? undefined },
        partSize: 16 * 1024 * 1024,
        queueSize: 2,
        leavePartsOnError: false,
      });
      try {
        await upload.done();
      } catch (err) {
        await upload.abort().catch(() => {});
        throw translate(err);
      }
    },
    async mkdir(path) {
      await send(new PutObjectCommand({ Bucket: bucket, Key: prefixOf(path), Body: '' }));
    },
    async move(from, to) {
      const info = await this.stat(from);
      if (!info) throw notFound();
      if (info.type === 'file') {
        if (info.size > COPY_LIMIT) throw httpError(413, 'This file is too large to rename on this provider.', 'EXTERNAL_TOO_LARGE');
        await copyKey(keyOf(from), keyOf(to));
        await removeKeys([keyOf(from)]);
        return;
      }
      const keys = await keysBelow(from);
      for (const key of keys) await copyKey(key, `${prefixOf(to)}${key.slice(prefixOf(from).length)}`);
      await removeKeys(keys);
    },
    async remove(path) {
      const info = await this.stat(path);
      if (!info) throw notFound();
      await removeKeys(info.type === 'file' ? [keyOf(path)] : await keysBelow(path));
    },
  };
}

module.exports = {
  type: 's3',
  fields: [
    { name: 'endpoint', max: 512 },
    { name: 'region', max: 64 },
    { name: 'bucket', required: true, max: 255 },
    { name: 'prefix', max: 512 },
    { name: 'accessKeyId', required: true, max: 256 },
    { name: 'secretAccessKey', required: true, secret: true, max: 512 },
    { name: 'forcePathStyle', kind: 'boolean' },
  ],
  validate(config) {
    if (config.endpoint) assertHttpUrl(config.endpoint, 'The endpoint');
    if (!/^[a-zA-Z0-9][a-zA-Z0-9.\-_]{1,254}$/.test(config.bucket)) throw httpError(400, 'That is not a valid bucket name.', 'EXTERNAL_CONFIG_INVALID');
  },
  hostOf: (config) => (config.endpoint ? new URL(config.endpoint).hostname : null),
  create,
};
