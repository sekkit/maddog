---
slugid: maddog-real-gaps--7c21
stage: audit
status: active
created: 2026-07-03
origin:
  - docs/cc/maddog-real-gaps--7c21/plan.md
  - docs/cc/maddog-fusion--3949/verification-matrix.md
  - docs/cc/maddog-fusion--3949/expert-verification-record.md
---

# Maddog 源码级对齐审计（实现 vs 参考项目/论文/spec）

本审计在 `maddog-real-gaps--7c21/plan.md` 的基础上，对每个功能组做了**实现源码 ↔ 参考项目源码/README/论文 ↔ spec 设计目标**的三方对齐。与既有 plan 的关系：

- plan 中 R1–R9 的判断全部得到源码级证实（引用见下），且 plan 的 8 个实现单元在审计时**全部未开始**。
- 本审计新增了一批 plan 未覆盖的缺口（标 `NEW`），其中若干会改变 plan 单元的实现取舍（见每组"对 plan 的影响"）。

分类标记：`未实现`（有入口无实体）· `假绿`（会产生看似通过的结果）· `bug`（行为错误）· `效果差`（实现了但达不到可用效果）· `spec漂移`（与 spec/参考契约明确不一致）· `需取舍`（方案本身要重新设计）。

严重度：P0（该功能组的核心声明不成立）· P1（用户可感知的错误/误导）· P2（质量/一致性问题）· P3（维护性/边角）。

---

## 1. 离线自改进 skilleval ↔ microsoft/SkillOpt + arXiv:2605.23904

SkillOpt 的核心循环是 rollout → reflect → aggregate → select → update → evaluate：在真实执行环境里回放任务、由 optimizer 模型对 skill 文档做有界编辑、**候选编辑只有在 held-out 验证分严格提升时才被接受**。对照之下：

| # | 发现 | 分类 | 严重度 | 证据 |
|---|------|------|--------|------|
| 1.1 | **候选生产端不存在**：`CandidateStore.Create` 在生产代码中零调用方（仅测试）。运行时生成的 dynamic skill、捕获的 bundle 都不会变成候选。自改进闭环缺"生成"这一环，端到端不可运行——候选只能手工构造 JSON 且 hash 必须等于渲染内容的 sha256（`verifyHash`），实际无人能走通 | 未实现 | **P0** | `internal/skilleval/candidate.go:152`（Create 无调用方）；grep 全仓仅 `RecordEvaluation/Get/List/Promote/Reject/Rollback` 被 CLI/desktop 使用 |
| 1.2 | **"replay" 不是重放**：`ReplayRunner.Run` 是单次 LLM 文本调用——把候选 skill body 当 system prompt、bundle 转写为 user prompt，要求"produce the best final answer"。无工具执行、无 agent 循环、无环境，`TotalTurns` 恒 1，且 **`Success/GoalMet = (回答非空)`**。与 SkillOpt 的环境内 rollout + 可验证评分完全不对齐 | 未达设计目标 | **P0** | `internal/skilleval/runner.go:31-59` |
| 1.3 | **评分与门禁结构性失效（假绿链）**：(a) `ruleScore` 对任何非空回答基础分 0.8 ≥ 0.70 阈值；(b) guardrail 成本检查是死代码——replay 从不写 `Tokens`，`newTokens` 恒 0 永不触发 2x 判定；(c) 回归检查是死代码——baseline 恒 success（见 1.4）且 replay 非空即 success，`newRate < oldRate` 只在回答为空时成立，而空回答早已被分数线拦截；(d) LLM scorer 的 prompt 只含两个 outcome 元组，**看不到任务内容和 skill 内容**，无法判断回答质量。合成结果：晋升门 ≈ "provider 连续 5 次返回非空文本" | 假绿 | **P0** | `internal/skilleval/scorer.go:63-72`（0.8 基础分）、`guardrail.go:64-74`（死成本检查）、`guardrail.go:48-52`（死回归检查）、`scorer.go:23-24`（无任务上下文） |
| 1.4 | **runtime 捕获无条件写 `Success:true, GoalMet:true`**（plan R6 证实未修）。每个用户回合都会产出一个"成功"bundle | 假绿 | P0 | `internal/control/controller.go:1401-1404` |
| 1.5 | **dry-run 可持久化 passing guardrail**：CLI `--dry-run --store-dir` 在完全无 provider 的情况下写入通过评估，`Promote` 即可放行；`RecordEvaluation` 不记录任何评估来源（dry-run 与否、bundle 数、模型、bundle ids），晋升门无法区分橡皮图章与真实评估。`DryRunReplay` 本身是自证成立：body 非空 → Success=true，GoalMet 直接抄 bundle 自己的 outcome | 假绿 | **P1** | `internal/cli/skilleval.go:88-89,106-115`；`internal/skilleval/candidate.go:238-258`；`runner.go:70-82` |
| 1.6 | **GUI 评估是功能性死路**（plan R5 证实）：`EvaluateSkillCandidate` 只用**候选自己的 source bundle**（CLI 的 `validateHeldOutBundles` 恰恰会拒绝它）做 `DryRunReplay`，`GuardrailConfig{MinScore:0.7}` 未设 MinBundles → 默认 5 → guardrail 永远输出 "need at least 5 bundles, got 1"。GUI 评估**永远不可能**产生可晋升评估，却发出 "offline replay scored skill candidate 0.80" 的通知 | bug + 误导 | **P1** | `desktop/app.go:4993-5015` |
| 1.7 | bundle schema 大半字段死亡：runtime 捕获从不填 `Dynamic`/`SkillName`/`History`/`Metrics`(压缩指标)/`Review`(人工审核)；`Messages` 每次都是**全 session 历史**，同 session 多个 bundle 高度重复且"task=本回合输入、messages=所有回合"错位 | 效果差 | P2 | `internal/control/controller.go:1394-1408` vs `internal/skilleval/bundle.go:83-98` |
| 1.8 | `internal/eval` 是 skilleval 的 v1 遗留副本（replay/runner/scorer/guardrail 双份并行漂移），现仅 `cmd/e2ebench/frontier_smoke.go` 与 `skilleval` 的 promote.go 引用 | 维护性 | P3 | `internal/eval/*.go`（629 行）；importers 仅 2 处 |
| 1.9 | **vs SkillOpt 的机制全景缺失**：无 optimizer 编辑步（不存在"从打分 rollout 生成 skill 改进"的任何代码）、无严格改进接受准则（只有绝对阈值）、无 rejected-edit buffer、无 epoch/batch 概念。当前系统即使修好 1.1–1.6，也只是"证据归档 + 人工晋升"，不是 SkillOpt 式自进化 | 需取舍 | — | 对照 `research/github-stars-sekkit-2026-06-27/readmes/microsoft__SkillOpt.md`（"accepted only when it strictly improves a held-out validation score"） |

**对 plan 的影响**：单元 2/3 只修 outcome 真实性和 GUI promotion-grade，**不会修 1.1（无生产端）和 1.2（无真实 rollout）**。建议：
- 在单元 2 前插入"候选生产"工作：dynamic skill 使用后 + bundle 捕获时自动 `Create` 候选（G1 原始方案本来就有此意图，落实丢失）。
- 单元 3 的"真实 provider replay"要明确降级声明：单次 LLM 重答 + 无环境验证 ≤ 弱证据；或按 1.9 重新取舍——要么做沙箱重执行（成本高），要么把功能定位改为"证据管理 + 人工审核",不再宣称 replay evaluation。
- 修 scorer：规则分不应给非空回答 0.8 底分；LLM scorer 必须携带 task + skill diff。

---

