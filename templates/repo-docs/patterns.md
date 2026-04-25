# Patterns

> The conventions a contributor needs in muscle memory. Don't list every pattern — only the ones that meaningfully diverge from "what you'd guess from looking at the code."

## Preferred patterns

- **<pattern name>** — when to use it, an in-repo example: `<file:line>`.

## Anti-patterns to avoid

- **<anti-pattern>** — why it's wrong here, the right alternative.

## Naming conventions

- Files: `<convention>` (e.g. `kebab-case.ts`, `snake_case.rb`)
- Classes / modules: `<convention>`
- Functions: `<convention>`
- Tests: `<convention>` (e.g. `*.test.ts`, `*_spec.rb`)
- Branches: `<convention>` (e.g. `feat/<short>`, `fix/<short>`)
- Commits: `<convention>` (e.g. `[task: ...]`, conventional commits, …)

## Structure conventions

- Where new modules go
- Where shared types/utilities live (or don't)
- Where tests live relative to source
- Anything load-bearing about folder layout

## Cross-cutting concerns

How this repo handles common cross-cutting concerns, with file pointers:

- **Logging** — `<file>` (logger setup), `<convention>`
- **Error handling** — `<convention>` (e.g. raise vs return Result)
- **Configuration / secrets** — `<convention>`
- **Feature flags** — `<file>` (if used)
- **Auth** — `<convention>`
- **Persistence access** — `<file>` (e.g. repository pattern, ORM scope)
