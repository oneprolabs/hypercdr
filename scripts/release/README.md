# HyperCDR 构建与发布说明

本目录集中管理中控平台的构建、镜像发布、安装包生成和平台运维脚本。Bootstrap 只负责 Portal 页面及资源分发，不参与中控平台镜像或安装包的核心构建。

## 推荐入口

```bash
cd /data/hypercdr-main/scripts/release
cp release.conf.example release.conf
# 按需编辑 release.conf
./release-all.sh 1.0.23.20260914 --config ./release.conf
```

`release-all.sh` 是完整发布入口，依次完成：

1. 构建中控平台 API、前端、升级器、注册执行器、comm-agent 和 oadp-comm-agent 镜像；
2. 推送平台镜像；
3. 构建/发布 Velero 及对象存储插件；
4. 同步并构建 OADP/OpenShift 相关镜像和资源；
5. 生成包含组件版本、镜像地址和 digest 的完整 `release-manifest.json`；
6. 根据该 manifest 生成中控平台安装包和 SHA256 校验文件；
7. 向已运行的中控平台登记候选版本（初次发布可使用 `--skip-register`）。

## 脚本职责

| 文件 | 作用 |
|---|---|
| `release-all.sh` | 完整发布入口 |
| `build-release.sh` | 构建平台镜像和二进制 |
| `push-release.sh` | 推送平台镜像 |
| `publish-runtime-images.sh` | 发布 Velero 等运行时镜像 |
| `sync-velero-plugins.sh` | 同步对象存储插件 |
| `mirror-community-oadp-images.sh` | 同步 OADP/OpenShift 镜像 |
| `build-community-oadp-bundle.sh` | 构建 OADP 部署资源 |
| `build-community-oadp-catalog.sh` | 构建 OADP Catalog 镜像 |
| `package-release.sh` | 根据已有 manifest 生成中控平台安装包，不构建镜像 |
| `publish-package.sh` | 将已有平台安装包发布到 Bootstrap Portal |
| `install-platform.sh` | 安装中控平台并配置 systemd 自启动 |
| `deploy-platform.sh` | 渲染或部署 Compose 配置 |
| `start-platform.sh` / `stop-platform.sh` | 启动或停止平台，不删除数据 |
| `restart-platform.sh` | 重启平台并等待健康检查 |
| `uninstall.sh` / `uninstall-platform.sh` | 一键入口和实际卸载逻辑 |
| `verify-platform.sh` | 验证已部署平台 |
| `verify-oadp-catalog.sh` | 验证 OADP Catalog |
| `common.sh` | 公共函数 |
| `templates/hypercdr.service` | systemd 服务模板 |

## 调用关系

```text
release-all.sh
├── build-release.sh
├── push-release.sh
├── publish-runtime-images.sh
├── sync-velero-plugins.sh
├── mirror-community-oadp-images.sh
├── build-community-oadp-bundle.sh
├── build-community-oadp-catalog.sh
└── package-release.sh
    └── hypercdr-installer-<版本>.tar.gz

publish-package.sh
└── 校验并发布 release-all.sh 已生成的平台安装包
```

Bootstrap 下的 `scripts/package-release.sh` 仅作为历史兼容入口，不能替代 `release-all.sh`。

## 产物位置

```text
/data/hypercdr-runtime/build/platform/<版本>/
/data/hypercdr-runtime/releases/community/<版本>/
├── hypercdr-installer-<版本>.tar.gz
├── hypercdr-installer-<版本>.sha256
├── release-manifest.json
└── manifest.json
```

## 安装和运维

```bash
./install-platform.sh docker --base-url https://HOST:3002 \
  --install-dir /var/lib/hypercdr --execute --confirm-prerequisites
systemctl status hypercdr.service
systemctl restart hypercdr.service
curl -k -o /dev/null -w 'ready=%{http_code}\n' https://HOST:3002/readyz
```

安装目录会生成生命周期脚本、`PLATFORM-LIFECYCLE.md` 和 `hypercdr.service`。默认卸载保留数据；只有显式使用 `--purge-data --execute` 才删除安装目录。

## 验证

```bash
bash -n scripts/release/*.sh
make verify
```

只有 systemd 为 `enabled/active`、5 个 Compose 服务运行且 `/readyz` 返回 HTTP 200，才表示部署成功。
