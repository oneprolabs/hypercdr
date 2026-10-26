# Agent—中控通信协议 V2 方案

状态：设计建议，未实现、未部署。审计日期：2026-10-26。
本方案基于现有 Comm Agent、WebSocket、平台调度、Velero 执行、
配额置换、Drill/恢复及组件升级业务，不要求引入 Kafka 或改为 gRPC。
已有实现缺口和证据见 [一致性审计](agent-task-consistency.zh.md)。

## 1. 设计结论

保留认证的双向 WebSocket；将消息按语义分为六类：会话、命令、
执行事件、观测、查询响应、交付回执。展示不是独立的业务状态来源。
所有输入经过统一接收和验证，再分别写事实、结果、读模型，
只有工作流协调器可以改变平台业务状态或发布恢复点。

采用控制面命令/查询分离、显式工作流状态机、持久化 inbox/outbox、
幂等协调器及 UI 投影。不是全量 event sourcing：关系数据库仍是当前状态
与结果的持久化基础，事件日志保留审计和恢复所需事实。

## 2. 当前消息评估

| 现有消息/字段 | 合理部分 | 需改进之处 |
| --- | --- | --- |
| register / register.accepted | 建立集群身份及能力协商 | 把协议能力、Agent 版本、会话身份分开管理 |
| heartbeat | 心跳及组件摘要必要 | 不依赖 ActiveTasks 数量判断某任务结束；组件状态需带观测新鲜度 |
| task.dispatch | 已包含分操作命令结构 | 明确 oneOf，禁止 operation 与输入不匹配；补 attempt/fence |
| task.accepted | 区分接单与执行正确 | 明确已持久化命令、拒绝原因及重复命令回复 |
| task.progress | 进度采样必要 | status/progress/percent/字节及 Velero 字典重复；混入阶段与展示事件 |
| task.completed | 有独立完成消息及结构化指标 | 与底层 Completed、业务 succeeded 混淆；结果过度可选 |
| task.failed | 有错误码与 retryable | retryable 未区分消息重传、查询重试及业务重新执行 |
| velero.event | 底层对象观测有诊断价值 | 应为 observation，不是第二套任务结果和恢复点发布接口 |
| inventory.report | 全量/局部清单及 hash 有用 | 同时混合主动上报和查询响应；不能补建成功任务或宣布备份成功 |
| logs / backup-content | 按需查询合理 | 保持查询语义，补分页、期限、错误结构及大小限制 |
| event.ack / event.error | 已存在可靠发送基础 | 限定交付语义；不能用任意 ACK 证明任务终态已验收 |
| message / map[string]any | 便于诊断和扩展 | 不能成为业务判断或前端解析规则；关键字段结构化并校验 |

特别风险：当前 Agent 收到 `platform.event.ack` 后调用
`ledger.markTerminalAcked(taskId)`，未在该分支按被确认的事件类型区分。
Velero 终态观测也使用可靠发送并携带 taskId，因此需审查并改为仅确认
精确 resultId/attempt 的执行结果，不能把观测消息 ACK 等同于结果 ACK。
这属于本次审计发现的风险，不据此断言已导致现场某次丢失。

## 3. 领域身份

| 标识 | 生命周期与含义 |
| --- | --- |
| tenantId / clusterId | 认证后由中控绑定；以会话及数据库归属为准 |
| sessionId / agentInstanceId | 一次连接及 Agent 进程实例，用于审计，不替代任务身份 |
| taskId | 用户可见的一次业务操作，备份、Drill、恢复等 |
| attemptId | 一次授权执行尝试，重试新建，重连继续使用原值 |
| commandId | 一条业务命令，重复下发相同 ID/内容；取消命令有自己的 ID |
| messageId | 一条逻辑通信消息，重传保持 ID 和内容不变 |
| resultId | 某次尝试的不可变最终执行结果，不能被观测消息确认或覆盖 |
| requestId | 查询调用关联，不混用 taskId |
| resourceRef | 集群、GVR、namespace、name、UID，标识底层资源 |

同一 attempt 内 result 只能有一个一致的终态；不同 resultId 若冲突，
记录协议冲突，不按到达顺序选一个。新 attempt 必须完成旧执行隔离，
涉及同一资源的操作需 Agent/底层共同校验 fencing token，不能只在中控丢消息。

## 4. 消息目录与处理权限

名称是 V2 目标命名，不要求 V1 原地重命名。V1 经适配器进入同一内部模型。

