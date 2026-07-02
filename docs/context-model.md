# Context 模型：Clue / ClueFinder / Relation / Dossier

> 设计 spec——`unit-model.md` 的「clue 后置」那半截：一个 unit 拿到后**怎么收评审素材**。`unit-model` 管 `diff → fragment → unit`（作用域），本文管 `unit → dossier`（证据）。落地锚点见 References；具体字段以代码为准。

## 理念 / 概念

一句话：**unit → 沿 typed relation 找「相关符号」→ 对每个符号取 clue → 去重汇成 dossier 喂 review loop。**

四个词（破案隐喻：点滴线索汇成卷宗，loop 据此办案）：

- **Clue** — 挂在**符号**上的一条评审证据。`kind: spec | case | rule | link | doc`（另有 run 级的 `history`）。两种来源、同一类型：
  - *authored*：`spec/case/rule/link`，作者共置在符号上，specgen 抽进 `spec.json`。
  - *derived*：`doc`，从符号源码抽的 docstring / doc 注释（Python + Go）。
- **Relation** — `unit 的符号 → 相关符号` 的有向、有类型边：`self | owner | caller | callee | used`（callee ⊇ 类/类型）。
- **ClueFinder** — unit 级接口（`Find(u Unit) []Clue`）。它是**组合结果、不是原语**：内部由两条正交轴拼成——
  - **关系轴** `RelationCollector`：`unit → [{symbol, relation}]`（self/owner/used 各一个 collector；caller/callee 走 call-graph walk）。
  - **来源轴**（符号 → clues）：查 spec index 取 authored marks + 读源码抽 derived doc，按到达它的 relation 打标。
- **Dossier** — 一个 unit **去重后**的 clue 集合（每条带 kind × relation），review loop 的证据输入。

两轴正交的意义：加一种关系或加一种来源**互不牵动**，不会相乘出一排新 finder。

## 流程

```
diff ─Splitter→ Fragment ─Merger(callchain + file)→ Unit
     → related = ⋃ RelationCollector(Unit)            # 关系轴: unit → [{symbol, relation}]
     → clues   = ⋃ cluesFor(symbol)  按 relation 打标   # 来源轴: authored marks + derived doc
     → dedup → Unit.Dossier → review loop（逐条核对契约守恒）
```

**唯一不变量：最终 dossier 去重**。收 clue 与 merge 的先后无关——碎片级收还是 unit 级收都行，末尾 dedup 让结果幂等：同 `(relation, kind, text)` 合一；指向**本 unit 成员**的边丢弃（chain unit 内成员 A 不把成员 B 当自己的 callee/used clue）。唯一真正的先后依赖在别处：Group 的 call-chain 策略需要「diff 内部调用邻接」这项 relation 查询——那是 merge 的输入，与收 clue 无关。

## 符号身份（join key）

- `symbol-id = <relpath>::<sym>` —— **仓内** key（spec index 的主键）。
- `fqn`（Python 点号 import 路径 / Go `importpath.Sym`）—— **跨仓** key。
- 仓内命中用 symbol-id；跨仓（依赖）命中用 fqn：`used` 关系先解析引用文件的 import（Python from-import / Go `pkg.Symbol` 选择子）到 fqn 精确匹配（消歧同名类型），再退裸名 fallback。

## 关键设计

1. **两轴正交（Relation × 来源）**——为何：否则每个 `(关系 × 来源)` 格子一个 finder，横竖重复（"找 used 符号""读 docstring""查 index"各被抄多份）。`ClueFinder` 保留为组合层；原语是 collector 与 per-symbol 取数。

2. **doc 是第 5 种 clue kind，但不进 spec.json**——为何：`spec.json` 保持 *curated*（authored）；docstring 由 ccr 运行时从源码抽取才 **adoption-free**——依赖没标注 spec-case，只要有 docstring / doc 注释就能吃到它的契约。抽象上它就是一种 clue，处理上与其它 kind 同一条路。

3. **关系分工 = 契约守恒三问**（见 workspace `docs/spec-aware-review.md` §1.3）：`self` 改的本体；`owner` 所属作用域的不变量（改方法必须看到类级 rule——否则类级 marker 只在"整类被改"时 fire，而那几乎不发生）；`caller` 上溯治理 spec（Q2 破没破原始需求）；`callee/used` 依赖的契约（Q3 影响面）。

4. **静态注入 vs loop tool 的界**：一步定界、高信号的进**静态 clue**（owner、直接一跳 used/callee 的 marks+doc、caller 上溯到的 spec）——关键契约不能指望模型自觉去查；开放式探索（走 N 层 caller、扫兄弟 func、深挖类型全貌）留 **loop tool**。判据：能一步定界进静态，要"探索"留 tool。

5. **门禁**：self 关系的 spec/rule/link 随 feature gate（消融用）；`owner/used` 恒开——廉价（文件读 + map 查，无 LLM/grep）且属正确性信号（authored 的用法约束必须 honor）；gate 只给**费边**（caller/callee 的 call-graph grep）做 leave-one-out。

6. **跨仓 = spec 随包发（Model A）+ 读依赖源码 doc**：依赖把自己的 `spec.json`（entry 带 fqn）打进包，ccr 审消费仓时发现并入（Go 按 go.mod requires 查 `GOMODCACHE`，Python 扫 venv site-packages），合并在**最低优先级**（本仓覆盖依赖）；docstring 直接读依赖源码（venv / 本仓）。

7. **可测性**：dry-run 的 `clue_coverage` 是 **relation × kind 矩阵**（键 `owner/rule`、`used/doc`、`caller/spec`…），每格在真实 diff 上 fire 多少免费可见——消融对比（feature gate）之外的常开观测层。

## References

- `diff → fragment → unit`：[`unit-model.md`](unit-model.md)
- 评审语义（契约守恒三问、caller 上溯 spec、聚合）：workspace `docs/spec-aware-review.md`
- 实现锚点：`internal/spec/related.go`（RelationCollector / RelatedFinder / SelfGates）、`internal/spec/{py,go}doc.go` + `docstring.go`（derived doc）、`internal/spec/deps.go`（依赖 spec 发现）、`internal/callgraph/walk.go`（caller/callee 走图 + 邻居 doc）、`internal/unit/clue.go`（Clue/Relation/Dossier 类型）
- 资产侧（symbol-id / fqn / marker / specgen，Go + Python）：`spec-case`
