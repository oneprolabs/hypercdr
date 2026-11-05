---
title: 平台运维与升级
description: 平台启停、服务状态检查、升级链路、备份恢复要点和卸载方式。
---

# 平台运维与升级

默认安装目录 `/var/lib/hypercdr` 保存 `.env`、TLS、Compose、持久化目录和版本清单。以下命令在该目录执行。

## 启停与状态

```bash
./start-platform.sh
./stop-platform.sh
./restart-platform.sh
sudo systemctl status hypercdr.service hypercdr-upgrade-runner.service
```

## 升级流程

```text
升级页面获取 GitHub 发布信息 → 创建升级任务
→ 宿主机 upgrade runner 读取队列 → 生成目标清单
→ 调用蓝绿部署脚本 → 切换流量 → 激活新清单
```

::: warning 任务入队后仍需检查结果

检查执行器运行状态；**任务入队只表示任务已提交，升级结果以执行器输出的任务状态为准**。集群组件升级还需要另行检查 Agent 上报的版本和任务结果。

:::

## 回滚

保留上一版本时可执行：

```bash
./deploy-blue-green.sh --rollback
```

该命令恢复应用流量与版本清单，**不恢复数据库**。如果升级过程中已执行数据库迁移，数据库结构不会随回滚回退。

## 备份与恢复

升级或迁移主机前，备份以下内容：

| 内容 | 说明 |
| --- | --- |
| PostgreSQL | 业务状态，必须与其他项一起才能完整恢复 |
| `.env` | 尤其 `HCDR_SECRET_KEY`，缺了它无法解密受保护数据 |
| TLS | 证书与私钥 |
| Compose / 代理配置 | 运行拓扑与外部代理规则 |
| 版本清单 | `current-release.json` 与 `releases/` |

::: danger 不要直接复制运行中的数据目录

数据库运行中直接复制 PostgreSQL 数据目录不是一致性备份。使用 PostgreSQL 逻辑备份，或停机做一致性备份。

:::

恢复需要匹配的 PostgreSQL/应用版本和**原有的秘密数据保护密钥**。只有数据库文件而没有密钥，不能视为完整备份。

恢复后检查：登录、存储凭据、Agent 连接、任务、升级执行器。

应用备份数据保存在对象存储中，其生命周期独立于平台数据库：恢复平台不会恢复业务备份，恢复业务备份也不会恢复平台。

## 卸载

```bash
./uninstall.sh --execute
```

默认保留数据。确认备份及清理范围后：

```bash
./uninstall.sh --purge-data --execute
```

`--purge-data` 会删除安装目录，**不可恢复**。

外部代理、外部对象存储和业务集群不是平台安装目录的一部分，应分别管理。

平台生命周期脚本的更多细节见 `scripts/release/PLATFORM-LIFECYCLE.md`。

## 相关

- [蓝绿部署与回滚](/docs/operations/blue-green)
- [故障排查](/docs/operations/troubleshooting)
- [配置参考](/docs/deployment/configuration)
