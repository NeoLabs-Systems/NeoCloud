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

The app window ([docs/clients.md](clients.md#app-window-lite)) is plain
HTML, CSS and JS in `internal/settingsui/ui`, embedded with `go:embed`;
rebuild the binary to see a change. `run --headless` prints a one-time URL
to open it in any browser. The tray icons in `internal/tray/icons` are
rendered from the logo's glyph (template PNGs for macOS, colored PNG/ICO
elsewhere); regenerate all five states together when the logo changes.

Launched without arguments (a double click) the binary runs the tray app,
once per config folder: a second launch finds `instance.lock` taken and
asks the running copy, over a loopback port and a random token in
`instance.json`, to open its window. On macOS a reopen event does the same.
The first tray launch from an installed location turns on start at login;
after that the setting is the user's. With arguments, the CLI behaves as
before.

Installable apps are built by `packaging/<target>/build.sh <tag> <outdir>
[arch]` (CI: the `lite-package` job in `_build-clients.yml`):

| Target | Asset | Notes |
| --- | --- | --- |
| macOS | `NeoCloud-<tag>-macos.dmg` | Universal `NeoCloud.app`, menu bar only (`LSUIElement`), drag to Applications. Signed ad hoc only: open it the first time with right-click › Open until Developer ID signing and notarisation exist. |
| Windows | `NeoCloud-Setup-<tag>-windows-x64.exe`, `…-arm64.exe` | NSIS, per user in `%LOCALAPPDATA%\Programs\NeoCloud`, Start-menu shortcut, uninstaller (keeps the config). GUI build (`-H=windowsgui`) with icon and version info from go-winres. Unsigned, so SmartScreen warns. |
| Linux | `NeoCloud-<tag>-linux-x86_64.AppImage` | Best effort; autostart via XDG points at the AppImage. |

The raw `neocloud-lite-*` binaries stay in the release for servers. The
icons in `packaging/icons` come from the logo through `render.sh`.

The security key lock ([docs/clients.md](clients.md#security-key-lock-lite))
lives in `internal/fido` (CTAPHID, CTAP2 and PIN protocol 1; hidraw on
Linux, IOKit via cgo on macOS, none on Windows) and `internal/keylock`
(enrolment, sealing, the lock watcher). The tests run against
`internal/fido/fidotest`, a software authenticator behind a fake HID bus,
so they need no hardware. With a real key: `neocloud-lite key enroll`,
then unplug and replug it while `neocloud-lite run --headless` is running.

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
