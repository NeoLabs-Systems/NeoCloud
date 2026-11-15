'use strict';

// A minimal in-process S3 (path-style) for the backup tests: enough of
// ListObjectsV2, Put/Get/Head/Copy/DeleteObjects, multipart upload and copy,
// and RestoreObject to exercise storage classes and Glacier restores. Never
// talks to AWS. Every request is recorded in `requests`.

const crypto = require('crypto');
const http = require('http');

const ARCHIVE = new Set(['GLACIER', 'DEEP_ARCHIVE']);

function xml(body) {
  return `<?xml version="1.0" encoding="UTF-8"?>${body}`;
}

function escapeXml(value) {
  return String(value).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

// Undoes `Content-Encoding: aws-chunked` (streamed bodies with a trailing
// checksum): "<hex size>[;chunk-signature=...]\r\n<data>\r\n" ... "0\r\n<trailers>".
function decodeAwsChunked(buffer) {
  const parts = [];
  let offset = 0;
  while (offset < buffer.length) {
    const lineEnd = buffer.indexOf('\r\n', offset);
    const size = parseInt(buffer.slice(offset, lineEnd).toString().split(';')[0], 16);
    if (!size) break;
    parts.push(buffer.slice(lineEnd + 2, lineEnd + 2 + size));
    offset = lineEnd + 2 + size + 2;
  }
  return Buffer.concat(parts);
}

function createFakeS3() {
  const objects = new Map(); // "bucket/key" -> { body, storageClass, restore: null | 'ongoing' | 'available' }
  const uploads = new Map(); // uploadId -> { bucket, key, storageClass, parts: Map }
  const requests = [];
  const buckets = new Set();

  function send(res, status, body, headers = {}) {
    res.writeHead(status, { 'content-type': 'application/xml', ...headers });
    res.end(body === undefined ? undefined : xml(body));
  }

  function error(res, status, code, message = code) {
    send(res, status, `<Error><Code>${code}</Code><Message>${escapeXml(message)}</Message></Error>`);
  }

  function readable(object) {
    return !ARCHIVE.has(object.storageClass) || object.restore === 'available';
  }

  function sourceOf(req) {
    const raw = decodeURIComponent(String(req.headers['x-amz-copy-source']).replace(/^\//, '').split('?')[0]);
    return objects.get(raw);
  }

  function copyRange(body, header) {
    if (!header) return body;
    const [, start, end] = /bytes=(\d+)-(\d+)/.exec(header);
    return body.subarray(Number(start), Number(end) + 1);
  }

  async function handle(req, res, url, body) {
    const [bucket, ...rest] = url.pathname.slice(1).split('/');
    const key = decodeURIComponent(rest.join('/'));
    const id = `${bucket}/${key}`;
    const q = url.searchParams;
    requests.push({ method: req.method, key, query: [...q.keys()].join('&'), storageClass: req.headers['x-amz-storage-class'] || null });

    if (!key) {
      if (req.method === 'HEAD') return buckets.has(bucket) ? send(res, 200) : send(res, 404);
      if (req.method === 'GET' && q.get('list-type') === '2') {
        const prefix = q.get('prefix') || '';
        let all = [...objects.keys()].filter((k) => k.startsWith(`${bucket}/${prefix}`)).sort();
        // With a delimiter, deeper keys collapse into CommonPrefixes (folders).
        const folders = new Set();
        const delimiter = q.get('delimiter');
        if (delimiter) {
          all = all.filter((k) => {
            const rest = k.slice(bucket.length + 1 + prefix.length);
            const cut = rest.indexOf(delimiter);
            if (cut < 0) return true;
            folders.add(prefix + rest.slice(0, cut + 1));
            return false;
          });
        }
        const commonPrefixes = [...folders].map((p) => `<CommonPrefixes><Prefix>${escapeXml(p)}</Prefix></CommonPrefixes>`).join('');
        const start = Number(q.get('continuation-token') || 0);
        const max = 1000;
        const page = all.slice(start, start + max);
        const truncated = start + max < all.length;
        const contents = page.map((k) => {
          const o = objects.get(k);
          return `<Contents><Key>${escapeXml(k.slice(bucket.length + 1))}</Key><Size>${o.body.length}</Size><LastModified>2026-01-02T03:04:05.000Z</LastModified><StorageClass>${o.storageClass}</StorageClass></Contents>`;
        }).join('');
        return send(res, 200, `<ListBucketResult><Name>${bucket}</Name><Prefix>${escapeXml(prefix)}</Prefix><KeyCount>${page.length}</KeyCount><IsTruncated>${truncated}</IsTruncated>${truncated ? `<NextContinuationToken>${start + max}</NextContinuationToken>` : ''}${contents}${commonPrefixes}</ListBucketResult>`);
      }
      if (req.method === 'POST' && q.has('delete')) {
        for (const match of body.toString().matchAll(/<Key>([^<]*)<\/Key>/g)) objects.delete(`${bucket}/${match[1].replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&')}`);
        return send(res, 200, '<DeleteResult></DeleteResult>');
      }
      return error(res, 400, 'NotImplemented');
    }

    if (req.method === 'POST' && q.has('uploads')) {
      const uploadId = crypto.randomUUID();
      uploads.set(uploadId, { bucket, key, storageClass: req.headers['x-amz-storage-class'] || 'STANDARD', parts: new Map() });
      return send(res, 200, `<InitiateMultipartUploadResult><Bucket>${bucket}</Bucket><Key>${escapeXml(key)}</Key><UploadId>${uploadId}</UploadId></InitiateMultipartUploadResult>`);
    }
    if (req.method === 'PUT' && q.has('uploadId')) {
      const upload = uploads.get(q.get('uploadId'));
      if (!upload) return error(res, 404, 'NoSuchUpload');
      let data = body;
      if (req.headers['x-amz-copy-source']) {
        const source = sourceOf(req);
        if (!source) return error(res, 404, 'NoSuchKey');
        if (!readable(source)) return error(res, 403, 'InvalidObjectState', "The operation is not valid for the object's storage class");
        data = copyRange(source.body, req.headers['x-amz-copy-source-range']);
      }
      const etag = `"${crypto.createHash('md5').update(data).digest('hex')}"`;
      upload.parts.set(Number(q.get('partNumber')), data);
      if (req.headers['x-amz-copy-source']) return send(res, 200, `<CopyPartResult><ETag>${etag}</ETag></CopyPartResult>`);
      return send(res, 200, undefined, { etag });
    }
    if (req.method === 'POST' && q.has('uploadId')) {
      const upload = uploads.get(q.get('uploadId'));
      if (!upload) return error(res, 404, 'NoSuchUpload');
      const numbers = [...body.toString().matchAll(/<PartNumber>(\d+)<\/PartNumber>/g)].map((m) => Number(m[1]));
      objects.set(`${upload.bucket}/${upload.key}`, {
        body: Buffer.concat(numbers.map((n) => upload.parts.get(n))),
        storageClass: upload.storageClass,
        restore: null,
      });
      uploads.delete(q.get('uploadId'));
      return send(res, 200, `<CompleteMultipartUploadResult><Bucket>${upload.bucket}</Bucket><Key>${escapeXml(upload.key)}</Key><ETag>"x"</ETag></CompleteMultipartUploadResult>`);
    }
    if (req.method === 'DELETE' && q.has('uploadId')) {
      uploads.delete(q.get('uploadId'));
      return send(res, 204);
    }
    if (req.method === 'POST' && q.has('restore')) {
      const object = objects.get(id);
      if (!object) return error(res, 404, 'NoSuchKey');
      if (!ARCHIVE.has(object.storageClass)) return error(res, 403, 'InvalidObjectState', 'Restore is not allowed for the object\'s current storage class');
      if (object.restore === 'ongoing') return error(res, 409, 'RestoreAlreadyInProgress', 'Object restore is already in progress');
      if (object.restore === 'available') return send(res, 200);
      object.restore = 'ongoing';
      object.restoreRequest = body.toString();
      return send(res, 202);
    }
    if (req.method === 'PUT' && req.headers['x-amz-copy-source']) {
      const source = sourceOf(req);
      if (!source) return error(res, 404, 'NoSuchKey');
      if (!readable(source)) return error(res, 403, 'InvalidObjectState', "The operation is not valid for the object's storage class");
      objects.set(id, { body: source.body, storageClass: req.headers['x-amz-storage-class'] || 'STANDARD', restore: null });
      return send(res, 200, '<CopyObjectResult><ETag>"x"</ETag></CopyObjectResult>');
    }
    if (req.method === 'PUT') {
      const data = /aws-chunked/.test(req.headers['content-encoding'] || '') || /^STREAMING-/.test(req.headers['x-amz-content-sha256'] || '')
        ? decodeAwsChunked(body)
        : body;
      objects.set(id, { body: data, storageClass: req.headers['x-amz-storage-class'] || 'STANDARD', restore: null });
      return send(res, 200, undefined, { etag: '"x"' });
    }
    const object = objects.get(id);
    if (req.method === 'HEAD') {
      if (!object) return send(res, 404);
      const headers = { 'content-length': object.body.length, 'last-modified': 'Fri, 02 Jan 2026 03:04:05 GMT' };
      if (object.storageClass !== 'STANDARD') headers['x-amz-storage-class'] = object.storageClass;
      if (object.restore === 'ongoing') headers['x-amz-restore'] = 'ongoing-request="true"';
      if (object.restore === 'available') headers['x-amz-restore'] = 'ongoing-request="false", expiry-date="Fri, 21 Dec 2040 00:00:00 GMT"';
      res.writeHead(200, headers);
      return res.end();
    }
    if (req.method === 'GET') {
      if (!object) return error(res, 404, 'NoSuchKey');
      if (!readable(object)) return error(res, 403, 'InvalidObjectState', "The operation is not valid for the object's storage class");
      const asked = /^bytes=(\d+)-(\d+)$/.exec(req.headers.range || '');
      if (asked) {
        const part = object.body.subarray(Number(asked[1]), Number(asked[2]) + 1);
        res.writeHead(206, { 'content-type': 'application/octet-stream', 'content-length': part.length, 'content-range': `bytes ${asked[1]}-${asked[2]}/${object.body.length}` });
        return res.end(part);
      }
      res.writeHead(200, { 'content-type': 'application/octet-stream', 'content-length': object.body.length });
      return res.end(object.body);
    }
    return error(res, 400, 'NotImplemented');
  }

  const server = http.createServer((req, res) => {
    const chunks = [];
    req.on('data', (chunk) => chunks.push(chunk));
    req.on('end', () => {
      handle(req, res, new URL(req.url, 'http://fake'), Buffer.concat(chunks)).catch((err) => error(res, 500, 'InternalError', err.message));
    });
  });

  return {
    objects,
    requests,
    createBucket(name) {
      buckets.add(name);
    },
    async start() {
      await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
      return `http://127.0.0.1:${server.address().port}`;
    },
    async stop() {
      server.closeAllConnections?.();
      await new Promise((resolve) => server.close(resolve));
    },
    /** Moves objects to a class the way a lifecycle rule would. */
    setStorageClass(match, storageClass) {
      for (const [id, object] of objects) if (match(id)) object.storageClass = storageClass;
    },
    /** Glacier finished every requested restore. */
    finishRestores() {
      for (const object of objects.values()) if (object.restore === 'ongoing') object.restore = 'available';
    },
  };
}

module.exports = { createFakeS3 };