## 2. frontier 升级 / advisor ↔ advisor-middleware（本地克隆源码）+ tinyctx router + spec §5.B

advisor-middleware 的核心设计：**executor 模型自己决定何时求助**——advisor 以工具形式暴露（Anthropic 原生 `advisor_20260301` / 任意 provider 的 fallback tool 双模式），system prompt 注入剩余次数，advisor 共享 executor 的完整对话上下文。

| # | 发现 | 分类 | 严重度 | 证据 |
|---|------|------|--------|------|
| 2.1 | **升级主干实现良好（对齐项）**：阈值升级（连续失败/streak/健康度）、frontier 输出 token 预算 + 超限降级 + 事件、frontier 也失败时回落 default、每回合重置、advisor 次数预算，均有实现和测试 | 对齐 | — | `internal/agent/upgrade.go:32-68`、`agent.go:976-984,1111-1252` |
| 2.2 | **tinyctx `ask_advisor` 输出契约缺失**：spec §5.B 明确"沿用 tinyctx ask_advisor 契约（≤100 词、enumerated steps、`Risks:`）"，内置 advisor skill body 无任何此类约束，advice 格式与长度不受控 | spec漂移 | P2 | `internal/skill/builtins.go:65-82` vs spec.md §5.B |
| 2.3 | **非 Anthropic executor 缺主动求助通道（主用例受损）**：native advisor 仅 Anthropic wire 支持；OpenAI 兼容 provider（spec 的主用例是 DeepSeek）下 executor 只能 (a) 被动等 3 次失败触发自动咨询，或 (b) 经 `run_skill` 调 advisor——而 subagent **只收到 executor 手写的 `arguments`，不共享对话上下文**（参考实现传完整 messages）。同时 advisor 没有 explore/review 那样的专用顶层工具（affordance 弱），system prompt 也不注入"剩余咨询次数" | 未达设计目标 | **P1** | `internal/provider/anthropic/anthropic.go:181,363`（native 仅 anthropic）；`internal/skill/tools.go:100-107`（subagent 无上下文）；`tools.go:371-386`（内置顶层工具无 advisor）；对照 `advisor-middleware/advisor_middleware/middleware.py:319-366` |
| 2.4 | 升级信号缺 spec 列举的 "goal/acceptance 控制轮" 与"难决策"触发——现只有工具失败类信号，模型自身的推理僵局/摇摆不会触发升级或咨询 | spec漂移 | P2 | `internal/agent/upgrade.go:43-66`（仅 3 类失败信号）vs spec.md §5.B |
| 2.5 | 自动咨询的 Question 硬编码同一句泛化提问，advisor 无法获得聚焦问题（参考实现由 executor 提出具体 question） | 效果差 | P3 | `internal/agent/advisor.go:102-104` |

**对 plan 的影响**：plan 没有覆盖本组（fusion 验证记录判定 Fulfilled）。建议新增小单元：(a) advisor skill 增加输出契约；(b) 为 OpenAI 兼容 executor 增加 `advisor` 顶层工具（复用 curateAdvisorContext 共享上下文 + 注入剩余次数）；(c) 可选：goal 控制轮信号。

---

## 3. runtime skill orchestration ↔ tinyctx orchestration_injector / dynamic_skill

| # | 发现 | 分类 | 严重度 | 证据 |
|---|------|------|--------|------|
| 3.1 | **Matcher 对中文任务完全失效**：`tokenize` 按 letter/digit 连续段切分，中文无空格 → 整句成为单一 token，与 skill name/description 永不相等 → 中文输入下匹配率≈0。产品有 zh 本地化、用户主要说中文 | 效果差 | **P1** | `internal/skill/matcher.go:91-112` |
| 3.2 | tokenize **先 sort 再截断 24 个** token：长任务只保留字典序最前的 24 个词（数字/a-c 开头优先），后字母词（vite/webpack/zustand…）被系统性丢弃 | bug | P2 | `matcher.go:76-89,110`（sort 在 tokenSet 截断之前） |
| 3.3 | MinScore=2 且命中一词计 2 分 → **单个非停用词重叠即匹配**，误匹配率高；停用词表仅 16 个英文词 | 效果差 | P2 | `matcher.go:25,61-74,114-124` |
| 3.4 | **"一次性 dynamic skill" 未强制**：`Store.Inject` 后没有任何代码调用 `Remove`，dynamic skill 会话级存活；且 Matcher 只排除 ScopeBuiltin，**后续回合会匹配到旧 dynamic skill**（ScopeCustom），过期 playbook 复利传播。expert-verification 已留 caveat，未闭环 | spec漂移 | P2 | `internal/skill/skill.go:352-378`（无 Remove 调用方）；`matcher.go:42`（仅 skip builtin） |
| 3.5 | **Validator 误杀链**：body 黑名单含 `\bremember\b`/`\bforget\b` —— 自然语言高频词（"remember to run tests"）直接使生成体无效；task 黑名单含 `\btruncate\b` —— "fix log truncate helper" 被判高危、整个编排跳过。这些 pattern 同时被 skilleval 的 `CheckPromotionGuardrail`/`CandidateStore` 复用，误杀沿链路放大 | bug | P2 | `internal/skill/validator.go:107,131-132`；`internal/skilleval/guardrail.go:36` |
| 3.6 | 高危判定 pattern 全为英文/shell/SQL；中文破坏性任务（"删库"、"清空所有表"）不识别。作为"高风险任务不自动生成"的安全闸，对中文用户形同虚设 | 效果差 | P2 | `validator.go:95-118` |
| 3.7 | 对齐项：`dynamic_skills` 默认 false（opt-in，避免每回合额外 LLM 调用）；注入为 turn-tail 提示不碰前缀缓存；body ≤2000 字符；memory 工具禁用——符合 spec"注入不覆盖 MADDOG.md、长度受限" | 对齐 | — | `internal/config/config.go:1594`；`internal/skill/orchestrator.go:95-102` |
| 3.8 | 生成的 dynamic skill 不进入 skilleval 候选（与 1.1 同源）：C1 与 C2 之间的管道不存在 | 未实现 | P0（同1.1） | `orchestrator.go:67-82`（Generate→Inject 后无候选创建） |

**对 plan 的影响**：plan 无本组单元。3.1/3.5 是小改动大收益（中文分词按 CJK 逐字 bigram 即可；黑名单改为工具声明检查而非正文词匹配）。3.8 应并入"候选生产"新单元。

---

## 4. context compression ↔ headroom / rtk / context-mode / fastcontext

| # | 发现 | 分类 | 严重度 | 证据 |
|---|------|------|--------|------|
| 4.1 | **raw 检索模型不可达且指针误导**：压缩后的 tool message 写明 "raw available at `raw://tool/<id>`"，但**没有任何模型可用工具能解引用该 ref**——`Controller.ToolResult` 只服务前端展开。对照 headroom 的可逆 CCR + `headroom_retrieve`、context-mode 的 FTS/BM25 检索：参考方案的核心是"压缩是可逆的、agent 可按需取回"，maddog 只做了"用户可见"。模型被引导去引用一个自己拿不到的资源，可能诱发幻觉工具调用或直接重跑命令 | 未达设计目标 | **P1** | `internal/agent/agent.go:2486,2566-2583`（写入指针文案）；`internal/control/controller.go:2712-2752`（仅前端 API）；grep `raw://` 全仓无模型侧消费者 |
| 4.2 | token 估算 `chars/4` 对 CJK 低估约 4 倍：中文输出的 saved_tokens/阈值推断失真；verification-matrix 的"≥50% token 节省"断言基于合成 fixture 而非真实负载 | 效果差 | P2 | `internal/contextpack/compressor.go:450-455`；`agent.go:2559-2564` |
| 4.3 | `isSignalLine` 子串匹配过宽（"0 failed"、"no errors" 也算信号行）——方向保守（多保留），无害但降低压缩率 | 效果差 | P3 | `compressor.go:269-306` |
| 4.4 | 对齐项：deterministic-first 策略、shell/test/log 专用压缩、raw store 会话生命周期、off/auto/aggressive 策略、压缩在消息创建时发生（不回写历史、前缀缓存安全）、panic fallback——本组是四个新功能组里**落地质量最高**的 | 对齐 | — | `internal/contextpack/`、`agent.go:2482-2527` |

