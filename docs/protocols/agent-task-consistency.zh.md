# Comm Agent 通信与任务一致性规范

状态：目标设计及代码审计，尚未完成协议改造或部署。2026-10-26。

消息分类、字段示例与按操作的结果契约见
[Agent—中控通信协议 V2 方案](agent-platform-protocol-v2-design.zh.md)。

本规范统一任务控制、执行结果、资源观测与恢复点发布的职责。与
`dr-task-state-machine.zh.md` 中备份创建入口、定时任务来源描述冲突时，
后续改造以本规范为目标；不能据此认定现网已具备这些保证。

## 1. 数据来源与职责

集群侧执行事实均经已认证的 Comm Agent 连接上报。平台另有用户配置、
策略计算、调度、配额、业务状态及审计数据，不能统称为集群上报数据。

只有一种集群执行通道，不等于只能有一种消息类型。允许多类型消息，
但必须只有一个任务状态归约器和一个恢复点发布器。

| 类别 | 负责方 | 权限边界 |
| --- | --- | --- |
| 配置、调度、任务身份、重试授权 | 中控 | 创建任务/执行尝试、持有配额预留 |
| 执行、观测、完整执行结果 | Comm Agent | 报告事实，不直接决定平台业务成功 |
| Backup 等底层资源状态 | Velero，经 Agent 转发 | 是执行证据，不是平台业务终态 |
| 清单、心跳、日志 | Comm Agent | 资源对账、存活、诊断，不直接发布恢复点 |
| 结果验收、恢复点发布、业务终态 | 中控统一协调器 | 验证结果及配额，原子发布并完成任务 |

## 2. 已确认的问题

1. `agent.task.completed`、`agent.velero.event`、inventory 三条处理路径
   都能调用 `CreateRestorePoint`，不是统一发布入口。
2. TaskCompleted 有结构化 `SizeMetricsV2`；VeleroEvent 没有对应字段，
   仅有通用 `velero` 字典。部分创建函数漏传已有任务指标。
3. 清单入口能补建任务并结束任务；它承担了超出资源观测的业务决策。
4. 多路径能覆盖待发布的 `pending_point`，完成结果没有统一不可变快照。
5. `UpdateTaskStatus` 通过字符串和 `completed_at` 保护终态，
   不是按任务类型、执行尝试与允许迁移表校验全部状态转换。
6. 现有 TaskCompleted 处理先按集群找任务；VeleroEvent 另有 commandId、
   planId、资源标签等检查。不同消息的关联校验没有统一。
7. Agent 已有本地 outbox、重发和任务 ledger，不是完全没有可靠性机制；
   但未发现服务端统一的持久化 messageId inbox/回执表，不能声称已实现
   跨消息类型、重启后的一致去重。现有业务唯一约束不等于消息消费去重。
8. 协议 Go 类型分布在平台和 Agent 两处；通用字典及可选字段较多。
   当前接收循环未统一校验完整 envelope 和任务操作所需字段。
9. 旧文档允许 Velero Schedule 事件创建任务；当前已有平台定时调度，
   且事件处理要求匹配既有任务。文档与实现需要同步。

已核实的故障实例：备份任务 `8bd7bd3e-0f71-4722-a982-ed5ebf11802d`
在北京时间 15:40:45 已持有完整指标并进入置换等待；15:41:40 清单同步
经未传指标的创建路径发布 `7772ea23-a5f1-41f0-8ab1-17d54ad1a436`
（任务事件原因为 `velero-schedule`，对应 inventory 分支）。
该点的任务指标为 New data 23,988 字节，而恢复点指标为空。
这是发布输入遗漏和多入口竞争，不是该任务没有上报指标。

## 3. 单一通信处理流水线

```text
已认证 WebSocket
  -> envelope/schema/身份/任务关联校验
  -> 持久化 inbox（消息内容及处理状态）
  -> 统一任务归约器（按计划、任务和尝试串行）
  -> 持久化执行结果、观测事实、后续协调意图
  -> 提交事务并确认消息持久化
  -> 统一协调器处理配额置换、恢复点发布及业务完成
```

WebSocket handler 只做解析与调用应用服务，禁止直接创建恢复点、
直接判定备份成功或另写一套保留/配额规则。历史导入也须经独立授权流程，
不能借 inventory 自动绕过这些规则。

## 4. 消息职责契约

