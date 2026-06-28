---
slugid: maddog-fusion--3949
stage: review
date: 2026-06-28
source_plan: docs/cc/maddog-fusion--3949/plan-external-schemes.md
reviewers: [architecture, context-rag, skill-agent-workflow, product-delivery]
candidates:
  - https://github.com/PabloNAX/ultracode-skill
  - https://github.com/pat-jj/harness-1
  - https://github.com/microsoft/fastcontext
  - https://github.com/alibaba/zvec
  - https://github.com/ryoiki-tokuiten/Iterative-Contextual-Refinements
---

# 外部候选方案专家评审

## 结论

这 5 个 repo 对 Maddog 都有参考价值，但没有一个应该作为 v1 主 runtime 的直接依赖或第五条开发主线。专家组共识是：保留当前 `L/D/E/F/G` 主线，把有价值的机制映射为 post-mainline spike 或后续模板增强。

优先级排序：

1. `microsoft/fastcontext` - 最高价值，进入 code intelligence / context roadmap。
2. `alibaba/zvec` - 有价值，v1 默认暴露本地 vector/hybrid store assessment，但不作为硬依赖。
3. `PabloNAX/ultracode-skill` - 参考 workflow packet 与 delegation artifact，不复制 skill prompt 或运行时。
4. `pat-jj/harness-1` - 参考 long-horizon eval harness，不引入训练、vLLM、CUDA 或模型栈。
5. `Iterative-Contextual-Refinements` - 参考 deep refinement / maker-checker 模式，v1 默认启用策略元数据但保留预算、kill switch 和人工审批 gate。

## 当前元信息确认

截至 2026-06-28，通过 GitHub API 与 `git ls-remote` 轻量确认：

| Repo | 默认分支 HEAD | 语言 | License | 结论 |
|---|---|---|---|---|
| `PabloNAX/ultracode-skill` | `bfa2d924` | n/a | MIT | workflow artifact reference |
| `pat-jj/harness-1` | `8ac40121` | Python | Apache-2.0 | eval harness reference |
| `microsoft/fastcontext` | `1522d6d6` | Python | MIT | high-priority benchmark/reference |
| `alibaba/zvec` | `c54f5e16` | C++ | Apache-2.0 | optional backend spike |
| `ryoiki-tokuiten/Iterative-Contextual-Refinements` | `27fb059f` | TypeScript | MIT | post-v1 refinement strategy |

## 专家组意见

### 架构组

- 不增加第五条主线；`L0 -> ... -> L4` 主执行顺序保持不变。
- FastContext 的价值在 repo explorer abstraction、compact exploration trace、file-line citation 和检索质量评测；应落到 `F1/F2/F3`，而不是直接嵌入 Python runner。
- zvec 适合作为 `VectorBackend` 候选，但 C++/CGO、Windows packaging、index migration、并发写入和 embedding pipeline 风险都不适合 v1 直接绑定。
- harness-1 和 ICR 都属于执行策略/评测思想，不应改变 Maddog 当前 Go kernel + Wails desktop 的单体边界。

### Context / RAG 组

- FastContext 应补进 `F2` benchmark：比较 built-in CodeGraph、mock backend、FastContext-style explorer 的 query latency、token chars returned、citation precision、task success signal。
- zvec 可作为 `F5` spike：验证 dense/sparse/FTS/hybrid 检索、WAL、Windows packaging、small repo incremental update 和 raw output 外置索引。
- harness-1 的 candidate docs、curated evidence、verification records、action/observation trajectories 可用于 `G5` long-horizon eval research。
- 不把任何候选方案的 raw trace 直接进入 prompt；仍遵守 E 阶段 raw-data externalization 和 shared sanitizer。

### Skill / Agent Workflow 组

- ultracode-skill 的有价值部分是 task packet、bounded fan-out、integration checklist、final verification artifact，可映射到 `L5` workflow artifact review。
- ICR 的 BFS/DFS、hypothesis、critique/correction、final judge isolation 可映射到 `L3b` iterative refinement templates。
- ICR 类 deep refinement 必须受 budget ledger、kill switch、human gate 控制，v1 默认启用策略元数据但不绕过 gate。
- 不复制外部 skill 的 prompt 文本、`.workflow` 约定或专用 runtime。

### 产品 / 交付组

- FastContext 和 zvec 的用户价值应通过 GUI 显示为 code intelligence backend health、benchmark result、index state、citation quality，而不是新增研究型页面。
- ultracode/harness/ICR 的价值应进入 run report、workflow template、review/eval report，不增加用户必须理解的新工作流入口。
- 所有新增 spike 都是 post-mainline：不能阻塞 provider/auth、readiness、budget、context compression、skill promotion、desktop loop surface。
- 可开发判断：当前主线已合理，新增候选只作为风险隔离的验证任务。

## 映射到 Maddog 开发计划

