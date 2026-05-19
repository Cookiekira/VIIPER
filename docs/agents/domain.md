# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root
- **`docs/adr/`** for architectural decisions related to the touched area

If any of these files don't exist, proceed silently.

## File structure

Single-context repo:

```
/
├── CONTEXT.md
├── docs/adr/
└── src/
```

## Use the glossary's vocabulary

When naming domain concepts in issues, tests, refactor proposals, and diagnostics, use the terms defined in `CONTEXT.md` and avoid drifting to conflicting synonyms.

## Flag ADR conflicts

If proposed work contradicts an ADR, surface the conflict explicitly instead of silently overriding.
