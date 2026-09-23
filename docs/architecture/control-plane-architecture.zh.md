# HyperCDR 中控平台架构设计

## 1. 文档范围

本文档描述当前 HyperCDR 中控平台在源码和 Docker Compose 部署中的实际架构，包括运行组件、通信方式、任务派发、数据持久化、集群 Agent 注册以及当前架构边界。

本文档描述的是当前已实现架构，不包含尚未落地的蓝绿部署或消息队列架构。

## 2. 总体架构

HyperCDR 采用中控平台集中管理模式。中控平台提供 Web 页面和 HTTP API，使用 PostgreSQL 保存持久化状态，并通过 WebSocket 与各个 Kubernetes、OpenShift 或 CCE 集群中的 Agent 建立连接。

Agent 在集群内部执行备份、同步、Drill、存储和恢复等操作，并将心跳、资源清单、任务进度及事件上报给中控平台。

## 2.1 技术架构组成

| 层次 | 技术 | 作用 |
|---|---|---|
| Web 前端 | React、TypeScript | 中控平台管理页面 |
| HTTP 后端 | Go `net/http` | REST API、认证、任务编排和调度 |
| WebSocket | Gorilla WebSocket | Agent 注册、任务、进度和事件双向传输 |
| 数据库 | PostgreSQL | 持久化用户、集群、任务、计划、调度、事件和配置 |
| 集群执行 | Kubernetes API | Agent 在 Kubernetes、OpenShift 和 CCE 中执行操作 |
| 备份恢复 | Velero/OADP、CSI 集成 | 备份、恢复、同步和 DR 流程 |
| 部署方式 | Docker Compose | 中控容器生命周期和私有服务网络 |
| 安全通信 | TLS/HTTPS/WSS | 浏览器和 Agent 的安全通信 |
| 登录挑战 | Cloudflare Turnstile（可选） | 登录过程的人机验证 |
| 注册辅助服务 | `cluster-registration-executor` | 受限的集群注册流程执行 |
| 平台生命周期 | `platform-upgrader` | 平台升级和部署生命周期操作 |

后端没有使用 Gin、Echo、Fiber 等 Go Web 框架，而是使用 Go 标准库 `net/http`，并由应用自身实现路由和中间件。

当前平台也没有使用 Redis、RabbitMQ、Kafka、NATS 或其他外部消息队列。任务由 PostgreSQL 持久化，实时派发通过 WebSocket 完成。

```text
                    用户 / 运维人员
                           |
                    HTTPS 外部访问端口
                           |
             +-------------v--------------+
             | 中控前端                   |
             | Web 页面 / TLS 终止        |
             +-------------+--------------+
                           |
                    内部 HTTP API
                           |
             +-------------v--------------+
             | 中控 API                   |
             | REST、调度、认证、任务编排 |
             | WebSocket 连接管理         |
             +------+------+--------------+
                    |      |
             SQL    |      | 内部 HTTP
                    |      +--> 集群注册执行器
             +------v---+  +--> 平台升级器
             | PostgreSQL|
             | 持久化状态 |
             +-----------+

             WebSocket over TLS
             +----------------------------------------+
             | 集群 Agent                               |
             | 任务执行、资源清单、DR 事件上报         |
             +----------------------------------------+
```

## 3. 运行组件

### 3.1 中控前端

容器：`hypercdr-platform-frontend`

中控前端通过私有 Docker 网络提供 HTTP 服务。在生产蓝绿 Compose 拓扑中，NPM 终止公网 TLS，并通过 HTTPS 转发到 HyperCDR edge：宿主机端口 `12443` 映射到 edge 容器端口 `443`；edge 再通过私有网络以 HTTP 转发到前端和 API。前端容器本身不发布宿主机端口。

前端负责展示集群、应用、存储、计划、任务、备份、同步和 Drill 等功能，并通过 API 与中控后端交互。

### 3.2 中控 API

容器：`hypercdr-platform-api`

中控 API 是系统的核心服务，主要负责：