| 候选 repo | Maddog 落点 | 处理方式 |
|---|---|---|
| `microsoft/fastcontext` | `F4 FastContext-style Repository Explorer Benchmark` | post-L4 benchmark / optional backend reference |
| `alibaba/zvec` | `F5 zvec Hybrid Store Spike` | post-L4 local storage backend spike |
| `PabloNAX/ultracode-skill` | `L5 Ultracode-style Workflow Artifact Review` | workflow artifact pattern only |
| `pat-jj/harness-1` | `G5 Long-Horizon Eval Harness Research` | eval/replay reference only |
| `Iterative-Contextual-Refinements` | `L3b Iterative Refinement Strategy Templates` | post-v1 maker-checker/deepthink strategy |

## 已落地的 Maddog 映射

### L5 workflow artifact contract

`ultracode-skill` 只吸收 workflow artifact 思路，不复制 prompt、`.workflow` 文件格式或 runtime 约定。Maddog 内置模板现在用 `artifacts` 元数据表达自身需要的可审计产物：

- `taskPacketFields`：任务包必须可追踪的字段，例如 request、workspace state、acceptance criteria、test plan。
- `boundedFanOut`：默认允许的并行 delegation 上限、深度与是否需要人工批准。
- `delegationArtifacts`：worker summary、changed files、tests run、concerns 等汇总产物。
- `integrationChecklist`：主线程合并、冲突处理、focused tests、UI contract 等集成检查项。
- `finalVerificationArtifacts`：run report、test summary、review notes 等最终验证产物。
- `runReportMapping`：把 artifact 关联到 Maddog run report 字段，例如 `final_verification -> report.finalStatus`。

这些字段已进入 `LoopTemplateV1`、desktop `WorkflowTemplateView` 和 Settings -> Workflows 展示；它们只描述 Maddog 自有审计契约，不引入外部 runtime 依赖。

### G5 long-horizon eval harness proposal

`harness-1` 的参考价值落到 Maddog replay/skilleval 的长期任务评估结构，而不是训练或模型服务栈。Maddog bundle/replay 现在可表达：

- `candidateDocs`：候选 skill 或策略的文档化行为、来源引用和摘要。
- `curatedEvidence`：人工或系统筛选过的失败信号、工具证据、上下文证据。
- `verificationRecords`：测试命令、状态和验证摘要。
- `trajectory`：action/observation 步骤轨迹，用于长任务 replay 解释。
- `budgetContext`：预算上限、已用、剩余、成本和币种。
- `harnessProposal`：replay report 中的 proposal 摘要，包含证据计数、失败计数、预算汇总和明确排除的 runtime 依赖。

明确排除项保持为：`training`、`cuda`、`vllm`、`checkpoint`、`model_serving`。因此该 spike 只增强 Maddog 离线评估和 run report 审计，不改变当前 provider/runtime 请求路径。

### L3b iterative refinement strategy metadata

`Iterative-Contextual-Refinements` 的 BFS/DFS、hypothesis、critique/correction、final judge isolation 思路映射为 Maddog 模板上的 `refinementStrategy` 元数据，并在 v1 默认启用：

- `enabled=true`：普通 `coding-task` 默认展示并启用 ICR-style 搜索策略。
- `searchModes`：可审计地声明 `bfs_hypothesis` 与 `dfs_correction`，不绑定外部实现。
- `critiqueRounds` / `correctionRounds`：限制 critique/correction 轮次。
- `finalJudgeIsolation`：声明最终 judge 隔离强度。
- `budgetCapTokens`：深度精炼的额外预算上限。
- `killSwitchRequired`：没有 kill switch 不允许启动。
- `humanApprovalRequired`：启动前必须有人工批准，避免隐藏消耗。

`EvaluateRefinementStrategy` 只在策略启用、预算足够、kill switch 可用且人工批准后返回 ready；否则保持 blocked 或 needs_human。因此该能力默认可见、可审计、可停止，但不会绕过标准 coding loop 的安全 gate。

## 不集成项

- 不把 FastContext Python runner 放入主请求路径。
- 不把 zvec 设为必需 vector store；v1 只默认启用 assessment/GUI/doctor 可见性。
- 不安装或复制 ultracode-skill 到 Maddog 内置 skill 集合。
- 不引入 harness-1 的训练、vLLM、CUDA、checkpoint 或模型服务栈。
- 不让 ICR 的 BFS/DFS deep refinement 绕过预算、kill switch 或人工审批 gate。

## 开发准入

新增候选任务进入开发前必须满足：

1. 主线 `L0 -> ... -> L4` 至少完成到可跑 `coding-task` run report。
2. shared sanitizer、budget ledger、readiness gate、kill switch 已可复用。
3. GUI 能显示 backend/template/eval 状态，不通过新 hidden config 启用高风险能力。
4. benchmark 或 spike 能独立失败，不影响 built-in CodeGraph、provider routing 和普通任务执行。
