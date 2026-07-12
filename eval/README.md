# eval — ccr 评审效果评估

把"ccr review 得好不好、快不快"变成一个可重复跑的流程：以 **trajectory（ATIF）为一等评估对象**，而不是只看最终 comment。本目录沉淀方法论与配套脚本；单次评估的产物（ATIF、总表、判定结论）留在评估工作区，不入库。

## 理念：两条正交评估轴

| 轴 | 问题 | 判据来源 |
|---|---|---|
| **质量** | 报的每条 finding 对不对、值不值；该报的漏没漏 | findings 对照真实 diff 逐条求证 + 独立复审找漏报 |
| **效率** | unit 有没有多余的；tool call 有没有多余的 | trajectory 的客观信号（空搜索、重复读、轮数、截断） |

两轴独立评：一单 review 可以又准又慢（质量高效率低），也可以快而空转（低成本交付 clean 但漏报）。合并成单一分数会把这两种病症混在一起。

对应 AGENTS.md「三追求 × 三抓手」的三追求：质量轴 = 准确性，效率轴 = 成本；第三追求**健壮性**（loop 真跑完）由 §3 的截断/超时/未收尾信号观测——wrap-up 上线后优先直读 `unit_incomplete` warning。

## 流程

```
~/.casecodereview/sessions/<repo>/*.jsonl        ← ccr review 自动落盘
        │
        ▼  python3 eval/collect.py --repo <repo> --out <dir> [--since DATE]
<dir>/*.atif.jsonl + SUMMARY.md + comments.json   ← 每 session 一条 ATIF、per-unit 总表、全部 comment payload
        │
        ├─▶ 效率轴: python3 eval/trajectory_judge.py <file>.atif.jsonl --no-llm
        │           objective pass：每链 rounds/duration/tool_freq/empty_searches/repeated_reads
        │           （可选 LLM judge：去掉 --no-llm，按 taxonomy 给 categories+evidence）
        │
        └─▶ 质量轴: 人 / agent 拿着 comments.json（或 PR 上实际交付的 findings）+ 真实 diff，
                    每条 finding 判 important / minor / debatable / wrong；
                    再独立 review 同一 diff，列 ccr 漏掉的重要问题（missed）
```

质量轴目前靠人或通用 agent 执行（fan-out：每 agent 判 2-3 个 PR），判定纪律见下节；它天然难全自动化——判 finding 真伪本身就是一次 review。

## 关键设计（方法论与经验）

### 1. 为什么用"后验信号"做质量 ground truth

一条 finding 是否 important，最硬的证据是**作者后来做了什么**：后续 PR 专门修了它 → important 实锤；ccr 没报的问题被后续 PR 修掉 → missed 实锤。评估时优先沿 PR 链找这类证据，再对无后验的条目做代码求证。（实例：#9 报的 health 端口错配 → 作者开 #10 专修；#9/#10 都没报的 builder 版本 vs go.mod 错配 → 作者在 #11 自己撞上才修，坐实连续两单漏报。）

### 2. 判定纪律

- finding 判定必须查代码求证，不能只读 finding 文本顺着信；
- 漏报判定**保守**：只列确信重要的（正确性/并发/安全），风格不算；
- pre-existing 问题不算本单漏报（diff 没碰它）；
- "clean 且确实无问题"是合格结论，不因交付 0 条扣分——要区分"正确的沉默"和"空转的沉默"。

### 2.5 复测操作法（treatment 后怎么再测）

