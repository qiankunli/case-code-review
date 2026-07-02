# AGENTS.md — case-code-review (`ccr`)

## 项目定位与边界

`ccr` 是**函数级、契约守恒**的 AI code review CLI，基于 [open-code-review (ocr)](https://github.com/alibaba/open-code-review) 地基重写、独立演进（衍生归属见 `NOTICE`）。两个理念支柱（详见 `README.md`）：

1. **捕获更多 context**——从 diff 定位到改动**函数**，收集它的 caller/callee 邻域 + 作者附着的 **spec/case/rule/link**。
2. **按 *review unit* 触发 review loop**——unit 是评审作用域，粒度是一条阶梯（函数 → 类 → 文件 → 模块/目录）。

> **与 ocr 的本质区别 = 把 unit 当一等概念**。一切按 unit 走：`--spec`(契约) / `--rule`(准则) / `--history`(上轮评审反馈) 都是按 symbol-id 注入的 **per-unit 上下文**，`Clue`/`ClueFinder` 把 context 挂到 unit。ocr 停在 file 级、没有 unit。判断一个新能力是否"对味"，就看它有没有让 unit 更一等。

**边界**：ccr 是**消费侧引擎**。spec/case/rule/link 资产的**定义、各语言写法、`spec.json` schema、symbol-id 契约**，以及产 `spec.json` 的 **`specgen` 抽取器**（Go + Python 参考实现），都在独立项目 [`spec-case`](https://github.com/qiankunli/spec-case)，**不在 ccr**。ccr 只消费 `spec.json` + 现场解析函数边界。

## 代码地图与核心模块

```
case-code-review/
├── cmd/ccr/        CLI 入口：review/scan/config/… 子命令；组装 Args、加载 spec.json
└── internal/
    ├── unit/       ★ 两类型两阶段：`Fragment`（原子，Splitter 产：Go go/ast、Python python3）→ Merger 归并成 `Unit`（评审作用域，WatermarkMerger）；context 抽象 Clue / ClueFinder（merge 后挂 `Unit.Clues`）。详见 `docs/unit-model.md`
    ├── spec/       ★ 消费 spec.json：SpecFinder/RuleFinder/LinkFinder 把 spec/case/rule/link 找成 Clue（廉价 finder）
    ├── history/    ★ 消费 --history（上轮评审 findings，symbol-id keyed）：Finder 挂成 ClueHistory，渲染成"核验是否已修"的 prompt（廉价 finder；评审反馈闭环的消费侧）
    ├── callgraph/  ★ caller/callee 邻域上下文（昂贵 finder）：CallerFinder（上溯 governing spec）/ CalleeFinder（下探依赖契约），共享有界 walk（深度 2、每分支停在最近带 spec 的邻居），git grep + go/ast / python3，Go+Python
    ├── agent/      ★ 评审编排：split→找 Clue（廉价 + 按预算闸门的昂贵 finder）→merge→每 review unit 一个 loop；按 Clue 渲染上下文；file 级 review-filter；`--dry-run` 只装配上下文、不调 LLM
    ├── diff/       diff/hunk 解析、评论行号解析
    ├── llmloop/    agentic 评审 loop（复用 ocr 引擎）
    ├── config/     模板 prompt、rule.json、tools 配置
    └── model · gitcmd · session · telemetry · tool · scan · viewer …   支撑模块
```

**主链路**：

```
diff ─Splitter─▶ Fragment ─Merger─▶ Unit ─ClueFinder 找 Clue(spec/case/rule/link + caller/callee)─▶ review loop
```

> 即：Splitter 把每个文件 diff 切成 `Fragment`（一函数一个 + 残余）→ Merger 归并成 `Unit`（评审作用域）→ 各 ClueFinder **对 Unit** 找 Clue 挂到 `Unit.Clues`（spec/case/rule/link 廉价直查；caller/callee 经 call-graph 上溯/下探，深度 2、Go+Python）→ 一个 Unit 一个 review loop。context 后置（对最终作用域收一次）。
>
> **Clue / ClueFinder**：context 抽象的三件——找的动作（`ClueFinder.Find(u Unit) []Clue`，对评审作用域 Unit 找）、找的结果（`Clue{Kind, Text, Ref}`，Text 内联 / Ref 按需指针）、挂哪（`Unit.Clues`，merge 后收）。加一类 context = 加一个 finder，不动主链路。

## 关键约定（核心四条）

1. **评审语义 = 契约守恒，不是找语法 bug**：核对 diff 有没有破坏函数的 spec/case/rule 不变量；**语法 / 静态检查交给 lint 类工具**（Python `ruff`、Go `go build`/`go vet` 之类），不是 ccr 的活。
2. **diff unit vs review unit 别混**：Splitter 从 diff 切出 diff unit；Merger 归并成 review unit（触发 loop 的那个，可能是单个、也可能是几个沿阶梯归并的更粗 unit）。成本超水位按文件归并——**降 loop 粒度、不降 context**。
3. **边界现场算、`spec.json` 只语义**：函数边界评审时现场解析（`go/ast`/`python3`）、**永不落盘**（不 stale）；`spec.json` 只有 `FuncID → spec/cases/rules/links`、**无行号**；join key 是 symbol-id `<relpath>::<symbol>`（与 spec-case 一致）。
4. **上下文分廉价 / 昂贵两档，重活有闸**：廉价 finder（spec.json 查 spec/case/rule/link）总跑；昂贵 finder（caller/callee 的 call-graph grep）走**预算闸门**——diff unit 数超水位就跳（反正要归并、per-func 上下文也被稀释）。link 指向的 doc/函数**内容**仍按需 tool 取，不预塞。

> 另：Go 改动先 `go build ./...` / `go test ./...` 再提交。

## References

- 理念：`README.md` · `README.zh-CN.md`
- spec/case/rule/link 资产、各语言写法、`spec.json` schema、symbol-id 契约、**产 `spec.json` 的 `specgen`**（Go + Python）：[`spec-case`](https://github.com/qiankunli/spec-case)
- 查覆盖 / 调试：`ccr review --dry-run` 打印每个 review unit 装配的上下文，不调 LLM（端到端：marker → specgen → spec.json → `--dry-run`）
- Unit 模型：`Fragment` 原子 + `Unit` 作用域、两条合并轴（call-chain 语义 / file 成本）、clue 后置——`docs/unit-model.md`
- Context 模型：unit → dossier——`Clue`(kind: spec/case/rule/link/doc) × `Relation`(self/owner/caller/callee/used) 两轴正交、doc 运行时抽取（adoption-free）、symbol-id 仓内 / fqn 跨仓、依赖 spec 随包发——`docs/context-model.md`
- 上游归属（Apache-2.0 衍生）：`NOTICE`