| 类别 | 目标消息 | 通信语义 | 持久化/重发 | 是否直接改变业务结果 |
| --- | --- | --- | --- | --- |
| 会话 | session.open / ready | 身份、能力、会话参数 | 握手超时重连 | 否 |
| 会话 | session.heartbeat | 存活及最近上报摘要 | 可覆盖，不逐条可靠投递 | 否 |
| 命令 | execution.start | 执行备份/恢复/清理/升级等 | 两端持久化、幂等重发 | 仅授权执行 |
| 命令 | execution.cancel | 请求停止指定 attempt | 可靠、独立 commandId | 否，等待取消结果 |
| 执行事件 | execution.accepted / rejected | 接单或拒绝 | 可靠 | 只影响执行准入 |
| 执行事件 | execution.phase.changed | 离散阶段迁移 | 可靠、少量 | 按允许迁移驱动工作流 |
| 执行事件 | execution.progress | 当前数值采样 | 可合并、可丢旧值 | 否 |
| 执行事件 | execution.notice | 结构化诊断事件，如拉镜像/等待卷 | 有限保留，关键错误可靠 | 不单独决定成功失败 |
| 执行事件 | execution.result | 成功、失败、取消的最终执行结果 | 可靠且不可变 | 验收后驱动协调器 |
| 观测 | observation.resource | Velero、Pod、卷等底层变化 | 普通采样可合并；重要事实可靠 | 不能自行结束任务 |
| 观测 | inventory.snapshot / delta | 资源现状与增量 | 版本化对账/重取全量 | 否 |
| 查询 | query.request / response | 日志、备份内容、执行状态/结果查询 | 有期限，调用方可重试 | 不产生执行副作用 |
| 回执 | delivery.receipt | 某条消息已持久化或被拒绝 | 原消息重传时重返原回执 | 否 |

组件升级的完成需中控结合新会话、版本/digest 观测做后置验证；
观测可作为协调器的结构化验证条件，但仍不能在消息 handler 中直接改成功。

## 5. Envelope 与传输规则

```json
{
  "protocolVersion": "2.0",
  "messageId": "msg-123",
  "kind": "event",
  "type": "execution.result",
  "sentAt": "2026-10-26T08:00:00Z",
  "scope": {"clusterId": "cluster-131", "agentInstanceId": "agent-instance"},
  "context": {"taskId": "task-1", "attemptId": "attempt-1", "commandId": "cmd-1"},
  "payload": {}
}
```

- kind 仅为 `command/event/observation/query/reply/receipt`；session 属于
  控制消息族，按具体请求/应答使用 query/reply 或 event，不增加多余封装层。
- schema 明确每个 type 允许的 kind、方向、上下文、必填和条件必填字段。
  task 上下文不重复散落在每个 payload；observations 的任务关联可选，
  未关联事实只入资源视图，不能据此创建执行结果。
- 可靠性是按消息类型及协商策略固定的，发送方不能随意设 ackRequired=false
  使完成结果退化为不可靠消息。V1 的 ackRequired 由适配器检查。
- schema 拒绝冲突字段、错误类型及未知枚举；扩展字段仅允许在限定命名空间，
  接收端可以保留诊断扩展，但不得依赖未知扩展做业务决定。
- 不支持的主版本拒绝；次版本只允许协商范围内的兼容扩展。
  不能把 Agent 软件版本与协议兼容性直接等同。
- 统一规定消息上限、批次上限、超时、压缩及慢消费者策略，值经压测确定。
  大清单/日志分页或分块，不能阻塞执行结果及取消命令。
- 单连接先用优先队列：控制/结果高优先、进度低优先；限流、合并旧采样。
  监测结果 ACK 延迟及队列深度，证据表明确有瓶颈再拆流或更换传输。

## 6. 结果、进度、阶段、通知必须分开

### 6.1 Progress：数值采样

```json
{
  "sampleSequence": 27,
  "phaseCode": "backup.transferring",
  "observedAt": "2026-10-26T08:00:00Z",
  "transfer": {
    "processedBytes": 1048576,
    "totalBytes": 4194304,
    "totalAvailability": "known",
    "bytesPerSecond": 262144
  }
}
```

一套数值口径，不同时发送多套含义不清的 progress/percent。
百分比由 processed/total 推导，未知总量不伪造百分比；有多阶段权重时由
中控工作流定义整体百分比。新阶段的计数可以从零开始，不能用整个任务的
`greatest(progress)` 掩盖阶段变化。采样丢失不影响业务正确性。