- **同工作负载重放**：历史 PR 用 merge commit 的双亲重建评审范围——`ccr review --from <merge>^1 --to <merge>^2`。分支已合并时 merge-base 会退化成分支自身，必须用 merge 双亲。**已固化为 `eval/replay.py`**：corpus（冻结的评审范围集，如 `eval/corpus/ccr-self.json`）× arms（feature gate 配置）矩阵重放，按 CCR_EVAL_TAG 捞回 transcript，聚合 finding（指纹精确 + path/symbol 宽松两档匹配）与 debrief 成本出对比报告——默认关 gate（如 `typed_briefing`）的"翻转许可证"走这里。
- **组合效果 vs 单项归因**：多个治理项同时上线时，端到端复测只能给**组合**效果；某一项"零触发/零变化"不等于没用（实例：repo_map 在源头消灭瞎猜后，search-suggest 一次都没触发——它退化成保险网，不是失效）。单项归因一律走 feature gate 消融（`--feature x=off` 的 dry-run json 或真跑）。
- **引擎类改动比内容不比计数**：换解析引擎（如 grep→typed graph）时 coverage 计数往往不变，变的是 clue **指向哪些符号**——从 dry-run json 的 `spec_cases`/`see_also` 文本抽 symbol-id 集合做对比，噪声符号（同名撞车、测试函数混入）一眼可见。
- **自举信号**：ccr review 自己的改动 PR，其 findings 是免费的质量样本——它抓过自己代码里的死 API 和仓库惯例违例；被评估工具评估自己，既是 dogfood 也是回归观测。

### 3. objective 信号怎么读（症状 → 病因方向）

| 信号 | 阈值感 | 指向 |
|---|---|---|
| empty_searches / code_search 比例高 | 整体 >1/3 就不正常 | agent 在猜符号名——上下文没给够（clue 缺口）或 search 工具描述/语义不清 |
| repeated_reads（同文件读 ≥2 次） | 链内出现即可疑 | prompt 已注入的内容没被利用，或 memory compression 把它丢了 |
| 长链无 task_done（500s+ 截断） | 出现即问题 | 撞 ConcurrentTaskTimeout / 轮数预算；这条链的结论不可信（可能没评完）。wrap-up 上线后 ccr 会自打 `unit_incomplete` warning——**优先直读 warnings**，trajectory 推断只作交叉验证 |
| 琐碎链（≤2 tools 直接 task_done） | 数量多则粒度问题 | unit 切得过细，或该 unit 本不值得独立 loop（成本 ≈ 一次全托确认） |
| 产 comment 链占比 | 参考值见基线 | 绝大部分 loop 花在"确认无问题"上——保障价值 vs 成本的权衡入口 |

### 4. 已知失败模式（按危害排序）

1. **大 PR 空转**：改动越大、并发语义越重，review 越容易交付 clean——恰好在价值最高处失效。信号：大 diff + 0 finding + 多条截断链。
2. **跨文件一致性漏报**：单 unit 视角看不到的矛盾（Dockerfile 工具链版本 vs go.mod、配置默认值 vs 探活端口）。这正是 spec/link 上下文要补的洞。
3. **细微并发窗口漏报**：锁释放间隙的 TOCTOU、同租户操作缺串行化。模型盯着单函数看不见跨函数时序。
4. **教科书事实错套真实路径**（误报型）：引用的底层事实是真的，但在实际执行路径上不成立（如 glibc getenv first-match 对 raw exec 成立，对中间隔了一层 bash last-wins 的链路不成立）。这类误报最难被 review-filter 拦住，因为论证看起来很扎实。
5. **凑数 finding**：给上一条 finding 打补丁的"附注型" finding（如"若采纳需加 import"），独立价值为零。

### 5. 首轮基线（2026-07-03，mono-sandbox/hostel，13 PR）

日期戳快照，只作对比锚点，不代表当前行为：

- 规模：13 session / 73 unit chain / 511 tool calls / prompt 3.9M tok；单 session p50 ≈ 500s。
- 效率：**49% 的 code_search 空结果**；57 次重复读；14/73 链未以 task_done 收尾（含 4 条 550s+ 截断）；8 条琐碎确认链；**12/73 链产出 comment**。
- 质量（4 组独立复判，12 个实质 PR）：交付 19 条 finding = **important 4 / minor 9 / debatable 4 / wrong 2**；4 条 important 全部被作者后续行动证实（后验实锤）；4 单"正确的沉默"，1 单"空转的沉默"（当天最大的并发 PR 交付 0 条且漏真竞态）。
- 漏报实锤：跨文件 build-breaker（builder 版本 vs go.mod）连续两单漏过；大 PR 的锁窗口竞态；unauthenticated 端点新增信息暴露。
- 结论倾向：precision 可用但非零误报（2/19，其一是 §4.4 型）；主要短板在大 PR 召回与跨文件一致性——优先投上下文（spec/link/caller 图与跨 unit 汇总面），而不是调 prompt。