**对 plan 的影响**：plan 无本组单元。建议新增：模型侧 `expand_tool_output(id, range)` 只读工具（有预算上限），或至少把指针文案改为"full output retained for the user"避免误导。

---

## 5. code intelligence backends ↔ serena / claude-context / codebase-memory-mcp / zvec

| # | 发现 | 分类 | 严重度 | 证据 |
|---|------|------|--------|------|
| 5.1 | **GUI benchmark 与所选 backend 无关**（plan R1 源码证实）：`RunCodeIntelligenceBenchmark(id)` 把 `LocalFilesBenchmarkBackend` 包一层改名为 "<id> local smoke"，HyperGraphRAG/外部 MCP 一律跑本地文件扫描 | 假绿 | **P0** | `desktop/app.go:5567-5607` |
| 5.2 | **CLI 默认混入 MockGraph**（plan R2 证实），且 mock 的 Query 精确返回 fixture 期望值 → 报告里 mock **永远满分**，视觉上"最好的 backend 是 MockGraph" | 假绿 | P1 | `cmd/codeintelbench/main.go:42-44,114-122` |
| 5.3 | **benchmark cases 硬编码 maddog 仓库特化期望**：`"RunBenchmark"→runner.go`、`"advisor frontier routing"→docs/cc/maddog-fusion--3949/tech.md`。GUI 的单 case 与 CLI 的默认 cases 在**任何其他仓库上都无意义**（期望文件不存在 → relevance 恒 0）。评测框架无法支撑其设计目标"评测后选择默认 backend" | 效果差 | **P1** | `cmd/codeintelbench/main.go:82-96`；`desktop/app.go:5560-5566` |
| 5.4 | **backend registry 是纯展示层**：`ToolMapping` 只被校验/能力推导/GUI 投影消费，**运行时没有任何查询经过 registry 路由**；外部 MCP backend 的启停/健康/查询都不经过它（用户还得另行把同一 server 配成普通 MCP plugin）；kind=mcp 也没有 benchmark adapter（CLI 只有 mock/builtin/hypergraphrag）。"可插拔 code intelligence 后端"只在 UI 意义上成立 | 未实现 | **P0** | grep `ToolMapping` 全仓：仅 `backend.go` 校验 + `desktop/app.go:4515` 投影；`cmd/codeintelbench/main.go:42-63`（无 mcp adapter） |
| 5.5 | **出厂示例杜撰 serena 工具名**：注释示例映射 `mcp__serena__context`/`mcp__serena__status`，serena 实际无 `context`/`status` 工具（其工具为 find_symbol/find_referencing_symbols/get_symbols_overview 等）。校验只查 `mcp__<server>__` 前缀不查存在性，照抄示例得到"valid"但指向虚构工具的配置 | bug | P2 | `internal/config/render.go:551-558`；对照 `readmes/oraios__serena.md` |
| 5.6 | serena/claude-context/codebase-memory 无 first-party preset（仅 known_overrides 有 codebase-memory-mcp 的 cwd 兼容 hint）；zvec 是向量库不是 MCP server，无任何 adapter——plan 单元 5 的降级决定正确，待执行 | 未实现 | P1 | `internal/plugin/known_overrides.go:82-86`；grep serena/zvec 全仓无其他实现 |

**对 plan 的影响**：单元 4 的"backend-to-benchmark adapter resolver"是对的，但要加上 5.3——**评测用例必须可移植**（从目标仓库自动生成 case：随机采样 symbol/doc 做期望），否则真 adapter 也测不出真结果。单元 5 落地时修 5.5 的示例。

---

## 6. HyperGraphRAG ↔ 上游源码（research/HyperGraphRAG）+ NeurIPS 2025 论文

| # | 发现 | 分类 | 严重度 | 证据 |
|---|------|------|--------|------|
| 6.1 | **契约无实现**：maddog 侧只有 CLI 契约 shim（`health/index/query --json` 子命令约定）；文档示例命令 `maddog-hypergraphrag` **不存在于本仓、上游仓库或任何发行渠道**——上游只有 Python 类 API（`HyperGraphRAG.insert/query`，见 `script_query.py`），无任何 CLI。用户必须自己从零写 wrapper，文档没有说明这一点，也没有提供参考实现 | 未实现 | **P0** | `internal/hypergraphrag/sidecar.go`（仅 shim）；`maddog/docs/HYPERGRAPHRAG.md`（示例命令）；`research/HyperGraphRAG/`（无 CLI 入口）；grep 本仓 scripts/tools 无 wrapper |
| 6.2 | **语义与成本错配（需重新取舍）**：上游是**文本知识超图 RAG**——indexing = 每 chunk LLM 抽取 n-ary 关系 + embedding 双写，论文验证域是医学等领域 QA。maddog 把它挂成 "code intelligence backend"：(a) benchmark 的 `BuildIndex` 同步跑、GUI 2 分钟超时、`UpdateIndex=BuildIndex` 全量重建——与上游成本模型（长时间、真金 API 费）完全不符；(b) "代码仓库→知识超图"的适配性无任何验证。更合理的定位是 docs/research/决策记录检索（本仓 `research/hypergraphrag-maddog-analysis.md` 自己也是这个结论），代码检索声明应撤下或改为实验性 | 需取舍 | **P1** | `internal/hypergraphrag/sidecar.go:65-75`；`research/HyperGraphRAG/hypergraphrag/operate.py`（LLM 抽取管线）；`research/hypergraphrag-maddog-analysis.md` |
| 6.3 | `BenchmarkInfo()` 的 health 探测用 `context.Background()` 无超时——sidecar 命令挂起会挂死整个 benchmark CLI 进程 | bug | P2 | `internal/hypergraphrag/sidecar.go:50` |
| 6.4 | `maddog hypergraphrag status` 只读配置不做真实 health（plan 单元 6 的 `--check` 待做）；agent 运行时零集成（无任何路径让 agent 用上 sidecar 查询结果） | 未实现 | P1 | `internal/cli/hypergraphrag.go`；plan 单元 6 |

**对 plan 的影响**：单元 6 的"真实可验证集成"若要成立，必须**随仓提供参考 sidecar wrapper**（如 `tools/hypergraphrag-sidecar/` 包装上游 insert/query 的小 Python CLI），否则"可配置 contract"永远停在纸面；同时 benchmark 需要"预建索引模式"（跳过 BuildIndex、只测查询），否则 2 分钟超时下永远 degraded。

---

## 7. 混合 review / provider profile / 外部 benchmark

