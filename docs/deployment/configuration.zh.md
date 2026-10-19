# 配置参考

以根目录 `env.example`、`docker-compose.yml` 和安装脚本为准。安装目录 `.env` 包含真实密钥，不可提交或公开。源码示例不是可直接运行的安装配置。

| 配置 | 当前用途 |
|---|---|
| `HCDR_BASE_URL` | 中控外部访问地址 |
| `HCDR_PUBLIC_BASE_URL` | 安装初始公开地址；安装后以数据库中平台设置为准 |
| `HCDR_HTTPS_PORT` | Edge 端口，默认 12443 |
| `HCDR_WEBSITE_PORT` | 可选官网端口，默认 12445 |
| `HCDR_AUTH_CHALLENGE_MODE` | `captcha` 图片验证码或 `turnstile` Cloudflare |
| `HCDR_TURNSTILE_SITE_KEY` / `HCDR_TURNSTILE_SECRET_KEY` | Turnstile 浏览器与服务端凭据 |
| `HCDR_TURNSTILE_VERIFY_URL` | 服务端校验地址，空值采用后端默认值 |
| `HCDR_DATABASE_URL` | PostgreSQL 连接地址 |
| `HCDR_SECRET_KEY` | 平台秘密数据保护密钥，备份与迁移时需保留 |
| `HCDR_RELEASE_MANIFEST_PATH` | 容器内活动清单路径 |
| `HCDR_IMAGE_REGISTRY` | 组件镜像仓库 |
| `HCDR_TLS_CERT_FILE` / `HCDR_TLS_KEY_FILE` | 宿主机 TLS 文件 |
| `HCDR_RELEASE_TOKEN` | 升级执行器访问凭据 |
| `HCDR_REGISTRATION_EXECUTOR_TOKEN` | 内部注册执行器凭据 |

Compose 未指定验证码模式时默认 turnstile；源码 env.example 示例为 captcha；发布安装包可能携带 CI 注入的验证码设置。以安装后 `.env` 为准，不应假设所有安装都有同一模式。

Turnstile 同时要求浏览器访问 Cloudflare 控件、API 出站访问校验服务及有效的站点配置。captcha 不需要 Cloudflare 凭据。

修改配置后需按安装目录 Compose 配置重建受影响容器，单纯刷新页面或 restart 现有容器不会重新载入 Compose 环境变量。不要通过手工改镜像标签替代蓝绿升级。

## 社区版注册与组织

社区版支持邮箱注册、Google 登录和组织邀请。一个账号只能属于一个组织；自行注册会创建组织并成为管理员，通过邀请注册会加入邀请指定的组织。没有组织切换功能。组织管理员通过 Settings → Organization / Members 管理本组织，不能修改平台 SMTP 或其他组织的数据。

| 配置 | 用途 |
|---|---|
| `HCDR_DEPLOYMENT_MODE` | `saas`（默认）或 `private`；private 关闭公开注册 |
| `HCDR_SAAS_SIGNUP_ENABLED` | 是否允许自行注册创建组织，默认 true；关闭后仍可接受明确的邀请 |
| `HCDR_GOOGLE_CLIENT_ID` / `HCDR_GOOGLE_CLIENT_SECRET` | Google OAuth Web 应用凭据；必须同时配置，未配置时隐藏入口 |

邮箱验证和邀请通过平台管理员在 SMTP 页面设置的默认配置发送，未配置时注册页明确显示不可发送邮件。Google 回调填写实际平台访问地址加 `/api/v1/saas/auth/google/callback`，例如 `https://app.hypercdr.com/api/v1/saas/auth/google/callback`。回调必须与 Google Cloud 中登记的地址完全一致；不要将 Client Secret 写入前端或提交到仓库。局域网 IP 不是 Google 常规 Web OAuth 的有效回调域名，开发验证需要域名或 localhost 转发。

Google 登录校验签名、issuer、audience、有效期、已验证邮箱、state、nonce、PKCE 和发起浏览器。同邮箱密码账号不会自动绑定 Google；必须登录后在 Sign-in & Security 验证密码再绑定。账号登录邮箱不可通过个人设置直接改为未经验证的邮箱。

新注册用户默认浅色主题；用户主题偏好存储在账号中。登录/注册页保持统一深色风格。

## 更改平台访问地址（IP 切换域名）

平台管理员进入 **Settings → Access & Login**，核对当前浏览器地址与已配置地址，
填写 HTTPS origin（不含路径），确认 DNS/反向代理/TLS 已就绪后保存。
页面展示验证邮件、邀请链接及 Google 回调地址。普通组织管理员没有此权限。
修改立即作用于所有 API 实例，不需要重启；升级时保留数据库设置。
`HCDR_PUBLIC_BASE_URL` 仅在首次初始化平台设置时提供默认值，不覆盖已保存的地址。

页面不可用时，可在安装主机执行：

```bash
sudo /var/lib/hypercdr/configure-public-url.sh https://app.example.com
```

该命令运行当前 API 镜像中的 `platform-configure-url`，使用同一套校验并更新同一
数据库记录；不会自动修改 DNS、证书或外部反向代理。源码开发环境可执行
`go run ./backend/cmd/platform-configure-url https://app.example.com`（需设置
`HCDR_DATABASE_URL`）。数据库及当前 API 镜像必须可用，API 本身不必运行。

配置改变后，新邮件使用新地址，已有邮件链接不会改写。Google Cloud 的回调 URI
须同步更新，进行中的 Google 登录可能需要重新开始。已安装 Agent 的连接地址
不会自动迁移，应保持旧入口可达或单独安排 Agent 地址迁移。
注册页遇到地址不一致时提供正式入口，平台管理员仍可通过原入口登录并修复设置。
