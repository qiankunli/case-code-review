# AGENTS.md — case-code-review (`ccr`)

## 项目定位与边界

`ccr` 是**函数级、契约守恒**的 AI code review CLI，基于 [open-code-review (ocr)](https://github.com/alibaba/open-code-review) 地基重写、独立演进（衍生归属见 `NOTICE`）。两个理念支柱（详见 `README.md`）：

1. **捕获更多 context**——从 diff 定位到改动**函数**，收集它的 caller/callee 邻域 + 作者附着的 **spec/case/rule/link**。
2. **按 *review unit* 触发 review loop**——unit 是评审作用域，粒度是一条阶梯（函数 → 类 → 文件 → 模块/目录）。

**边界**：ccr 是**消费侧引擎**。spec/case/rule/link 资产的**定义、各语言写法、`spec.json` schema、unit-id 契约**都在独立项目 [`spec-case`](https://github.com/qiankunli/spec-case)；产 `spec.json` 的 **specgen 抽取器不在 ccr**（在 spec-case / 被测仓）。ccr 只消费 `spec.json` + 现场解析函数边界。

## 代码地图与核心模块

- `cmd/ccr/` — CLI 入口（review / scan / config / provider / rules / viewer 等子命令）；组装 Args、加载 spec.json、构建工具注册表。
- `internal/unit/` — **Unit 抽象 + 两阶段**：`Splitter` 把一个文件 diff 切成 **diff unit**（`AutoSplitter` 路由 Go `go/ast` / Python `python3`，否则退 file）；`Merger` 把 diff unit 归并成 **review unit**（`WatermarkMerger` 超水位按文件 coalesce，保留 spec context）。
- `internal/spec/` — 消费 `spec.json`（unit-id → spec/cases/rules/links），渲染成评审上下文（SpecBuilder/RuleBuilder/LinkBuilder 的逻辑）。
- `internal/agent/` — 评审编排：`split → merge → 每个 review unit 一个 loop`，注入 spec/case + rule（合并 rule.json）+ link（指针）；review-filter 按**文件**级后处理。
- `internal/diff/` — diff/hunk 解析与评论行号解析；`internal/llmloop/` — agentic 评审 loop（复用 ocr 引擎）；`internal/config/` — 模板 prompt、`rule.json`、tools 配置。
- 其余 `model`/`gitcmd`/`session`/`telemetry`/`tool`/`scan`/`viewer` 等为支撑模块。

## 关键约定

- **评审语义 = 契约守恒**：核对 diff 有没有破坏函数的 spec/case/rule 不变量；**语法交给 lint，不是 ccr 的活**。
- **两阶段术语别混**：**diff unit**（Splitter 从 diff 发现，一函数一个）vs **review unit**（Merger 归并后、真正触发 loop 的；可能是单个 diff unit，也可能是几个沿阶梯归并成的更粗 unit）。
- **函数边界评审时现场算、永不落盘**（Go `go/ast`、Python `python3`，新鲜、不 stale）；**`spec.json` 只有语义**（FuncID → spec/cases/rules/links），**无行号**。
- **unit-id = `<relpath>::<symbol>`**——specgen 与 ccr 共用的 join key（与 spec-case 一致）。
- **上下文精干**：spec/case/rule 与 link **指针**预注入；caller/callee、link 指向的 doc/函数**内容按需** tool 取，不预塞。
- **成本有界**：review unit 数超水位（`WatermarkMerger`）按文件归并——**降 loop 粒度、不降 context**（coalesce 取 spec 并集）。
- **公开仓**：不得引用任何内网仓 / 机器相关绝对路径。
- **Go 改动后**先 `go build ./...` / `go test ./...` 再提交。

## References

- 理念：`README.md` · `README.zh-CN.md`
- spec/case/rule/link 资产、各语言写法、`spec.json` schema、unit-id 契约：[`spec-case`](https://github.com/qiankunli/spec-case)
- 上游归属（Apache-2.0 衍生）：`NOTICE`