### 6. 首轮治理复测（2026-07-03，同日）

针对基线病灶的第一轮治理（search-suggest #73 / repo_map #74-76 / 截断 wrap-up #77 / typed graph #81）后，重跑基线最差三单（#6/#11/#17）：

- **空搜索 32/53（60%）→ 0/25**；code_search 次数减半，总 tool call 降 1/3，prompt token 净降 10%。suggestion 一次未触发——L1 在源头消灭了瞎猜，L0 退化为保险网（预期分工）。
- typed graph 的 dry-run A/B（零 LLM）：`Shell.Run` 邻域 grep 模式 8 个符号 5 个错（同名撞车+测试函数），typed 模式精确 3 个——契约上溯基线从碰运气变权威。
- 方法论验证：**基线（量化病灶）→ 治理 → 同工作负载复测**，单日闭环；n=1 的波动性 caveat 记录在案。

### 7. 第二轮治理：trajectory 统计驱动 briefing 预载（2026-07-03）

方法论增量：不止"读单链症状"（§3），还可以**跨 session 聚合 tool call、按 unit scope 分桶、抽样查询内容归类**，直接回答"剩余成本花在哪"，再让数据替直觉排优先级。

- **统计口径**：扫 `~/.casecodereview/sessions/` 全部 main_task 链，按 scope（func/file/callchain）统计每 unit 的 tool 频次；file_read 再按"读自己的文件 vs 别的文件 × 整读 vs 区间读"四分；code_search 抽样看查询串归类。
- **发现**（source-preload #67 上线后 ~340 unit）：
  - code_search 是剩余大头（file 单元 4.1 次/unit），抽样归类后大多是**改动符号的使用点扫描**（callers/字面量引用），且常一次改动拆成多连搜；
  - **直觉被数据反转**：优化最初瞄准 callchain 特化，但 callchain 只占 unit 的 4% 且已是搜索最少的 scope——最大蛋糕是 type-agnostic 的 usage 扫描；
  - 预载 budget miss 仅 7%，但集中在恰好最核心的大文件上（`agent.go` 等），miss 单元贡献了大量文件内搜索；
  - 前后对照要**分桶看**：预载对整读有效（own/full 0.47→0.12 次/unit），对区间读几乎无效（own/ranged 不降）——"上线了"不等于"每个症状都治了"。
- **落地**：PR #86（usage-sites 预 grep / 大文件 ranged 降级 / callchain 邻居函数体，三个独立 gate）；设计见 `docs/context-model.md` 关键设计 8。
- **复测待做**：gate 消融（`usage_sites/ranged_preload/neighbor_source` leave-one-out）+ 同工作负载重放（§2.5），看 code_search 次数与空搜率的边际变化。

### 8. 采集面：Debrief 与 run manifest（session schema v2）

指标体系"结果 × 归因"的数据来源，原则：**写盘时顺手记当时才知道的事，别指望事后从 trajectory 反推**（unit 怎么形成、策略性跳过 vs 故障、briefing 降级——这些事后推不出来）。

