---
title: 用户指南
description: 第一次使用 HyperCDR 的完整路径，以及账号、组织与权限的规则。
---

# 用户指南

这个板块假设平台已经安装好，你手上有一个可登录的地址和一个待保护的集群。目标是走完一次完整流程：**注册集群 → 准备存储和策略 → 配置保护 → 备份成功 → 演练验证恢复**。

## 阅读顺序

<div class="hcdr-grid">
  <a class="hcdr-card" href="/docs/user-guide/first-use"><b>1. 首次使用</b><span>从集群注册到恢复验证的五个阶段，包含每一步的验收标准。</span></a>
  <a class="hcdr-card" href="/docs/user-guide/users-and-organizations"><b>2. 用户、组织与权限</b><span>账号归属、组织隔离、平台管理员边界和绑定邮箱规则。</span></a>
</div>

## 开始之前

- 平台地址可访问，管理员账号密码已修改（初始为 `admin` / `admin123`）。
- 有一个**测试 namespace** 可以随便折腾，不要直接拿业务环境练手。
- 准备好 kubeconfig，并确认它对应的账号有创建资源的权限。
- 有一个 S3 兼容的对象存储地址、bucket 和凭据。

## 一个提醒

先在小范围跑通，再扩大保护范围。用测试 namespace 验证完整流程（含恢复）之后，再把生产应用接进来。恢复能力没验证过之前，保护配置的数量没有意义。

## 平台还没装？

从[安装部署](/docs/deployment/)开始。
