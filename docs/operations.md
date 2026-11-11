# Running NeoCloud

How to put NeoCloud behind a reverse proxy, keep it healthy, back it up,
restore it, and upgrade (or roll back) without surprises.

## Reverse proxy

NeoCloud listens on `PORT` (3344) over plain HTTP. In front of it, a proxy
terminates TLS. Whatever proxy you use, it needs:

| Need | Why |
| --- | --- |
| No request body limit | Single-request uploads carry the whole file. Resumable uploads send 8 MiB parts. |
| Request and response buffering off | Uploads stream straight into encryption; downloads and ZIPs stream out. A buffering proxy first writes everything to its own disk and times out on big files. NeoCloud sends `X-Accel-Buffering: no` on downloads for nginx. |
| Read/send timeouts of an hour | A large single upload is one long request. NeoCloud's own request timeout is 60 min. |
| WebSockets on `/socket.io/` | Live folder updates in the web app. |
| `X-Forwarded-For` and `X-Forwarded-Proto` | Client addresses for rate limits and the audit log, https detection for cookies. |

On the NeoCloud side set:

```ini
PUBLIC_URL=https://cloud.example.com
ALLOWED_ORIGINS=https://cloud.example.com
# Proxies trusted to set X-Forwarded-*: their addresses, or a hop count.
# Default with an https PUBLIC_URL: loopback, link-local and private addresses.
# TRUST_PROXY=
```

`TRUST_PROXY` decides whose `X-Forwarded-For` and `X-Forwarded-Proto` count:
the client address behind the per-address rate limits and the audit log, and
whether a request came in over https. It takes `true`/`false`, a number of
hops (`2` for Cloudflare in front of nginx), or a comma-separated list of
proxy addresses and subnets (`172.18.0.1,10.0.0.0/8`, or Express's names
`loopback`, `linklocal`, `uniquelocal`).

Left empty with an https `PUBLIC_URL`, it trusts proxies connecting from
`loopback, linklocal, uniquelocal`: the same host (nginx or Caddy on
127.0.0.1) and private networks, which covers a Docker bridge (172.16.0.0/12)
and a proxy container on the same Docker network. A client connecting from a
public address is never trusted, so it can't pick its own address by sending
`X-Forwarded-For`. A client on your private network still could if it can
reach the port directly; list the proxy's address instead
(`TRUST_PROXY=127.0.0.1` or `172.18.0.5`) when that matters. Older releases
defaulted to one hop, which trusted whichever peer connected.

A hop count (`1`, `2`, `true`) trusts whatever sits that many hops away,
including a client that connects straight to port 3344. Use one only when
nothing but the proxy can reach that port, and prefer addresses.

Keep port 3344 reachable from the proxy only. `install.sh` and
`neocloud start` listen on every interface, so firewall 3344 or put the proxy
on the same host. The shipped `compose.yaml` publishes it on
`127.0.0.1:3344` for a proxy on the host; a proxy in another container
reaches it over the Docker network without any published port. Docker's
published ports bypass host firewalls such as ufw, so `"3344:3344"` exposes
the port to the whole network.

Sign-in is also throttled per account, independent of addresses (see
`/api/auth` in [api.md](api.md)): after 5 failed passwords or codes in 15
minutes each try waits longer, and 10 lock the account's sign-in for at most
15 minutes. A pending two-factor sign-in takes at most 5 codes.

NeoCloud keeps idle connections open for 65 s (`keepAliveTimeout`), longer
than the 60 s most proxies use, so the proxy closes idle connections first
and never sends a request into one NeoCloud is closing (the classic random
502).

### nginx

```nginx
server {
    listen 443 ssl http2;
    server_name cloud.example.com;
    # ssl_certificate ... ;

    client_max_body_size 0;
    proxy_request_buffering off;
    proxy_buffering off;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;

    location / {
        proxy_pass http://127.0.0.1:3344;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
    }
}

# In the http { } block:
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      '';
}
```

The empty `Connection` for non-WebSocket requests keeps upstream keep-alive
working.

### Caddy

```caddy
cloud.example.com {
    reverse_proxy 127.0.0.1:3344 {
        flush_interval -1
        transport http {
            read_timeout 1h
            write_timeout 1h
        }
    }
}
```

Caddy has no body limit by default, proxies WebSockets on its own and sets
`X-Forwarded-*`. `flush_interval -1` streams downloads without buffering.

### Traefik (Docker labels)

