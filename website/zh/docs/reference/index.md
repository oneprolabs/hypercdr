---
title: 参考
description: 术语、支持与适配范围、限制与安全建议，以及 CCE、OpenShift 专项说明。
---

# 参考

这里放**边界性**内容：术语怎么定义、什么组合验证过、哪些限制绕不过去。

## 本板块内容

<div class="hcdr-grid hcdr-grid-3">
  <a class="hcdr-card" href="/docs/reference/glossary"><b>术语表</b><span>平台里反复出现的名词，一句话解释。</span></a>
  <a class="hcdr-card" href="/docs/reference/support-scope"><b>支持与适配范围</b><span>哪些入口和组合已经验证，哪些只是代码里存在。</span></a>
  <a class="hcdr-card" href="/docs/reference/limitations"><b>限制与安全建议</b><span>技术上做不到的事，以及默认不安全的默认值。</span></a>
  <a class="hcdr-card" href="/docs/reference/cce"><b>Huawei CCE</b><span>CCE 第一阶段的支持范围、注册方式与存储约定。</span></a>
  <a class="hcdr-card" href="/docs/reference/openshift"><b>OpenShift 与 OADP</b><span>OADP 支持范围、兼容边界和发布资产要求。</span></a>
  <a class="hcdr-card" href="/docs/reference/openshift-checklist"><b>OpenShift POC 清单</b><span>迁移验证的逐项检查表。</span></a>
</div>

::: warning 一句话原则

**代码里存在的能力，不等于所有版本、驱动和拓扑都已通过验证。** 正式环境请记录实际平台版本、组件 manifest、集群版本、节点架构、存储驱动，以及完整备份恢复结果。

:::
