# sekkit Starred Repos 对 Maddog 的技术价值分析

生成时间：2026-06-27  
数据来源：GitHub REST starred API + 各 repo 默认分支 README raw 文件  
样本规模：4782 个 starred repos，4689 份 README 下载成功，93 个 README 缺失或未匹配常见文件名。

## 数据包

- `stars.json`：GitHub starred API 原始结果
- `repos.compact.json`：归一化后的 metadata、README 路径、Maddog 相关性信号
- `repos.csv`：便于表格筛选的索引
- `meta/`：每个 repo 一份 GitHub metadata JSON
- `readmes/`：下载到本地的 README 文件
- `readme_manifest.json`：README 下载状态与 raw URL
- `index.md`：按初筛分数排序的前 100 项索引

## Maddog 当前缺口映射

根据 `docs/cc/maddog-fusion--3949/spec.md` 与 `tech.md`，Maddog 当前最值得借鉴外部方案的地方不是重新造一个 agent host，而是补强以下六条能力线：

1. Frontier provider / advisor / 自动升级：需要 provider 路由、预算、失败信号、成本可见性。
2. Context 预算：需要 tool output 压缩、代码检索、日志压缩、长任务上下文续航。
3. Skill 编排与自改进：需要运行时 skill 选择、dynamic skill、离线 replay eval、版本晋升。
4. MCP / tool / auth：需要外部工具接入、tool catalog、OAuth/API key/官方 auth 的 GUI 管理。
5. Desktop GUI：需要把 provider、frontier、小模型、后台页面、advisor、metrics 显示到 Wails app。
6. 安全与可观测性：需要 sandbox/permission、token/cost/rate limit/session health 面板。

## P0：最值得优先借鉴

