# 跨 unit 协作：从"各查各案"到"并案"（设计方向，待实现）

> 想法讨论的沉淀，未落地。隐喻链的下一环：Clue（线索）→ Dossier（卷宗）→ Briefing（交底）→ Debrief（复盘）→ **Case Conference（碰头会）/ Case Board（案情板）**。

## 理念

一次需求变更围绕一个主题，它的几个 unit 之间必然有协作面——就像同一大案分线侦查的几个侦探，定期碰头能互通进展、避免重复跑腿。由此出发有两个候选收益：

1. **跨 unit 一致性召回**：单 unit 视角看不到的矛盾（Dockerfile 工具链版本 vs go.mod、配置默认值 vs 探活端口）是 eval 基线里最硬的漏报类别（`eval/README.md` §4.2），且恰好发生在价值最高的大改动上。chain 合并只覆盖**调用相邻**的改动函数；同主题但不相邻的 unit（同一配置键、同一常量、file/func 混合）之间今天没有任何共享面。
2. **省重复 tool call**：briefing 预载消掉了"读自己的文件"，但不同 unit 的 loop 仍可能重复执行相同的 code_search+args。

**先量化再设计**（§7 方法论）：对 post-preload 时代 68 个多 unit session（3150 次 main-task tool call）统计，**跨 unit 精确重复的 (tool, args) 只占 5.9%**，最重的 session 8-15%。结论：收益 2 是小奖品——而且缓存只省 tool 执行（grep 本来就廉价），省不掉发起它的那个 LLM 轮次；要省轮次必须让内容在模型开口前就在 context 里，那是 briefing 的地盘，不是通信机制的。**真正的奖品是收益 1（质量），不是收益 2（成本）。**

对"多 Agent 团队"的总判断（全能解 ≠ 最优解）在 ccr 语境下的收敛：

- ccr 的**执行层并行已经存在**——一个 unit 一个 loop 就是并行分工；缺的是**共享面**，不是角色化。
- 角色化的三项成本（上下文重建、文档交接衰减、流程完备 ≠ 单步质量）对分钟级生命周期的 review loop 全部成立且被放大：loop 只活 6-7 轮，没有"定期轮询 inbox"的空闲窗口，Mailbox/轮询模型假设的长驻 teammate 在这里不存在。
- 北极星检验（"是否让 unit 更一等"）：共享面应该挂在 **run 与 unit 的既有模型**上（briefing 注入、汇总 pass），而不是引入一套平行的 actor 模型（team/inbox/lock）。

## 业界扫描（2026-07）

围绕"multi-agent 能否提高 review 质量与速度"收了一轮业界证据，按结论组织：

**质量收益有三条被验证的路径，没有一条是"角色化团队"：**

1. **Generator-verifier（否证者）**：Cognition 报告 review agent 平均每 PR 抓 2 个 bug（58% severe）；CodeAgent 论文的消融显示大幅收益来自 QA-Checker 监督者（去掉它确认率 92.96%→73.23%），**而不是 CEO/CTO/Coder 那套角色扮演**；Refute-or-Promote 提出对抗式否证（kill mandates、context 不对称、跨模型 critic），且其最有效的误报杀手是**一次实证测试**而非更好的对抗话术。
2. **并行分工靠上下文分桶，不靠角色**：Anthropic 的 +90.2%（research 任务）中 80% 的方差由 token 用量解释——multi-agent 本质是"把更多 token 花在多个独立 context window 里"；商业工具中 Greptile 的形态是"代码图 + 并行 agent 各审一块"——恰是 ccr 的 unit 分桶 + codegraph 已有形态。
3. **专业化按 stage 不按人设**：AutoReview（FSE'25）的三 agent 是 Detect→Locate→Repair 流水线专业化（F1 +18.7%）；CodeRabbit 是 AST/SAST/LLM 多层管道。没有一家商业 review 工具 ship 了带 mailbox 的角色团队。

**两个直接影响碰头会设计的负面结果：**

- **辩论/多轮共识有反效果**："More Rounds, More Noise" 实测多轮 review 召回小升、精度大降，**最优轮数 = 1**；同源模型投票会在"语法合理但语义错误"的答案上收敛，共识把少数派的正确答案滤掉、把共享错误放大。
- **MAST 失败分类**（1600+ trace，14 种失败模式）：spec/设计缺陷 41.8% + agent 间失调 36.9% + 验证缺失 21.3%——协调本身就是主要失败源，且"gains on benchmarks are often minimal"；修系统设计比堆 agent 有效。

**速度**：并行 wall-clock 收益真实（Anthropic 并行 spawn + 并行 tool 省 90% 时间）但 ccr 的 unit 并发已经拿到了它；代价是 multi-agent ≈ **15× chat 的 token**——research 可以偶发地烧，review 每 PR 都跑，经济性约束严得多。Anthropic 自己的适用条件也排除了本场景："大多数 coding 任务的真并行度低于 research"、"需要共享上下文/相互依赖的任务不适合"。

