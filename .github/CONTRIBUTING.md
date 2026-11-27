# Contributing to NeoCloud

## Development

```bash
cd flutter_app && flutter pub get && cd ..
go run ./cmd/neocloud serve     # server on http://127.0.0.1:3344
cd flutter_app && flutter run -d chrome --dart-define NEOCLOUD_BACKEND_URL=http://127.0.0.1:3344
```

The server is Go 1.27 (`cmd/neocloud`, `internal/`); the client is Flutter. Use a
temporary `NEOCLOUD_HOME` so development never touches your own files. See
[GUIDELINES.md](GUIDELINES.md) for the layout and rules.

## Tests

```bash
gofmt -l cmd internal            # must be empty
GOWORK=off go vet ./...
GOWORK=off go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...   # see staticcheck.conf
GOWORK=off go test -race ./...
cd flutter_app && flutter analyze && flutter test
cd clients/lite && go test ./... && go test -tags e2e ./e2e/...
```

## Release

`beta` is where work lands; `main` is stable. Every push to `beta` publishes a
beta release; **Release Stable** (run by hand) promotes `beta` into `main`. The
release workflows build the web client, commit it with the new version
(`internal/buildinfo/version.go` and `package.json`) and attach the signed
server archives, the clients and the Android app.

`package.json` is not a Node project: Node.js installations read it during
`neocloud update` to find the way to the Go version.

Repository secrets:

| Secret | Used for |
| --- | --- |
| `NEOCLOUD_RUNTIME_SIGNING_PRIVATE_KEY` | Signs the manifest installs update from. Releases stop before tagging if it is missing or does not match `internal/manager/public_key.txt`. |
| `DISCORD_RELEASE_WEBHOOK_URL` | Release announcement. |

`go run ./cmd/neocloud-release runtime-keygen --output <folder outside the repo>`
writes a new key pair: commit the public key and store the private key as the
secret. Installs only accept updates signed with the key they already carry.
