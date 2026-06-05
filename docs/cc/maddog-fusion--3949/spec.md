---
title: maddog 融合方案需求文档
slugid: maddog-fusion--3949
date: 2026-06-05
status: draft
source_skill: cc-brain
needs_tech: true
---

# maddog 融合方案（Reasonix × tinyctx）

## 1. 背景与问题

maddog 要把 **DeepSeek-Reasonix** 的 agent 运行时/桌面 app 与 **tinyctx** 的"智能层"融合成**一个 app**。

研究已**代码核实**：Reasonix 与 tinyctx 在三层**硬冲突**（线协议 / 上下文哲学 / 宿主循环，见 §7）。因此**不采用**双进程互相代理，而采用**以 Reasonix 为基座、移植 tinyctx 机制**的单体方案。

## 2. 目标 (Goals)

1. **多 provider**：OpenAI 兼容 + Claude 兼容，**仅 maddog 自用**。〔已由 Reasonix 原生满足〕
2. **用顶级模型提准确度**（goal #2）：`advisor` 显式二次意见 + **失败信号自动升级**到 frontier。
3. **自动优化 skill**（goal #3）：**运行时自动编排**（选/生成）**＋ 离线自改进闭环**（两者都要）。
4. **单一 app**：单体、单一可分发产物。

## 3. 非目标 (Non-Goals)

- 不支持 **codex** 或其它外部 CLI 作为 host —— tinyctx 的 wire-level 代理整体退役。
- 不引入 tinyctx 的 **wire 层上下文改写管线**（snapshot/ctx-pack/compactor/historian/agent_rules）——与 Reasonix 前缀缓存冲突，且 Reasonix 已原生具备等价物。
- 不做**在线自我改写**；自改进走治理式**离线**闭环。
- v1 不重写 tinyctx "半 A" 的等价物（Reasonix 已有：目录树 / codegraph / `/compact` / memory / dedup）。

## 4. 关键架构决策（已锁定）

| 决策 | 内容 |
|---|---|
| 基座 | **Reasonix**（Go kernel `internal/` + Wails desktop + 单一 `control.Controller` 多前端） |
| Provider | 复用 Reasonix registry：`kind="openai"`（DeepSeek/任意 OpenAI 兼容，`/chat/completions`）+ `kind="anthropic"`（Claude，`/v1/messages`） |
| tinyctx 取舍 | 拆两半：**半 A**（codex 耦合的上下文代理）**丢弃**；**半 B**（与宿主无关的智能：router 升级 / advisor / auto-skill / 自改进）**移植** |
| 进程/语言 | Python 基本退役；**唯一可选留存** = 离线自改进工具（out-of-band，不在请求路径上） |
| 与既有判断 | 与 tinyctx 自有结论 `docs/reasonix-evaluation.md`（"借机制，不借架构"）同向 |

## 5. 能力范围（v1 = 三者都做；建议内部分阶段 A→B→C1→C2）

### A. 多 provider〔已具备，验收即可〕
- **行为**：配置 `[[providers]]` 即可用 DeepSeek / 任意 OpenAI 兼容 / Claude。
- **验收**：openai 与 anthropic 两 kind 各跑通一次 chat + 一次 tool-call。

### B. advisor + 失败信号升级（goal #2）
- **advisor**：内置 skill `advisor`，pin 到一个 frontier provider；触发 = 显式 `/advisor` ＋ 自动（连续失败/难决策）。沿用 tinyctx `ask_advisor` 契约（≤100 词、enumerated steps、`Risks:`）。
- **升级策略**：loop 内"失败信号 → 本回合执行切到 frontier"。信号借鉴 tinyctx `router.py`：`error_streak ≥ 阈值`、goal/acceptance 控制轮、本地健康度下降。
- **复用 Reasonix**：`default_model`/`planner_model`/`subagent_models` 选路接缝；`internal/evidence` 失败信号。
- **边界**：升级默认开、可关、**对用户可见**（显示路由原因）；每会话 frontier **预算上限**。

### C. 自动优化 skill（goal #3，两者都要）
- **C1 运行时编排**：每任务自动选/组合 skill；无匹配 → 生成**一次性 dynamic skill** → `validator` → 注入。高风险任务**不自动生成**；注入**不覆盖** `REASONIX.md`、长度受限。对应 tinyctx `orchestration_injector` + `dynamic_skill`。
- **C2 离线自改进**：治理式闭环 —— capture 可回放证据 → held-out **replay 评测** → **打分前沿** → 过 guardrail/回归**才晋升** skill 版本。对应 tinyctx self-improvement plan + `eval_harness`。
- **复用 Reasonix**：`.reasonix/skills`、`run_skill`、`/skill`；`internal/evidence`、`internal/checkpoint`、`benchmarks/e2e`。

