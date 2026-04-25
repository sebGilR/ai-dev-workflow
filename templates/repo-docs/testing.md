# Testing

> What "tested" actually means in this repo, and how to run the right tests fast.

## Test stack

- Unit: `<framework>` (e.g. RSpec, Vitest, pytest, Go test)
- Integration: `<framework>`
- End-to-end / browser: `<framework>` (e.g. Playwright)
- Coverage threshold: `<%>` (or "none enforced")

## Fast feedback

```
# entire test suite
# fastest meaningful subset (single file or focused tag)
# single test by name
# watch mode
```

## Integration / end-to-end

```
# how to run integration suite (e.g. requires local DB / Docker stack)
# what setup is required (docker compose up?)
```

## What MUST have tests

- New public API surfaces
- Bug fixes (regression test for the bug)
- Critical-path business logic (e.g. payment, auth, tenant isolation)
- Migration scripts that have data side-effects

## What CAN ship without tests

- One-off scripts in `scripts/`
- Pure refactors with no behavior change (rely on existing suite passing)
- Throwaway prototypes

## Test data / fixtures

- Where fixtures live: `<path>`
- Who owns regenerating them: `<process>`
- Anti-pattern: don't hand-edit binary fixtures; regenerate them via `<command>`.

## Mocks vs real systems

What gets mocked, what runs against the real thing. Be explicit — many bugs hide in this seam.

- DB: `<mocked? real? in-memory?>`
- External APIs: `<mocked at HTTP layer / SDK layer / real with VCR>`
- Time / randomness: `<frozen? seeded?>`
