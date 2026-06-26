# case-code-review (`ccr`)

> AI 代码评审 CLI——在 [open-code-review](https://github.com/alibaba/open-code-review) 的基础上继续深化。｜ English: [README.md](./README.md)

## 理念

孤立的 diff 不足以评审好——它答不了这次改动**有没有破坏它服务的需求**、**有没有影响依赖它的代码**。ccr 在 open-code-review (ocr) 之上做两件事：

**1. 捕获更多 context。** ccr 从 diff 定位到改动的**函数**，再为它收集 diff 本身不带的上下文：

- 函数的 **caller / callee 邻域**——谁依赖它、它依赖谁；以及
- 作者**附着在函数上的 spec / case / rule / link**——它的契约、场景、审查准则、策展的 "see also"。

**2. 按 *unit* 触发 review loop，而非按文件。** ocr 按**文件**触发一个 review loop；ccr 提出 **unit**——一个函数是一个 unit，文件是退化的 unit——**按 unit 触发一个 loop**。每个改动函数各自评审、聚焦，不被同文件里不相干的改动稀释。改动牵动很多函数时，ccr 超过阈值把一个文件的函数**归并回一个 loop**，成本有界。

落到实处：ccr 找的是**需要背景才找得到**的 bug——一次改动悄悄破坏了某个 caller 的假设、或违反了 diff 看不见的不变量——对着函数的真契约**逐条核对**。（语法仍归 lint。）

## 四类上下文

作者写在函数上，由独立项目 [`spec-case`](https://github.com/qiankunli/spec-case) 维护（Go marker / Python decorator）：

| 维度 | 答什么 |
|---|---|
| **spec** | 函数的契约（它必须保证什么） |
| **case** | 要核对的具体场景 |
| **rule** | 审查准则——评审时盯什么 |
| **link** | 策展的 "see also"——一篇 doc，或另一个要保持一致的函数 |

上下文走**精干**原则：有界、确定相关的预先注入；更大的展开（caller、callee、链接的 doc）评审时**按需取**。

## 状态

早期 WIP。函数级切分（Go + Python）、成本 governor、spec/case/rule/link 注入已落地；更深的上下文（沿 caller 上溯到治理它的 spec）在路线图上。

## License

Apache-2.0（见 `LICENSE` / `NOTICE`）。