- 用户认证和会话管理；
- 集群注册、注销和 Agent 凭证管理；
- 集群、应用、存储资源和 DR 配置管理；
- 备份、同步、Drill 和恢复任务编排；
- 任务状态、进度和事件处理；
- Agent WebSocket 注册及连接管理；
- Agent 心跳、资源清单和事件接收；
- 定时计划执行；
- 注册 token 校验；
- 发布信息和平台配置集成；
- Cloudflare Turnstile 验证（启用时）。

API 在容器内部监听 `18080` 端口，通过内部 Compose 网络访问 PostgreSQL、注册执行器和升级器。

### 3.3 PostgreSQL

容器：`hypercdr-postgres`

PostgreSQL 是中控平台的持久化数据源，保存：

- 用户和认证信息；
- 集群、Agent 和凭证信息；
- 任务及任务事件；
- 计划和调度配置；
- 存储资源和 DR 配置；
- 迁移、恢复及相关工作流记录。

任务状态以 PostgreSQL 中的记录为准，通常会经历 `queued`、`dispatched`、进度更新以及 `succeeded` 或 `failed` 等状态。

当前系统不使用 Redis，也没有 RabbitMQ、Kafka、NATS 等外部消息队列。PostgreSQL 负责任务的可靠持久化，WebSocket 负责实时传输。

### 3.4 集群注册执行器

容器：`hypercdr-cluster-registration-executor`

该组件是集群注册流程使用的受限辅助服务，不是通用消息队列。它通过内部 HTTP 接口与 API 通信，并使用注册会话卷保存必要的会话数据。

该容器采用只读文件系统、删除 Linux capabilities、限制临时文件系统和禁止外部暴露端口等安全措施。

### 3.5 平台升级器

容器：`hypercdr-platform-upgrader`

升级器负责中控平台的升级操作，使用部署目录和 Docker socket 执行容器编排变更。它属于平台运维辅助组件，不参与日常 Agent 任务派发。

### 3.6 集群 Agent

Agent 部署在被管理的 Kubernetes、OpenShift 或 CCE 集群中，使用集群 API 执行 Velero/OADP 和 DR 操作。

Agent 主动向中控平台建立 WebSocket 连接，完成以下工作：

- 使用安装 token 或已有 Agent 凭证注册；
- 发送心跳；
- 上报节点、Namespace、应用和存储资源清单；
- 接收中控平台派发的任务；
- 上报任务进度、日志和事件；
- 执行备份、同步、Drill、恢复和清理操作。

## 4. 通信路径

| 通信路径 | 协议 | 作用 |
|---|---|---|
| 浏览器 -> 中控前端 | HTTPS | 页面访问和用户登录 |
| 前端 -> 中控 API | HTTP/API | 页面数据查询和操作请求 |
| 中控 API -> PostgreSQL | PostgreSQL 协议 | 持久化状态和事务处理 |
| 中控 API -> 注册执行器 | 内部 HTTP | 集群注册辅助操作 |
| 中控 API -> 平台升级器 | 内部 HTTP | 平台升级操作 |
| Agent -> 中控 API | WebSocket over TLS | 注册、心跳、任务、进度和事件 |
| Agent -> 集群 API | Kubernetes API | 集群侧实际执行 |

正常任务派发不需要中控平台主动连接集群，Agent 会主动维持到中控平台的出站 WebSocket 连接。

## 5. 集群注册和连接生命周期

1. 操作人员从中控页面获取集群注册命令。
2. Agent 安装脚本使用真实的中控平台地址和端口校验 token。
3. Agent 连接配置的 WebSocket 地址。
4. Agent 发送 `agent.register` 消息。
5. API 校验安装 token 或 Agent 凭证，并创建或更新集群记录。
6. API 返回注册确认和 Agent 凭证。
7. Agent 开始发送心跳、资源清单和事件。
8. Agent 重连后，API 从 PostgreSQL 查询 `queued` 或 `dispatched` 状态的任务并尝试重新派发。

中控平台的内部地址、外部访问地址、公开地址和 Agent WebSocket 地址都应从部署配置生成，不能假设固定使用默认端口。用户安装时使用自定义端口，注册命令也必须使用该端口。

## 6. 任务执行模型

任务创建和任务派发是两个独立步骤：

