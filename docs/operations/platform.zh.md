# 平台运维与升级

默认安装目录 `/var/lib/hypercdr` 保存 `.env`、TLS、Compose、持久化目录和版本清单。以下命令在该目录执行。

```bash
./start-platform.sh
./stop-platform.sh
./restart-platform.sh
sudo systemctl status hypercdr.service hypercdr-upgrade-runner.service
```

平台升级页面获取 GitHub 发布信息并创建升级任务；宿主机 upgrade runner 读取队列，生成目标清单并调用蓝绿部署脚本。检查执行器运行状态，不要把任务入队当成完成。集群组件升级需要另外检查 Agent 上报的版本和任务结果。

保留上一版本时可使用 `./deploy-blue-green.sh --rollback`，它恢复应用流量与版本清单，不恢复数据库。详见[蓝绿部署](../deployment/blue-green-deployment.zh.md)。

## 备份与恢复

升级或迁移主机前，备份 PostgreSQL、`.env`（尤其密钥）、TLS、Compose/代理配置以及版本清单。数据库运行中不要直接复制 PostgreSQL 数据目录作为一致性备份；使用 PostgreSQL 逻辑备份或停机一致性备份。凭据与备份限制访问并存放于独立位置。

恢复需要匹配的 PostgreSQL/应用版本和原有秘密数据保护密钥；只有数据库文件而没有密钥不能视为完整备份。恢复后检查登录、存储凭据、Agent 连接、任务及升级执行器。应用备份位于对象存储，其生命周期独立于平台数据库。

## 卸载

```bash
./uninstall.sh --execute
```

默认保留数据。确认备份及清理范围后，`./uninstall.sh --purge-data --execute` 会删除安装目录，不可恢复。平台生命周期脚本详见 [脚本说明](../../scripts/release/PLATFORM-LIFECYCLE.md)。外部代理、外部对象存储和业务集群不是平台安装目录，应分别管理。
