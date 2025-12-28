# Project Context

## Purpose

CCM (Claude Code Multiplexer) is a small Go HTTP proxy that lets remote clients use a local
Claude/Anthropic subscription. It forwards `/v1/*` requests to Anthropic, injects refreshed OAuth
bearer tokens from the local credential store, and optionally enforces per-user bearer tokens.

## Tech Stack

- Go 1.22 with the standard library HTTP server/client
- Config decoding via `gopkg.in/yaml.v3` plus built-in JSON support
- Nix flake provides build/package (`.#aimux`), devShell (Go 1.22), overlay, and NixOS module
- Tooling: `go test`, `gofmt`; no additional runtime services required; CI builds via GitHub Actions

## Project Conventions

### Code Style

- Follow standard Go style and `gofmt`; keep functions small and readable
- Prefer standard library primitives (http, context, sync) before adding dependencies
- Configuration structs carry JSON/YAML tags; durations accept strings (e.g., `60s`) or integer
  seconds

### Architecture Patterns

- Single binary entrypoint in `cmd/aimux/main.go`; core logic in `internal/aimux`
- `Service` implements `http.Handler`, wrapping authenticator, credential store, and HTTP client
- `CredentialStore` reads `~/.claude/claude/.credentials.json`, refreshes tokens via Anthropic
  OAuth, persists updates
- Authenticator maps pre-shared bearer tokens to usernames; if no users are configured, the proxy is
  open
- Request pipeline strips hop-by-hop headers, applies config headers/beta value, forwards to
  `cfg.AnthropicBase`, and streams SSE responses
- Packaging: `flake.nix` exposes package/app/overlay plus NixOS module (`services.aimux`); defaults
  put service state in `/var/lib/aimux`

### Testing Strategy

- Unit/integration-style tests with `httptest` in `internal/aimux/service_test.go`
- Upstream behavior is stubbed to validate auth enforcement, header handling, SSE passthrough, and
  OAuth refresh logic
- Prefer fast, hermetic tests; no external network calls

### Git Workflow

- No project-specific branching rules documented; default to short-lived feature branches with PRs
- For capability or behavior changes, follow the OpenSpec workflow (create change proposals,
  validate, and wait for approval before implementation)
- Releases: GitHub Action on `v*` tags runs tests, cross-builds (linux/darwin × amd64/arm64), tars
  binaries, and publishes to GitHub Releases

## Domain Context

- Targets Anthropic and ChatGPT; requests must include provider prefixes (default `/claude` and
  `/chatgpt`) followed by `/v1/...`
- Defaults: listen `:8080`; Anthropic base `https://api.anthropic.com`; OAuth token endpoint
  `https://console.anthropic.com/v1/oauth/token`; beta header `oauth-2025-04-20`
- Downstream clients authenticate with `Authorization: Bearer <token>` when users are configured;
  upstream authorization uses refreshed OAuth access tokens
- Uses `RequestTimeout` for upstream response headers (default 60s) and passes through SSE streams
  to clients

## Important Constraints

- Do not start implementation for major changes without an approved OpenSpec proposal
- Preserve credential security: refresh requires stored `refresh_token`; credentials are persisted
  with `0600` permissions
- Keep proxy behavior transparent: avoid forwarding hop-by-hop headers; keep the beta header unless
  the client sets one (Anthropic only)
- Requests without known provider prefixes return 404; maintain streaming support and header
  overrides

## External Dependencies

- Anthropic API (`cfg.AnthropicBase`, default `https://api.anthropic.com`)
- Anthropic OAuth token endpoint `https://console.anthropic.com/v1/oauth/token`
- Local Claude credential file (default `~/.claude/claude/.credentials.json`)
- Go module dependency: `gopkg.in/yaml.v3`
- Nix inputs: `nixpkgs` (24.05), `flake-utils`; outputs include overlay and modules as above