| # | 发现 | 分类 | 严重度 | 证据 |
|---|------|------|--------|------|
| 7.1 | **review 的 "code intelligence context" 名不副实**：`reviewChangedFileContext` 只是从 findings 提取的文件片段列表，不接任何 code intelligence backend（与 5.4 同源）。G4 方案文字"Code intelligence backend 提供 affected symbols/context"未实现 | spec漂移 | P2 | `internal/cli/review.go:147,175-190`；`internal/review/report.go:52-64` |
| 7.2 | **确定性规则只在 CLI 路径运行**：`AnalyzeUnifiedDiff` 仅 `maddog review` 命令调用；GUI/模型经 `review` 顶层工具或 run_skill 走的是内置 skill body（只是文字上"提到"规则类别），**拿不到确定性 findings**。违反 R7"CLI 与 desktop 行为一致" | spec漂移 | P2 | `internal/cli/review.go:142-150`（唯一调用方）；`internal/skill/tools.go:380-382`（review 工具直连 skill） |
| 7.3 | 规则集 5 条且 Go 中心：`, _ :=` 类误报（`for i, _ := range` 合法惯用法）、无其他语言的 error-handling 检查。对照 open-code-review（10 语言、1505 条 ground-truth 基准、精度优先取舍）——按 G4 "v1 聚焦高置信模式"的自我定位可接受，但对外声明应避免"混合 review 已对标 OCR" | 效果差 | P3 | `internal/review/rules.go:126-149` |
| 7.4 | 对齐项：provider profile/角色/预算/状态（D 组）实现扎实——frontier 预算跨回合累计、超限事件+降级、角色投影、凭证脱敏、多 auth 模式统一。与 litellm/CodexBar/goose 的借鉴点（profile 模型/可观测/GUI 配置）一致，未发现新缺口 | 对齐 | — | `internal/agent/agent.go:976-990`；expert-verification D 组 |
| 7.5 | 外部 coding-agent-benchmark 有真实模型模式（`-Model`，默认 `icodeeasy/gpt-4.1`）与 LocalSmoke fixture 双路径，strict real gate（plan 单元 7）待做；`cmd/e2ebench/frontier_smoke.go` 仍用 legacy `internal/eval`（见 1.8） | 确认 | P2 | `scripts/run-coding-agent-benchmark.ps1:23-25`；`cmd/e2ebench/frontier_smoke.go:11` |

---

## 8. 横切结论

1. **plan 的方向正确且必要**：R1–R9 全部源码证实。但 plan 以"验证真实性"为主轴，本审计发现三条 plan 未覆盖的主线：
   - **断链**：C1（dynamic skill）→C2（候选）之间没有管道（1.1/3.8）——不修它，单元 2/3/7 修得再好也没有输入。
   - **评测有效性**：即便接上真实 backend/provider，评分器（1.3）与 benchmark 用例（5.3）本身无区分度，"真实执行"仍测不出真实结论。
   - **中文有效性**：matcher（3.1）、高危闸（3.6）、token 估算（4.2）对中文用户系统性失效/失真。
2. **建议的单元增补**（并入 plan 或另开工作线）：
   - 新单元 A（候选生产）：controller 捕获 + orchestrator 生成后自动创建候选；置于现单元 2 之前。
   - 新单元 B（评分器修复）：去掉 0.8 底分、scorer 带任务/skill 上下文、评估记录带 provenance（dry-run/bundles/model）；与单元 3 合并实施。
   - 新单元 C（可移植 benchmark 用例）：从目标仓库自动生成期望（symbol 抽样），并入单元 4。
   - 新单元 D（中文可用性）：CJK 分词、黑名单改造、（可选）tokenizer 接入；独立小单元。
   - 单元 6 增补：随仓 hypergraphrag 参考 wrapper + 预建索引 benchmark 模式，否则按 plan 的降级路径把 HyperGraphRAG 改标 external-contract-only。
   - 新单元 E（advisor 补齐）：输出契约 + OpenAI 兼容 executor 的主动求助工具（共享上下文）；小。
   - 新单元 F（raw 输出模型侧取回）：`expand_tool_output` 只读工具或修改指针文案；小。
3. **快速修复清单（低成本高信度，可先行）**：
   - `sidecar.go:50` health 加超时（6.3）。
   - `validator.go` 移除 `\bremember\b`/`\bforget\b` 正文匹配、收窄 `\btruncate\b`（3.5）。
   - `matcher.go` tokenize 先截断后排序（3.2）。
   - `render.go` serena 示例改为真实工具名（5.5）。
   - GUI `EvaluateSkillCandidate` 通知文案不再报告无效评分（1.6 的止血）。
   - 压缩指针文案改为不误导模型（4.1 的止血）。
   - 删除或合并 `internal/eval`（1.8）。

## 9. 复扫记录（2026-07-04，针对 commit `c83229a6a` "Close Maddog audit verification gaps"）

### 已修复（源码+测试验证）

| 原编号 | 状态 | 验证 |
|---|---|---|
| 1.3 评分假绿 | **修复**：ruleScore 降为 0.45 基础分且仅限 dry-run 路径；promotion-grade 强制模型评分（scorer 失败给 0 分）；scorer prompt 携带 task + candidate skill body | `scorer.go` RequireModelScore/scoreReplayPrompt；tests 绿 |
| 1.4 无条件 Success:true | **修复**：controller 捕获写 `OutcomeConfidenceUnverified` + reason | `controller.go:1402` |
| 1.5 dry-run 可解锁晋升 | **修复**：`RecordEvaluationWithProvenance` 记录 mode/provider/model/bundleIDs/PromotionGrade；`Promote` 要求 `PromotionGrade=true`；dry-run 恒 false | `candidate.go:318`；CLI/GUI 均走共享 `EvaluateCandidate` |
| 1.6 GUI 单 source bundle 死路 | **修复**：GUI 拆为 provider replay（held-out 自动发现/显式路径 + 真实 provider）与显式 dry-run preview 两路径，通知文案区分 | `desktop/app.go:4994-5089` |
| 5.1 GUI benchmark 换名 smoke | **修复**：按 registry 解析真实 backend；builtin→共享 MCP adapter（`codegraph/mcp_benchmark.go`），hypergraphrag→sidecar，kind=mcp→显式报错（不伪装）；LocalFiles 包装类已删除 | `desktop/app.go:5657-5763` |
| 5.2 CLI 默认混 mock | **修复**：mock 改为 `-include-mock` opt-in | `cmd/codeintelbench/main.go:30,43-45` |
| 6.4 status 无真实检查 | **部分修复**：新增 `maddog hypergraphrag health/index/query` 真实契约子命令 | `internal/cli/hypergraphrag.go:44-106` |
| 6.1 文档误导 | **诚实化**：docs 明确 "does not ship a bundled `maddog-hypergraphrag` executable"、无 sidecar 时状态为 external-contract | `maddog/docs/HYPERGRAPHRAG.md` |
| R7 dev mock 固定通过 | **修复**：browser mock guardrailPass=false、区分 preview/replay、promote 抛错 | `bridge.ts` diff |
| R8 strict gate | **修复**：`-RequireRealCapabilities` + completion_audit 五字段读写闭环 | `run-maddog-regression.ps1:56-64,798-803` |
| R9 文档收敛 | **修复**：verification-matrix 改为 promotion-grade/preview-only 表述，"Partial real-mode ready" | matrix diff |

### 修复引入的新问题

