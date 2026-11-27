# NeoCloud — Engineering Guidelines

NeoCloud is self-hosted, multi-user, encrypted file storage: a Go server with a
Flutter client (web under `/app`, Android and iOS) and a Go lite client, which is
the desktop client (Windows, macOS, Linux).

Generic building blocks shared by the NeoLabs apps (accounts primitives, sealed
secrets, rate limits, SSRF-safe HTTP, LLM provider clients, the CLI shell,
signed releases, service supervision) live in
[`neolabs-go`](https://github.com/NeoLabs-Systems/neolabs-go), and shared screens
in [`neolabs-flutter`](https://github.com/NeoLabs-Systems/neolabs-flutter).
NeoCloud-specific code stays here.

## Directory Map

```
cmd/
  neocloud/          The single binary: server (`neocloud serve`) and CLI
  neocloud-release/  Release tooling used by CI (signed archives, web check, notes)
internal/
  config/ runtimeapp/ buildinfo/  Environment, paths, startup and version
  application/       Builds every domain, connects them, owns the background workers
  httppolicy/ apijson/ proxytrust/ webui/  HTTP rules, request/response helpers, static files
  schema/ cryptox/   The database schema; key hierarchy and segmented AES-GCM
  auth/ oauth/ settings/ admin/ members/ accountdelete/ accountmail/  Accounts
  files/ vaults/ notes/ comments/ activity/ publicshares/ webdav/ office/  The file data plane
  imaging/ documentconvert/ searchindex/ photos/  Previews, conversion, search
  sync/ realtime/ handoff/ imports/ externalstorage/ outbound/  Moving data
  ai/ neorouter/ agentapi/ signing/  AI, integrations and document signing
  backups/ maintenance/ loghistory/ updates/ manager/ analytics/  Operations
flutter_app/         Flutter client; strings in `lib/l10n/*.arb` (de, en)
clients/lite/        Desktop tray client (its own Go module)
landing/             Public website at /
```

## Core Principles

- **No duplication.** Search the domain package, `internal/` and `neolabs-go` before adding a helper.
- **No speculative features, no dead code.** Build what the task needs.
- **Readable over clever.** Split a file when it holds two subjects.
- **Existing installs keep working.** Database, blob format, HTTP API and CLI commands are contracts.

## Go

- `gofmt`, `go vet`, `staticcheck` (see `staticcheck.conf`) and `go test -race` must pass.
- Handlers parse the request and call the domain; business logic does not live in routing code.
- Parameterized SQL only. Schema changes are new ordered migrations; never edit a shipped one.
- Error codes and texts are part of the API. Non-admins never see technical error text.

## Invariants

- **Every node lookup is scoped to the account.** Another account's id behaves like a missing one: `404`, never `403`.
- **Nothing is written to disk in plaintext**, except the private temporary folder Office conversion needs.
- **A blob file is moved into place before its row is committed.** Files whose last reference goes away are deleted after the commit, with their previews.
- **Every tree mutation records its change journal row in the same transaction.** Sync clients depend on it.
- `NEOCLOUD_MASTER_KEY` is generated once and never replaced by code.
- Never log secrets, tokens, other accounts' file names or file contents.
- Outbound requests to addresses a person typed go through `internal/outbound` or the shared SSRF guard.
- AI reads only the account's own files that are not in a vault and not excluded.
- Desktop tokens reach files, transfers, storage, sync, links and vaults; the phone app's token reaches what a browser session does.

## Flutter / Dart

- Web, Android and iOS only; desktop belongs to `clients/lite`.
- Shared screens and blocks come from `neolabs_flutter`; the app supplies wording, calls and its own pages.
- `main_controller.dart` is the root `ChangeNotifier`. `lib/main_<feature>.dart` is thin UI; logic lives in `lib/src/<feature>/`.
- Strings come from the ARB files, colors from the palette.
- Gold marks primary actions and the selection only; status is a dot plus a word.
- The logo is NeoRouter's orbital mark with the glyph turned 42°.
- Never run `flutter build web` during development; releases build and commit it.

## Documentation

Keep `docs/api.md` in step with route changes. No `CHANGELOG.md`; GitHub Releases are the history.