```yaml
services:
  neocloud:
    image: ghcr.io/neolabs-systems/neocloud:${NEOCLOUD_VERSION:-0}
    environment:
      PUBLIC_URL: https://cloud.example.com
      ALLOWED_ORIGINS: https://cloud.example.com
      # Traefik reaches NeoCloud over the Docker network (a private address),
      # which the default trusts; no `ports` needed.
    labels:
      - traefik.enable=true
      - traefik.http.routers.neocloud.rule=Host(`cloud.example.com`)
      - traefik.http.routers.neocloud.entrypoints=websecure
      - traefik.http.routers.neocloud.tls.certresolver=letsencrypt
      - traefik.http.services.neocloud.loadbalancer.server.port=3344
```

Traefik v3 cuts requests off after 60 s by default. Raise it in the static
configuration, or large single uploads fail:

```yaml
entryPoints:
  websecure:
    address: ":443"
    transport:
      respondingTimeouts:
        readTimeout: 3600s
        writeTimeout: 3600s
```

Traefik doesn't buffer unless you add the `buffering` middleware (don't), and
proxies WebSockets without extra configuration.

### Cloudflare

Cloudflare's proxy (the orange cloud) rejects request bodies over **100 MB**
on the Free and Pro plans (200 MB Business, 500 MB Enterprise) with `413`, and
ends requests that send no response for 100 s with `524`. The lite desktop
client and the mobile app upload large files as resumable 8 MiB parts and are not
affected; single-request uploads of big files through Cloudflare are. Either
make sure clients use resumable uploads for anything large, or use a DNS-only
(grey cloud) record or a Cloudflare Tunnel with the same limits in mind.

With Cloudflare in front of nginx, list nginx's address plus Cloudflare's
ranges (or set `TRUST_PROXY=2` if only nginx can reach port 3344) so rate
limits see the visitor's address, not Cloudflare's.

## Health and shutdown

`GET /api/health` answers `{ status, db, storage }`: `SELECT 1` on the
database and a write-access check on the data and storage directories. `200`
when both are fine, `503` otherwise. It is cheap; the Docker image's
`HEALTHCHECK` polls it every 30 s, and so can a load balancer.

On `SIGTERM`/`SIGINT` the server stops accepting connections, answers new
uploads on existing connections with `503 SERVER_STOPPING`, and lets running
requests finish for up to **30 s** before it closes what is left. The
service managers are configured to wait for that: compose's
`stop_grace_period: 40s`, systemd `TimeoutStopSec=40`, launchd
`ExitTimeOut 40`. `neocloud stop` and `neocloud restart` wait too.

## Disk space

Uploads (single, resumable parts, completing a resumable upload, uploads into
share links) are refused with `507 DISK_FULL` when the storage disk would drop
below `NEOCLOUD_MIN_FREE_DISK` (default `1GB`). A full disk would otherwise
fail writes halfway, including the database's. Admin › Health shows free
space on the storage and data disks; `neocloud doctor` warns below the floor.

## Database care

All of this runs on its own; nothing to configure.

- SQLite runs in WAL mode with `busy_timeout` 5 s, a 64 MB page cache and
  `journal_size_limit` 64 MB. `PRAGMA optimize` runs on every start and once a
  day.
- Every hour the WAL is checkpointed (`TRUNCATE`) when it has grown past
  32 MB, so long-lived readers can't let it grow without bound.
- Once a day a `quick_check` runs on a worker thread with its own read-only
  connection. Admin › Health shows its result (`database`: `ok`, `damaged`,
  or `pending` until the first check after a start). A request never runs an
  integrity check itself.
- A blob scrub rotates through all stored files about once a week (a slice
  every hour): each blob must exist, have the size its content implies, and
  its first and last encrypted segments must authenticate. Admin › Health
  shows the number of damaged blobs (`damagedBlobs`); the server log names
  them.
- Blob files are fsynced before they are renamed into place and their folder
  after, so a power cut can't leave a database row pointing at missing bytes.
- A database written by a newer NeoCloud (higher schema version) is refused
  at start with a message naming `neocloud rollback`, instead of running on
  tables this version doesn't know.

`neocloud doctor` checks the install without changing anything: it opens the
database read-only, never runs migrations (it says how many are pending), and
runs `quick_check`. `neocloud doctor --full` runs the slower `integrity_check`
instead and prints how long it took. Both are safe while the server runs.

## Backups

Configure targets in Admin › Backups: a local folder or mounted NAS, SFTP,
S3-compatible storage, or WebDAV. Each target has a schedule (manual, daily,
weekly at an hour) and a retention (how many snapshots to keep).