沿用现有名称时也必须明确以下语义，不因重命名增加迁移风险。

| 消息 | 必需内容 | 允许的业务影响 |
| --- | --- | --- |
| platform.task.dispatch | taskId、commandId、attemptId、operation、输入摘要 | 授权一次执行尝试 |
| agent.task.accepted | 同一身份组、已持久化执行意图 | 进入 accepted，不代表执行成功 |
| agent.task.progress | 身份组、sequence、阶段及进度 | 更新非终态进度，不覆盖结果 |
| agent.task.completed / failed | 身份组、不可变 result、resultId/摘要 | 验收执行结果，驱动协调器 |
| agent.velero.event | task/attempt、资源 UID、resourceVersion、观测时间 | 记录底层事实或触发结果核查 |
| agent.inventory.report | 快照版本、范围、观测时间、完整性标记 | 资源对账、触发补报请求 |
| heartbeat / logs | 会话身份、观测时间、诊断内容 | 更新存活或诊断，不结束业务任务 |

平台定时和用户手动操作都先创建 task/attempt，再下发给 Agent。
自主出现且没有平台任务身份的 Backup 标记为未关联观测，不自动生成成功任务。
如需接管旧 Velero Schedule 或历史备份，提供显式接管/导入操作及审计。

Agent 重启后应结合 ledger 与资源标签恢复原 task/attempt，重新构建并上报
同一标准结果。中控不能因为 inventory 中某对象为 Completed 就绕过结果验收。

## 5. 身份、版本及顺序

- tenantId、clusterId、agentId 以认证会话及数据库归属为准，消息自报值
  不得授予权限。逐条校验任务、计划、执行集群、command、attempt、资源身份。
- taskId 为业务操作；attemptId 为一次授权尝试；commandId 为该尝试的
  执行命令；messageId 为一条逻辑消息。重传不生成新 ID，重试不复用旧尝试。
- 每个尝试提供 fencing token，旧 Agent/旧尝试不能影响新尝试。
  Agent 侧也须执行隔离，不能只在服务端丢弃迟到消息。
- sequence 在同一尝试内单调，状态不按消息时间戳排序；跨通道到达顺序
  不作为业务依据。资源版本仅用于相应对象，不能跨资源比较其数值。
- 过期进度不使任务倒退；迟到结果须留存并分类，不能误归到新尝试。
- 由单一版本化 schema 定义两端 DTO、必填及条件必填字段，并在 CI
  做生成结果及双向兼容测试。能力协商与 envelope 版本分开管理。
- v1 兼容由唯一适配器转换为内部标准消息；旧 Agent 缺少的字段不能
  凭空生成成功证据。未满足能力时明确报告兼容/结果待确认状态。

## 6. 执行状态与业务状态分离

执行尝试状态：`created -> dispatched -> accepted -> running ->
execution_succeeded | execution_failed | canceled`。

备份业务阶段：`queued -> syncing -> collecting_result -> finalizing ->
waiting_old_point_deletion -> publishing -> succeeded`。
配额有空位时跳过删除阶段。具体阶段名称可映射现有 UI，无需照搬为所有任务的状态。

- Agent 执行成功不等于平台任务成功。只有对应恢复点原子发布完成，
  业务任务才能成为 succeeded；恢复/Drill 还须满足各自就绪与清理契约。
- 使用显式迁移表及条件更新；同一事务记录状态、原因、结果引用及后续动作。
  不接受任意字符串直接改写任务状态。
- 业务终态不可被进度或迟到观测复活；审计信息可追加，结果冲突须显式记录。
- 同计划新同步准入检查覆盖排队、执行、结果采集、清理与发布全过程。
  后端计划行锁内检查，不仅依赖前端和 handler 的预查询。
- 超时/断连先核查执行尝试；失联不等于失败。停止执行的确认、重试授权
  与 fencing 协同，禁止旧尝试尚未隔离就启动可能冲突的新同步。

## 7. 恢复点发布门槛

统一发布器只接受已验收的不可变 `BackupExecutionResult`：

- taskId/attemptId、源集群、计划及仓库归属一致。
- Backup 名称、命名空间、UID 与本次尝试对应，具有终态及数据可恢复证据。
- 恢复点使用的 logical/newData/reuse 字段属于同一结果版本。
  原始观测字典不能直接覆盖已验收结果或待发布快照。
