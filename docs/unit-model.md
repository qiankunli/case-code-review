# Unit 模型：Fragment 原子 + Unit 作用域

> 设计 spec。落地锚点见末尾 References；具体字段/阈值以代码为准。

## 理念

ccr 区别于 ocr 的一等概念是 **unit**——评审的作用域。一次 review loop 跑在一个 unit 上。它有两个层级、两个类型、两个流水线阶段：

- **Fragment（原子）**：单文件的一段变更——一个函数的 hunks，或函数外的残余。Splitter 产出。**纯数据**：无 context、无分组。
- **Unit（作用域）**：1..N 个 Fragment 按某条轴成组 + 收齐的上下文（Clues）。Merger 产出，**review loop 真正跑的东西**。

历史术语对照：旧的 *diff unit* → **Fragment**；*review unit* → **Unit**。两者拆成独立类型，各自演进——Fragment 是 Splitter 的稳定原子，只有 Unit 会长（文件粗化 → 调用链 → 未来类/模块）。

## 流程

```
diff ─Splitter─▶ Fragment（每文件：每函数一个 + 残余一个）
     ─Merger（两步顺序）─▶ Unit
          ① call-chain（语义轴）：跨文件、调用相邻、且都在 diff 里的 Fragment 成一簇
          ② file-coalesce（成本轴）：残余若超水位，按同文件并
     ─Finders 对 Unit 收 Clue（spec/case/rule/link · caller/callee · history）─▶ review loop
```

func unit = 1 Fragment；file 粗化 = 1 Fragment（多符号、整文件 diff）或同文件多 Fragment；call-chain = N 个跨文件 Fragment。

## 关键设计

1. **Fragment 原子 / Unit 作用域，两类型**——为何：类型级区分 diff 与 review（拿到哪个一眼清、传错即编译错），且各自独立演进。Fragment 不背 context/grouping，那些是 Unit 的。

2. **Clue 挂作用域、在 merge 之后收**——为何：context 是"这次一起审的范围"的属性。对最终 Unit 收一次，比旧的"在原子上收、合并时 union"少一道；且成员去重天然（已知整簇成员，caller/callee 命中本簇成员就跳过）。

3. **调用图两种用途，别混为一谈**：
   - *adjacency*（给 merge 决定分组）：轻量问"changed X 是否调 changed Y"。
   - *context*（给 Clue）：问"governing spec / 依赖的契约"。
   两者都用 call-graph，但目的、产物不同。

4. **merge = 两步顺序，不是框架**——为何：现仅两轴（语义/成本）。README 的粒度阶梯（类/模块）等**第三条真实轴落地**再泛化。YAGNI：不建 pass-pipeline / 注册表，就先 call-chain 再 file-coalesce 顺序写。

5. **跨文件 Unit 的落地**：reviewUnit 渲染**所有 Fragment 的 diff**（按文件分块、带文件头）；评论已 per-comment 自带路径，**评论路由不改**；change-files 列表遍历成员路径；行数 = 成员求和。

6. **命名**：`Unit` 留给作用域（贴合"unit 是评审作用域"的北极星）；原子叫 `Fragment`。

## 触发的合并轴（语义 vs 成本）

- **call-chain（语义）**先做：`X 调 Y`、两者都在本次 diff → 它们的改动在交互，合在一起审才看得见交互 bug。只合**都变了的**相邻；只一个变的邻居走 context 注入（不合并）。
- **file-coalesce（成本）**收尾：剩下的若 Unit 数仍超水位，同文件并粗——降 loop 数、不降 context。
- adjacency 计算是 costly 的，沿用现有预算闸门：unit 太多就跳过语义合并、退回文件粗化。

## References

- 实现锚点：`internal/unit`（`Fragment` / `Unit` / `Splitter` / `Merger`）、`internal/agent`（`reviewUnit`、finder 装配与 clue 收集时机）、`internal/{spec,callgraph,history}`（finders）。
- 上层定位：`AGENTS.md`（unit 一等概念）、`README.md`（理念）。
