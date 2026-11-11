'use strict';

// Any S3-compatible object store: AWS S3, Backblaze B2, MinIO, Hetzner,
// Wasabi, Cloudflare R2.

const {
  DeleteObjectsCommand,
  GetObjectCommand,
  HeadBucketCommand,
  ListObjectsV2Command,
  S3Client,
} = require('@aws-sdk/client-s3');
const { Upload } = require('@aws-sdk/lib-storage');
const { httpError } = require('../../../utils/http_error');

function createS3Adapter({ endpoint, region, bucket, accessKeyId, secretAccessKey, prefix = '', forcePathStyle = false }) {
  if (!bucket || !accessKeyId || !secretAccessKey) {
    throw httpError(400, 'Bucket, access key and secret key are required.', 'BACKUP_CONFIG_INVALID');
  }
  const client = new S3Client({
    region: region || 'us-east-1',
    endpoint: endpoint || undefined,
    forcePathStyle: Boolean(forcePathStyle),
    credentials: { accessKeyId, secretAccessKey },
  });
  const base = String(prefix || '').replace(/^\/+|\/+$/g, '');
  const keyFor = (key) => (base ? `${base}/${key}` : key);

  async function listKeys(prefixKey) {
    const keys = [];
    let token;
    do {
      const page = await client.send(new ListObjectsV2Command({ Bucket: bucket, Prefix: keyFor(prefixKey), ContinuationToken: token }));
      for (const item of page.Contents || []) keys.push(base ? item.Key.slice(base.length + 1) : item.Key);
      token = page.IsTruncated ? page.NextContinuationToken : undefined;
    } while (token);
    return keys;
  }

  async function deleteKeys(keys) {
    for (let i = 0; i < keys.length; i += 1000) {
      const batch = keys.slice(i, i + 1000).map((key) => ({ Key: keyFor(key) }));
      if (batch.length > 0) await client.send(new DeleteObjectsCommand({ Bucket: bucket, Delete: { Objects: batch, Quiet: true } }));
    }
  }

  return {
    async open() {
      await client.send(new HeadBucketCommand({ Bucket: bucket }));
    },
    async close() {
      client.destroy();
    },
    list: listKeys,
    async put(key, openSource) {
      await new Upload({ client, params: { Bucket: bucket, Key: keyFor(key), Body: openSource() } }).done();
    },
    async get(key) {
      const result = await client.send(new GetObjectCommand({ Bucket: bucket, Key: keyFor(key) }));
      return result.Body;
    },
    async delete(key) {
      await deleteKeys([key]);
    },
    async deletePrefix(prefixKey) {
      await deleteKeys(await listKeys(prefixKey));
    },
  };
}

module.exports = { createS3Adapter };