1. API 或调度器在 PostgreSQL 中创建持久化任务。
2. 任务初始状态为 `queued`。
3. API 根据目标集群找到对应的 Agent WebSocket 连接。
4. API 通过 WebSocket 发送任务，并将任务更新为 `dispatched`。
5. Agent 使用任务 ID 上报进度和任务事件。
6. API 更新数据库中的任务记录，并向页面提供结果。
7. 已完成或失败的终态任务不再重新派发。

如果 Agent 断线后重新连接，API 会从 PostgreSQL 查询未完成任务并尝试重新派发。该机制不依赖 Redis，但要求任务执行具备幂等性，并正确处理重复派发和状态转换。

## 7. 定时调度模型

计划配置保存在中控平台，由中控平台调度器负责执行。定时计划触发后会生成普通的一次性任务；集群侧不会自行创建中控任务。因此，定期备份和定期 DR 操作的控制权属于中控平台。

## 8. 数据持久化和恢复

系统持久化数据主要分为以下几类：

- PostgreSQL 数据：用户、配置、集群身份、任务、事件和工作流记录；
- 安装部署目录：Compose 文件、生成的环境配置、TLS 文件和部署元数据；
- 注册会话卷：集群注册过程使用的会话数据；
- 集群侧资源：Agent、Secret、Velero/OADP 资源以及被管理的应用数据。

恢复中控平台时，需要保证数据库状态和部署配置相互匹配。只恢复容器而不恢复 PostgreSQL，无法恢复任务记录和集群身份。

## 9. 安全边界

- 页面和 API 通过 TLS 对外提供服务；
- 用户认证由中控 API 负责，登录挑战可使用 Cloudflare Turnstile；
- 注册 token 用于初始注册，长期通信使用签发的 Agent 凭证；
- 注册执行器和升级器使用独立的内部接口和凭证；
- PostgreSQL 和内部辅助服务不应直接暴露给用户；
- 注册执行器使用只读文件系统、受限 capabilities 和无外部端口配置。

## 10. 当前架构边界

当前架构适合单实例中控平台和中等规模集群管理，主要边界包括：

- WebSocket 连接归属于单个 API 进程；
- PostgreSQL 同时承担任务状态存储和待派发任务查询，高并发时可能产生数据库压力；
- 没有用于跨中控实例路由任务的分布式消息代理；
- 高频进度上报会增加数据库写入和 WebSocket 流量；
- 多实例部署需要额外实现连接路由、任务租约、分布式锁和幂等派发。

## 11. 大规模集群演进建议

如果未来需要管理大量集群和高并发任务，建议按以下阶段演进：

1. 先完善任务幂等、租约、超时、重试和死信状态；
2. 增加 Outbox 表和独立 Dispatcher，保证数据库任务和待发送事件一致；
3. 在规模达到瓶颈后引入 NATS JetStream 或 RabbitMQ 等持久化消息系统；
4. 将 WebSocket 网关、调度器和任务 Worker 独立部署并水平扩展；
5. 对任务进度进行合并、采样和异步归档，降低数据库写入压力。

消息队列只负责任务和事件的实时传输、削峰及解耦，PostgreSQL 仍应作为任务最终状态的权威数据源，不建议直接使用 Redis 替代任务数据库。

## 12. Docker Compose 部署组件

标准 Docker Compose 部署包含：

- `hypercdr-postgres`
- `hypercdr-platform-api`
- `hypercdr-platform-frontend`
- `hypercdr-platform-upgrader`
- `hypercdr-cluster-registration-executor`

生产 edge 通过宿主机 `12443` 端口接收 NPM 的 HTTPS 流量。前端、API、PostgreSQL、升级器和注册执行器通过私有 Compose 网络通信；只有 edge 接入代理网络。

## 13. 主要源码和文档参考

- `docker-compose.yml`
- `backend/internal/httpserver/agent_connection.go`
- `backend/internal/httpserver/task_dispatch.go`
- `backend/internal/store/postgres.go`
- `docs/protocols/platform-agent-messages.md`
- `docs/protocols/dr-task-state-machine.md`
- `docs/agent/agent-design.md`
