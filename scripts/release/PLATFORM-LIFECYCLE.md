# HyperCDR 中控平台生命周期管理

安装目录默认为 `/var/lib/hypercdr`，其中的脚本使用该目录下的
`docker-compose.yaml`，不会删除平台数据。

## 脚本

| 脚本 | 用途 |
|---|---|
| `start-platform.sh` | 启动中控平台并等待健康检查 |
| `stop-platform.sh` | 停止中控容器，保留数据 |
| `restart-platform.sh` | 先停止再启动平台 |
| `uninstall.sh` | 一键卸载平台（默认保留数据） |

## 常用示例

```bash
cd /var/lib/hypercdr
./start-platform.sh
./stop-platform.sh
./restart-platform.sh
./uninstall.sh --execute
```

卸载并删除安装目录（不可恢复，请确认后执行）：

```bash
./uninstall.sh --purge-data --execute
```

## systemd 开机自启

安装程序会创建并启用 `hypercdr.service`。主机重启后 Docker 就绪时平台会自动启动。

```bash
systemctl status hypercdr.service
systemctl start hypercdr.service
systemctl stop hypercdr.service
systemctl restart hypercdr.service
systemctl is-enabled hypercdr.service
```

停止或卸载不会删除数据库和持久化数据，只有显式指定 `--purge-data` 才会删除安装目录。
