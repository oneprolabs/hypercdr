---
title: 卸载集群 Agent
description: Agent 或控制台无法重连时，使用版本匹配的离线卸载脚本清理集群内资源。
---

# 卸载集群 Agent

正常的集群解除注册应**从控制台发起**：平台先处理自身与对象存储的生命周期，再要求在线 Agent 移除自身的 Kubernetes 资源。

仅在 Agent 或控制台已经无法重连时，才使用离线卸载脚本。

## 脚本的获取位置

每次安装 Agent 时，与版本匹配的卸载脚本会保存在 Agent Namespace 的 `configmap/hypercdr-agent-uninstaller` 中。执行前先取出并审阅：

```bash
kubectl -n hypercdr-agent get configmap hypercdr-agent-uninstaller \
  -o jsonpath='{.data.uninstall-agent\.sh}' > /tmp/uninstall-hypercdr-agent.sh
chmod 0700 /tmp/uninstall-hypercdr-agent.sh
/tmp/uninstall-hypercdr-agent.sh --namespace hypercdr-agent
```

第一次运行是 **dry run**，只打印计划，不做实际删除。

## 执行卸载

确认计划无误后添加 `--execute`：

```bash
/tmp/uninstall-hypercdr-agent.sh \
  --namespace hypercdr-agent \
  --execute
```

## 清理残留的 Velero 资源

如果删除已完成正常等待，但只剩部分 `stale` 的 Velero 资源（对应控制器已不存在），可以使用受控方式重新执行：

```bash
/tmp/uninstall-hypercdr-agent.sh \
  --namespace hypercdr-agent \
  --execute \
  --force-finalizers
```

## 脚本不删除的对象

::: warning 脚本的处理范围

脚本是幂等的，finalizer 移除**仅限**指定且可识别的 HyperCDR Agent Namespace 内的 Velero 对象。

脚本**不会**删除：

- 应用 Namespace；
- Longhorn 资源；
- 外部对象存储中的备份对象；
- 其他 Namespace 中的 Velero 资源。

:::

## 控制台仍可访问时的处理

离线清理无法通知已不存在的控制台。如果平台仍然存在，卸载完成后应在平台上执行 **Force Remove**，清除该集群留下的过期记录。

## 从控制台下载脚本

控制台可达时，也可以直接下载同一份脚本：

```bash
curl -k -fsSL https://CONTROL-PLANE/uninstall-agent.sh \
  -o /tmp/uninstall-hypercdr-agent.sh
```

下载后同样执行 `chmod 0700`，先 dry run 再执行。

## 相关

- [集群注册与管理](/docs/manual/clusters)
- [故障排查](/docs/operations/troubleshooting)
