# Configuration Reference

ai-mux (AI Multiplexer) configuration reference documentation.

## Overview

- **Configuration Format**: YAML or JSON (auto-detected by file extension)
- **CLI Flags**: Only `--config` to specify configuration file path
- **Environment Variables**: Not supported
- **Default Behavior**: If no config file specified, all defaults are used

## Configuration Fields

### Server Settings

#### `listen`

**Type:** `string` **Required:** No **Default:** `:8080`

The network address ai-mux binds to for incoming connections.

**Examples:**

```yaml
listen: ":8080"              # All interfaces, port 8080
listen: "127.0.0.1:8080"     # Localhost only
listen: "0.0.0.0:3000"       # All interfaces, port 3000
```

---

#### `state_dir`

**Type:** `string` **Required:** No **Default:** `~/.ai-mux`

Directory where ai-mux stores its state files. Credentials are stored in:

- Claude: `{state_dir}/claude/.credentials.json`
- ChatGPT: `{state_dir}/chatgpt/auth.json`

**Behavior:**

- Parent directories are created automatically with `0700` permissions
- Credentials files are created with `0600` permissions
- The path expands `~` to the user's home directory

**Examples:**

```yaml
state_dir: "/var/lib/ai-mux"         # System service
state_dir: "/home/alice/.ai-mux"     # User service
state_dir: "~/.config/ai-mux/state"  # Custom location
```

---

#### `log_level`

**Type:** `string` **Required:** No **Default:** `info`

Structured log level for the proxy. Supports `debug`, `info`, `warn`, `error`.

**Examples:**

```yaml
log_level: "debug"
log_level: "info"
```

---

### Provider Configuration

#### `providers`

**Type:** `array of strings` **Required:** Yes **Default:** `[]`

List of AI providers to enable. At least one provider must be configured.

**Supported values:**

- `claude` - Enable Claude/Anthropic API (prefix: `/claude`)
- `chatgpt` - Enable ChatGPT/OpenAI API (prefix: `/chatgpt`)

**Behavior:**

- Each provider requires valid OAuth credentials in the state directory
- Provider prefixes are hardcoded: `/claude` for Claude, `/chatgpt` for ChatGPT
- Requests to unknown prefixes return `404 Not Found`

**Examples:**

```yaml
# Single provider
providers:
  - claude

# Multiple providers
providers:
  - claude
  - chatgpt
```

**Credential Requirements:**

- **Claude**: Must have `{state_dir}/claude/.credentials.json` with valid OAuth tokens
- **ChatGPT**: Must have `{state_dir}/chatgpt/auth.json` with valid OAuth tokens

---

### Timeout Settings

#### `request_timeout`

**Type:** `duration` **Required:** No **Default:** `60s`

Timeout waiting for upstream response headers. Streaming responses (SSE) continue after this point.

**Format:** Go duration string or integer seconds

- String: `"30s"`, `"1m"`, `"500ms"`, `"1m30s"`
- Integer: `60` (interpreted as seconds)

**Examples:**

```yaml
request_timeout: "60s"
request_timeout: "2m"
request_timeout: 120
```

---

#### `refresh_check_interval`

**Type:** `duration` **Required:** No **Default:** `10m`

Interval used by background credential refreshers (Claude/ChatGPT). Accepts Go duration strings or
integer seconds.

**Examples:**

```yaml
refresh_check_interval: "10m"
refresh_check_interval: 600
```

---

### Authentication

#### `users`

**Type:** `array of objects` **Required:** No **Default:** `[]` (empty - no authentication)

List of users with bearer tokens for authentication.

**Behavior:**

- If empty, ai-mux accepts **all requests** without authentication
- If configured, clients must send `Authorization: Bearer <token>` header
- Tokens must be unique across all users
- Tokens must be at least 16 characters long
- User names are used for logging only, not sent to upstream

**User Object Fields:**

- `name` (string, required): User identifier for logging
- `token` (string, required): Bearer token for authentication

