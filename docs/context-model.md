# Context 模型：Clue / ClueFinder / Relation / Dossier

> 设计 spec——`unit-model.md` 的「clue 后置」那半截。`unit-model` 管 `diff → fragment → unit`（作用域）；本文管一个 unit 拿到后**怎么收评审素材**。落地锚点见末尾 References；具体字段以代码为准。

## 理念

一句话：**unit → 沿 typed relation 找「相关符号」→ 每个符号取 clue → 汇成 unit 的 Dossier → 一 Dossier 一 review loop。**

两条**正交轴**，别拍平成一维 finder：

- **关系轴**（找哪些符号）——`Relation`
- **来源轴**（符号上有啥）——`ClueFinder`

四个词（破案隐喻：点点滴滴的线索 = `Clue`，汇成一份案卷 = `Dossier`，loop 据此破案）：

- **Clue** — 挂在符号上的一条评审证据。`kind: spec | case | rule | link | doc`。两种来源、同一类型：
  - *authored*：`spec/case/rule/link`，作者共置在符号上，specgen 抽进 `spec.json`。
  - *derived*：`doc`，从符号源码抽的 docstring。
- **ClueFinder** — `符号 → []Clue`（来源轴）。实现：`SpecJSON`（authored）、`Docstring`（derived）。
- **Relation** — `unit 的符号 → 相关符号`（关系轴），有向、有类型：`self | owner | caller | callee | used`（callee ⊇ class）。`RelationCollector` 产出。
- **Dossier** — 一个 unit 的**去重后 clue 集合**，每条带 `(relation, 源符号)`。它是 `Relation × ClueFinder` 的**汇集结果，不是原语**；是 review loop 的证据输入。

## 流程

```
diff ─Splitter→ Fragment ─Merger(callchain + file)→ Unit
     → related = ⋃ RelationCollector(Unit)                                # 关系轴: unit → [{symbol, relation}]
     → Dossier = dedup{ (clue, relation, sym) | {sym,rel}∈related, clue∈⋃ClueFinder.Find(sym) }   # 来源轴 + 去重
     → review loop（逐条核对契约守恒）
```

**唯一不变量：Dossier 去重。** 收 clue 与 merge 的先后无关——碎片级收还是 unit 级收、都行，只要汇进 Dossier 时 dedup 让结果幂等：

- 同 `(clue, symbol)` 合一（跨 fragment 重复的合掉）。
- 丢掉指向**本 unit 成员**的边（chain unit 内成员 A 不把成员 B 当自己的 callee/used clue——它已在 unit 内）。

（唯一真正的先后依赖在别处：**Group 的 call-chain 策略需要「diff 内部调用邻接」这一项 relation 查询**来决定谁跟谁归组——那是 merge 的输入，与 clue 收集无关。）

## 符号身份（join key）

- `symbol-id = <relpath>::<sym>` —— **仓内** key。
- `fqn`（Python 点号 import 路径 / Go `importpath.Sym`）—— **跨仓** key。
- Clue 挂在符号上；仓内命中用 symbol-id，跨仓（依赖）命中用 fqn。

## 关键设计

1. **两轴正交（Relation × ClueFinder）**——为何：加一种关系或加一种来源互不牵动。否则每个 `(关系 × 来源)` 格子一个 finder，横竖重复（"找 used 符号""读 docstring""查 Index"各被抄多份）。`Dossier` 是二者的汇集结果，不再是原语。

2. **doc 是第 5 种 clue kind，但默认从源码抽、不进 spec.json**——为何：`spec.json` 保持 *curated*；docstring 走 ccr 运行时抽取才 **adoption-free**（依赖没标注 spec-case 也能吃到它的契约）。抽象上它就是一种 clue，处理上与其它 kind 同一条路（`Docstring` ClueFinder）。

3. **关系分工 = 契约守恒三问**（见 `docs/spec-aware-review.md` §1.3）：`owner` = 所属作用域的不变量；`caller` = 上溯到治理 spec（Q2 破没破原始需求）；`callee/used` = 依赖的契约（Q3 影响面）；`self` = 改的本体。

4. **静态注入 vs loop tool 的界**：能一步定界、高信号的进 **Dossier**（owner、直接一跳的 used/callee 的 clue、caller 上溯到的 spec）；开放式探索（走 N 层 caller、扫兄弟 func、深挖某类型全貌）留 **loop tool**。判据：一步定界进静态，要"探索"留 tool。

5. **跨仓 = spec 随包发（Model A）+ 读依赖源码 docstring**：依赖把自己的 `spec.json`（以 fqn 为主键）打进包；ccr 审消费仓时发现并入（Go 走 `GOMODCACHE`，Python 走 venv site-packages），docstring 直接读依赖源码。合并成一个**双键 Index**。

6. **可测性**：dry-run 的 `clue_coverage` 升级成 **关系 × 来源** 矩阵（Dossier 里 owner/used/caller × spec/doc/… 各多少），每格在真实 diff 上 fire 多少免费可见。gate 只给**费边**（caller/callee 的 call-graph grep）做 leave-one-out；owner/used/clue 便宜且属正确性 → 默认 on、不 gate。

## References

- `diff → fragment → unit`：[`unit-model.md`](unit-model.md)
- 评审语义（契约守恒三问、caller 上溯 spec、聚合）：workspace `docs/spec-aware-review.md`
- 资产侧（symbol-id / fqn / clue marker / specgen，Go + Python）：`spec-case`