### 6.2 Phase：执行阶段事实

例如 `backup.preparing/transferring/collecting_result` 与
`restore.applying_resources/restoring_data/waiting_workloads/validating`。
每阶段的进入、完成、失败是离散事件。平台的 `waiting_quota_cleanup`
等业务阶段由平台产生，不强迫 Agent 假装在执行平台侧动作。

### 6.3 Notice：用于时间线的结构化事件

```json
{
  "eventId": "notice-1",
  "code": "workload.image_pull_started",
  "severity": "info",
  "phaseCode": "restore.waiting_workloads",
  "resource": {"kind": "Pod", "namespace": "demo", "name": "web-1", "uid": "pod-uid"},
  "attributes": {"container": "web", "image": "registry.example/web:v1"},
  "observedAt": "2026-10-26T08:00:00Z"
}
```

UI 根据 code 和 attributes 本地化成“一条一条”的文案。多个镜像用
资源 UID、容器、镜像及事件码区分，重复观测合并计数/最近发生时间。
ImagePullBackOff 是诊断事实，不直接等于 Drill 失败；是否失败由就绪
条件、超时和执行结果决定。message 只作诊断回退，不被程序解析为状态。

### 6.4 Result：不可变执行结果

```json
{
  "resultId": "result-1",
  "outcome": "succeeded",
  "operation": "backup",
  "startedAt": "2026-10-26T07:58:00Z",
  "finishedAt": "2026-10-26T08:00:00Z",
  "backup": {
    "artifact": {"name": "backup-1", "namespace": "velero", "uid": "backup-uid"},
    "storageRepositoryId": "repository-1",
    "recoverability": "verified",
    "sizeMetrics": {
      "schemaVersion": 2,
      "logical": {"bytes": 930196244, "availability": "known"},
      "newData": {"bytes": 36414, "availability": "known"},
      "measurementSource": "velero-kopia",
      "measuredAt": "2026-10-26T08:00:00Z"
    }
  }
}
```

这只是字段示例，不替代 JSON Schema。recoverability 的 verified 必须
对应明确证据与检查范围，不宣称已通过应用恢复演练。
结果按 operation 使用 oneOf：backup、restore、cleanup、upgrade 等，
不能任意混合。reuse 从同口径数据派生，避免双方独立发送矛盾结果。

指标 availability 统一为 known/unavailable/not_applicable；pending
属于结果采集过程，不包装成最终完整结果。known 必须有数值，允许 0；
unavailable 必须有 reasonCode，不填伪造数值。新版本默认先重试采集；
若业务允许缺指标降级，必须有明确策略、质量状态和界面提示。

## 7. 状态模型及操作结果契约

执行尝试：`created -> accepted -> running -> succeeded/failed/canceled`；
dispatched 作为命令投递状态另存，不等同于 Agent 已接单。

平台工作流：`queued -> executing -> finalizing -> succeeded/failed/canceled`，
细阶段独立保存。执行成功可以与业务 finalizing 同时存在。
失败结果按重试策略产生新 attempt，不让旧 attempt 从 failed 复活。

| 操作 | Agent 必须提供的执行事实 | 平台成功门槛 |
| --- | --- | --- |
| backup | 备份资源身份、存储归属、完成及结果质量、指标 | 数据可恢复检查符合策略、配额置换完成、新点原子发布 |
| restore / Drill / takeover | 恢复资源、目标命名空间、卷结果、工作负载就绪与验证结果 | 对应业务要求的全部阶段满足，不只看 Velero Restore Completed |
| cleanup | 每个目标的删除结果及资源身份，部分失败独立记录 | 预期对象已删除或有可靠的原本不存在证据，才释放对应配额 |
| storage-sync | 配置应用及连通/可用验证结果 | 目标仓库在集群上的可用性满足要求 |
| upgrade | 已发起变更、目标版本/digest、执行结果 | 新 Agent/组件重新观测验证通过，不能仅凭旧进程的完成消息 |
| cancel | 停止结果、目标 attempt、是否已进入不可取消阶段 | 停止被确认且副作用归属明确，才能结束并释放可释放的锁 |

取消命令是请求，不是取消成功；指令投递超时不是执行失败。
现有 `platform.task.cancel` 返回不支持，而另有 backup-cancel 业务命令；
迁移时统一取消语义及能力协商，不能保留两个含义不一致的承诺。

## 8. 接收、协调与发布

