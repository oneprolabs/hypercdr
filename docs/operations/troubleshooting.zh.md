# 故障排查

先记录访问地址、平台版本、出错时间、集群类型和具体错误。日志分享前去掉 token、密码、kubeconfig 和私钥。

| 现象 | 优先检查 |
|---|---|
| 页面无法打开 | 主机 TCP 12443、安全组/防火墙、Docker 与 Edge 状态、证书与域名 |
| 官网无法访问 | 是否带 `--install-website`，12445 是否开放，官网容器状态 |
| 502 | Edge 后端目标、活动颜色 API/前端健康、容器日志；区分外部代理返回的 502 |
| `component_target_unavailable` | 活动 manifest 是否完整、对应集群类型组件是否存在；不要把错误文本作为 shell 安装脚本运行 |
| 集群 offline | Agent pods/日志、WSS 地址及证书、网络出口、注册凭据与平台记录 |
| 镜像拉取失败 | manifest 中完整地址、节点到仓库网络、仓库认证、CA；不要替换为已废弃的仓库 |
| Turnstile 控件成功但登录失败 | 后端到 Cloudflare 的网络、站点/secret 配置及服务端校验错误 |
| 备份或恢复失败 | 任务详情、Agent/Velero/OADP 日志、对象存储权限、PVC/StorageClass 与资源映射 |
| UI 升级一直等待 | `hypercdr-upgrade-runner.service` 状态与日志、Release 网络和目标 manifest |

```bash
cd /var/lib/hypercdr
sudo docker compose --env-file .env -f docker-compose.yaml ps
sudo docker compose --env-file .env -f docker-compose.yaml logs --tail 100
sudo journalctl -u hypercdr-upgrade-runner.service -n 100 --no-pager
```

部署日志、健康检查、真实登录与集群任务分别验证不同环节。`/healthz` 成功不能证明注册组件和存储可用，`/readyz` 当前也不是数据库完整可用性证明。