| 编号 | 问题 | 严重度 |
|---|---|---|
| N1 | **晋升从"假绿"变成"结构性死路"**：guardrail 要求所有 bundle/baseline `Confidence==verified`，但**没有任何生产路径把捕获的 bundle 标为 verified**（controller 恒写 unverified，仅测试 fixture 手写 verified）。真实捕获的 bundle 上任何评估都会失败于 "outcome is not verified"。plan 单元 2 预留的 "human review 或 eval signal" 验证机制未实现 | **P0**（与 1.1 并列为当前闭环的两个断点） |
| N2 | replay outcome 被 runner 自动标 `verified`（reason "provider replay completed"）——完成≠验证，Success 仍是 answer!=""；使 guardrail 的 replayed-verified 检查形同虚设，混淆 confidence 语义 | P2 |
| N3 | 新增的 hypergraphrag CLI health/index/query 全部用 `context.Background()` **无超时**（hypergraphrag.go:49,69,95）——6.3 的无超时问题不但未修（sidecar.go:62 依旧），还扩散到了新命令；sidecar 挂起 = 命令挂死 | P2 |
| N4 | GUI dry-run preview 手搓 EvaluationResult 绕过共享 `EvaluateCandidate`（MinBundles:1 + source bundle）——有 PromotionGrade=false 兜底，但与共享服务漂移 | P3 |

### 仍未处理（文件级确认未动，git log 停留在前一 commit）

| 原编号 | 内容 | 严重度 |
|---|---|---|
| 1.1/3.8 | **候选生产端仍缺失**：`CandidateStore.Create` 依旧零生产调用方，dynamic skill/bundle 不会变成候选 | **P0** |
| 1.2 | replay 本体仍是单次无工具 LLM 调用、Success=非空（现在被 N1 的 verified 墙挡住，但 replay 质量问题未变） | P0(设计) |
| 1.7 | bundle 死字段（Dynamic/SkillName/History/Metrics/Review）捕获时仍不填充 | P2 |
| 1.8 | `internal/eval` 遗留副本仍在（e2ebench 仍引用） | P3 |
| 2.2–2.5 | advisor 输出契约 / 非 Anthropic fallback tool / 上下文共享 / goal 信号 —— 全部未动 | P1–P3 |
| 3.1–3.6 | matcher 中文失效 / 排序截断 / 单词匹配 / dynamic skill 会话存活 / validator remember·truncate 误杀 / 高危闸英文 only —— 全部未动 | P1–P2 |
| 4.1 | raw:// 指针模型不可达且文案误导 —— 未动 | P1 |
| 4.2 | chars/4 token 估算 CJK 失真 —— 未动 | P2 |
| 5.3 | benchmark 用例仍硬编码本仓期望（CLI defaultBenchmarkCases + desktop 内联 case："RunBenchmark"→runner.go、tech.md）——在非 maddog 仓库上仍无意义 | **P1** |
| 5.4 | ToolMapping 仍是纯展示；kind=mcp 无 benchmark adapter（desktop 现在诚实报错，能力缺口仍在）；runtime 查询路由仍不存在 | P1 |
| 5.5 | render.go serena 示例仍映射不存在的工具名 | P2 |
| 5.6 | serena/claude-context/codebase-memory presets、zvec 降级标注 —— plan 单元 5 未执行 | P1 |
| 6.2 | HyperGraphRAG 语义/成本错配：无预建索引 benchmark 模式，2 分钟超时下真实 sidecar 的 index 几乎必然超时 | P1 |
| 7.1–7.3 | review 的 code-intel context 名不副实 / 规则 CLI-only / Go 中心规则 —— 未动 | P2–P3 |

### 复扫验证命令

- `go test ./internal/skilleval ./internal/cli ./internal/control ./internal/hypergraphrag ./cmd/codeintelbench -count=1` — 全绿。
- `go test . -run "SkillCandidate|EvaluateSkillCandidate|RunCodeIntelligenceBenchmark" -count=1`（desktop）— 绿。
- grep 验证：`OutcomeConfidenceVerified` 生产写入方仅 `runner.go`（replay 自标）；`CandidateStore.Create` 生产调用方 0；matcher/validator/builtins/compressor/render/rules/eval/agent.go 的 last-commit 均为前一提交。

## 10. 第二轮复扫记录（2026-07-04，针对 commit `36ec71e30` "Fix Maddog reaudit closure blockers"）

### 本轮已修复（源码 + 测试验证）

| 原编号 | 状态 | 验证 |
|---|---|---|
| 1.1/3.8 候选生产端缺失（P0） | **修复**：`captureReplayBundle` 通过"已完成的 `run_skill` 调用 × 注入的 dynamic skill（ScopeCustom + Path=(dynamic)）"检测本回合是否真正使用了 dynamic skill；命中时 bundle 记录 `Dynamic`+`SkillName` 并调用 `CandidateStore.Create` 自动创建候选。C1→C2 管道首次打通 | `controller.go` `dynamicRunSkillUsed`/`runSkillCallName`；`controller_test.go` +153 行 |
| N1 verified 死锁（P0） | **修复**：`CaptureBundle` 新增 `verifiedOutcomeEvidenceReason`——human review approved / 成功的 `StepProof` 回执 / 成功的验证命令（go test/build/vet、npm·pnpm·yarn test/build/typecheck、pytest、cargo、mvn/gradle/make test、tsc）任一命中且 FinalAnswer 非空 → `Success/GoalMet=true` + `Confidence=verified`。真实捕获的 bundle 现在可以进入 guardrail 通过路径，晋升端到端首次可达 | `bundle.go` +67；`bundle_test.go` |
| N2 replay 自标 verified | **修复**：replay outcome 改为 `unverified`（"completion is scored separately"）；guardrail 移除了 replayed-verified 检查（replay 质量由模型评分承担） | `runner.go`、`guardrail.go` -3 |
| N3 sidecar 无超时 | **修复**：`SidecarConfig.Timeout` + `withTimeout`（默认 `DefaultSidecarTimeout=2m`，尊重外部 deadline）覆盖 BuildIndex/Query/health（含 BenchmarkInfo 路径）；CLI 三个子命令加 `--timeout`；超时错误独立报文 | `sidecar.go`、`cli/hypergraphrag.go` |
| N4 GUI dry-run 绕过共享服务 | **修复（安全性质成立）**：共享 `EvaluateCandidate` 与 GUI 预览路径都把 dry-run 的 guardrail 强制为 "dry-run preview is not promotion-grade evidence"（保留 min-bundles 反馈）；GUI 预览仍是手搓 result（漂移风险保留为 P3） | `evaluation.go:106-108`、`desktop/app.go:5076` |

### 本轮新发现

| 编号 | 问题 | 严重度 |
|---|---|---|
| N5 | **verified 推导取"第一条成功"而非"最终状态"**：`verifiedOutcomeEvidenceReason` 遍历回执时 `if !receipt.Success { continue }`，命中第一条成功验证命令即返回——早期 `go test` 通过、其后修改代码、**最后一次 `go test` 失败**的回合仍被标为 verified success（且强制 `Success/GoalMet=true`）。另外 `go build`/`go vet`/`tsc` 也算验证——编译通过≠任务达成，信号偏弱。建议：取最后一条验证命令的结果，或要求最后一次成功之后无失败的验证回执 | **P1**（新信任链的健全性缺口） |
| N6 | 捕获从 async goroutine 改为同步在 turn 路径执行（全量历史 JSON marshal + 候选创建），会话越长延迟越大 | P3 |
| N7 | dynamic skill 检测只覆盖 `run_skill` 工具调用；slash 路径（`/<name>`）使用 dynamic skill 不会产生候选 | P3 |
| N8 | `verifiedOutcomeEvidenceReason` 的 human-review 分支是生产死代码：controller 从不设置 `opts.Review`，也没有任何 CLI/GUI 动作可以人工核准某个 bundle——实际可达的验证信号只有测试/构建回执 | P3 |

### 仍未处理（per-file git log 确认未动：matcher/validator/builtins/compressor/render/rules/agent.go 均停留在审计前提交）

