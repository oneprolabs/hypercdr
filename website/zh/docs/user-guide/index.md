---
title: 用户指南
description: 首次使用 HyperCDR 的完整流程，以及账号、组织与权限的规则。
---

# 用户指南

本板块假设平台已经安装完成，并且有一个可登录的地址和一个待保护的集群。目标是完成一次完整流程：**注册集群 → 准备存储和策略 → 配置保护 → 备份成功 → 演练验证恢复**。

## 阅读顺序

<div class="hcdr-grid">
  <a class="hcdr-card" href="/docs/user-guide/first-use"><b>1. 首次使用</b><span>集群注册到恢复验证的五个阶段，以及每个阶段的验收标准。</span></a>
  <a class="hcdr-card" href="/docs/user-guide/users-and-organizations"><b>2. 用户、组织与权限</b><span>账号归属、组织隔离、平台管理员边界和绑定邮箱规则。</span></a>
</div>

## 开始之前

- 平台地址可访问，管理员密码已修改（初始为 `admin` / `admin123`）。
- 准备一个**测试 Namespace**，仅用于验证，不要直接在业务环境中操作。
- 准备好 kubeconfig，并确认它对应的账号有创建资源的权限。
- 准备 S3 兼容的对象存储地址、bucket 和凭据。

## 实施建议

先在较小范围内验证，再扩大保护范围。在测试 Namespace 上完成包含恢复的完整流程后，再接入生产应用。恢复能力未经验证时，保护配置的数量没有实际意义。

## 尚未安装平台

从[安装部署](/docs/deployment/)开始。
