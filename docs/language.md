# Language：源码事实边界

## 理念 / 概念

`internal/language` 是 ccr 唯一的源码语言边界。它把不同解析后端收敛成稳定的语言事实：语言、symbol-id、definition/span、call、reference、doc、依赖根与质量等级。`unit`、`codegraph` 消费这些事实，不感知 AST、query capture、编译器进程或 grammar。

这个边界解决的不是“少写几个 switch”，而是让解析技术可以独立演进：当前后端与未来 gotreesitter 都只能在该包内替换，使用方不增加 backend flag，也不按语言分支。

当前结构化后端覆盖 Go、Python、TypeScript/JavaScript；allowlist 中的其它语言仍可评审，但在对应后端落地前保持 file-scope 降级。

## 流程

```text
Source ──Analyzer──▶ Analysis(definitions/calls/references)
                       ├──▶ unit：diff → Fragment
                       ├──▶ codegraph：caller/callee 解析与 walk
                       └──▶ briefing/comment tagging：span / enclosing symbol

Source + diff snippet ──ReferencesIn──▶ bare name / FQN / dependency source
                                         └──▶ spec used relation

Repository ──ScanRepository──▶ RepositoryIndex ──adapter──▶ codegraph.Extraction

Go repository ──semantic overlay──▶ resolved CallGraph ──▶ codegraph.TypedGraph
```

解析失败和不支持的语言仍按原有契约降级：unit 回到 file scope，repo-map 忽略缺失的语言后端，caller/callee 回到启发式或无结果。

## 关键设计

### language 只拥有事实，不拥有评审策略

`Fragment` / `Unit`、PageRank、spec walk、Clue 与 prompt 都不进入 language。转换放在了解两边模型的消费方：例如 codegraph 将 `RepositoryIndex` 转成自己的 `Extraction`，method bare-name alias 仍是排名策略。

### symbol-id 属于语言边界

`<relpath>::<symbol>` 是所有语言定义的共同身份，也是 unit/spec/codegraph 的 join key。由 language 统一构造和拆解，避免基础事实反向依赖 unit。

### 后端实现不得泄漏

公开事实模型不暴露 parser tree/node、gotreesitter query capture 或语言编译器对象。生产代码中的解析器依赖由边界测试限制在 `internal/language`；未来新增语言应增加 grammar/query 与契约 fixture，而不是在消费包新增 AST walker。

同样，扩展名集合、symbol owner/bare-name、语言可见性、定义搜索语法、注释、import/reference 解析与依赖根发现都属于这个边界。消费方可以决定读取什么资产、如何 grep、排名或生成 clue，但不能自行解释源码语言或生态布局。
