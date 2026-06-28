---
slugid: maddog-fusion--3949
stage: review
date: 2026-06-28
source_plan: docs/cc/maddog-fusion--3949/plan-external-schemes.md
source_tech: docs/cc/maddog-fusion--3949/tech.md
source_reference: https://github.com/cobusgreyling/loop-engineering
reviewers: [architecture, product-gui, reliability-security-cost, delivery-plan]
---

# loop-engineering 参考方案专家评审收敛

## 结论

`cobusgreyling/loop-engineering` 对 Maddog 有参考价值，但只移植模式，不引入它的 CLI、Grok `/loop` 语义或外部 agent host。它应作为四组增强方案之前的公共底座：Loop template、readiness audit、budget/run log、maker-checker、human gate、kill switch、run report。

补充后的文档已将该底座写入：

- `docs/cc/maddog-fusion--3949/tech.md`：新增“外部方案参考”，明确 loop-engineering 的取舍与安全成本约束。
- `docs/cc/maddog-fusion--3949/plan-external-schemes.md`：新增 L0-L4 实现单元，作为 D/E/F/G 四组方案的前置公共层。

## 专家组意见

### 架构组

- 必须定义 Maddog 自有 `LoopTemplateV1`，不能直接搬 `patterns/registry.yaml`。
- Readiness audit 必须是 pre-run gate，输出到 event/wire/GUI。
- Budget/run log 需要持久化契约、存储目录、retention、脱敏规则和 GUI 查询接口。
- Maker-checker 是执行模式，不等同于 advisor；v1 先做 read-only checker gate。
- Provider/model GUI 必须补 maker/checker/default/small/frontier/advisor 角色映射，且只做 `ProviderEntry` projection，不新增第二套 provider store。

### 产品 / GUI 组

- GUI 必须形成 Loop Control Surface：Workflow Templates、Run Readiness、Budget & Cost、Maker-Checker、Live Run、Run Reports。
- Settings 必须能配置和显示 default/small/frontier/advisor provider、auth mode、base URL、official/API key/icodeeasy 状态和连接测试。
- Live Run 必须显示 template、phase、agent role、当前模型、frontier upgrade、readiness warnings、budget meter、maker-checker gate 和 human approval prompts。
- Readiness 如果只是提示文本而不阻塞，会失去安全边界；Budget 如果只统计不 enforcement，也不满足设计目标。

### 可靠性 / 安全 / 成本组

- 后台执行必须有 `RunID / LoopID / TurnID / StepID`，所有模型调用、工具调用、预算扣减、human gate、kill switch、checker verdict 都要能追踪到同一执行链。
- Kill switch 分 turn cancel、loop stop、global emergency stop 三层，传播到 agent context、provider stream、MCP/plugin、子进程树。
- Budget 是硬上限；frontier、advisor、dynamic skill generation、replay judge 都必须进入同一个 loop/session budget ledger。
- 凭据只允许存储 env var 名称、auth profile id 或系统凭据引用；run log、session、replay bundle 不得包含 API key、OAuth token、icodeeasy token、Authorization header。
- Human gate 必须覆盖文件删除、外部网络、git push/merge、凭据变更、动态 skill 晋升、预算提升、官方 auth 登录、切换付费 frontier provider。

### 开发计划组

- loop-engineering 必须补成显式 P0，而不是 D/E/F/G 之后的附加项。
- P0 先实现只读 registry + readiness evaluator + run log，不急着做复杂 scheduler。
- D/E/F/G 继续保持原四组方案，但通过 Loop 元模型统一验收。
- 至少一个 `coding-task` 模板必须能从 GUI 启动，跑完后留下 run log、budget log、readiness snapshot 和 replay/export 元数据。

## 已写入计划的实现单元

- L0：`LoopTemplateV1` schema 与内置模板 registry。
- L1：`ReadinessResult` schema 与 pre-run gate。
- L2：`LoopRun/RunLog`、预算账本与 kill switch 契约。
- L3：Maker-checker execution contract 与 human gate。
- L4：Desktop Loop Control Surface 与 run reports。

## 开发准入标准

开始实现前必须冻结以下契约：

1. `LoopTemplateV1` schema。
2. `ReadinessResult` schema 和 blocker/warning/approval 阈值。
3. `RunLog` schema、Maddog 存储路径、retention、脱敏策略。
4. `MakerCheckerMode` execution contract、checker 工具权限、返工轮次。
5. `ProviderRole/Profile` projection，覆盖 default/small/frontier/advisor/maker/checker。
6. Budget ledger 默认模型，至少明确 per-run frontier hard cap。
7. Credential model，明确 `api_key_env`、`official_auth_profile`、`proxy_api_key_env`、`oauth_device_flow` 的边界。

## 必测项

- Readiness：缺 key、预算为 0、log 目录不可写、human gate 未配置、kill switch disabled 时阻止启动。
- Budget：请求前超限、stream 中途超限、并发超限、frontier 失败重试不放大。
- Kill：长 provider stream、MCP stdio、子进程树在 turn cancel、loop stop、global stop 后无残留。
- Human gate：`git push`、删除文件、修改凭据、动态 skill 晋升、预算提升都必须等待审批。
- Redaction：run log、replay bundle、event stream 不出现 API key、Authorization、OAuth token、dotenv secret。
- GUI：刷新/重启后 pending human gate、预算状态、run report 不丢失。

## 可开发判断

修订后的设计文档已经具备进入开发的结构。推荐开发顺序：

1. L0 template registry。
2. D1 provider profile projection。
3. L1 readiness audit。
4. D3 provider usage/budget/status。
5. L2 run log + shared sanitizer + budget ledger + kill。
6. E1-E3 context compression/raw-data externalization。
7. F1-F3 code intelligence backend/capability GUI。
8. G1-G4 skill self-evolution/review。
9. L3 maker-checker。
10. D2 official/API key/icodeeasy auth configuration。
11. L4 GUI 控制面整合。

## 第二轮完整计划复审

第二轮复审扩大到 P0 Loop 底座和 D/E/F/G 四组初始方案。架构、产品/GUI、安全成本均给出无阻塞结论；测试/交付组指出 `tasks.md` 当时仍是旧 A/B/C 清单。该执行清单问题已通过新增 **Current Active Backlog** 修复。

已完成修订：

- `tasks.md` 增加 `active_plan: docs/cc/maddog-fusion--3949/plan-external-schemes.md`。
- `tasks.md` 保留旧 A/B/C 为历史基线，并新增 **Current Active Backlog**。
- 新 backlog 覆盖 L0-L4、D1-D3、E1-E3、F1-F3、G1-G4，包含状态、文件范围、依赖、测试焦点和验收标准。
- 计划中冻结 credential model v1、budget ledger v1、shared sanitizer、MCP capability/risk enum、maker-checker isolation、replay/run log 脱敏策略。

第二轮后的准入顺序：

`L0 -> D1 -> L1 -> D3 -> L2 -> E1 -> E2 -> E3 -> F1 -> F2 -> F3 -> G1 -> G2 -> G3 -> G4 -> L3 -> D2 -> L4`