| 原编号 | 内容 | 严重度 |
|---|---|---|
| 1.2 | replay 本体仍是单次无工具 LLM 调用、Success=非空（现在上游 bundle 有真实 verified 信号，但 replay 侧质量不变） | P0(设计) |
| 1.7 余项 | History/Metrics(压缩指标)/Review 捕获仍不填充（Dynamic/SkillName 本轮已补） | P2 |
| 1.8 | `internal/eval` 遗留副本 | P3 |
| 2.2–2.5 | advisor：输出契约 / 非 Anthropic fallback tool / 上下文共享 / goal 信号 | P1–P3 |
| 3.1–3.6 | matcher 中文失效 / 排序截断 / 单词匹配 / dynamic skill 会话存活 / validator `remember`·`truncate` 误杀 / 高危闸英文 only。**注意**：3.1 现在直接抑制新管道——中文任务不触发匹配/生成 → 无 dynamic skill → 无候选 | **P1**–P2 |
| 4.1 | raw:// 指针模型不可达且文案误导 | P1 |
| 4.2 | chars/4 token 估算 CJK 失真 | P2 |
| 5.3 | benchmark 用例仍硬编码本仓期望（desktop 内联 + CLI defaults） | P1 |
| 5.4 | ToolMapping 纯展示；kind=mcp 无 benchmark adapter（desktop 诚实报错） | P1 |
| 5.5 | render.go serena 示例杜撰工具名 | P2 |
| 5.6 | serena/claude-context/codebase-memory presets、zvec research-only 标注（plan 单元 5） | P1 |
| 6.2 | HyperGraphRAG 索引成本 vs benchmark 假设：CLI `index --timeout` 可手动预建索引后单测查询（部分缓解），但 desktop benchmark 仍在 2m ctx 内同步 BuildIndex，无预建索引模式 | P1→P2（部分缓解） |
| 7.1–7.3 | review：code-intel context 名不副实 / 规则 CLI-only / Go 中心 | P2–P3 |

### 复扫验证命令（本轮）

- `go test ./internal/skilleval ./internal/control ./internal/cli ./internal/hypergraphrag ./cmd/codeintelbench -count=1` — 全绿。
- desktop：`go test . -run "SkillCandidate|EvaluateSkillCandidate|CaptureReplay|RunCodeIntelligenceBenchmark" -count=1` — 绿。
- per-file `git log -1`：matcher.go/validator.go（616c19179）、builtins.go/render.go/agent.go（41dc26932）、compressor.go（baa752f39）、rules.go（817151089）——均早于两个修复提交，确认未动。

### 当前状态一句话

两轮修复后，自改进闭环（dynamic skill → 候选 → provider replay 评估 → verified-bundle guardrail → 晋升）**首次在结构上可走通**；剩余风险集中在：N5（verified 误判会污染新信任链）、1.2（replay 证据强度弱）、3.1（中文任务进不了管道入口）、以及 5.x/6.x/2.x/4.x 中未动的既有缺口。

## 11. 第三轮复扫记录（2026-07-04，针对 commit `06e8d9018` "Close Maddog audit gap loop" + `e72d978f4` + `9629f3a7c`）

本轮是覆盖面最大的修复波（69 文件，+2493/-841），`9629f3a7c` 仅为测试字符串清理。验证：`go test ./...` 68 包全绿 + desktop 全绿。

### 本轮已修复（逐项源码验证）

| 原编号 | 状态 | 关键证据 |
|---|---|---|
| N5 verified 首胜误判（P1） | **修复**：改为取**最后一条**验证命令，失败即否决整个 verified 推导；剔除 `go build`/`go vet`/`tsc`/typecheck 等弱信号，只留真实测试命令 | `bundle.go` `lastVerification` 逻辑 |
| N7 slash 路径不产候选 | **修复**：`dynamicSlashSkillUsed` + `submitCommandOrTurn` 传 raw slash 输入 | `controller.go:1529-1541` |
| N8 human review 死代码 | **修复**：新增 `ApplyHumanReview`/`SaveBundle` + CLI `skilleval review` 子命令（approve/deny 会重算 outcome/Confidence 并重写 bundle ID） | `bundle.go`、`cli/skilleval.go:243-274` |
| 1.2 replay 无工具（P0 设计） | **实质改善**：新增 `AgentReplayRunner`——真实 agent 循环（MaxSteps 8）+ 只读工具注册表（∩ candidate.AllowedTools）；replay 不再自评 Success（恒 false/unverified，"scored separately"），质量判定全部交给 promotion-grade 模型评分；desktop 传 live `ToolRegistry`，CLI 按 AllowedTools 构建 builtin 只读集。guardrail 相应改为 `verifiedSuccessRate` 可比才对比，cost 检查因 Tokens 回填复活 | `runner.go:26-74`、`evaluation.go:75-96`、`guardrail.go:51-67` |
| 1.7 bundle 死字段 | **修复**：捕获改为 turn 级 Messages（不再整 session 重复）、History（turn task）、Metrics（agent 压缩记录）、Review（经 human review 路径） | `controller.go:1414-1445`、`agent.go` `ToolCompressions` |
| 1.8 internal/eval 遗留副本 | **修复**：包删除，promote.go 并入 skilleval，e2ebench 迁移 | `internal/eval` 不存在 |
| 2.2 advisor 输出契约 | **修复**：builtin body 强制 ≤100 词 + numbered steps + `Risks:` 行 | `builtins.go:73-77` |
| 2.3 非 Anthropic 无主动求助 | **修复**：`fallbackAdvisorTool`（executor 自拟 question + `curateAdvisorContext` 共享近期上下文 + 预算扣减），仅在 native advisor 缺席且 advisor 启用时注册——与 advisor-middleware 双模式对齐；2.5 的硬编码问题在工具路径同步解决 | `advisor_tool.go`、`agent.go:879-884` |
| 3.1 中文匹配失效（P1） | **修复**：CJK 逐字 unigram + bigram 分词 | `matcher.go:122-166` |
| 3.2 排序截断偏置 | **修复**：`selectMatchTokens` 取头+尾、保持原序 | `matcher.go:103-120` |
| 3.3 单词重叠即匹配 | **修复**：MinScore 2→4，精确名 +5；stop token 改 rune 计数 | `matcher.go:24,37` |
| 3.4 dynamic skill 会话存活 | **修复**：`cleanupGeneratedRuntimeSkills` 在所有 turn 路径 defer 执行——一次性生命周期落实 | `controller.go:1345-1365`、`turn_orchestrator.go:168,184` |
| 3.5 validator 误杀 | **修复**：`\bremember\b`/`\bforget\b` 移出正文黑名单；`\btruncate\b` 收窄为 `truncate (table|database)` | `validator.go:101,128` |
| 3.6 高危闸英文 only | **修复（基础版）**：新增中文破坏性短语表（删库/清空所有表/删除全部数据等） | `validator.go:150-164` |
| 4.1 raw:// 模型不可达（P1） | **修复**：新增 `tool_result` 只读工具，接受 `raw://tool/<id>` 或裸 ID；输出走标准截断路径且不递归压缩；TOOL_CONTRACT 文档同步 | `raw_tool_result_tool.go`、`agent.go:2513-2516` |
| 4.2 chars/4 CJK 失真 | **修复**：估算改为 ascii/4 + CJK×1 + other/2 | `agent.go:2606-2629` |
| 5.3 benchmark 用例仓库特化（P1） | **修复**：`DefaultBenchmarkCases(root)` 从目标仓库派生（首个 Go 符号 + 首个 md 标题，含 skip dirs 和 fallback），CLI 与 desktop 均已接线；测试断言不得出现 maddog 特化 fixture | `bench_cases.go`、`desktop/app.go:5710`、`main.go:42` |
| 5.4 ToolMapping 纯展示（benchmark 部分） | **修复**：`MappedMCPBenchmarkBackend` 经 live tool registry 按 ToolMapping 真实执行查询；desktop 经 `Controller.ToolRegistry()` 接线，kind=mcp 不再报"无 adapter" | `mcp_mapped_benchmark.go`、`desktop/app.go:5754`、`port.go` |
| 5.5 serena 示例杜撰工具名 | **修复**：改为 `find_symbol`/`get_symbols_overview`，附 preset 注释（zvec 标注 research-only） | `render.go` |
| 6.2 索引成本错配（部分） | **修复**：sidecar 支持 `IndexMode=query_only/prebuilt` 跳过 Build/UpdateIndex；desktop hypergraphrag benchmark 固定用 query_only（先手动 `maddog hypergraphrag index --timeout` 预建） | `sidecar.go` `indexMode()`、`desktop/app.go:5751` |
| 7.2 规则 CLI-only | **修复**：`prepareSubagentReviewTask` 把 `AnalyzeUnifiedDiff`+`BuildTask` 挂进 review/security-review 子代理工具路径，GUI/模型触发的 review 也带确定性 findings | `tools_review.go`、`tools.go:394,398` |
| 2.4 goal/难决策信号 | **部分**：`FailureSignal` 增加 `GoalAcceptanceLoop`/`DifficultDecision` 字段，upgrade policy 和 advisor context 消费它们——**但见 R3-1** | `evidence.go:57-59`、`upgrade.go:59-74` |
| 5.6 presets | **部分**：`KnownBackendPresets`（serena/claude-context/codebase-memory + zvec research_only）已定义——**但见 R3-2** | `presets.go` |

