# ai-mux (AI Multiplexer)

ai-mux 是一个轻量级 Go
HTTP 代理，将基于 OAuth 的 AI 服务订阅（Claude、ChatGPT 等）转换为标准的 API/Token 认证方式。它支持多用户远程访问、自动 OAuth 刷新和灵活的多提供商支持。

**灵感来源：** [SagerNet/sing-box](https://github.com/SagerNet/sing-box) CCM 服务 **语言：**
[English Documentation](README.md)

## 特性

- **自动 OAuth 令牌刷新** - 无缝管理 Claude/Anthropic 凭证
- **多用户访问控制** - 基于 Bearer Token 的共享访问认证
- **多提供商路由** - 通过 `/claude` 与 `/chatgpt` 前缀区分 Anthropic 与 ChatGPT
- **灵活配置** - 支持 YAML/JSON 配置及请求头自定义
- **Nix 优先分发** - 包含 Flake 包和 NixOS 模块
- **TLS 支持** - 可选 HTTPS 加密连接

## 快速开始

### 安装

#### 使用 Go 工具链

```bash
go install github.com/yourusername/ai-mux/cmd/ai-mux@latest
```

#### 使用 Nix Flake

```bash
# 构建
nix build .#ai-mux

# 直接运行
nix run .#ai-mux -- --config /path/to/config.yaml

# 开发环境
nix develop
```

### 基本使用

```bash
ai-mux --config config.yaml
```

## 配置

ai-mux 使用 YAML 或 JSON 配置文件。命令行仅支持 `--config` 参数指定配置文件路径。

### 快速开始

**最小配置：**

```yaml
listen: ":8080"
state_dir: "~/.ai-mux"
providers:
  - claude # 启用 Claude 提供商
  - chatgpt # 启用 ChatGPT 提供商
```

### 配置示例

#### 单提供商（仅 Claude）

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

#### 多提供商与 TLS

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

### 主要配置字段

| 字段                     | 类型     | 默认值      | 说明                                       |
| ------------------------ | -------- | ----------- | ------------------------------------------ |
| `listen`                 | string   | `:8080`     | 监听的网络地址                             |
| `state_dir`              | string   | `~/.ai-mux` | 凭证和状态文件目录                         |
| `providers`              | []string | `[]`        | 提供商列表：`claude`、`chatgpt`            |
| `log_level`              | string   | `info`      | 日志级别：`debug`、`info`、`warn`、`error` |
| `request_timeout`        | duration | `60s`       | 上游 API 请求超时                          |
| `refresh_check_interval` | duration | `10m`       | 后台凭证刷新间隔                           |
| `users`                  | []User   | `[]`        | Bearer token 认证（空=无认证）             |
| `tls.enabled`            | bool     | `false`     | 启用 HTTPS                                 |
| `tls.cert_path`          | string   | `""`        | TLS 证书路径（启用时必需）                 |
| `tls.key_path`           | string   | `""`        | TLS 私钥路径（启用时必需）                 |

### 提供商设置

启用提供商前，必须有有效的 OAuth 凭证：

- **Claude**：凭证位于 `{state_dir}/claude/.credentials.json`
- **ChatGPT**：凭证位于 `{state_dir}/chatgpt/auth.json`

详细的凭证文件格式和配置选项请参阅[配置指南](docs/configuration.md)。

## 部署

### 独立服务

```bash
# 创建配置文件
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

# 运行服务
ai-mux --config config.yaml
```

### NixOS 模块

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

## 工作原理

1. **客户端请求** - 客户端携带 Bearer Token 访问 `/claude/v1/...` 或 `/chatgpt/v1/...`
2. **身份认证** - ai-mux 验证 Token 是否匹配配置的用户
3. **令牌刷新** - ai-mux 在需要时自动刷新 OAuth 令牌
4. **请求转发** - 认证通过的请求被转发到对应的 AI 服务提供商 API
5. **响应流式传输** - SSE 响应实时流式返回给客户端

### 凭证管理

ai-mux 将 OAuth 凭证存储在 `{state_dir}/claude/.credentials.json`：

```json
{
  "claudeAiOauth": {
    "accessToken": "...",
    "refreshToken": "...",
    "expiresAt": 1234567890
  }
}
```

ChatGPT OAuth 凭证存储在 `{state_dir}/chatgpt/auth.json`（权限保持 `0600`）：

```json
{
  "access_token": "openai-access-token",
  "refresh_token": "openai-refresh-token",
  "expires_at": 1234567890,
  "account_id": "acct-123"
}
```

令牌会在启动时和后台定期刷新，并写回磁盘。

## 安全注意事项

- **令牌存储** - 凭证文件权限为 `0600`
- **无认证模式** - 如果未配置用户，ai-mux 接受所有请求
- **建议使用 TLS** - 生产环境建议启用 TLS
- **令牌长度** - 用户令牌长度至少 16 个字符
- **敏感日志** - 日志中令牌会被脱敏（仅显示前 8 个字符）

## 开发

### 从源码构建

```bash
# 克隆仓库
git clone https://github.com/yourusername/ai-mux.git
cd ai-mux

# 构建
go build ./cmd/ai-mux

# 运行测试
go test ./...
```

### Nix 开发环境

```bash
# 进入包含 Go 1.22 的开发 shell
nix develop

# 包含的 pre-commit hooks：
# - gofmt（代码格式化）
# - gotest（测试执行）
# - nixfmt（Nix 格式化）
# - prettier（YAML/JSON/Markdown）
```

## 许可证

MIT 许可证 - 详见 [LICENSE](LICENSE)

## 致谢

- 灵感来源于 [SagerNet/sing-box](https://github.com/SagerNet/sing-box) 的 CCM 服务
- 本项目独立维护，与 Anthropic 无关联
- 由 ai-mux 贡献者维护

## 链接

- **文档：** [docs/](docs/)
- **配置指南：** [docs/configuration.md](docs/configuration.md)
- **问题反馈：** 在 GitHub 上报告 Bug 和功能请求
