# How the encryption works

```
NEOCLOUD_MASTER_KEY (in .env, generated once)
 └─ wraps a random data key per account
     └─ derives a key per file blob
         └─ AES-256-GCM in 64 KB segments, each one authenticated
```

Segments are bound to their file, their position and whether they are the
last one, so a blob can't be altered, reordered, swapped or cut short
without detection. Any byte range decrypts on its own, which is what lets
videos seek.

Backups copy the encrypted blobs as they are and encrypt the database
snapshot too, so a backup target never sees a file name or a byte of
content.

This protects against a stolen disk, a leaked backup or a nosy storage
provider. It does not protect against someone who controls the running
server. For that there are **vault folders**: their names and contents are
encrypted on your devices with a passphrase (and a recovery key) the server
never sees. The format is specified in [clients.md](clients.md).

What full-text search, previews and the other features keep on the server is
listed in [SECURITY.md](../SECURITY.md).
