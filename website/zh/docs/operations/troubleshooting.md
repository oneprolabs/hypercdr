---
title: 故障排查
description: 按现象对照原因，附日志采集命令和常见误判。
---

# 故障排查

先记录**访问地址、平台版本、出错时间、集群类型和具体错误**。日志分享前去掉 token、密码、kubeconfig 和私钥。

## 现象对照表

| 现象 | 优先检查 |
| --- | --- |
| 页面无法打开 | 主机 TCP 12443、安全组/防火墙、Docker 与 Edge 状态、证书与域名 |
| 文档站无法访问 | 文档站是独立部署的服务，检查其容器状态与反向代理配置，与中控安装无关 |
| 502 | Edge 后端目标、活动颜色 API/前端健康、容器日志；区分外部代理返回的 502 |
| `component_target_unavailable` | 活动 manifest 是否完整、对应集群类型组件是否存在；**不要把错误文本当作 shell 脚本执行** |
| 集群 offline | Agent pods/日志、WSS 地址及证书、网络出口、注册凭据与平台记录 |
| 镜像拉取失败 | manifest 中完整地址、节点到仓库网络、仓库认证、CA；不要替换为已废弃的仓库 |
| Turnstile 控件成功但登录失败 | 后端到 Cloudflare 的网络、站点/secret 配置及服务端校验错误 |
| 备份或恢复失败 | 任务详情、Agent/Velero/OADP 日志、对象存储权限、PVC/StorageClass 与资源映射 |
| UI 升级一直等待 | `hypercdr-upgrade-runner.service` 状态与日志、Release 网络和目标 manifest |

## 采集日志

```bash
cd /var/lib/hypercdr
sudo docker compose --env-file .env -f docker-compose.yaml ps
sudo docker compose --env-file .env -f docker-compose.yaml logs --tail 100
sudo journalctl -u hypercdr-upgrade-runner.service -n 100 --no-pager
```

按需追加服务名，聚焦到 Edge、API 或具体容器。

## 常见误判

::: warning `/healthz` 成功不代表环境可用

部署日志、健康检查、真实登录与集群任务分别验证不同环节。`/healthz` 成功不能证明注册组件和存储可用；`/readyz` 当前也不是数据库完整可用性的证明。

:::

::: warning 502 要区分来源

外部代理返回的 502 和 Edge 内部返回的 502 原因完全不同。先看响应头，再决定查哪一层。

:::

::: warning 错误文本不是命令

`component_target_unavailable` 这类标识是错误码，不是可以直接粘贴执行的脚本。把错误信息当命令执行是常见的部署事故。

:::

## 定位顺序

```text
1. 页面能不能打开        → 网络、端口、证书
2. 能不能登录            → 认证配置、验证码、数据库
3. 集群是不是 online     → Agent、WSS、凭据
4. 任务能不能跑          → 存储、PVC、资源映射
5. 恢复能不能用          → 演练、缓存预检、目标映射
```

从下往上是依赖关系。跳过前面直接查后面，通常会浪费时间。

## 需要提交 issue 时

附上：平台版本、集群类型与版本、复现步骤、脱敏后的错误信息和相关日志片段。**不要附 kubeconfig、token 或私钥。**

## 相关

- [平台运维与升级](/docs/operations/platform)
- [Velero 恢复缓存](/docs/operations/restore-cache)
- [任务与恢复点](/docs/manual/tasks)
