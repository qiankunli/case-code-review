# case-code-review (`ccr`)

> AI-powered code review CLI — built on top of [open-code-review](https://github.com/alibaba/open-code-review) and deepened further. ｜ 中文见 [README.zh-CN.md](./README.zh-CN.md)

## Why

Most AI code review reads a diff in isolation. It can flag style or a local bug, but it can't tell whether a change **breaks the requirement it serves** or **the code that depends on it** — those answers need context the diff doesn't carry.

ccr's bet: **review a change together with the context bound to it.**

## How it works

```
diff → function → gather the function's bound context → coalesce by file if too many → one review loop per unit
```

1. **Function, not file.** A diff is split into the *functions* it changes (Go and Python today; other files fall back to file scope). Each changed function gets its own focused review loop — not diluted by unrelated changes in the same file.

2. **Find the bugs that need context.** ccr *is* hunting bugs — but the ones that matter aren't local syntax (that's lint's job); they're a change that quietly breaks a caller's assumption or violates an invariant the diff doesn't show. Finding those needs background. The contract (spec/case) makes the question concrete — *does this change still satisfy what the function must guarantee?* — so it's checklist-checking against real contracts, not open-ended guessing.

3. **Four kinds of context** — authored on the function, injected when its change is reviewed:

   | dimension | answers |
   |---|---|
   | **spec** | the function's contract (what it must guarantee) |
   | **case** | concrete scenarios to verify |
   | **rule** | review criteria — what to watch for |
   | **link** | curated "see also" — a doc, or another function to keep consistent |

   Context is kept **lean**: bounded, known-relevant context is injected up front; broader expansion (callers, callees, linked docs) is fetched on demand during review.

4. **Bounded cost.** When a change touches many functions, ccr coalesces a file's functions back into one review loop above a threshold — trading per-function focus for fewer LLM calls, never dropping the gathered context.

The four context dimensions and their per-language authoring (Go markers / Python decorators) live in the separate [`spec-case`](https://github.com/qiankunli/spec-case) project.

## Status

Early WIP. Function-level splitting (Go + Python), the cost governor, and spec/case/rule/link injection are in; deeper context (walking callers up to the governing spec) is on the roadmap.

## License

Apache-2.0 (see `LICENSE` / `NOTICE`).