### 本轮仍开放（复扫后的完整剩余清单）

| 编号 | 问题 | 严重度 |
|---|---|---|
| R3-1 | **goal/难决策信号有消费无生产**：`GoalAcceptanceLoop`/`DifficultDecision` 没有任何赋值方——`FailureSignalSince` 不计算它们，controller/agent 不设置它们；upgrade/advisor 的对应分支永远不触发（测试用手工构造信号通过，掩盖了缺口）。2.4 实为门面修复 | **P1** |
| R3-2 | `KnownBackendPresets` 无消费方（仅自测试引用）：presets 未投影到 Capabilities/配置流，用户看不到也用不上；5.6 实际交付的只有 render.go 注释 | P2 |
| R3-3 | `IndexMode` 无 TOML 配置通路：config.go 无 `index_mode` 字段，CLI codeintelbench 不设置——CLI 对 hypergraphrag 仍全量 BuildIndex（2 分钟超时内基本必超时）；只有 desktop 硬编码 query_only | P2 |
| R3-4 | `MappedMCPBenchmarkBackend` 发送 kitchen-sink 参数对象（query/symbol/name/top_k/max_results/budget_tokens 全塞）——`additionalProperties:false` 的严格 MCP server 会拒绝；最终需要 per-preset 参数模板 | P3 |
| 1.2 余项 | agent replay 限只读工具、无工作区/测试执行——replay 证据仍是 LLM 评审的轨迹而非可验证结果。相对 SkillOpt 的取舍已明确并诚实标注，作为已接受设计记录 | P2(已接受) |
| 7.1 余项 | review 的 "code context" 仍是 findings 派生文件片段（`ChangedFileCodeContext`），不接 code intelligence backend | P3 |
| 5.4 余项 | agent 运行时仍不经 ToolMapping 路由代码智能查询（registry = 状态 + benchmark）——范围决策，记录在案 | P3(范围) |
| R3-5 | `verifiedOutcomeEvidenceReason` 的 StepProof 不对称：验证命令取最后一条且失败否决，但失败的 StepProof 不清除更早的成功 StepProof（仅在无验证命令时兜底生效） | P3 |

### 收敛判断

原审计 7 组 33 项 + 三轮复扫新增 12 项（N1-N8、R3-1~R3-5），**当前仅剩上表 8 项**：1 个 P1（R3-1）、3 个 P2（R3-2/R3-3/1.2余项）、4 个 P3。原有的全部 P0 已闭合；自改进闭环（编排生成 → 一次性使用 → 候选创建 → held-out agent replay → 模型评分 → verified guardrail → 人工可审晋升/回滚）每一环都有真实实现和真实测试。R3-1 建议下轮优先：要么在 goal loop/controller 中真实计数 acceptance 轮次并检测摇摆决策，要么删掉这两个字段及其消费分支，避免再次形成"看起来已实现"的假象。

## 12. 第四轮复扫记录（2026-07-04，针对 commit `e7868d3b2` "Fix code intelligence audit gaps"）

验证：`go test ./...` 0 FAIL、desktop 定向测试绿、frontend `npm run test:all` 绿。

### 本轮已修复（第 11 节剩余清单的销项）

| 原编号 | 状态 | 关键证据 |
|---|---|---|
| R3-1 goal/难决策信号无生产方（P1） | **修复**：完整生产链落地——`goalMachine.advance` 产出 controlSignal（readiness 拦截→DifficultDecision+summary；blocked→DifficultDecision）→ `advanceGoalAfterTurn` 调 `executor.RecordControlSignal` → agent pending 队列在下一 Run 的 ledger reset 后落为 `goal_control` receipt → `FailureSignalSince` 单独提取（不污染工具健康统计）→ 新增 `evaluateInitialRouting` 在回合开始即评估。`DifficultDecision` 仅触发 advisor 不升级。行为有集成测试锁定 | `goal.go:196-283`、`turn_orchestrator.go:248-250`、`agent.go` `RecordControlSignal`/`applyPendingControlSignals`/`evaluateInitialRouting`、`evidence.go:144-215` |
| R3-2 presets 无消费方 | **修复**：desktop Capabilities 现在把未配置的 `KnownBackendPresets` 投影为 `configured:false` 行（含 notes/capabilities/tool mapping；zvec 标 researchOnly），已配置同名 backend 时去重；frontend types/契约测试同步 | `desktop/app.go` presets 投影、`capabilities-code-intelligence.test.ts` |
| R3-3 IndexMode 无 TOML 通路 | **修复**：`index_mode` 加入 `CodeIntelligenceBackendConfig`（toml tag + render 输出），CLI codeintelbench 把 `backend.IndexMode` 传入 sidecar——用户可配 `index_mode = "query_only"` 走预建索引 | `config.go`、`render.go`、`cmd/codeintelbench/main.go` |
| R3-4 kitchen-sink 参数 | **修复**：`mappedBenchmarkArgs` 按工具 JSON schema 只发送声明过的属性；schema 要求不支持的必填参数时明确报错；仅在 schema 缺失/不可解析时才回退全量参数 | `mcp_mapped_benchmark.go` `mappedBenchmarkArgs`/`benchmarkArgCandidate` |

### 本轮新记录

| 编号 | 问题 | 严重度 |
|---|---|---|
| R5-1 | **GoalAcceptanceLoop 语义偏宽（有意设计，建议复核）**：`goal.go` 对每个继续中的 goal 回合无条件写 `GoalAcceptanceLoop = g.turns`（总回合数），upgrade policy 在 `>= threshold`（默认 3）时升级——**任何 ≥3 轮的健康 goal 都会在下一轮切到 frontier 并消耗一次 advisor**，即使推进顺利。测试明确锁定该行为（"want third goal turn to route to frontier"），是有意取舍而非疏漏；但它把"健康的长计划"与"acceptance 摇摆"混为一谈，与 spec"失败信号自动升级"的框架有张力，对配置了 frontier 的用户有真实成本影响（有预算上限+事件可见兜底）。建议：改为计数 `g.intercepts`+blocked 轮次（或无进展轮次），或把 goal-loop 阈值独立成可配项并写入文档 | P2（设计复核） |

