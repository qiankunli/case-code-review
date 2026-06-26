# case-code-review (`ccr`)

> AI 代码评审 CLI——在 [open-code-review](https://github.com/alibaba/open-code-review) 的基础上继续深化。｜ English: [README.md](./README.md)

## 这是什么

`ccr` 把 code review 的最小作用域从**文件**抬到 **unit**（`diff → unit → unit context → review loop`，file 是 unit 的退化情形），让每个 unit 携带**精准的 spec/case 上下文**、各自起独立的 review loop。

取名 `case-code-review`：unit 是 e2e-harness `common.Case` 的**评审侧孪生**——同一份"需求/契约"资产，黑盒侧跑 harness、评审侧挂 case。

两个核心扩展点：

- **`UnitSplitter`**（`diff → unit`）：默认文件级；语言相关实现（Go `go/ast`）切到函数级。
- **`ContextBuilder`**（`unit → unit context`）：精干原则——只放 spec/case、rule、函数身份等廉价有界的上下文；caller/callee 等展开留给 review loop 按需 tool call，不预建。

spec/case 的来源与各语言表达由独立项目 `spec-case` 维护（与 e2e-harness 同源）。

## 状态

早期 WIP。

## License

Apache-2.0（见 `LICENSE` / `NOTICE`）。
