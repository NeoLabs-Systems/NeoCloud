'use strict';

// The SFTP adapter against an in-memory stand-in for ssh2-sftp-client: paths
// stay inside the chosen folder, symlinks are not listed, the host key is
// pinned, and a private address is refused before any connection is made.

const test = require('node:test');
const assert = require('node:assert/strict');
const { Readable } = require('stream');

process.env.NEOCLOUD_HOME = require('fs').mkdtempSync(require('path').join(require('os').tmpdir(), 'neocloud-sftp-'));
process.env.NEOCLOUD_MASTER_KEY = '00'.repeat(32);
process.env.NEOCLOUD_EXTERNAL_ALLOW_PRIVATE = 'true';
const sftp = require('../../server/services/external_storage/adapters/sftp');

function fakeClient({ fingerprint = 'abc123' } = {}) {
  const tree = new Map([
    ['/srv/data', 'd'], ['/srv/data/a.txt', '-'], ['/srv/data/sub', 'd'], ['/srv/data/link', 'l'], ['/etc/passwd', '-'],
  ]);
  const contents = new Map([['/srv/data/a.txt', Buffer.from('0123456789')]]);
  const calls = [];
  return {
    calls,
    tree,
    contents,
    async connect(options) {
      calls.push(['connect', options]);
      if (options.hostVerifier && !options.hostVerifier(fingerprint)) throw new Error('Host denied (verification failed)');
    },
    async end() {},
    async exists(path) {
      return tree.get(path) || false;
    },
    async list(path) {
      return [...tree].filter(([key]) => key.startsWith(`${path}/`) && !key.slice(path.length + 1).includes('/'))
        .map(([key, type]) => ({ name: key.slice(path.length + 1), type, size: contents.get(key)?.length || 0, modifyTime: 1767323045000 }));
    },
    async stat(path) {
      return { size: contents.get(path)?.length || 0, modifyTime: 1767323045000 };
    },
    createReadStream(path, range) {
      calls.push(['read', path, range]);
      const body = contents.get(path);
      return Readable.from([range.end === undefined ? body : body.subarray(range.start, range.end + 1)]);
    },
    async put(stream, path) {
      const chunks = [];
      for await (const chunk of stream) chunks.push(chunk);
      contents.set(path, Buffer.concat(chunks));
      tree.set(path, '-');
    },
    async mkdir(path) {
      tree.set(path, 'd');
    },
    async rename(from, to) {
      tree.set(to, tree.get(from));
      tree.delete(from);
      if (contents.has(from)) contents.set(to, contents.get(from));
      contents.delete(from);
    },
    async delete(path) {
      tree.delete(path);
    },
    async rmdir(path) {
      for (const key of [...tree.keys()]) if (key === path || key.startsWith(`${path}/`)) tree.delete(key);
    },
  };
}

function adapter(client, config = {}) {
  return sftp.create({ host: '127.0.0.1', username: 'u', password: 'p', path: '/srv/data', ...config }, { userId: 1, clientFactory: () => client });
}

test('lists files and folders, hides symlinks, reads a range, writes, renames, deletes', async () => {
  const client = fakeClient();
  const sftpAdapter = adapter(client);
  await sftpAdapter.open();
  const { entries } = await sftpAdapter.list('', { limit: 100 });
  assert.deepEqual(entries.map((entry) => [entry.name, entry.type]).sort(), [['a.txt', 'file'], ['sub', 'folder']]);
  assert.equal(entries.find((entry) => entry.name === 'a.txt').size, 10);

  const chunks = [];
  for await (const chunk of await sftpAdapter.read('a.txt', { start: 2, end: 4 })) chunks.push(chunk);
  assert.equal(Buffer.concat(chunks).toString(), '234');

  await sftpAdapter.write('sub/b.txt', Readable.from([Buffer.from('bee')]), { size: 3 });
  assert.equal(client.contents.get('/srv/data/sub/b.txt').toString(), 'bee');
  await sftpAdapter.move('sub/b.txt', 'sub/c.txt');
  assert.ok(client.tree.has('/srv/data/sub/c.txt'));
  await assert.rejects(sftpAdapter.move('a.txt', 'sub/c.txt'), { code: 'NAME_EXISTS' });
  await sftpAdapter.remove('sub');
  assert.ok(!client.tree.has('/srv/data/sub'));
  await assert.rejects(sftpAdapter.remove('missing'), { code: 'EXTERNAL_NOT_FOUND' });
});

test('every remote path stays below the chosen folder', async () => {
  const client = fakeClient();
  const sftpAdapter = adapter(client);
  await sftpAdapter.open();
  await assert.rejects(sftpAdapter.read('../../etc/passwd'), { code: 'INVALID_PATH' });
  await assert.rejects(sftpAdapter.list('../..', { limit: 10 }), { code: 'INVALID_PATH' });
  await assert.rejects(sftpAdapter.remove('../data2'), { code: 'INVALID_PATH' });
});

test('the host key seen first is the only one accepted afterwards', async () => {
  const first = adapter(fakeClient({ fingerprint: 'key-one' }));
  await first.open();
  assert.equal(first.fingerprint, 'key-one');
  const same = adapter(fakeClient({ fingerprint: 'key-one' }), { hostKey: 'key-one' });
  await same.open();
  const changed = adapter(fakeClient({ fingerprint: 'key-two' }), { hostKey: 'key-one' });
  await assert.rejects(changed.open(), { code: 'EXTERNAL_HOST_KEY' });
});

test('the connection goes to the checked address, and metadata addresses are refused even on private networks', async () => {
  const client = fakeClient();
  await adapter(client, { host: 'localhost' }).open();
  assert.match(client.calls[0][1].host, /^(127\.0\.0\.1|::1)$/);
  const blocked = adapter(fakeClient(), { host: '169.254.169.254' });
  await assert.rejects(blocked.open(), { code: 'EXTERNAL_HOST_BLOCKED' });
});

test('validation asks for a password or key and a sane port', () => {
  assert.throws(() => sftp.validate({ host: 'h', username: 'u' }), /password or a private key/);
  assert.throws(() => sftp.validate({ host: 'h', username: 'u', password: 'p', port: 70000 }), /port/);
  assert.throws(() => sftp.validate({ host: 'h', username: 'u', password: 'p', path: '/a/../b' }), /folder/);
});
