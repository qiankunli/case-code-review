# case-code-review (`ccr`)

> AI-powered code review CLI — built on top of [open-code-review](https://github.com/alibaba/open-code-review) and deepened further. ｜ 中文见 [README.zh-CN.md](./README.zh-CN.md)

## Philosophy

A diff alone is too little to review well — it can't tell whether a change breaks the requirement it serves or the code that depends on it. ccr builds on open-code-review (ocr) with two ideas:

**1. Capture more context.** ccr locates the *functions* a diff changes, then gathers the context the diff doesn't carry:

- the function's **caller / callee neighborhood** — who depends on it, what it relies on; and
- the **spec / case / rule / link** the author attached to the function — its contract, scenarios, review criteria, and curated "see also".

**2. Review per *unit*, with a merge step.** A *unit* is the scope of one review loop. ocr only reviews per **file**; ccr makes the unit the granularity, on a ladder: **function → class → file → module / directory**. From a diff ccr first collects the changed units (functions), then **merges** them — by a strategy — into the units that actually trigger loops, coarsening *up that ladder* as the change grows: a file's many changed functions become one file unit; a sweeping change becomes file- or even directory-level units. So focus is highest for small changes and cost stays bounded for big ones — one loop per merged unit.

The payoff: ccr finds the bugs that need background — a change quietly breaking a caller's assumption, or violating an invariant the diff doesn't show — checklist-checked against the function's real contract. (Syntax stays lint's job.)

## The four context dimensions

Authored on the function, in the separate [`spec-case`](https://github.com/qiankunli/spec-case) project (Go markers / Python decorators):

| dimension | answers |
|---|---|
| **spec** | the function's contract (what it must guarantee) |
| **case** | concrete scenarios to verify |
| **rule** | review criteria — what to watch for |
| **link** | curated "see also" — a doc, or another function to keep consistent |

Context is kept **lean**: bounded, known-relevant context is injected up front; broader expansion (callers, callees, linked docs) is fetched on demand during review.

## Status

Early WIP. Function-level splitting (Go + Python), the cost governor, and spec/case/rule/link injection are in; deeper context (walking callers up to the governing spec) is on the roadmap.

## License

Apache-2.0 (see `LICENSE` / `NOTICE`).
