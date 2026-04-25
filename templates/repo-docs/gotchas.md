# Gotchas

> Things that have bitten this repo's contributors and that aren't obvious from reading the code. Format: short title → what happens → how to diagnose / fix.

## Local environment traps

- **<title>** — what the trap is, the symptom, the fix.

## Common failure modes

- **<failure>** — when it happens, how to recognize it (logs / error message), how to recover.

## Safety notes

Operations that need extra care or explicit confirmation:

- **<operation>** — why it's risky, what to check before running it.

## Cross-repo footguns

If this repo depends on or interacts with sibling repos / external services, list the integration-points where things go wrong:

- **<integration>** — symptom, root cause, fix.

## Substitution / config traps

If renaming a variable or env var requires updating it in multiple places, list those places. Consistency traps cause hours of debugging.

- **`<VAR_NAME>`** — must match in: `<file:line>`, `<file:line>`, `<file:line>`.