**Examples:**

```yaml
# No authentication (open access)
users: []

# Single user
users:
  - name: "alice"
    token: "your-secret-token-at-least-16-chars"

# Multiple users
users:
  - name: "alice"
    token: "alice-secret-token-at-least-16-chars"
  - name: "bob"
    token: "bob-secret-token-at-least-16-chars"
  - name: "team"
    token: "shared-team-token-at-least-16-chars"
```

---

### TLS Configuration

#### `tls.enabled`

**Type:** `bool` **Required:** No **Default:** `false`

Enable HTTPS instead of HTTP.

---

#### `tls.cert_path`

**Type:** `string` **Required:** No (but required if TLS enabled) **Default:** `""`

Path to TLS certificate file for HTTPS.

**Requirements:**

- Must exist and be readable
- Must be set together with `tls.key_path` when `tls.enabled` is `true`

---

#### `tls.key_path`

**Type:** `string` **Required:** No (but required if TLS enabled) **Default:** `""`

Path to TLS private key file for HTTPS.

**Requirements:**

- Must exist and be readable
- Must be set together with `tls.cert_path` when `tls.enabled` is `true`

**Examples:**

```yaml
# No TLS (HTTP only)
tls:
  enabled: false

# TLS enabled (HTTPS)
tls:
  enabled: true
  cert_path: "/etc/certs/ai-mux.crt"
  key_path: "/etc/certs/ai-mux.key"

# Using Let's Encrypt
tls:
  enabled: true
  cert_path: "/var/lib/acme/ai-mux.example.com/fullchain.pem"
  key_path: "/var/lib/acme/ai-mux.example.com/key.pem"
```

---

## Complete Configuration Examples

### Minimal Configuration (HTTP, No Auth)

```yaml
listen: ":8080"
state_dir: "~/.ai-mux"
providers:
  - claude
```

### Production Configuration (HTTPS, Multi-User, Multi-Provider)

```yaml
# Server
listen: ":443"
state_dir: "/var/lib/ai-mux"
log_level: "info"

# Providers
providers:
  - claude
  - chatgpt

# Authentication
users:
  - name: "alice"
    token: "alice-secure-token-min-16-chars"
  - name: "bob"
    token: "bob-secure-token-min-16-chars"
  - name: "team-shared"
    token: "team-shared-token-min-16-chars"

# TLS
tls:
  enabled: true
  cert_path: "/etc/certs/ai-mux.example.com.crt"
  key_path: "/etc/certs/ai-mux.example.com.key"

# Timeouts
request_timeout: "120s"
refresh_check_interval: "10m"
```

### Development Configuration (Localhost, Single User)

```yaml
listen: "127.0.0.1:8080"
state_dir: "/tmp/ai-mux-dev"
log_level: "debug"

providers:
  - claude

users:
  - name: "dev"
    token: "dev-token-12345678"

request_timeout: "30s"
```

---

## Credential File Format

### Claude Credentials

ai-mux stores Claude OAuth credentials in `{state_dir}/claude/.credentials.json`:

```json
{
  "claudeAiOauth": {
    "accessToken": "sk-ant-...",
    "refreshToken": "sk-ant-...",
    "expiresAt": 1234567890000,
    "scopes": ["user:inference"],
    "subscriptionType": "max",
    "isMax": true,
    "rateLimitTier": "default_claude_max_20x"
  }
}
```

**Fields:**

- `accessToken`: Current OAuth access token
- `refreshToken`: Refresh token used to get new access tokens
- `expiresAt`: Expiration timestamp in milliseconds since epoch
- `scopes`, `subscriptionType`, `isMax`, `rateLimitTier`: Optional metadata

**Automatic Refresh:**

- Tokens are refreshed automatically 60 seconds before expiration
- Refreshed credentials are written back to the file
- File permissions are maintained at `0600`

### ChatGPT Credentials

ChatGPT OAuth credentials are stored in `{state_dir}/chatgpt/auth.json`:

