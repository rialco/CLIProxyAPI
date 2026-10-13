# ATID CLI Proxy

A local AI proxy for Claude, Codex, OpenAI-compatible clients, and other built-in
providers. This fork is based on [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI).

The management frontend lives in `web/` and is built with the server. Quick Start,
dynamic plugins, and the plugin store are not available in ATID. The server never
downloads an upstream management panel over the customized frontend.

## Local development

Requirements: Go 1.26+, Bun, Python 3, and Make.

```bash
make setup
make dev
```

Open <http://127.0.0.1:8318/management.html>. Private development access keys are
stored in `.dev/access.json`; never commit or share that file.

```bash
make smoke       # Check local API health and authentication
make check       # Build both components and run Go/frontend tests and frontend lint
make web-build   # Rebuild the management UI without restarting the Go server
```

See [DEVELOPMENT.md](DEVELOPMENT.md) for the code map, configuration isolation,
provider setup, and deferred work. The existing Homebrew instance on port 8317
uses separate configuration and credentials.

## Source and license

- Fork: <https://github.com/rialco/CLIProxyAPI>
- Upstream server: <https://github.com/router-for-me/CLIProxyAPI>
- Frontend provenance: [web/UPSTREAM.md](web/UPSTREAM.md)
- [MIT license](LICENSE)

The upstream Go module path and API protocol names are retained for compatibility.