| 项目 | 对 Maddog 的价值 | 建议落点 |
|---|---|---|
| [headroomlabs-ai/headroom](https://github.com/headroomlabs-ai/headroom) | 面向 agent 的 context compression 层，覆盖 tool outputs、logs、RAG chunks、files、history，并提供 library/proxy/MCP 形态。 | 做 Maddog `ToolOutputCompressor`：插在 `executeBatch` 后、provider call 前；Wails 显示原始 token、压缩后 token、命中策略。不要直接变成强依赖 sidecar，优先移植算法/接口思想。 |
| [BerriAI/litellm](https://github.com/BerriAI/litellm) | provider gateway 的成熟参考：多 provider 统一接口、OpenAI-compatible、cost tracking、guardrails、load balancing、logging。 | 对齐 Maddog provider config：为 OpenAI/Anthropic/icodeeasy/official auth 建统一 `ProviderProfile`、`BudgetPolicy`、`ProviderEvent`。Go 内实现，不引入 Python proxy 到请求路径。 |
| [microsoft/SkillOpt](https://github.com/microsoft/SkillOpt) | 用 trajectory、validation gate、best_skill artifact 做离线 skill 优化，和 Maddog C2 完全同向。 | 作为 `cmd/e2ebench` + evidence replay 的算法蓝本：session bundle -> candidate skill edit -> held-out replay -> guardrail -> promote。 |
| [DeusData/codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp) | 高性能 codebase knowledge graph / MCP，强调本地索引、多语言、低 token 查询。 | 优先验证能否作为可选 MCP 后端；中长期把其“快速本地 graph query + token 节省”抽象进 Maddog codegraph/memory 接口。 |
| [oraios/serena](https://github.com/oraios/serena) | agent-first 的 semantic code retrieval、editing、refactoring、debugging 工具，通过 MCP 接任意 LLM client。 | 给 Maddog 的代码工具层补“语义编辑工具”的参考：比纯 grep/file edit 更接近 IDE 操作；适合做可选 MCP provider 或本地 tool adapter。 |
| [zilliztech/claude-context](https://github.com/zilliztech/claude-context) | 面向 coding agent 的 semantic code search MCP，让大代码库按语义进入上下文。 | 与现有 codegraph/dedup 做对照基准：检索召回、token 成本、构建时间、增量更新。 |
| [modelcontextprotocol/servers](https://github.com/modelcontextprotocol/servers) | MCP reference implementations 与 server 组织方式。 | Maddog MCP 管理页和内置示例的规范参考：server manifest、权限说明、安装状态、健康检查。 |
| [microsoft/playwright-mcp](https://github.com/microsoft/playwright-mcp) | 浏览器自动化 MCP 的结构化 accessibility snapshot 方案，也明确区分 MCP 与 CLI/skills 的取舍。 | 桌面 app 的“后台页面/浏览器任务”能力可以先接 Playwright MCP；同时吸收其 MCP vs CLI skill 的选择原则，避免 tool schema 过胖。 |
| [graykode/abtop](https://github.com/graykode/abtop) | 类 htop 的 AI coding agent 运行监控：token、context window、rate limit、child process、ports。 | 直接启发 Maddog Desktop 的 Runtime Monitor：当前任务、frontier 升级原因、token/context、水位线、后台进程和端口。 |
| [steipete/CodexBar](https://github.com/steipete/CodexBar) | provider limit/reset、spend、status polling、OAuth/API key/session 复用的桌面化呈现。 | 用于 Maddog provider 设置页：各 provider 的 auth 状态、剩余额度、reset 时间、status badge、成本扫描。 |

## P1：很有价值，但适合分阶段吸收

| 项目 | 值得借鉴的方案 | Maddog 使用方式 |
|---|---|---|
| [mksglu/context-mode](https://github.com/mksglu/context-mode) | MCP tool output sandbox、SQLite session continuity、routing enforcement、context reduction。 | 与 headroom/rtk 一起做 context 策略对照；可借其“raw data 不直接进上下文”的事件索引思想。 |
| [rtk-ai/rtk](https://github.com/rtk-ai/rtk) | CLI output proxy，压缩常见 dev commands 输出，单二进制、低延迟。 | 对 Maddog shell tool 做命令级 formatter/compressor，尤其是 test/build/log 输出。 |
| [dirac-run/dirac](https://github.com/dirac-run/dirac) | hash-anchored edits、AST manipulation、并行操作、以 context curation 提升准确度和成本效率。 | 不替换 Maddog agent loop，但可借鉴编辑稳定性：hash anchor patch、AST-aware edit、并行读取/修改计划。 |
| [colbymchenry/codegraph](https://github.com/colbymchenry/codegraph) | 本地预索引 code knowledge graph，自动同步代码变化，面向多 agent。 | 作为 Maddog codegraph 的对照实现；重点看增量更新和本地化 UI。 |
| [tirth8205/code-review-graph](https://github.com/tirth8205/code-review-graph) | 面向 review 的 persistent code map，强调减少大仓库 review 的上下文。 | 用于 Maddog review skill 的代码检索前置层，而不是每次 review 重新扫全仓。 |
| [supermemoryai/supermemory](https://github.com/supermemoryai/supermemory) | memory/context engine + app，可本地运行。 | 参考 memory API、dashboard、自托管形态；不要直接替代 Maddog session/evidence。 |
| [MemTensor/MemOS](https://github.com/MemTensor/MemOS) | self-evolving memory、hybrid retrieval、cross-task skill reuse。 | 给 C2 自改进和长期 memory 设计提供评测指标与分层记忆思路。 |
| [ComposioHQ/composio](https://github.com/ComposioHQ/composio) | 大规模 toolkit、tool search、context management、authentication、sandbox workbench。 | 重点借 auth + tool catalog UX：API key/OAuth/官方 session 的 provider 化配置。 |
| [rivet-dev/agentos](https://github.com/rivet-dev/agentos) | 轻量 VM / permission binding / agent orchestration，定位为 sandbox 替代。 | 作为高风险任务隔离方案调研；短期不并入主路径，避免破坏单体交付。 |
| [alibaba/open-code-review](https://github.com/alibaba/open-code-review) | deterministic pipelines + LLM Agent 的混合 code review，支持精确行级评论和规则集。 | 用于 Maddog `cc-review`/PR review：规则先筛、LLM 再解释，降低纯 LLM review 漏报和噪声。 |

## P2：产品/交互参考为主

| 项目 | 可借鉴点 | 注意事项 |
|---|---|---|
| [aaif-goose/goose](https://github.com/aaif-goose/goose) | Rust agent，CLI/Desktop/API 三形态，多 provider，支持 subscription auth。 | 适合作为“完整桌面 agent 产品”的参考；不要倒向另一个 host 架构。 |
| [OpenHands/OpenHands](https://github.com/OpenHands/OpenHands) | coding agents control center、local/remote/cloud backend、automation canvas。 | 借 UI/任务编排，不借大而全平台化架构。 |
| [bytedance/deer-flow](https://github.com/bytedance/deer-flow) | long-horizon harness、subagents、memory、sandboxes、message gateway。 | 借“长任务状态机”和 subagent 协作，不引入 Python runtime 到 Maddog 请求路径。 |
| [langchain-ai/deepagents](https://github.com/langchain-ai/deepagents) | sub-agent isolated context、checkpointing、tracing/eval/deployment。 | 参考概念即可；Maddog 已有 Go agent loop，不宜迁移 LangGraph 栈。 |
| [iamzhihuix/skills-manage](https://github.com/iamzhihuix/skills-manage) | 跨平台 skill library、安装/卸载、Markdown preview、collections、batch install。 | 对 Maddog 的 skill 管理 GUI 很直接：本地 skill、dynamic skill、内置 skill、marketplace 分区。 |
| [farion1231/cc-switch](https://github.com/farion1231/cc-switch) | 多 AI CLI 的 desktop all-in-one 配置与切换。 | 借 provider/profile 管理 UX；Maddog 不应支持外部 CLI 作为 host。 |
| [iOfficeAI/AionUi](https://github.com/iOfficeAI/AionUi) | 本地 cowork / multi-agent UI，支持自定义 assistants。 | 可参考“后台 agent 列表 + 状态 + 自定义 profile”的页面组织。 |
| [nexu-io/open-design](https://github.com/nexu-io/open-design) | native desktop、skills/design systems、sandboxed preview、artifact export。 | 对 artifact/preview/export 有参考价值；与 Maddog 核心 coding runtime 关联较弱。 |

## 结论

最应该进入 Maddog 后续技术计划的不是单个“大框架”，而是四个可落地组合：

1. **Provider/auth/成本组合**：LiteLLM 的 gateway 设计 + CodexBar 的 provider 状态 UI + Goose 的 subscription auth 经验。用于补齐 OpenAI/Anthropic/API key/icodeeasy/official auth 的统一配置和可见性。
2. **Context 组合**：Headroom + rtk + context-mode + Dirac。用于 tool output 压缩、shell 输出过滤、raw data 外置、hash/AST 稳定编辑。
3. **Code intelligence 组合**：codebase-memory-mcp + Serena + claude-context + CodeGraph。用于本地 codegraph、语义检索、MCP adapter、review 前置索引。
4. **Skill 自进化组合**：SkillOpt + skills-manage + open-code-review。用于 dynamic skill GUI、离线 replay eval、validation gate、规则/LLM 混合 review。

建议下一步把 P0 项拆成 Maddog 的 4 个 spike：

1. `provider-observability-spike`：实现 provider profile、budget、usage/status UI 的最小闭环。
2. `context-compression-spike`：为 shell/test/log/code search 输出加压缩策略与 token delta 统计。
3. `code-intelligence-mcp-spike`：接入一个 code search MCP，对比现有 codegraph。
4. `skillopt-replay-spike`：用现有 evidence/session 产出一个可 replay 的 skill eval bundle。

