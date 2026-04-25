# Architecture

> Single source of truth for "what this repo is and how it's shaped." Keep it short and concrete — link out to longer docs rather than restating them.

## Repo purpose

What this repo is for, in 2-3 sentences. What problem it solves. Who uses it. What it explicitly is *not*.

## Position in larger system

If this is one repo of several (e.g. multi-repo platform), name the siblings and the relationships:

- This repo: `<name>` — deploys to `<host/node>`
- Sibling A: `<name>` — deploys to `<host/node>`, this repo calls it via `<protocol>` (sync? async? degradable?)
- Upstream system of record: `<name>` — direction of data flow
- Downstream consumers: `<list>`

## Main components

The 3-7 components that matter most. Per-component:

- **`<component>`** — purpose, language/framework, entry point path, key dependencies.

## Key data flows

The 2-3 flows a new contributor needs to understand end-to-end:

1. **<flow name>** (e.g. "incoming order"): `<source> → <step> → <step> → <sink>`. Code paths: `<paths>`.

## Boundaries

- What lives in this repo
- What lives elsewhere (and where)
- Hard rules (e.g. "no synchronous calls to <X>", "no shared state with <Y>")

## Decisions and trade-offs

ADRs in `docs/adr/` cover individual decisions. Reference the most load-bearing ones here:

- ADR ###: `<title>` — one-sentence summary.

## Pointers

- Detailed module docs: `<paths>`
- Runbooks / ops: `<paths>`
- External: link to design doc / Notion / dashboards.