A run first takes a consistent copy of the database (SQLite's online backup),
then uploads exactly the blobs that copy references and the target doesn't
have yet, then the database (encrypted with a key derived from
`NEOCLOUD_MASTER_KEY`), then the manifest. A snapshot without a manifest is
unfinished and ignored. Because the blob list comes from the copy, a restored
database never references a blob the backup lacks; files uploaded while the
run is going are in the next one. The local folder adapter fsyncs every file
and folder before the manifest is written; SFTP does when the server supports
OpenSSH's fsync extension.

A failed scheduled run is retried in the same slot after 10, 30 and 90
minutes. Runs that a crash or kill left in `running` are marked failed
(`interrupted`) when the server starts.

**Without a scheduled target**, the server keeps a daily encrypted snapshot of
the database (not the files) in `data/backups/daily-*.db.enc`, the last 14.
That protects against a damaged database or a bad upgrade, not against losing
the disk: set up a real target for that.

**Keep the master key somewhere else** (`neocloud keys export`). Without it,
no backup can ever be decrypted.

### Verify

```sh
neocloud backup verify <target-id>
# or: POST /api/admin/backups/targets/:id/verify
```

Downloads the newest snapshot's database, decrypts it, runs `quick_check` on
it, and checks that every blob its manifest lists is on the target. Nothing
local changes. The result (`lastVerification`) is listed with the target in
Admin › Backups and `neocloud backup list`. Verify after setting up a target
and every now and then.

### Restore

A restore replaces the database, so the server must be stopped; the command
refuses while it runs (it checks the server's PID file and `/api/health`).

```sh
neocloud stop
neocloud backup snapshots <target-id>
neocloud backup restore <target-id> <snapshot>
neocloud start
```

The snapshot is downloaded, decrypted and checked first (`quick_check`, and a
snapshot from a newer schema than this version knows is refused); only then
are missing blobs fetched and the database swapped. The previous database is
kept as `data/neocloud.db.before-restore-<time>`. It needs the same
`NEOCLOUD_MASTER_KEY` the backup was made with; on a new machine, put it in
`.env` before restoring.

With Docker:

```sh
docker compose stop neocloud
docker compose run --rm neocloud neocloud backup restore <target-id> <snapshot>
docker compose start neocloud
```

## Upgrades and rollback

### Git installs

```sh
neocloud update                 # follow the branch
neocloud update --tag v0.3.0    # move a pinned install to a release
neocloud rollback               # undo the last update
```

`neocloud update` refuses if the install folder has local changes, then:

1. takes an encrypted database snapshot (`data/backups/pre-update-*.db.enc`,
   the last 5 are kept) and records the current commit,
2. pulls (`--ff-only`) or checks out the tag,
3. installs dependencies with `npm ci` (exactly the lock file),
4. restarts and waits up to 60 s for `/api/health`.

If the new version doesn't come up healthy, it rolls back on its own.
`neocloud rollback` does the same by hand: stop, `git reset --hard` to the
previous commit, `npm ci`, and, only if the update changed the database
schema, restore the pre-update snapshot (the newer database is kept as
`neocloud.db.before-restore-*`). If the schema didn't change, the current
data stays, including anything written since the update.

`install.sh` also uses `npm ci`; `NEOCLOUD_VERSION=v0.3.0` pins a fresh install
to a release.

### Docker

`compose.yaml` pins the image to a major version
(`ghcr.io/neolabs-systems/neocloud:${NEOCLOUD_VERSION:-0}`), so
`docker compose pull && docker compose up -d` brings fixes and features but no
breaking release. Before a major upgrade, copy the database while stopped:

```sh
docker compose stop neocloud
docker compose run --rm --entrypoint sh neocloud -c 'cp /data/data/neocloud.db /data/data/neocloud.db.before-upgrade'
NEOCLOUD_VERSION=1 docker compose up -d
```

To roll back, set the previous `NEOCLOUD_VERSION` and `docker compose up -d`.
If the newer version had already migrated the database, the older one refuses
to start with a clear message; put the copy back first:

```sh
docker compose stop neocloud
docker compose run --rm --entrypoint sh neocloud -c 'cd /data/data && mv neocloud.db neocloud.db.newer && rm -f neocloud.db-wal neocloud.db-shm && cp neocloud.db.before-upgrade neocloud.db'
NEOCLOUD_VERSION=0 docker compose up -d
```
