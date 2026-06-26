# AGENTS.md — case-code-review (`ccr`)

## 项目定位与边界

`ccr` 是**函数级、契约守恒**的 AI code review CLI，基于 [open-code-review (ocr)](https://github.com/alibaba/open-code-review) 地基重写、独立演进（衍生归属见 `NOTICE`）。两个理念支柱（详见 `README.md`）：

1. **捕获更多 context**——从 diff 定位到改动**函数**，收集它的 caller/callee 邻域 + 作者附着的 **spec/case/rule/link**。
2. **按 *review unit* 触发 review loop**——unit 是评审作用域，粒度是一条阶梯（函数 → 类 → 文件 → 模块/目录）。

**边界**：ccr 是**消费侧引擎**。spec/case/rule/link 资产的**定义、各语言写法、`spec.json` schema、unit-id 契约**都在独立项目 [`spec-case`](https://github.com/qiankunli/spec-case)；产 `spec.json` 的 **specgen 抽取器不在 ccr**（在 spec-case / 被测仓）。ccr 只消费 `spec.json` + 现场解析函数边界。

## 代码地图与核心模块

```
case-code-review/
├── cmd/ccr/        CLI 入口：review/scan/config/… 子命令；组装 Args、加载 spec.json
└── internal/
    ├── unit/       ★ Unit 两阶段（Splitter→diff unit：Go go/ast、Python python3；Merger→review unit：WatermarkMerger 归并）+ context 抽象 Clue / ClueFinder / Unit.Clues
    ├── spec/       ★ 消费 spec.json：SpecFinder/RuleFinder/LinkFinder 把 spec/case/rule/link 找成 Clue
    ├── agent/      ★ 评审编排：split→ClueFinder 找 Clue→merge→每 review unit 一个 loop；按 Clue 渲染上下文；file 级 review-filter
    ├── diff/       diff/hunk 解析、评论行号解析
    ├── llmloop/    agentic 评审 loop（复用 ocr 引擎）
    ├── config/     模板 prompt、rule.json、tools 配置
    └── model · gitcmd · session · telemetry · tool · scan · viewer …   支撑模块
```

**主链路**：

```
diff ─Splitter─▶ diff unit ─ClueFinder 找 Clue(spec/rule/link；caller/callee 待加)─▶ Merger(并 Clue) ─▶ review unit ─▶ review loop
```

> 即：从 diff 切出 diff unit → 各 ClueFinder 为它找 Clue（挂到 `Unit.Clues`：spec/case/rule/link 已接；caller/callee 待加，依赖 call-graph）→ 归并成 review unit（成员 Clue 取并集）→ 一个 review unit 一个 review loop。
>
> **Clue / ClueFinder**：context 抽象的三件——找的动作（`ClueFinder.Find(u) []Clue`）、找的结果（`Clue{Kind, Text, Ref}`，Text 内联 / Ref 按需指针）、挂哪（`Unit.Clues`）。加 caller/callee 只是再加 ClueFinder，主链路不动。

## 关键约定（核心四条）

1. **评审语义 = 契约守恒，不是找语法 bug**：核对 diff 有没有破坏函数的 spec/case/rule 不变量；**语法 / 静态检查交给 lint 类工具**（Python `ruff`、Go `go build`/`go vet` 之类），不是 ccr 的活。
2. **diff unit vs review unit 别混**：Splitter 从 diff 切出 diff unit；Merger 归并成 review unit（触发 loop 的那个，可能是单个、也可能是几个沿阶梯归并的更粗 unit）。成本超水位按文件归并——**降 loop 粒度、不降 context**。
3. **边界现场算、`spec.json` 只语义**：函数边界评审时现场解析（`go/ast`/`python3`）、**永不落盘**（不 stale）；`spec.json` 只有 `FuncID → spec/cases/rules/links`、**无行号**；join key 是 unit-id `<relpath>::<symbol>`（与 spec-case 一致）。
4. **上下文精干、重活按需**：spec/case/rule 与 link **指针**预注入；caller/callee、link 指向的 doc/函数**内容按需** tool 取，不预塞。

> 另：Go 改动先 `go build ./...` / `go test ./...` 再提交。

## References

- 理念：`README.md` · `README.zh-CN.md`
- spec/case/rule/link 资产、各语言写法、`spec.json` schema、unit-id 契约：[`spec-case`](https://github.com/qiankunli/spec-case)
- 上游归属（Apache-2.0 衍生）：`NOTICE`
