# 构建与发布维护指南

依据 `.github/workflows/release.yml`、`config/registries.conf` 和 `scripts/release/`。

## GitHub Actions

推送 `v*` tag 触发发布；版本格式为 `MAJOR.MINOR.PATCH.YYYYMMDD`。也可 workflow_dispatch 指定版本、仓库 profile 和 deploy。手动触发的 deploy 默认 false；**tag 推送在发布成功后会自动部署到工作流配置的环境**，不是给所有已安装平台自动升级。

测试和镜像构建从 prepare-release 后并行运行；最终发布等待必要任务成功。发布产物包括版本化安装包和组件 manifest。GitHub Release 位于仓库的 Releases 页面，不是源码目录。每个版本保留自己的清单，不应以最新清单替换所有已安装版本。

镜像仓库由 registry profile 和 GitHub 配置解析，当前在线安装入口默认使用 `registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr`。核对工作流输出的实际地址；凭据使用 CI secrets，不写入文档或源码。

PR 检查不发布镜像、不部署。推送普通代码提交不等同于发布 tag。

## 部署与版本清单

CI 部署调用蓝绿脚本。独立安装用户从 Release 获取安装器，平台升级页面检测发布信息并创建升级任务；宿主机 `platform-upgrade-runner.sh` 读取任务及组件信息，再调用 `deploy-blue-green.sh`。

流量切换和当前清单更新由部署脚本控制。不得只改版本标签或手工填充缺失的组件版本。切换失败需查看脚本与执行器日志；数据库迁移不因切换流量而自动反向迁移。

## 本地发布工具

`scripts/release/release-all.sh --config /absolute/path/to/release.conf` 仍是本地发布入口。使用仓库中的配置示例核对必需参数，实际配置、密码文件和产物放在源码目录外。远程 Actions 是公开发布入口，本地工具不是普通用户的安装前提。

安装见[安装指南](deployment-guide.zh.md)，开发见[构建与源码运行](build-release-install.md)。
