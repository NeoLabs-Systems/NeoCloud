'use strict';

// Any S3-compatible object store: AWS S3, Backblaze B2, MinIO, Hetzner,
// Wasabi, Cloudflare R2.
//
// On AWS every object is written in the target's storage class. GLACIER
// (Flexible Retrieval) and DEEP_ARCHIVE objects can be listed, HEADed,
// copied over and deleted, but not read until a restore made them available
// for a while: `archived` tells the runner, verify and restore to work from
// metadata and to ask for a restore first.

const {
  CompleteMultipartUploadCommand,
  CopyObjectCommand,
  CreateMultipartUploadCommand,
  AbortMultipartUploadCommand,
  DeleteObjectsCommand,
  GetObjectCommand,
  HeadBucketCommand,
  HeadObjectCommand,
  ListObjectsV2Command,
  RestoreObjectCommand,
  S3Client,
  UploadPartCopyCommand,
} = require('@aws-sdk/client-s3');
const { Upload } = require('@aws-sdk/lib-storage');
const { httpError } = require('../../../utils/http_error');

const STORAGE_CLASSES = ['STANDARD', 'GLACIER', 'DEEP_ARCHIVE', 'GLACIER_IR', 'STANDARD_IA'];
const ARCHIVE_CLASSES = new Set(['GLACIER', 'DEEP_ARCHIVE']);
// CopyObject copies at most 5 GB; larger objects are copied in parts.
const MAX_SINGLE_COPY = 5 * 1024 ** 3;
const COPY_PART_SIZE = 512 * 1024 ** 2;
const RESTORE_DAYS = 7;

function isArchiveClass(storageClass) {
  return ARCHIVE_CLASSES.has(storageClass);
}

// x-amz-restore: 'ongoing-request="true"' while Glacier works on it,
// 'ongoing-request="false", expiry-date="..."' once the copy is readable.
function restoreState(header) {
  if (!header) return 'none';
  return /ongoing-request="true"/.test(header) ? 'ongoing' : 'available';
}

