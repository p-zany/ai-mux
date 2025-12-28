# ai-mux (AI Multiplexer)

ai-mux is a lightweight Go HTTP proxy that converts OAuth-based AI service subscriptions (Claude,
ChatGPT, etc.) into standard API/token authentication. It enables multi-user remote access with
automatic OAuth refresh and flexible provider support.

**Inspired by:** [SagerNet/sing-box](https://github.com/SagerNet/sing-box) CCM service **Language:**
[中文文档](README.zh.md)

## Features

- **Automatic OAuth Token Refresh** - Seamlessly manages Claude/Anthropic credentials
- **Multi-User Access Control** - Bearer token authentication for shared access
- **Multi-Provider Routing** - Provider-prefixed paths for Anthropic (`/claude`) and ChatGPT
  (`/chatgpt`)
- **Flexible Configuration** - YAML/JSON config with header customization
- **Nix-First Distribution** - Flake package and NixOS module included
- **TLS Support** - Optional HTTPS for secure connections

## Quick Start

### Installation

#### Using Go Toolchain

```bash
go install github.com/yourusername/ai-mux/cmd/ai-mux@latest
```

#### Using Nix Flake

```bash
# Build
nix build .#ai-mux

# Run directly
nix run .#ai-mux -- --config /path/to/config.yaml

# Development shell
nix develop
```

### Basic Usage

```bash
ai-mux --config config.yaml
```

## Configuration

ai-mux uses YAML or JSON configuration files. The CLI accepts only `--config` to specify the file
path.

### Quick Start

**Minimal configuration:**

```yaml
listen: ":8080"
state_dir: "~/.ai-mux"
providers:
  - claude # Enable Claude provider
  - chatgpt # Enable ChatGPT provider
```

### Configuration Examples

#### Single Provider (Claude only)

```yaml
listen: ":8080"
state_dir: "~/.ai-mux"
log_level: "info"

providers:
  - claude

users:
  - name: "alice"
    token: "your-secret-token-at-least-16-chars"
```

#### Multi-Provider with TLS

```yaml
listen: ":443"
state_dir: "/var/lib/ai-mux"
log_level: "info"

providers:
  - claude
  - chatgpt

users:
  - name: "alice"
    token: "alice-secret-token-at-least-16-chars"
  - name: "bob"
    token: "bob-secret-token-at-least-16-chars"

tls:
  enabled: true
  cert_path: "/etc/certs/ai-mux.crt"
  key_path: "/etc/certs/ai-mux.key"

request_timeout: "120s"
refresh_check_interval: "10m"
```

### Key Configuration Fields

| Field                    | Type     | Default     | Description                                   |
| ------------------------ | -------- | ----------- | --------------------------------------------- |
| `listen`                 | string   | `:8080`     | Network address to bind to                    |
| `state_dir`              | string   | `~/.ai-mux` | Directory for credentials and state           |
| `providers`              | []string | `[]`        | List of providers: `claude`, `chatgpt`        |
| `log_level`              | string   | `info`      | Log level: `debug`, `info`, `warn`, `error`   |
| `request_timeout`        | duration | `60s`       | Timeout for upstream API requests             |
| `refresh_check_interval` | duration | `10m`       | Interval for background credential refreshes  |
| `users`                  | []User   | `[]`        | Bearer token authentication (empty = no auth) |
| `tls.enabled`            | bool     | `false`     | Enable HTTPS                                  |
| `tls.cert_path`          | string   | `""`        | Path to TLS certificate (required if enabled) |
| `tls.key_path`           | string   | `""`        | Path to TLS private key (required if enabled) |

### Provider Setup

Before enabling a provider, you must have valid OAuth credentials:

- **Claude**: Credentials in `{state_dir}/claude/.credentials.json`
- **ChatGPT**: Credentials in `{state_dir}/chatgpt/auth.json`

See the [Configuration Guide](docs/configuration.md) for credential file formats and detailed
options.

## Deployment

### Standalone Service

```bash
# Create configuration
cat > config.yaml <<EOF
listen: ":8080"
state_dir: "/var/lib/ai-mux"
providers:
  - claude
  - chatgpt
users:
  - name: "team"
    token: "shared-secret-at-least-16-chars"
EOF

# Run service
ai-mux --config config.yaml
```

### NixOS Module

```nix
{
  services.ai-mux = {
    enable = true;
    settings = {
      listen = ":8080";
      state_dir = "/var/lib/ai-mux";
      providers = [ "claude" "chatgpt" ];
      users = [
        { name = "alice"; token = "alice-secret-token-at-least-16"; }
      ];
    };
  };
}
```

## How It Works

1. **Client Request** - Clients send requests to `/claude/v1/...` or `/chatgpt/v1/...` with their
   bearer token
2. **Authentication** - ai-mux validates the token against configured users
3. **Token Refresh** - ai-mux automatically refreshes OAuth tokens when needed
4. **Request Forwarding** - Authenticated requests are forwarded to the respective AI provider API
5. **Response Streaming** - SSE responses are streamed back to clients in real-time

### Credential Management

ai-mux stores OAuth credentials in `{state_dir}/claude/.credentials.json`:

```json
{
  "claudeAiOauth": {
    "accessToken": "...",
    "refreshToken": "...",
    "expiresAt": 1234567890
  }
}
```

ChatGPT OAuth credentials are stored separately in `{state_dir}/chatgpt/auth.json` (0600
permissions):

```json
{
  "access_token": "openai-access-token",
  "refresh_token": "openai-refresh-token",
  "expires_at": 1234567890,
  "account_id": "acct-123"
}
```

Tokens are refreshed proactively (on startup and in the background) and written back to disk with
secure permissions.

## Security Considerations

- **Token Storage** - Credentials are stored with `0600` permissions
- **No Auth Mode** - If no users are configured, ai-mux accepts all requests
- **TLS Recommended** - Use TLS for production deployments
- **Token Length** - User tokens must be at least 16 characters
- **Sensitive Logs** - Tokens are masked in logs (only first 8 chars shown)

## Development

### Building from Source

```bash
# Clone repository
git clone https://github.com/yourusername/ai-mux.git
cd ai-mux

# Build
go build ./cmd/ai-mux

# Run tests
go test ./...
```

### Nix Development

```bash
# Enter dev shell with Go 1.22
nix develop

# Pre-commit hooks included:
# - gofmt (code formatting)
# - gotest (test execution)
# - nixfmt (Nix formatting)
# - prettier (YAML/JSON/Markdown)
```

## License

MIT License - see [LICENSE](LICENSE) for details.

## Attribution

- Inspired by the CCM service in [SagerNet/sing-box](https://github.com/SagerNet/sing-box)
- This project is independent and not affiliated with Anthropic
- Maintained by the ai-mux contributors

## Links

- **Documentation:** [docs/](docs/)
- **Configuration Guide:** [docs/configuration.md](docs/configuration.md)
- **Issues:** Report bugs and feature requests on GitHub
