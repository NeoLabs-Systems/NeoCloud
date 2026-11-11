# Development

Read [GUIDELINES.md](../GUIDELINES.md) before contributing. The
[API reference](api.md) lists every endpoint and [clients.md](clients.md)
the contracts the clients rely on.

## Server

```bash
npm install
npm run dev     # server with --watch on port 3344
npm test        # unit, integration and security tests (temporary data dir)
npm run check   # syntax check every server file
```

## Web and mobile app

[`flutter_app/`](../flutter_app) targets the web, Android and iOS. Run it
against the dev server:

```bash
cd flutter_app
flutter run -d chrome --dart-define=NEOCLOUD_BACKEND_URL=http://localhost:3344
flutter analyze && flutter test
```

Production serves the web build under `/app` from `server/public`. It is
generated: every release builds it from the released source and commits it
with the release, so each tag carries its own bundle. *Build Web Client*
(run by hand) rebuilds it on a branch outside a release.

## NeoCloud Lite (desktop)

[`clients/lite/`](../clients/lite) is the desktop client for Windows, macOS
and Linux, a Go tray executable (Go 1.26+):

```bash
cd clients/lite
go build -o neocloud-lite ./cmd/neocloud-lite
./neocloud-lite login --server http://localhost:3344
./neocloud-lite pair add ~/NeoCloud /Documents --direction both
./neocloud-lite run              # tray icon; `run --headless` for servers
go test ./... && go test -tags e2e ./e2e/...   # e2e spawns a real server
```

cgo is needed only for the macOS tray, so macOS builds on a Mac:

```bash
CGO_ENABLED=1 GOOS=darwin  GOARCH=arm64 go build ./cmd/neocloud-lite   # also amd64
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/neocloud-lite
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build ./cmd/neocloud-lite   # also arm64
```

## Branches and releases

- `beta` is where work lands. Every push publishes a prerelease
  `vX.Y.Z-beta.N` with the Lite binaries, the Android APK and the Docker
  images tagged `beta`.
- `main` is stable. *Release Stable* (run by hand) merges `beta` into
  `main`, cuts `vX.Y.Z` from the conventional commits and moves `latest`.
- The release commit carries the version stamp and `server/public`, so a
  tag is complete for a git install (`install.sh`, `neocloud update --tag`)
  and matches the Docker image built from it.
- Pushes that change only `docs/` or Markdown files run no release and skip
  the CI test jobs. *CI result* is the one check to mark as required.
- *Release Stable* has a dry-run input that prints the next version without
  pushing anything. Before the first stable release it tags the root commit
  `v0.0.0`, the baseline semantic-release counts from (it ignores the beta
  tags), so the `0.1.0` betas become `v0.1.0`, not `v1.0.0`.