- 指标包含 schemaVersion、known/availability、单位、口径、采集来源及时间。
  **已知 0、尚未采集、采集失败、不适用必须分开表达**，禁止用 null、
  空对象或默认 0 冒充完整结果。
- 新 Agent 的必需指标未就绪时处于 collecting_result，执行明确的重试和期限。
  若允许降级发布，须是显式策略：保存 unavailable 原因、审计及 UI 提示，
  而不是静默 available 且 New data 为空。默认不静默降级。
- 在同一计划锁内确认配额、保留规则及预留归属；满配额先取得明确删除结果，
  不能把“下发删除”或“不确定的查询缺失”当作删除成功。
- 原子提交恢复点、任务恢复点关联、预留 published、业务 succeeded 与事件。
  后续通知用持久化 outbox 驱动；索引等非关键工作不阻断发布事务。
- 唯一键至少包括租户、源集群及底层资源身份；兼容既有名字唯一键，
  明确同名新 UID 的冲突策略，不能静默覆盖另一次备份。

## 8. ACK、幂等与故障恢复

- 保证至少一次投递及业务幂等，不宣称网络 exactly-once。
- Agent 在执行前持久化意图；关键事件先写可靠 outbox，再发送。
  必须验证挂载可跨 Pod 重建保留，不能仅有容器文件或 emptyDir。
- inbox 唯一键包含认证集群和 messageId，保存 payload 摘要、处理结果及回执。
  同 ID 同内容重发返回原回执；同 ID 不同内容拒绝并审计。
- `persisted=true` 只表示平台可靠接收并承担恢复处理责任，不表示业务成功。
  只有 inbox 和处理意图提交后可 ACK；崩溃后由可恢复 worker 继续处理。
- 重试型错误保持原消息；不可重试错误由 Agent 记录拒绝原因与告警，
  不得静默丢弃一个尚未被平台验收的完成结果。
- 周期 reconcile、重连和下一次同步准入都调用同一恢复规则。
  失败/取消且未开始删除的旧预留可释放；已开始删除则先核实结果再继续。
- 管理面提供 pending result、冲突、重试次数、最后失败原因及人工处理入口。
  修复数据操作须单独授权及审计，不作为常规链路的一环。

## 9. 实施顺序与验收

### P0：收敛发布责任

先覆盖本次实际时序的回归：完整结果先到、置换等待、清单同步后到；
同时覆盖 Velero 完成事件后到的相邻风险路径。
将现有三入口改为标准事实输入并经唯一发布器处理；禁止 handler 直接创建。
冻结已验收结果，杜绝 pending_point 被弱观测覆盖。上线前保留现有故障记录，
不做回填迁移证明修复有效；以新备份在创建事务内指标完整为验收依据。

### P1：可靠消息及严格状态机

落地 inbox/outbox、版本化结果、统一关联校验、迁移表、attempt/fencing
及持久化协调动作，补齐可观测性。schema 及协议字段变更均要求测试及文档。

### P2：兼容灰度及现场验证

先上兼容中控，再升级单个验证集群的 Agent，确认后扩展。不得自动变更
47 或其他集群。未协商能力的 Agent 不直接启用严格必填字段。

验收至少覆盖：三类消息全部排列、重复投递、旧进度覆盖终态、旧 attempt
迟到、结果冲突、同 ID 不同内容、跨集群/租户伪造、入库后 ACK 前崩溃、
Agent/平台重启、指标延迟或失败、已知零值、配额满/保留满、删除失败或取消、
发布中断、手动/定时并发和旧 Agent 兼容。每种时序只能有一个业务结果、
一个恢复点、一个配额归属，不能靠人工 SQL 或历史回填通过验收。

## 10. 审计定位

- `backend/internal/httpserver/agent_messages.go`：消息分发及完成处理。
- `backend/internal/httpserver/backup_events.go`：事件与清单创建路径。
- `backend/internal/httpserver/restore_point_replacement.go`：延迟置换发布。
- `backend/internal/store/postgres_tasks.go`：状态写入及锁释放。
- `backend/internal/store/postgres_restore_points.go`：恢复点 upsert。
- `backend/internal/protocol/messages.go`、`agent/comm-agent/pkg/protocol/messages.go`：两端 DTO。
- `agent/comm-agent/internal/wsclient/client.go`、`outbox.go`：执行上报、观测上报及可靠发送。