## 6. 建议内部阶段（不缩小范围，仅降 big-bang 风险）

1. **A** —— provider 验收（最小）
2. **B** —— advisor + 失败信号升级（最快拿到"提准确度"价值）
3. **C1** —— 运行时 skill 编排
4. **C2** —— 离线自改进闭环（最重；**依赖**前序阶段产生的可回放证据，故置后）

## 7. 附录 · 三层冲突证据（已代码核实，`[已知]`）

| 层 | 证据 |
|---|---|
| 线协议 | Reasonix `DeepSeek-Reasonix/internal/provider/openai/openai.go:1,132` 只发 `/chat/completions`；`.../anthropic/anthropic.go:118` 发 `/v1/messages`；全仓**无** `/responses`。tinyctx `<tinyctx>/tinyctx/proxy.py:917` **仅收** `/v1/responses`（FastAPI）。→ Reasonix 指向 tinyctx 会 404。 |
| 上下文 | Reasonix `REASONIX.md` 约定前缀**字节级稳定** + `control.Compose` 仅在 turn tail 追加；tinyctx `proxy.py` 每轮 `hoist/embed instructions`、`auto_scout:1585` prepend、`inject_advisor_hint:1713` 改 prefix。→ 透明代理会打碎 Reasonix 前缀缓存。 |
| 宿主循环 | tinyctx `router.py:56` codex handoff、`proxy.py:937` codex session id、`codex_profile_bootstrap`、`.codex-plugin`。→ tinyctx 假设宿主是 codex，Reasonix 不是。 |

## 8. 主要风险 / 待确认 (Needs Verification → 交 cc-tech)

- `[推测]` `router.py` 的升级信号能否干净映射到 Reasonix loop 现有钩子（核对 `internal/agent`、`internal/evidence` 暴露的信号）。
- `[推测]` advisor 作为 `runAs=subagent` skill + pin frontier 的最简实现路径（核对 skill 子代理 + `subagent_models` 机制）。
- dynamic skill 生成的安全边界（validator 规则；不可覆盖 system/memory；高风险任务禁用）。
- 离线自改进的数据来源与回放保真度（Reasonix session/evidence 格式是否足够支撑 replay）。
- frontier provider 选择与**成本护栏**（预算、可见性）。
- 桌面 UI 呈现：路由决策 / advisor 面板 / skill 编排 dashboard（可借鉴 tinyctx `dashboard.py`）。

## 9. 未决问题

### 延迟到 cc-tech

> 以下 4 项经 cc-document-review 标记为 P1，用户决定延迟到 `/cc-tech` 阶段与技术方案一并解决。

- [影响 goal #3][需研究] **成功标准量化** —— goal #2（提准确度）和 goal #3（自动优化 skill）缺少可测量 KPI（如 task success rate lift、advisor intervention rate、cost-per-task ceiling），cc-tech 应结合 Reasonix 现有 metrics 面提出具体指标。
- [影响 §5 能力范围][用户决策] **v1 优先级分级** —— 当前所有能力（advisor / 运行时编排 / dynamic skill / 离线自改进）均声明为 v1 must-have；cc-tech 应评估各能力的技术依赖与风险后给出推荐优先级（must-have / should-have / could-have）。
- [影响 §1 背景][需研究] **"什么都不做"基线** —— 当前分开使用 Reasonix + tinyctx 的具体痛点未量化；cc-tech 应在方案中补充现状成本/摩擦分析，作为融合方案的价值锚点。
- [影响 goal #3][用户决策] **auto-skill 解耦** —— goal #3 将运行时编排与离线自改进绑为"两者都要"；cc-tech 应评估两者是否可独立交付：运行时编排先落地，离线自改进作为加速器而非共需。

### 延迟到规划阶段

- [影响 §5.C1][需研究] "高风险任务"的判定标准（validator 规则）
- [影响 §5.B/§5.C][技术] 各新路径的降级/fallback 策略（advisor 不可用、dynamic skill 生成失败、validator 拒绝、replay 证据损坏）
- [影响 §5.B][需研究] Frontier 成本护栏的具体参数（预算上限值、用户覆盖控制、告警阈值）
- [影响 §5][需研究] 桌面 UI 的 v1 最小范围（advisor 面板、路由可见性等是否 v1 硬需还是可 CLI-only）

## 10. 交接

`needs_tech: true` —— 建议下一步 `/cc-tech maddog-fusion--3949` 做技术方案（接口契约、信号映射、数据模型、迁移与回归），并在技术方案中解决上述 §9 的延迟项。
