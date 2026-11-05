---
title: 配置参考
description: HyperCDR 环境变量、验证码模式、社区版注册配置，以及平台访问地址的修改方法。
---

# 配置参考

以根目录 `env.example`、`docker-compose.yml` 和安装脚本为准。安装目录 `.env` 包含真实密钥，**不可提交或公开**。源码示例不是可直接运行的安装配置。

## 核心环境变量 {#env}

| 配置 | 当前用途 |
| --- | --- |
| `HCDR_BASE_URL` | 控制台外部访问地址 |
| `HCDR_PUBLIC_BASE_URL` | 安装时的初始公开地址；安装后以数据库中的平台设置为准 |
| `HCDR_HTTPS_PORT` | Edge 端口，默认 12443 |
| `HCDR_WEBSITE_PORT` | 历史产品门户的兼容配置，默认 12445；当前源码不再自动启动旧门户，也不能用此项配置独立文档站 |
| `HCDR_AUTH_CHALLENGE_MODE` | `captcha` 为图片验证码（安装参数里也写作 `image`），`turnstile` 为 Cloudflare 验证 |
| `HCDR_TURNSTILE_SITE_KEY` / `HCDR_TURNSTILE_SECRET_KEY` | Turnstile 浏览器与服务端凭据 |
| `HCDR_TURNSTILE_VERIFY_URL` | 服务端校验地址，空值采用后端默认值 |
| `HCDR_DATABASE_URL` | PostgreSQL 连接地址 |
| `HCDR_SECRET_KEY` | 用于保护平台敏感数据的密钥，备份与迁移时需保留 |
| `HCDR_RELEASE_MANIFEST_PATH` | 容器内当前版本清单的路径 |
| `HCDR_IMAGE_REGISTRY` | 组件镜像仓库 |
| `HCDR_TLS_CERT_FILE` / `HCDR_TLS_KEY_FILE` | 宿主机 TLS 文件 |
| `HCDR_RELEASE_TOKEN` | 升级执行器访问凭据 |
| `HCDR_REGISTRATION_EXECUTOR_TOKEN` | 内部注册执行器凭据 |

## 验证码模式 {#captcha}

Compose 未指定验证码模式时，默认使用 `turnstile`。源码 `env.example` 的示例为 `captcha`，发布安装包也可能携带由 CI 注入的验证码设置。**以安装后的 `.env` 为准**，不要假设所有安装都使用同一模式。

Turnstile 要求浏览器可以访问 Cloudflare 控件，API 可以出站访问校验服务，并且站点配置有效。`captcha` 不需要 Cloudflare 凭据。

## 修改配置之后 {#apply}

修改配置后，需按安装目录的 Compose 配置重建受影响的容器。只刷新页面或重启现有容器，**不会**重新载入 Compose 环境变量。

不要手工修改镜像标签来代替蓝绿升级。

## 社区版注册与组织 {#signup}

社区版支持邮箱注册、Google 登录和组织邀请。一个账号只能属于一个组织：自行注册会创建组织，注册者成为该组织管理员；通过邀请注册会加入邀请指定的组织。平台不提供组织切换。组织管理员通过 Settings → Organization / Members 管理本组织，不能修改平台 SMTP 或其他组织的数据。

| 配置 | 用途 |
| --- | --- |
| `HCDR_DEPLOYMENT_MODE` | `saas`（默认）或 `private`；private 关闭公开注册 |
| `HCDR_SAAS_SIGNUP_ENABLED` | 是否允许自行注册创建组织，默认 true；关闭后仍可接受明确的邀请 |
| `HCDR_GOOGLE_CLIENT_ID` / `HCDR_GOOGLE_CLIENT_SECRET` | Google OAuth Web 应用凭据；必须同时配置，未配置时入口隐藏 |

邮箱验证和邀请邮件使用平台管理员在 SMTP 页面设置的默认配置发送；未配置时，注册页会提示无法发送邮件。

Google 回调填写实际平台访问地址加 `/api/v1/saas/auth/google/callback`，例如 `https://app.hypercdr.com/api/v1/saas/auth/google/callback`。回调必须与 Google Cloud 中登记的地址完全一致。不要把 Client Secret 写入前端或提交到仓库。局域网 IP 不是 Google 常规 Web OAuth 的有效回调域名，开发验证需要域名或 localhost 转发。

::: info Google 登录的校验范围

Google 登录会校验签名、issuer、audience、有效期、已验证邮箱、state、nonce、PKCE 以及发起登录的浏览器。同一邮箱的密码账号**不会**自动绑定 Google，需要在登录后到 Sign-in & Security 验证密码再绑定。登录邮箱不能通过个人设置直接改为未验证的邮箱。

:::

新注册用户默认使用浅色主题，主题偏好保存在账号中。登录页和注册页统一使用深色风格。

## 更改平台访问地址（从 IP 改为域名） {#access-url}

平台管理员进入 **Settings → Access & Login**，核对当前浏览器地址与已配置地址，填写 HTTPS origin（不含路径），确认 DNS/反向代理/TLS 已就绪后保存。页面会显示验证邮件、邀请链接和 Google 回调地址。普通组织管理员没有此权限。

修改对所有 API 实例立即生效，不需要重启；升级时数据库设置保持不变。`HCDR_PUBLIC_BASE_URL` 仅在首次初始化平台设置时提供默认值，不覆盖已保存的地址。

页面不可用时，可在安装主机执行：

```bash
sudo /var/lib/hypercdr/configure-public-url.sh https://app.example.com
```

该命令运行当前 API 镜像中的 `platform-configure-url`，使用同一套校验并更新同一条数据库记录；**不会**自动修改 DNS、证书或外部反向代理。源码开发环境可执行 `go run ./backend/cmd/platform-configure-url https://app.example.com`（需设置 `HCDR_DATABASE_URL`）。数据库和当前 API 镜像必须可用，API 本身无需运行。

::: warning 更改地址后的影响

- 新邮件使用新地址，已有邮件里的链接不会改写。
- Google Cloud 的回调 URI 须同步更新，进行中的 Google 登录可能需要重新开始。
- 已安装 Agent 的连接地址不会自动迁移，应保持旧入口可达，或单独安排 Agent 地址迁移。
- 注册页遇到地址不一致时会提供正式入口，平台管理员仍可通过原入口登录并修复设置。

:::
