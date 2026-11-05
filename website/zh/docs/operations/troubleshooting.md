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
| 文档站无法访问 | 文档站独立部署，检查容器状态与反向代理配置；与控制台安装无关 |
| 502 | Edge 后端目标、活动颜色的 API/前端健康状态、容器日志；区分外部代理返回的 502 |
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

按需追加服务名，限定 Edge、API 或指定容器。

## 常见误判

::: warning `/healthz` 只覆盖部分环节

部署日志、健康检查、真实登录和集群任务分别验证不同环节。`/healthz` 成功时，注册组件和存储仍可能不可用；`/readyz` 目前也不能说明数据库完整可用。

:::

::: warning 502 需先确认来源

外部代理返回的 502 与 Edge 内部返回的 502 原因不同。先看响应头，再确定排查层级。

:::

::: warning 错误码不能直接执行

`component_target_unavailable` 这类标识是错误码，无法直接执行。把错误信息作为命令执行是常见的部署事故。

:::

## 定位顺序

```text
1. 页面能不能打开        → 网络、端口、证书
2. 能不能登录            → 认证配置、验证码、数据库
3. 集群是不是 online     → Agent、WSS、凭据
4. 任务能不能跑          → 存储、PVC、资源映射
5. 恢复能不能用          → 演练、缓存预检、目标映射
```

各项按顺序存在依赖关系。跳过前项直接检查后项，通常会延长排查时间。

## 提交 issue

附上：平台版本、集群类型与版本、复现步骤、脱敏后的错误信息和相关日志片段。**不要附 kubeconfig、token 或私钥。**

## 相关

- [平台运维与升级](/docs/operations/platform)
- [Velero 恢复缓存](/docs/operations/restore-cache)
- [任务与恢复点](/docs/manual/tasks)
