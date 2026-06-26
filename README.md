# case-code-review (`ccr`)

> AI-powered code review CLI — built on top of [open-code-review](https://github.com/alibaba/open-code-review) and deepened further. ｜ 中文见 [README.zh-CN.md](./README.zh-CN.md)

## What it is

`ccr` lifts the unit of review from **file** to **unit** (`diff → unit → unit context → review loop`; a file is just the degenerate unit), so each unit carries **precise spec/case context** and runs its own independent review loop.

The name: `ccr` binds each review unit to the **spec/case** (the requirement/contract a change must satisfy) — case-driven, white-box code review.

Two extension points:

- **`UnitSplitter`** (`diff → unit`): file-level by default; a language-aware impl (Go `go/ast`) splits down to function level.
- **`ContextBuilder`** (`unit → unit context`): kept lean — only cheap, bounded, known-relevant context (spec/case, rule, symbol identity). Callers/callees and other expansions are pulled on demand by the review loop's tool calls, not pre-built.

spec/case sources and per-language expression live in the separate [`spec-case`](https://github.com/qiankunli/spec-case) project.

## Status

Early WIP.

## License

Apache-2.0 (see `LICENSE` / `NOTICE`).