**Claude Code Agent Teams**（shipped 的 lead/共享任务表/mailbox/文件锁，即本文讨论的机制原型）：官方定位是**交互式长会话**——多方面调查、竞争假设 debug、跨层协作，人做 lead 的场景；不是嵌入式 per-PR 批处理管道。与本文"Mailbox 归宿在外层编排"的判断一致。

**对本文方案的修正**：碰头会（P1）必须是"**一轮汇总 + 一轮否证**"，不是多轮辩论——否证者用 kill-mandate 姿态（默认怀疑、找反证），有条件时跨模型（routing 池现成）；这是把业界唯一稳定复现的质量增益（verifier）接到跨 unit 面上。

## 方案空间（按 ROI 排序）

| 机制 | 治什么 | 形态 | 判断 |
|---|---|---|---|
| **P1 碰头会**（cross-unit synthesis pass） | 跨 unit 一致性漏报 | 全部 loop 结束后追加**一个** loop：输入 = 各 unit 的 findings + debrief 摘要 + diff 总览，专职找跨 unit 矛盾 | 最高优先。一次额外 loop 的确定性成本，直指已知漏报；无调度耦合，feature gate 即可消融 |
| **P2 run 级 briefing 补给** | 重复搜索背后的共同缺口 | 从 debrief/session 挖"多个 unit 都在搜什么"，把高频共同查询升级为 run 级预载（repo_map 先例） | 数据驱动，等 P1 上线后攒 debrief 数据再定内容；usage-sites 已覆盖一部分 |
| **P3 案情板**（Case Board） | 早完成的 unit 给后启动的 unit 递线索 | run 级 append-only 黑板：loop 收尾写一条"给同案侦探的备忘"，后启动 unit 的 briefing 注入当前板面 | 依赖**分波调度**（先 func 后 chain/coalesce）才有时序收益，牺牲 wall-clock；等 P1 证明"共享内容有用"再评估 |
| ✗ in-run agent team（Lead/Mailbox/共享任务表/文件锁） | — | 长驻角色 + 轮询 | **不做**。轮询窗口不存在、角色交接衰减、锁与状态机的复杂度都为分钟级 loop 付不起；"context 质量 > 流程完备"的结论反对它 |

> Mailbox/共享任务列表这类机制的正确归宿是**外层编排**（devloop 级的长驻协作，跨 run、跨仓、跨天），不是一次 review run 内部。

## 关键设计（P1 碰头会展开）

1. **输入是 debrief 不是 transcript**：碰头会读各 unit 的 findings（`finding` 记录）+ debrief（clue_refs、materials、outcome）+ 文件级 diff 总览——都是 schema v2 已落盘的结构化数据，不重放各 loop 的对话（那是交接衰减）。
2. **专职找"跨"**：prompt 明确只报跨 unit 矛盾（版本/配置/契约在两个 unit 里不一致），单 unit 内的问题各 loop 已经报过——避免变成第二遍全量 review。
3. **一轮汇总 + 一轮否证，不做辩论**（业界扫描的直接结论）：碰头会产出先过一个 kill-mandate 姿态的否证者（默认怀疑、找反证，有条件时用 routing 池里的异构模型做 cross-model critic），再进评论管道。多轮共识被实证证伪（精度崩、共享错误放大），不采用。
4. **产出走既有管道**：碰头会的 finding 同样过 review-filter、打指纹、落 `finding` 记录，posterior.py 无差别消费。
5. **量测挂指标体系**：准确性侧看它对§4.2 类漏报的召回（固定回归集里有两个实锤案例可当验收样本：builder 版本 vs go.mod、健康端口错配）；成本侧它就是一个多出来的 unit（debrief 照落，`formed: conference`）；gate `cross_unit` 消融。

## References

- 漏报证据与方法论：`eval/README.md` §4.2（跨文件一致性）、§7（数据驱动优先级）
- 既有共享面：run 级 repo_map（`internal/agent/repomap.go`）、briefing（`docs/context-model.md` 关键设计 8）
- 采集面（碰头会的输入）：debrief / finding 记录（`eval/README.md` §8）
- 业界扫描来源：[Anthropic multi-agent research system](https://www.anthropic.com/engineering/multi-agent-research-system) · Cognition [Don't Build Multi-Agents](https://cognition.com/blog/dont-build-multi-agents) / [Multi-Agents: What's Actually Working](https://cognition.com/blog/multi-agents-working) · [MAST: Why Do Multi-Agent LLM Systems Fail (arXiv:2503.13657)](https://arxiv.org/abs/2503.13657) · [CodeAgent (arXiv:2402.02172)](https://arxiv.org/html/2402.02172v4) · [AutoReview (FSE'25)](https://dl.acm.org/doi/10.1145/3696630.3728618) · [More Rounds, More Noise (arXiv:2603.16244)](https://arxiv.org/pdf/2603.16244) · [Refute-or-Promote (arXiv:2604.19049)](https://arxiv.org/html/2604.19049) · [Claude Code Agent Teams docs](https://code.claude.com/docs/en/agent-teams) · [Greptile vs CodeRabbit](https://www.greptile.com/greptile-vs-coderabbit)
