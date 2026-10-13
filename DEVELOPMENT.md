# ATID CLI Proxy development

This checkout is the `rialco/CLIProxyAPI` fork. `origin` points to the fork;
`upstream` points to `router-for-me/CLIProxyAPI`. Read `AGENTS.md` before changes.

## Requirements

- Go 1.26 or newer, as declared in `go.mod`.
- Make, Python 3.9 or newer (standard library only), and Bun 1.3.14.
- On macOS, Xcode Command Line Tools for the native toolchain.

On macOS, install Go 1.26 with `brew install go@1.26`. The Makefile
automatically finds Go on PATH or in the standard Apple Silicon/Intel Homebrew
`go@1.26` location. For direct `go` commands, use:

```bash
export PATH="$(brew --prefix go@1.26)/bin:$PATH"
export GOCACHE="$PWD/.dev/go-build"
export GOMODCACHE="$PWD/.dev/go-mod"
```

No database, Docker, or `.env` file is needed for local file storage.

## Start and verify

From the repository root:

```bash
make setup
make dev
```

Keep that terminal open; Ctrl+C stops the development server. In another terminal:

```bash
make smoke
make test
```

`make dev` rebuilds the frontend and Go server before starting it. Restart it after source
changes. The server watches its configuration for supported hot-reload changes.

| Resource | Development value |
| --- | --- |
| API base | `http://127.0.0.1:8318` |
| OpenAI-compatible base | `http://127.0.0.1:8318/v1` |
| Management UI | `http://127.0.0.1:8318/management.html` |
| Configuration | `.dev/config.yaml` |
| Private client and management keys | `.dev/access.json` |
| OAuth credentials | `.dev/auths/` |
| Runtime logs and UI assets | `.dev/logs/`, `.dev/static/` |
| Executable | `bin/cli-proxy-api-dev` |

The setup creates random keys with owner-only permissions and never prints their
values. Re-running setup preserves existing configuration and keys. The entire
`.dev/` directory and generated binaries are excluded from Git. To choose another
port on first setup, run `python3 scripts/dev.py setup --port 8319` before `make setup`.
If changing the port later, update both `server.port` in the config and `base_url`
in the private access file so the smoke check uses the same address.

The configured management key in `.dev/config.yaml` is authoritative. Never
replace it with a value from `.dev/access.json` just to make a smoke check pass.
If the existing key is preserved only as a bcrypt hash, omit `management_key`
from the access file; `make smoke` explicitly skips the authenticated management
check, which must then be verified through the panel with the user's existing key.

Development runs from `.dev/`, so its optional `.env`, logs, authentication files,
and UI assets remain separate from the Homebrew installation on port 8317.
Embedded model catalogs are selected with `--local-model`. The management frontend
is maintained in `web/`; `make build` compiles it and copies the single-file HTML
into `.dev/static/` and `bin/static/`. Bun is required to build the frontend.
Run `make web-build` after UI changes, then reload the browser. The server never
downloads an upstream panel. Docker builds include the customized frontend.

Quick Start, dynamic library loading, plugin installation, and plugin management
routes have been removed. Legacy internal extension interfaces remain where the
core model registry, request pipeline, usage accounting, and Home configuration
coordinator depend on their types. Normal runtime hosts cannot load extensions,
even if an old configuration contains `plugins.enabled: true`. Home no longer
fetches or stages plugin installation/deletion work. Management v8 omits legacy
plugin configuration on reads and rejects plugin configuration writes.

The smoke check verifies health, authenticated model listing, and v8 management
authentication. It sends no model requests. An empty model list is expected before
connecting a provider; the smoke check does not prove upstream inference works.

## Connect a provider for manual testing

```bash
make login-claude
```

Finish the browser authorization. This saves credentials under `.dev/auths/`.
The running development server watches that directory. Use a separate T3 Code
provider instance with `ANTHROPIC_BASE_URL=http://127.0.0.1:8318` and the development
`client_api_key` from `.dev/access.json`; use `management_key` only for the dashboard.
Provider requests use the connected account's limits. Do not point development
at the Homebrew authentication directory: both processes would refresh the same
credential files.

## Code map

| Directory | Responsibility |
| --- | --- |
| `cmd/server/` | Flags, configuration loading, login modes, server startup |
| `internal/api/` | HTTP routes, middleware, management handlers |
| `sdk/api/handlers/` | OpenAI, Claude, Gemini and related protocol handlers |
| `sdk/cliproxy/` | Service lifecycle, provider registration, config/auth watchers |
| `sdk/cliproxy/auth/` | Credential scheduling, routing, execution coordination |
| `internal/runtime/executor/` | Provider-specific upstream requests |
| `internal/translator/` | Request and response protocol translation |
| `internal/thinking/` | Canonical reasoning configuration and provider conversion |
| `internal/config/`, `internal/watcher/` | v8 YAML configuration and reloads |
| `internal/registry/` | Embedded and remotely refreshed model catalogs |
| `internal/store/` | Optional persistence backends |
| `internal/tui/` | Terminal management interface |
| `test/`, adjacent `*_test.go` files | Integration and package tests |

## Change workflow

```bash
git switch -c feature/my-change
# Edit the relevant package and its regression tests.
gofmt -w .
make check
make dev
# In another terminal:
make smoke
```

`make check` builds the frontend and Go server, runs `go test ./...`, and runs the frontend tests and lint checks. For a focused test,
after exporting the Go environment above:

```bash
go test -run TestName ./path/to/package
```

Preserve the module path in `go.mod`; a GitHub fork does not require changing Go
imports. New management features target `/v8/management`. Follow the repository's
payload ordering rules and translator restrictions in `AGENTS.md`.

## Deferred work

- [ ] Migrate the Homebrew instance's OAuth credentials to development after the
  initial application customization. Stop the Homebrew service before taking
  a private backup and copying credentials into `.dev/auths/`; then verify that
  development loads the accounts and can use them. Keep only one instance active
  with those OAuth sessions to avoid competing token refreshes. Reauthenticate
  only credentials that cannot be renewed. Update client connections separately
  when switching their traffic from port 8317 to port 8318. Migration is deferred;
  no credentials have been copied.