function createS3Adapter({
  endpoint,
  region,
  bucket,
  accessKeyId,
  secretAccessKey,
  prefix = '',
  forcePathStyle = false,
  storageClass = 'STANDARD',
  // Tests only: lets a small object take the multipart copy path.
  maxSingleCopy = MAX_SINGLE_COPY,
  copyPartSize = COPY_PART_SIZE,
}) {
  if (!bucket || !accessKeyId || !secretAccessKey) {
    throw httpError(400, 'Bucket, access key and secret key are required.', 'BACKUP_CONFIG_INVALID');
  }
  if (!STORAGE_CLASSES.includes(storageClass)) {
    throw httpError(400, `Storage class must be one of ${STORAGE_CLASSES.join(', ')}.`, 'INVALID_VALUE');
  }
  const client = new S3Client({
    region: region || 'us-east-1',
    endpoint: endpoint || undefined,
    forcePathStyle: Boolean(forcePathStyle),
    credentials: { accessKeyId, secretAccessKey },
  });
  const base = String(prefix || '').replace(/^\/+|\/+$/g, '');
  const keyFor = (key) => (base ? `${base}/${key}` : key);
  const relative = (key) => (base ? key.slice(base.length + 1) : key);

  /** Every object below prefixKey: { key, size, storageClass }. */
  async function listObjects(prefixKey) {
    const objects = [];
    let token;
    do {
      const page = await client.send(new ListObjectsV2Command({ Bucket: bucket, Prefix: keyFor(prefixKey), ContinuationToken: token }));
      for (const item of page.Contents || []) {
        objects.push({ key: relative(item.Key), size: Number(item.Size || 0), storageClass: item.StorageClass || 'STANDARD' });
      }
      token = page.IsTruncated ? page.NextContinuationToken : undefined;
    } while (token);
    return objects;
  }

  async function listKeys(prefixKey) {
    return (await listObjects(prefixKey)).map((object) => object.key);
  }

  async function deleteKeys(keys) {
    for (let i = 0; i < keys.length; i += 1000) {
      const batch = keys.slice(i, i + 1000).map((key) => ({ Key: keyFor(key) }));
      if (batch.length > 0) await client.send(new DeleteObjectsCommand({ Bucket: bucket, Delete: { Objects: batch, Quiet: true } }));
    }
  }

  function copySource(key) {
    return `${bucket}/${encodeURIComponent(keyFor(key)).replace(/%2F/g, '/')}`;
  }

  async function multipartCopy(key, size) {
    const target = { Bucket: bucket, Key: keyFor(key) };
    const { UploadId } = await client.send(new CreateMultipartUploadCommand({ ...target, StorageClass: storageClass }));
    try {
      const parts = [];
      for (let start = 0, part = 1; start < size; start += copyPartSize, part += 1) {
        const end = Math.min(start + copyPartSize, size) - 1;
        const result = await client.send(new UploadPartCopyCommand({
          ...target,
          UploadId,
          PartNumber: part,
          CopySource: copySource(key),
          CopySourceRange: `bytes=${start}-${end}`,
        }));
        parts.push({ PartNumber: part, ETag: result.CopyPartResult?.ETag });
      }
      await client.send(new CompleteMultipartUploadCommand({ ...target, UploadId, MultipartUpload: { Parts: parts } }));
    } catch (err) {
      await client.send(new AbortMultipartUploadCommand({ ...target, UploadId })).catch(() => {});
      throw err;
    }
  }

  return {
    storageClass,
    archived: isArchiveClass(storageClass),
    async open() {
      await client.send(new HeadBucketCommand({ Bucket: bucket }));
    },
    async close() {
      client.destroy();
    },
    list: listKeys,
    listObjects,
    async put(key, openSource) {
      await new Upload({ client, params: { Bucket: bucket, Key: keyFor(key), Body: openSource(), StorageClass: storageClass } }).done();
    },
    async get(key) {
      const result = await client.send(new GetObjectCommand({ Bucket: bucket, Key: keyFor(key) }));
      return result.Body;
    },
    /** HEAD: { size, storageClass, restore: 'none' | 'ongoing' | 'available' }, or null when missing. */
    async stat(key) {
      try {
        const head = await client.send(new HeadObjectCommand({ Bucket: bucket, Key: keyFor(key) }));
        return { size: Number(head.ContentLength || 0), storageClass: head.StorageClass || 'STANDARD', restore: restoreState(head.Restore) };
      } catch (err) {
        if (err?.name === 'NotFound' || err?.$metadata?.httpStatusCode === 404) return null;
        throw err;
      }
    },
    /** Asks Glacier for a readable copy for RESTORE_DAYS; tier 'Standard' or 'Bulk'. */
    async requestRestore(key, tier = 'Standard') {
      try {
        await client.send(new RestoreObjectCommand({
          Bucket: bucket,
          Key: keyFor(key),
          RestoreRequest: { Days: RESTORE_DAYS, GlacierJobParameters: { Tier: tier } },
        }));
      } catch (err) {
        if (err?.name !== 'RestoreAlreadyInProgress' && err?.Code !== 'RestoreAlreadyInProgress') throw err;
      }
    },
    /** Rewrites an object in place in the target's storage class; never deletes. */
    async changeStorageClass(key, size) {
      if (size > maxSingleCopy) return multipartCopy(key, size);
      await client.send(new CopyObjectCommand({
        Bucket: bucket,
        Key: keyFor(key),
        CopySource: copySource(key),
        StorageClass: storageClass,
        MetadataDirective: 'COPY',
      }));
    },
    async delete(key) {
      await deleteKeys([key]);
    },
    async deletePrefix(prefixKey) {
      await deleteKeys(await listKeys(prefixKey));
    },
  };
}

module.exports = { STORAGE_CLASSES, createS3Adapter, isArchiveClass };
