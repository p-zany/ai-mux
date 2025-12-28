# 配置参考

ai-mux (AI Multiplexer) 配置参考文档。

## 概览

- **配置格式**：YAML 或 JSON（根据文件扩展名自动检测）
- **命令行参数**：仅支持 `--config` 指定配置文件路径
- **环境变量**：不支持
- **默认行为**：如果未指定配置文件，使用所有默认值

## 配置字段

### 服务器设置

#### `listen`

**类型：** `string` **必填：** 否 **默认值：** `:8080`

ai-mux 绑定的网络地址，用于接收传入连接。

**示例：**

```yaml
listen: ":8080"              # 所有网卡，端口 8080
listen: "127.0.0.1:8080"     # 仅本地访问
listen: "0.0.0.0:3000"       # 所有网卡，端口 3000
```

---

#### `state_dir`

**类型：** `string` **必填：** 否 **默认值：** `~/.ai-mux`

ai-mux 存储状态文件的目录。凭证存储位置：

- Claude：`{state_dir}/claude/.credentials.json`
- ChatGPT：`{state_dir}/chatgpt/auth.json`

**行为：**

- 父目录会自动创建，权限为 `0700`
- 凭证文件权限为 `0600`
- 路径中的 `~` 会展开为用户主目录

**示例：**

```yaml
state_dir: "/var/lib/ai-mux"         # 系统服务
state_dir: "/home/alice/.ai-mux"     # 用户服务
state_dir: "~/.config/ai-mux/state"  # 自定义位置
```

---

#### `log_level`

**类型：** `string` **必填：** 否 **默认值：** `info`

结构化日志级别。支持 `debug`、`info`、`warn`、`error`。

**示例：**

```yaml
log_level: "debug"
log_level: "info"
```

---

### 提供商配置

#### `providers`

**类型：** `字符串数组` **必填：** 是 **默认值：** `[]`

要启用的 AI 提供商列表。至少必须配置一个提供商。

**支持的值：**

- `claude` - 启用 Claude/Anthropic API（前缀：`/claude`）
- `chatgpt` - 启用 ChatGPT/OpenAI API（前缀：`/chatgpt`）

**行为：**

- 每个提供商都需要在状态目录中有有效的 OAuth 凭证
- 提供商前缀是硬编码的：Claude 为 `/claude`，ChatGPT 为 `/chatgpt`
- 对未知前缀的请求返回 `404 Not Found`

**示例：**

```yaml
# 单个提供商
providers:
  - claude

# 多个提供商
providers:
  - claude
  - chatgpt
```

**凭证要求：**

- **Claude**：必须有 `{state_dir}/claude/.credentials.json`，包含有效的 OAuth 令牌
- **ChatGPT**：必须有 `{state_dir}/chatgpt/auth.json`，包含有效的 OAuth 令牌

---

### 超时设置

#### `request_timeout`

**类型：** `duration` **必填：** 否 **默认值：** `60s`

等待上游响应头的超时时间，SSE 等流式响应在此之后会继续。

**格式：** Go 时长字符串或整数秒数

- 字符串：`"30s"`、`"1m"`、`"500ms"`、`"1m30s"`
- 整数：`60`（解释为秒）

**示例：**

```yaml
request_timeout: "60s"
request_timeout: "2m"
request_timeout: 120
```

---

#### `refresh_check_interval`

**类型：** `duration` **必填：** 否 **默认值：** `10m`

后台凭证刷新检查间隔（适用于 Claude/ChatGPT）。支持 Go 时长字符串或整数秒数。

**示例：**

```yaml
refresh_check_interval: "10m"
refresh_check_interval: 600
```

---

### 身份认证

#### `users`

**类型：** `对象数组` **必填：** 否 **默认值：** `[]`（空 - 无认证）

带有 bearer token 的用户列表，用于身份认证。

**行为：**

- 如果为空，ai-mux **接受所有请求**，不进行认证
- 如果配置了用户，客户端必须发送 `Authorization: Bearer <token>` 头
- 令牌在所有用户中必须唯一
- 令牌长度至少 16 个字符
- 用户名仅用于日志记录，不会发送到上游

**用户对象字段：**

- `name`（string，必填）：用于日志的用户标识
- `token`（string，必填）：用于认证的 Bearer 令牌

**示例：**

```yaml
# 无认证（开放访问）
users: []

# 单用户
users:
  - name: "alice"
    token: "your-secret-token-at-least-16-chars"

# 多用户
users:
  - name: "alice"
    token: "alice-secret-token-at-least-16-chars"
  - name: "bob"
    token: "bob-secret-token-at-least-16-chars"
  - name: "team"
    token: "shared-team-token-at-least-16-chars"
```

---

### TLS 配置

#### `tls.enabled`

**类型：** `bool` **必填：** 否 **默认值：** `false`

启用 HTTPS 而不是 HTTP。

---

#### `tls.cert_path`

**类型：** `string` **必填：** 否（但如果启用 TLS 则必填） **默认值：** `""`

用于 HTTPS 的 TLS 证书文件路径。

**要求：**

- 文件必须存在且可读
- 当 `tls.enabled` 为 `true` 时，必须与 `tls.key_path` 一起设置

---

#### `tls.key_path`

**类型：** `string` **必填：** 否（但如果启用 TLS 则必填） **默认值：** `""`

用于 HTTPS 的 TLS 私钥文件路径。

**要求：**

- 文件必须存在且可读
- 当 `tls.enabled` 为 `true` 时，必须与 `tls.cert_path` 一起设置

