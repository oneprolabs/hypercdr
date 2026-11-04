---
title: 卸载集群 Agent
description: Agent 或控制面无法重连时，用版本匹配的离线卸载脚本清理集群内资源。
---

# 卸载集群 Agent

正常的集群解除注册应**从控制面发起**：平台会先处理平台侧和对象存储的生命周期，再要求在线 Agent 移除自己的 Kubernetes 资源。

只有 Agent 或控制面已经无法重连时，才使用离线卸载脚本。

## 脚本从哪里来

每次 Agent 安装时，都会把版本匹配的卸载脚本保存在 Agent 命名空间的 `configmap/hypercdr-agent-uninstaller` 里。执行前先取出并审阅：

```bash
kubectl -n hypercdr-agent get configmap hypercdr-agent-uninstaller \
  -o jsonpath='{.data.uninstall-agent\.sh}' > /tmp/uninstall-hypercdr-agent.sh
chmod 0700 /tmp/uninstall-hypercdr-agent.sh
/tmp/uninstall-hypercdr-agent.sh --namespace hypercdr-agent
```

第一次运行是 **dry run**，只打印计划，不做实际删除。

## 执行卸载

确认计划无误后加上 `--execute`：

```bash
/tmp/uninstall-hypercdr-agent.sh \
  --namespace hypercdr-agent \
  --execute
```

## 清理残留的 Velero 资源

如果删除完成了正常等待，但只剩一些 `stale` 的 Velero 资源（对应控制器已经不存在），可以用受控的兜底方式重跑：

```bash
/tmp/uninstall-hypercdr-agent.sh \
  --namespace hypercdr-agent \
  --execute \
  --force-finalizers
```

## 它的边界

::: warning 脚本会做什么，不会做什么

工具是幂等的，finalizer 移除**仅限于**指定且可识别的 HyperCDR Agent 命名空间内的 Velero 对象。

它**不会**删除：

- 应用命名空间；
- Longhorn 资源；
- 外部对象存储里的备份对象；
- 其他命名空间中的 Velero 资源。

:::

## 控制面还活着的情况

离线清理无法通知已经不存在的控制面。如果平台仍然存在，卸载完成后应在平台上执行 **Force Remove**，清除它留下的过期集群记录。

## 从在线平台下载脚本

控制面可达时，也可以直接下载同一份脚本：

```bash
curl -k -fsSL https://CONTROL-PLANE/uninstall-agent.sh \
  -o /tmp/uninstall-hypercdr-agent.sh
```

下载后同样先 `chmod 0700`，先 dry run 再执行。

## 相关

- [集群注册与管理](/docs/manual/clusters)
- [故障排查](/docs/operations/troubleshooting)
