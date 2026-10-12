# 配置参考

以根目录 `env.example`、`docker-compose.yml` 和安装脚本为准。安装目录 `.env` 包含真实密钥，不可提交或公开。源码示例不是可直接运行的安装配置。

| 配置 | 当前用途 |
|---|---|
| `HCDR_BASE_URL` | 中控外部访问地址 |
| `HCDR_PUBLIC_BASE_URL` | 公共地址配置；在线入口当前不转发同名 CLI 参数 |
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