**示例：**

```yaml
# 无 TLS（仅 HTTP）
tls:
  enabled: false

# 启用 TLS（HTTPS）
tls:
  enabled: true
  cert_path: "/etc/certs/ai-mux.crt"
  key_path: "/etc/certs/ai-mux.key"

# 使用 Let's Encrypt
tls:
  enabled: true
  cert_path: "/var/lib/acme/ai-mux.example.com/fullchain.pem"
  key_path: "/var/lib/acme/ai-mux.example.com/key.pem"
```

---

## 完整配置示例

### 最小配置（HTTP，无认证）

```yaml
listen: ":8080"
state_dir: "~/.ai-mux"
providers:
  - claude
```

### 生产配置（HTTPS，多用户，多提供商）

```yaml
# 服务器
listen: ":443"
state_dir: "/var/lib/ai-mux"
log_level: "info"

# 提供商
providers:
  - claude
  - chatgpt

# 身份认证
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

# 超时设置
request_timeout: "120s"
refresh_check_interval: "10m"
```

### 开发配置（本地，单用户）

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

## 凭证文件格式

### Claude 凭证

ai-mux 将 Claude OAuth 凭证存储在 `{state_dir}/claude/.credentials.json`：

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

**字段：**

- `accessToken`：当前 OAuth 访问令牌
- `refreshToken`：用于获取新访问令牌的刷新令牌
- `expiresAt`：过期时间戳（自纪元以来的毫秒数）
- `scopes`、`subscriptionType`、`isMax`、`rateLimitTier`：可选元数据

**自动刷新：**

- 令牌在过期前 60 秒自动刷新
- 刷新后的凭证会写回文件
- 文件权限保持为 `0600`

### ChatGPT 凭证

ChatGPT OAuth 凭证存储在 `{state_dir}/chatgpt/auth.json`：

```json
{
  "access_token": "openai-access-token",
  "refresh_token": "openai-refresh-token",
  "expires_at": 1234567890,
  "account_id": "acct-123"
}
```

**字段：**

- `access_token`：当前 OpenAI 访问令牌
- `refresh_token`：用于获取新访问令牌的刷新令牌
- `expires_at`：访问令牌过期的 Unix 时间戳
- `account_id`：OpenAI 账户 ID（自动设置 `ChatGPT-Account-Id` 头）

**自动刷新：**

- 令牌在启动时及后台周期性刷新
- 更新写入 `{state_dir}/chatgpt/auth.json`，权限为 `0600`

---

## 行为详情

### 请求路由

- 请求必须带有提供商前缀：
  - Claude：`/claude/v1/...`
  - ChatGPT：`/chatgpt/v1/...`
- 未知前缀返回 `404 Not Found`
- 前缀是硬编码的，不可配置

### API 端点

提供商 API 端点是硬编码的：

- **Claude**：`https://api.anthropic.com`
- **ChatGPT**：`https://api.openai.com`

OAuth 令牌端点也是硬编码的：

- **Claude**：`https://api.anthropic.com/v1/oauth/token`
- **ChatGPT**：`https://auth.openai.com/oauth/token`

### 请求头处理

**移除的头（Hop-by-Hop）：**

- `Connection`
- `Keep-Alive`
- `TE`
- `Trailers`
- `Transfer-Encoding`
- `Upgrade`
- `Proxy-*`（所有代理头）
- `Host`

**重写的头：**

- `Authorization`：始终设置为 `Bearer {刷新后的访问令牌}`
- `ChatGPT-Account-Id`：当 ChatGPT 凭证包含 `account_id` 时自动设置

### 流式传输支持

- `Content-Type: text/event-stream` 的响应会被流式传输
- 使用 32KB 缓冲区，每次写入后刷新
- 保持 SSE 实时传输到客户端

### 日志记录

每个请求记录：

- 远程地址
- HTTP 方法
- 请求路径
- 用户名（来自认证）
- 响应状态码
- 响应字节数
- 请求耗时
- 上游主机

**安全性：** 日志中的令牌会被脱敏（仅显示前 8 个字符）

### 凭证刷新

- Claude OAuth 在过期前 60 秒刷新，并写回 `{state_dir}/claude/.credentials.json`
- ChatGPT OAuth 在启动时及后台周期性刷新，写入 `{state_dir}/chatgpt/auth.json`（权限 `0600`）

### 优雅关闭

- 监听 `SIGINT` 和 `SIGTERM` 信号
- 停止接受新连接
- 等待最多 10 秒完成进行中的请求
- 记录关闭事件

---

## Nix 模块配置

### NixOS 系统服务

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

## 验证规则

启动时会验证配置：

1. **必填字段：**
   - `listen` 不能为空
   - `state_dir` 不能为空
   - `providers` 必须包含至少一个提供商

2. **提供商验证：**
   - 每个提供商必须是 `claude` 或 `chatgpt`
   - 提供商凭证文件必须存在且可读
   - 凭证文件必须包含有效的 JSON

3. **TLS 验证：**
   - 如果 `tls.enabled` 为 `true`，`tls.cert_path` 和 `tls.key_path` 都必须设置
   - 两个文件都必须存在且可读

4. **用户验证：**
   - 用户名不能为空
   - 令牌不能为空
   - 令牌长度至少 16 个字符
   - 令牌必须唯一（无重复）

5. **超时验证：**
   - `request_timeout` 必须为正数
   - `refresh_check_interval` 必须为正数

如果验证失败，ai-mux 会退出并显示错误消息。