### 剩余清单（审计收口状态）

| 编号 | 内容 | 定性 |
|---|---|---|
| R5-1 | goal 回合数升级语义 | P2，有意设计，建议复核取舍 |
| 1.2 余项 | agent replay 限只读、无工作区/测试执行，证据强度为 LLM 评审级 | P2，已接受的设计取舍（已诚实标注） |
| 5.4 余项 | agent 运行时不经 ToolMapping 路由（registry = 状态 + benchmark） | P3，范围决策 |
| 7.1 余项 | review "code context" 为 findings 派生片段，不接 code intelligence backend | P3 |
| R3-5 | verified 推导中 StepProof 失败不清除更早成功（仅无验证命令时兜底生效） | P3 |

### 收口判断

原审计 33 项 + 四轮复扫新增 13 项，**全部 P0/P1 已闭合**。剩余 5 项均为 P2/P3：1 项是需要产品层面复核的有意设计（R5-1），1 项是已声明接受的取舍（1.2），3 项是低影响边角。除非 R5-1 的成本行为被判定为不可接受，本审计可以收口；后续任何新功能应直接进入常规 review 流程而非本审计线。

## 13. 第五轮整体 review（2026-07-04，针对 commit `7c6ee20b2` "Fix goal control loop routing signal" + R5-1 专家组结论）

验证：`go test ./...` 无失败、`go vet`（control/agent）干净、desktop 全量绿、frontend `npm run test:all` 绿。

### R5-1 专家组执行记录

- 专家 A（信号语义对齐）完成全量分析，推荐 churn 语义（候选 d：拦截/blocked 计数升级、DifficultDecision advisor-only、纯长度退出升级信号）；专家 B（成本/配置面）与专家 C（实现爆炸半径）因会话额度耗尽未产出。落地方案与专家 A 推荐方向一致。

### R5-1 已修复（commit `7c6ee20b2`）

- 移除了 `advance()` 中对每个继续回合无条件写 `GoalAcceptanceLoop = g.turns` 的覆盖；改为：readiness 拦截分支计 `g.intercepts`、blocked 分支计 `g.blocks`。**健康 goal 不再产生任何路由信号**——R5-1 的成本放大问题消除。新测试锁定：健康长 goal 不升级、拦截产生 advisor-only 信号、重复 blocked 达阈值才升级（`goal.go:204,276-277`、`turn_orchestrator_test.go` 三个新用例、`goal_test.go`、`evidence_test.go`）。
- **语义特性（记录，非缺陷）**：`g.intercepts` 是连续计数（healthy continue 清零；非 strict 模式下第二次连续 complete 会被强制放行，intercepts 实际封顶 1）；`g.blocks` 是 same-reason streak 且 ≥3 即终止 goal（终止时 cont=false 不发信号）。因此**默认配置（threshold=3、非 strict）下 GoalAcceptanceLoop 升级分支实际不可达**——goal 摇摆在默认配置产生的是 advisor 咨询而非 frontier 升级；strict 模式连续 3 次拦截可达。这是保守方向的取舍（错也错在便宜侧），与专家 A 的"累计 churn 字段"建议相比信号更弱，可接受；若未来希望非 strict 也能因反复摇摆升级，需要新增跨 continue 的累计计数字段。

### 本轮新确认缺陷（专家 A 发现，已独立复核）

| 编号 | 问题 | 严重度 |
|---|---|---|
| R6-1 | **`resetRoutingForTurn` 是死代码（自引入起从未被调用；`git log --all -G` 全历史无调用点）**，三个后果：(a) `advisorTurnUses` 永不重置——`advisor_max_uses_per_turn`（默认 1）实际是**每 Agent 生命周期 1 次**：首次自动咨询后，本会话的自动 advisor、fallback advisor 工具、native advisor 注入全部静默失效（`advisorRemaining()` 恒 0）；(b) `upgraded`/`onFrontier` 无每回合复位——升级是会话粘性而非 spec §5.B 的"本回合"，且 `downgradeFromFrontier` 不清 `upgraded`，一次降级后本会话永不再评估升级；(c) 本审计第 2 组 2.1 曾判"每回合重置……对齐"，系当时只读了函数体未查调用方，**判断有误，予以更正**。修复建议：在 `Agent.Run` 开头调用 `resetRoutingForTurn()`（先复位，再 `applyPendingControlSignals` + `evaluateInitialRouting`，控制信号驱动的升级仍在回合开始生效，语义自洽） | **P1**（advisor 预算失效部分）/ P2（粘性升级部分） |

### R6-1 修复记录（2026-07-04，本审计线直接执行）

- `internal/agent/agent.go`：在 `Run` 开头接线 `a.resetRoutingForTurn()`（先复位路由/advisor 回合预算，再应用 pending 控制信号，再做初始路由评估——控制信号驱动的升级仍在回合开始生效）；函数补文档注释，明确 `frontierTokens` 有意不复位（会话级预算）。
- 新增测试：`TestRunResetsAdvisorTurnBudgetAcrossTurns`（两回合各自触发一次咨询，advisor 不再"用一次就死"）、`TestRunDoesNotStickToFrontierAcrossTurns`（升级只作用于挣得它的回合，健康后续回合回到 default provider）。
- 更新测试：`TestTurnOrchestratorRepeatedBlockedTurnsProduceUpgradeSignal` 原断言"只有 1 次 advisor 咨询"——该期望恰好编码了预算永不重置的 bug（第三回合的 advisor-before-frontier 被耗尽的假预算挡掉）；改为期望 2 次（blocked 难决策咨询 + 升级前咨询），并断言第二次问题含 `goal_acceptance_loops=2`。
- 验证：`go test ./...` 全绿、desktop 全绿（本改动纯 Go，wire/前端无涉）。

### 收口后剩余清单

| 编号 | 内容 | 定性 |
|---|---|---|
| ~~R6-1~~ | ~~resetRoutingForTurn 死代码~~ | **已修复（见上）** |
| 1.2 余项 | agent replay 限只读、LLM 评审级证据 | P2，已接受取舍 |
| 5.4 余项 | agent 运行时不经 ToolMapping 路由 | P3，范围决策 |
| 7.1 余项 | review "code context" 不接 backend | P3 |
| R3-5 | StepProof 首胜不对称 | P3 |
| （记录） | 默认配置下 GAL 升级分支不可达（advisor-only），strict 可达 | 特性说明，建议写入配置文档 |

## 附：审计方法与材料

- maddog 侧：`internal/skilleval`、`internal/eval`、`internal/control`、`internal/agent`、`internal/evidence`、`internal/skill`、`internal/contextpack`、`internal/codegraph`、`internal/hypergraphrag`、`internal/review`、`internal/cli`、`cmd/codeintelbench`、`cmd/e2ebench`、`desktop/app.go`、`scripts/run-coding-agent-benchmark.ps1`。
- 参考侧：`advisor-middleware/`（完整源码）、`research/HyperGraphRAG/`（完整源码）、`research/coding-agent-benchmark/`、`research/github-stars-sekkit-2026-06-27/readmes/`（SkillOpt、serena、claude-context、codebase-memory-mcp、zvec、headroom、rtk、context-mode、dirac、fastcontext、litellm、goose、CodexBar、open-code-review 等）、`research/hypergraphrag-maddog-analysis.md`。
- 设计目标侧：`docs/cc/maddog-fusion--3949/spec.md`、`plan-external-schemes.md`、`verification-matrix.md`、`expert-verification-record.md`。
