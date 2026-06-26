# case-code-review (`ccr`)

> AI 代码评审 CLI——在 [open-code-review](https://github.com/alibaba/open-code-review) 的基础上继续深化。｜ English: [README.md](./README.md)

## 为什么

多数 AI 代码评审只看孤立的 diff。它能挑出风格或局部 bug，却答不了这次改动**有没有破坏它服务的需求**、**有没有影响依赖它的代码**——这些答案需要 diff 本身不带的上下文。

ccr 的赌注：**带着改动绑定的上下文来评审这次改动。**

## 怎么做

```
diff → 函数 → 收齐这个函数绑定的上下文 → 函数过多则按文件归并 → 一个 unit 一个 review loop
```

1. **按函数，不按文件。** diff 被切成它实际改动的**函数**（目前支持 Go 和 Python；其它文件退回文件级）。每个改动函数各起一个聚焦的 review loop——不被同文件里不相干的改动稀释。

2. **契约守恒，而非找 bug。** 语法是 lint 的活、不是 ccr 的。ccr 问的是：*这次改动还满足治理这个函数的契约吗？* 契约是具体的，所以评审变成**逐条核对 checklist**，而不是漫无边际地推理。

3. **四类上下文**——作者写在函数上，评审它的改动时注入：

   | 维度 | 答什么 |
   |---|---|
   | **spec** | 函数的契约（它必须保证什么） |
   | **case** | 要核对的具体场景 |
   | **rule** | 审查准则——评审时盯什么 |
   | **link** | 策展的 "see also"——一篇 doc，或另一个要保持一致的函数 |

   上下文走**精干**原则：有界、确定相关的预先注入；更大的展开（caller、callee、链接的 doc）评审时**按需取**。

4. **成本有界。** 一次改动牵动很多函数时，ccr 超过阈值就把一个文件的函数**归并回一个 review loop**——拿"每函数聚焦"换更少的 LLM 调用，但**不丢已收的上下文**。

四类上下文及其各语言写法（Go marker / Python decorator）由独立项目 [`spec-case`](https://github.com/qiankunli/spec-case) 维护。

## 状态

早期 WIP。函数级切分（Go + Python）、成本 governor、spec/case/rule/link 注入已落地；更深的上下文（沿 caller 上溯到治理它的 spec）在路线图上。

## License

Apache-2.0（见 `LICENSE` / `NOTICE`）。
