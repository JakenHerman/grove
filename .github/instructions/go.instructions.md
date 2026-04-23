---
applyTo: "**/*.go"
---

# grove — Go code instructions

## Before you suggest Go code

1. Check whether the symbol you are changing is public (capitalized,
   in `grove.go`, `solver.go`, `sensitivity.go`, `export.go`, or
   `reader.go`). If it is, the change is user-visible and the docs
   **must** be updated in the same PR — see `AGENTS.md` for which
   guide page owns which surface.
2. Keep the pure-Go pitch intact: no new runtime dependencies, no
   CGO outside `//go:build highs`.

## Required before commit

- `gofmt -w .` — mandatory; CI enforces `gofmt -d .` producing no
  diff.
- `go vet ./...` — must be clean.
- `go test ./...` — must pass.
- For solver/benchmark-adjacent changes, also run
  `go test -bench . -run=^$` and sanity-check the numbers.

## Conventions

- Match the existing structure: public API in `grove.go`, solver in
  `solver.go`, sensitivity reporting in `sensitivity.go`, file I/O
  in `export.go` / `reader.go`.
- GoDoc comments on exported identifiers start with the identifier
  name, per standard Go style.
- **Do not** prefix new public symbols with `// Stable.` before
  v1.0. Those markers are added all at once in the v1.0 epic (#27).
- Solver math comments should reference sections of Bertsimas &
  Tsitsiklis, *Introduction to Linear Optimization*, when relevant.
  That is the textbook grove models itself on.
- Test files mirror the file under test (`foo.go` ↔ `foo_test.go`).
  Prefer table-driven tests.

## What not to do

- Do not introduce goroutines, channels, or concurrency into the
  solver hot path without an explicit issue discussing it.
- Do not change `grove.WarnLPRelaxationOnly` semantics outside of
  issue #17.
- Do not add narrating comments like `// increment i`. Comments
  should explain *why*, not *what*.
- Do not add a new public symbol without also adding (or updating)
  coverage in the corresponding `_test.go` file.