```text
消息 -> 认证/schema/归属校验 -> inbox
     -> ExecutionService：接单、阶段、不可变结果
     -> ObservationService：底层事实和清单
     -> TelemetryProjection：进度与通知
     -> QueryService：查询响应

WorkflowCoordinator：读取已验收结果与验证条件
     -> 配额/保留/删除等步骤
     -> RestorePointPublisher
     -> 业务状态与 UI 读模型
```

只有协调器拥有业务终态写权限，只有发布器拥有恢复点发布权限。
HTTP/WebSocket handler、inventory ingester、Velero observer 不直接调用发布。
通过内部接口/依赖边界及测试约束，而不是只写文档约定。

对备份，pending 发布记录只引用 task/attempt/resultId 和配额归属，
不再允许任意消息覆盖一份可变的 CreateRestorePointInput。
在事务内校验预留和结果版本，原子写恢复点、任务关联、预留状态及业务终态。
故障后重复协调得到相同结果，不依靠事后指标回填恢复正确性。

## 9. 消息可靠性与对账

- inbox 对认证集群/messageId 唯一，保存内容摘要及处理意图；同 ID 不同内容
  为协议冲突。重复命令由 Agent ledger 返回既有状态，不能再次产生副作用。
- delivery.receipt 的 received/duplicate 只证明可靠接收，rejected 提供
  rejectionCode。它不能被 UI 当业务成功，也不能标记任意 task 的结果已确认。
- 只有精确匹配 execution.result 的 receipt 能删除该结果 outbox 项并记录
  resultId 已交付；其他事件的回执不能修改结果交付状态。
- 消息交付重试、只读查询重试、业务执行重试用不同策略字段；只有平台
  授权新的 attempt 才允许业务重新执行。
- 心跳带活动 attempt 摘要；重连先交换执行账本摘要，中控按需查询缺失结果。
  未知任务和不一致结果进入对账，不自动构造成功任务。
- inventory 全量采用 snapshotId、scope、observedAt、complete；增量采用
  baseRevision/revision 和显式 tombstone。基线不一致时重取全量。
  部分清单、漏报或短暂查不到不能证明资源已删除。
- 至少一次交付配合业务幂等；不保证网络顺序和 exactly-once。
  同 attempt 阶段与采样分别有序，不能用一条全局序号让高频进度阻塞结果。
- 可恢复 worker 处理已接收消息；在提交前崩溃不 ACK，提交后 ACK 丢失则
  返回原回执。现场重启验证须覆盖 Agent 本地状态卷的实际持久性。

## 10. 展示与信息安全

中控输出前端专用读模型：businessStatus、phase、progress、conditions、
timeline、resultSummary。前端不解析 Velero 字典或英文 message 判定状态。
物理仓库占用、逻辑恢复点大小、新增数据和传输进度是不同指标，明确单位与口径。

控制消息中的仓库凭据按操作最小化下发，不复制到事件、日志和结果中。
对 image/URL、日志及错误进行凭据脱敏；备份内容和日志查询校验租户、
任务/资源范围及访问权限。数据库时间 UTC，UI 经统一时区工具转换。
traceId 可选用于链路追踪，不用于授权、幂等或业务关联。

## 11. 兼容、范围及验收

单一 schema 源生成两端模型和校验器；对每个消息提供正常、非法及兼容样例。
先落地内部标准模型与 V1 adapter，再按消息族启用 V2，最后移除旧创建路径。
旧 Agent 上报不足时显式进入待核查或兼容降级，不伪造新字段。

第一阶段只收敛备份结果/发布、进度/展示、ACK 语义；
第二阶段统一恢复/Drill、清理、升级及取消；
第三阶段收敛 schema、持久化对账及灰度升级。
不得一次性强推新协议到所有集群，也不把新旧 handler 同时直接发布当兼容方案。

验收必须重放所有相关消息排列及重复投递，并注入 ACK 丢失、Agent/平台重启、
旧 attempt 迟到、指标失败、删除部分失败、取消竞争及跨租户伪造。
要求相同事实集归约为相同业务结果；每次授权执行至多一个业务结果/恢复点，
重复消息不额外占配额，观测/进度/文案不能越权改变结果。

监测：结果 ACK 延迟、可靠队列长度/最老年龄、重复率、拒绝率、结果冲突数、
结果待采集时长、置换等待时长及对账积压。阈值按业务 SLA 和压测确定，
不通过无限重试或静默丢消息隐藏问题。