- **run manifest**（session_start / session_end）：schema_version、tool_version、features 全量、params（watermark/预算）、diff 规模——趋势必须 join 的混杂因子；`CCR_EVAL_TAG` 环境变量给 run 打 population 标（固定回归集 vs 滚动生产，两者不可比）。
- **Debrief**（每 unit 一条，loop 收尾写；Briefing 进去、Debrief 出来）：
  - 健壮性 → `outcome`（completed/truncated/timeout/llm_error/**skipped_policy**——governor 决定和故障分开算）+ `degradations`（token guard 丢 related/own source）；
  - 粒度 → `formed`（func/file/**coalesce**/chain——Scope 区分不了"天然整文件"和"被成本轴并粗"）+ 行数；
  - context → `clues`（relation×kind 计数）+ `clue_refs`（symbol-id 列表，比内容不比计数）+ `materials` 逐条结局 + `usage_sites` 条数；
  - 成本 → rounds/tool_calls/tokens（cache 拆开）/duration，从 scope 的 llm 记录聚合，eval 不用再手写统计脚本。
- **Finding**（每条最终交付的 finding 一条记录，过滤后写；transcript 里的 code_comment tool call 是过滤前的，会多算）：`fingerprint`（path+content 的短 hash，**刻意不含行号**——relocation 和后续编辑会挪行，指纹的职责是让复跑复现的同一 finding join 到同一人工标注）+ `symbol_id` + 行区间；配 manifest 的 `git_head` 锚点，后验扫描从这里向前走 git 历史（"后续 commit 是否改了 finding 指过的行/符号" → important 实锤 / 漏报候选）。
- **后验扫描脚本**：`eval/posterior.py <session.jsonl|dir> [--labels out.jsonl]`——按锚点（diffCommit > diffTo > git_head）对每条 finding 走 git line-log，判 `line_touched`（实锤候选，附 commit）/ `file_touched` / `untouched`；候选仍需人工对照 commit 与 finding 文本确认（§2 判定纪律）。首次自验：dogfood session 的两条 os.Stderr finding 精确指回修它们的 commit。

### 8.5 人工标注统一约定：ccr:fp footer + ccr:label 回复（2026-07-12）

质量轴 ground truth 的第二来源（第一是 §1 后验实锤）：**人在 finding 出现的地方顺手打标**，工具负责把标注收回 fingerprint 键的 labels jsonl。三件套：

- **可关联**：任何 surface（GitHub/GitLab 行级评论、汇总列表）发布 finding 时，正文末尾带
  `ccr:fp=<fingerprint>` footer（inline 评论用 `<sub>` 包裹防碍眼）。fingerprint 即 §8 的
  path+content 短 hash——评论、session finding、复跑重现的同一 finding 由它 join。
- **打标语法**：对该评论**回复**一行 `ccr:label=<verdict>`（可跟自由文本理由），verdict 词表
  与 §2 判定纪律一致：`important | minor | debatable | wrong`（`wrong` 即误报）。放开头，
  剩余正文自动作为 note 收录。**纪律：处理 PR comment 时每条都标、不只标误报**——采纳的标
  important/minor、不采纳的标 wrong/debatable——只标一侧会让 labels 集有偏。
- **回收**：`eval/labels.py github <owner>/<repo> <pr> [--out f.jsonl]`——识别 ccr 评论
  （fp footer，退化匹配 "devloop code-review" 头）、收人类回复的 label，产出
  `{fingerprint, label, note, path, line, source, at}` 行。**labels 是跨 run 复用的 ground
  truth 资产，入库**（`eval/labels/<owner>-<repo>.jsonl`，与"产物不入库"的 run 输出不同）；
  posterior 的后验 label 与人工 label 在 eval 汇总时按 fingerprint 合并，人工优先。

设计取舍：用**回复文本**而不是 emoji reaction——reaction 无词表、跨 forge API 不一致、不可携带理由；文本标记 grep 即得，且回复线程天然挂在 finding 上。误报样本（`wrong`）积累后喂 review-filter 的回归集：§4.4 型"教科书事实错套"（首例：PR 评审中"sort_keys 只排顶层"——实际 `json.dumps(sort_keys=True)` 递归排序）。

### 9. typed_briefing 翻转许可证（2026-07-05，replay.py 首次全量应用）

`eval/replay.py` 对 ccr-self corpus（6 PR × base/typed × 2 runs，70 unit/arm）的判定：