```json
{
  "access_token": "openai-access-token",
  "refresh_token": "openai-refresh-token",
  "expires_at": 1234567890,
  "account_id": "acct-123"
}
```

**Fields:**

- `access_token`: Current OpenAI access token
- `refresh_token`: Refresh token for obtaining new access tokens
- `expires_at`: Unix timestamp when the access token expires
- `account_id`: OpenAI account ID (automatically sets `ChatGPT-Account-Id` header)

**Automatic Refresh:**

- Tokens refresh proactively on startup and in the background
- Updates are written to `{state_dir}/chatgpt/auth.json` with `0600` permissions

---

## Behavior Details

### Request Routing

- Requests must include a provider prefix:
  - Claude: `/claude/v1/...`
  - ChatGPT: `/chatgpt/v1/...`
- Unknown prefixes return `404 Not Found`
- Prefixes are hardcoded and not configurable

### API Endpoints

Provider API endpoints are hardcoded:

- **Claude**: `https://api.anthropic.com`
- **ChatGPT**: `https://api.openai.com`

OAuth token endpoints are also hardcoded:

- **Claude**: `https://api.anthropic.com/v1/oauth/token`
- **ChatGPT**: `https://auth.openai.com/oauth/token`

### Header Processing

**Removed Headers (Hop-by-Hop):**

- `Connection`
- `Keep-Alive`
- `TE`
- `Trailers`
- `Transfer-Encoding`
- `Upgrade`
- `Proxy-*` (all proxy headers)
- `Host`

**Rewritten Headers:**

- `Authorization`: Always set to `Bearer {refreshed_access_token}`
- `ChatGPT-Account-Id`: Set automatically when ChatGPT credentials contain `account_id`

### Streaming Support

- Responses with `Content-Type: text/event-stream` are streamed
- Uses 32KB buffer with flush after each chunk
- Preserves real-time SSE delivery to clients

### Logging

Each request logs:

- Remote address
- HTTP method
- Request path
- User name (from authentication)
- Response status code
- Response bytes
- Request duration
- Upstream host

**Security:** Tokens in logs are masked (only first 8 characters shown)

### Credential Refresh

- Claude OAuth tokens refresh 60 seconds before expiration and are persisted back to
  `{state_dir}/claude/.credentials.json`
- ChatGPT OAuth tokens refresh proactively on startup and in the background. Updates are written to
  `{state_dir}/chatgpt/auth.json` with `0600` permissions

### Graceful Shutdown

- Listens for `SIGINT` and `SIGTERM` signals
- Stops accepting new connections
- Waits up to 10 seconds for in-flight requests to complete
- Logs shutdown events

---

## Nix Module Configuration

### NixOS System Service

```nix
{
  services.ai-mux = {
    enable = true;
    settings = {
      listen = ":8080";
      state_dir = "/var/lib/ai-mux";
      providers = [ "claude" "chatgpt" ];
      users = [
        { name = "alice"; token = "alice-token-at-least-16"; }
        { name = "bob"; token = "bob-token-at-least-16"; }
      ];
      request_timeout = "60s";
    };
  };
}
```

---

## Validation Rules

Configuration is validated on startup:

1. **Required Fields:**
   - `listen` must not be empty
   - `state_dir` must not be empty
   - `providers` must contain at least one provider

2. **Provider Validation:**
   - Each provider must be `claude` or `chatgpt`
   - Provider credential files must exist and be readable
   - Credential files must contain valid JSON

3. **TLS Validation:**
   - If `tls.enabled` is `true`, both `tls.cert_path` and `tls.key_path` must be set
   - Both files must exist and be readable

4. **User Validation:**
   - User names must not be empty
   - Tokens must not be empty
   - Tokens must be at least 16 characters
   - Tokens must be unique (no duplicates)

5. **Timeout Validation:**
   - `request_timeout` must be positive
   - `refresh_check_interval` must be positive

If validation fails, ai-mux exits with an error message.