- **健壮性（决定性）**：timeout 截断 9 → 3（llm_error +1）；#93 最典型——base 5 个 timeout、typed 0。机理：预载成为 File 消息后参与 dedup/evict，context 压力下少跑 LLM 压缩、少撞 600s 墙。
- **成本**：prompt token 6.23M → 6.02M（-3.3%，且 base 被 timeout 天花板低估）；rounds 473 vs 462，持平。冒烟那个 -61% 未复现——单小 commit 的偶然，再证 n=1 不可信。
- **准确性**：finding 层噪声主导（同 arm 两 run 指纹零重合），无系统性回退证据；两 arm 各抓到一条 evict 家族真问题（base 抓到的快照竞态经人工核实为真 → 修复 PR，**回归集自审首次产出实锤 bug**）。
- 方法论注：run 间 finding 零重合说明质量轴在小 corpus 上只能判"无回退"、不能判优劣——精细质量对比需要更大 corpus 或按 finding 家族聚合。

### 10. reviewbench：corpus × arms 的 worksheet 评测编排（2026-07-12）

replay.py 的升级形态（`eval/reviewbench/`，独立 uv 项目）：编排骨架换成 case-harness 的
eval_harness——Experiment/Env（对照臂）× corpus（case=merged PR）铺成 Worksheet 大表，
reconciler 缺啥补啥（几百个 500s 的 run，断点续跑是刚需），报表 pivot 出 per-arm 对比。
三条评测线共用：gate 消融（Env 换 `config.features`）、ccr vs ocr（Env 换 `config.engine`）、
全 PR 回放打 metric。

```bash
cd eval/reviewbench && uv sync
# corpus：本地 clone 枚举 merged PR（merge 双亲，docs-only 不入册）
uv run python -m reviewbench.corpus_build <repo-path> --limit 30 --out ../corpus/<name>.json
# 一键三臂消融（review_team off/on/on+post_bulletin）
uv run python -m reviewbench.run experiments/gates-review-team.yaml
# ccr vs ocr
uv run python -m reviewbench.run experiments/ccr-vs-ocr.yaml
```

- 质量轴 = `judge_precision`（LLM 对照真实 diff 逐条判 findings 真伪，§2 判定纪律进
  prompt；0 finding 弃权——正确沉默的对错归 posterior 漏报扫描）；judge 模型从
  `EVAL_JUDGE_BASE/_KEY/_MODEL` 读，未配置时弃权不报错。judge 是第一道近似，
  §8.5 的人工标注按 fingerprint 校准它。
- 成本/健壮性 = measurement 通道（findings 数、duration_s、engine_failed），不进加权分。
- 每 solve 打 `CCR_EVAL_TAG=reviewbench:<env>:<case>:<uuid>`，session transcript 仍可按
  tag 捞回做 debrief 级分析（replay.py 的口径不丢）。
- corpus json 入库（`eval/corpus/`）时不带机器路径，repo 用 `--repo` / yaml 指定。
- 依赖 case-harness（git 源）；replay.py（stdlib-only、debrief 级聚合）保留，两者互补。

## References

- ATIF 导出：`ccr export --format atif <session.jsonl>`（session 落盘位置见 `internal/session/`）
- 失败分类法与 per-chain 判定：`eval/trajectory_judge.py` 顶部 docstring
- 固定回归集重放：`eval/replay.py`（corpus × arms）· corpus 定义：`eval/corpus/`（merge 双亲，仓库相对，可入库；跑出的产物不入库）
- 后验扫描：`eval/posterior.py`（finding → line_touched/file_touched/untouched）
- 人工标注回收：`eval/labels.py`（forge 回复 `ccr:label=` → labels jsonl）· 标注资产：`eval/labels/`
- 评测编排：`eval/reviewbench/`（corpus × arms → worksheet → 报表；experiments/ 有三臂消融与 ccr-vs-ocr 模板）
- unit / context 模型：`docs/unit-model.md` · `docs/context-model.md`
