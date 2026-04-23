# AGENTS.md — working on grove

This file is for humans *and* for AI coding agents (Cursor, Codex,
Claude Code, Aider) picking up work on this repo. Read it once at the
start of every session.

**grove's one-line pitch:** a pure-Go linear-programming library with
an ergonomic named-variable DSL, a built-in two-phase simplex solver,
and zero CGO. Everything in this file exists to keep that pitch true.

---

## The north star rule: docs and code move together

Every PR that changes a public surface **must** update every
doc artifact that currently references that surface. No exceptions.
PRs that update code without updating docs (or vice versa) should be
bounced.

There are four doc artifacts for this project and they all live in
this repo:


| Artifact            | Role                                                                                                                  | Audience                                                        |
| ------------------- | --------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| `README.md`         | GitHub repo landing + pkg.go.dev package description + terminal/IDE preview.                                          | Someone who just clicked into the repo or imported the package. |
| `ROADMAP.md`        | Versioned plan of record. Source of truth the docs site cites.                                                        | Anyone deciding whether to adopt grove.                         |
| `docs/index.html`   | Marketing landing page at [https://jakenherman.github.io/grove/](https://jakenherman.github.io/grove/).               | Someone who followed a link.                                    |
| `docs/guide/*.html` | Narrative reference docs at [https://jakenherman.github.io/grove/guide/](https://jakenherman.github.io/grove/guide/). | Someone using grove.                                            |


Nothing else is "the docs." If you write a design note, it becomes an
issue comment or a guide page, never a new `.md` floating in the repo.

---

## What to update when you change X

Use this table as your sync checklist.


| If you change…                           | Update                                                                                                                                                                                                                        |
| ---------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A public type/function in `grove.go`     | `docs/guide/modeling.html` or `docs/guide/solving.html` (whichever owns it), `docs/guide/api-reference.html`, and — only if it changes the quickstart — `README.md`.                                                          |
| Solver behavior in `solver.go`           | `docs/guide/solving.html` (How it works) and `docs/guide/sensitivity.html` if duals/reduced costs are affected. Regenerate any printed example outputs on that page.                                                          |
| Sensitivity output format                | `docs/guide/sensitivity.html` (the sample output block must match).                                                                                                                                                           |
| File I/O (`export.go` or new readers)    | `docs/guide/file-io.html` and `docs/guide/api-reference.html`.                                                                                                                                                                |
| Solver options / the `Solver` interface  | `docs/guide/solving.html` and `docs/guide/file-io.html` ("Swappable solvers").                                                                                                                                                |
| Integer/MIP behavior                     | `docs/guide/integer-variables.html` and — until v0.4 retires it — the "One thing to know…" section of `README.md`.                                                                                                            |
| Add a new guide page under `docs/guide/` | Add an entry to the `MANIFEST` in `docs/guide/assets/docs.js` (slug + title + href) in the correct reading order — this drives sidebar, prev/next, and TOC automatically. Add a row to the docs-pointer table in `README.md`. |
| Landing-page messaging                   | `docs/index.html` only. Never duplicate marketing copy into `README.md`.                                                                                                                                                      |
| Release bullets (finish / add / reword)  | `ROADMAP.md`. When a release ships, flip the header from "Target: Qx" to "Status: **shipped.**" and check every sub-bullet.                                                                                                   |
| Examples directory (`examples/`)         | Cross-check the summary in `docs/guide/examples.html` and the list in `README.md`.                                                                                                                                            |
| Any test, benchmark, or `go.mod`         | No doc update required unless the change is user-visible (e.g. raising the minimum Go version — that's in `README.md` and `docs/guide/index.html`).                                                                           |


---

## Repository layout

```
.
├── grove.go              Public API: Problem, Var, Expr, Constraint, Result.
├── solver.go             Two-phase revised simplex. The LP oracle.
├── sensitivity.go        SensitivityReport and its formatter.
├── export.go             LP/MPS writers. (v0.2 adds readers.)
├── highs_stub.go         Placeholder that errors until v0.3 turns it on.
├── examples/             Runnable example programs: scheduling, allocation, diet.
├── bench_test.go         Benchmarks. v1.0 adds a regression harness around this.
├── docs/
│   ├── index.html        Landing page. Shipped to GH Pages from this folder.
│   ├── guide/            Narrative docs. Hand-rolled static; no build step.
│   │   ├── assets/
│   │   │   ├── docs.css  Design system for the guide site.
│   │   │   └── docs.js   Chrome injection (sidebar, TOC, prev/next). Has the MANIFEST.
│   │   └── *.html        One page per topic. Each has a bare <article data-doc-slug="..."> and docs.js fills the chrome.
├── README.md             Short. Pointers into docs/guide/.
├── ROADMAP.md            Source of truth for the release plan.
├── AGENTS.md             This file.
└── LICENSE               MIT.
```

The zero-build ethos matters: `docs/` is served as-is by GitHub Pages
from `main:/docs`. If a change to the docs would require a bundler,
transpiler, or new `package.json` under `docs/`, push back or escalate.

---

## Non-negotiables

1. **No new runtime dependencies.** grove's whole pitch is `go get` and
  you're solving LPs. `go.mod` should stay empty (or close to it) for
   the pure-Go path. Test-only imports are fine with judgement.
2. **No CGO except under a build tag.** v0.3 introduces HiGHS behind
  `//go:build highs`. That is the *only* sanctioned cgo path. Any
   other cgo use needs an architecture discussion first.
3. **Never delete `README.md` or `ROADMAP.md`.** They each do jobs the
  docs site can't:
  - `README.md` is the only artifact pkg.go.dev, `gh repo view`, and
  IDE-import-hover ever see.
  - `ROADMAP.md` is cited as the canonical roadmap by both the
  landing page and `docs/guide/roadmap.html`.
4. **No bundlers, frameworks, or build steps under `docs/`.** The
  chrome is injected by a single vanilla JS file. Adding Docusaurus,
   Vite, webpack, or equivalent is out of scope without a separate
   plan.
5. **Don't hand-edit the sidebar, TOC, or prev/next on individual
  guide pages.** They're all generated by `docs/guide/assets/docs.js`
   from the `MANIFEST` constant. Update the manifest; don't fight the
   chrome.
6. **Don't change `grove.WarnLPRelaxationOnly` semantics outside of
  issue #17.** It's load-bearing for v0.1–v0.3 users and has a
   scheduled retirement in v0.4.

---

## Development commands

```bash
go test ./...              # unit tests
go test -run TestFoo ./...
go test -bench . -run=^$   # benchmarks
gofmt -w .                 # MANDATORY before commit
go vet ./...

go run ./examples/scheduling
go run ./examples/allocation
go run ./examples/diet

python3 -m http.server 8000 -d docs    # preview docs locally
# then open http://localhost:8000/ and /guide/
```

A PR must pass `go test ./...`, `gofmt -d .` (no diff), and
`go vet ./...`. CI enforces this.

---

## Issue tracking, milestones, and release discipline

Work is tracked on GitHub with a **milestone per release** and an
**epic issue per release** that holds the checklist of subtasks.


| Release | Epic issue | Milestone |
| ------- | ---------- | --------- |
| v0.2    | #24        | v0.2      |
| v0.3    | #25        | v0.3      |
| v0.4    | #26        | v0.4      |
| v1.0    | #27        | v1.0      |


The epic issues are labeled `type:epic`. Each subtask issue lives in
the matching milestone and carries `area:*` labels (`area:solver`,
`area:modeling`, `area:io`, `area:docs`, `area:bench`, `area:ci`).

### When you pick up a subtask issue

1. Read the epic (`#24`–`#27`) to refresh the release's theme and
  definition of done.
2. Read the relevant ROADMAP.md release section for context the issue
  body may have abbreviated.
3. Read every guide page listed under the issue's "Docs to touch on
  merge" section.
4. Implement the change.
5. Update every doc artifact listed in the issue body.
6. Run `gofmt -w . && go test ./...` locally.
7. Open a PR titled e.g. `v0.2: implement CPLEX LP file parser
  (closes #1)`— always include the`closes #N` keyword.
8. In your PR description, note which doc artifacts were updated so the
  reviewer can verify the north-star rule.

### When a subtask lands

GitHub auto-ticks the checkbox in the epic when the PR's `closes #N`
keyword merges. If you manually closed the subtask for any reason,
also manually tick the checkbox in the epic body.

### When an epic's checklist is fully ticked

1. Flip the matching section in `ROADMAP.md` from
  `Target: …` to `Status: **shipped.`** and tick every bullet.
2. Update `CHANGELOG.md` (v0.4 onwards).
3. Bump any explicit `v0.X` references in `README.md` and
  `docs/guide/*.html` that still say "coming in v0.X".
4. Tag the release: `git tag vX.Y.Z && git push --tags`. Pre-1.0 is
  allowed to break API; v1.0+ is not.
5. Publish a GitHub Release citing the closed subtask issues.
6. Close the epic issue.

---

## API surface discipline

Before v1.0, the pre-1.0 semver rules apply: minor versions can break
API when the ROADMAP section for that release says they will.

From v1.0 onwards, every exported type, function, and constant in the
frozen set carries a `// Stable.` GoDoc tag as its first line. Removing
or changing a signature on a `// Stable.` symbol is a 2.0-level
breaking change and requires a ROADMAP entry.

If you're adding a new public symbol in a pre-1.0 release, **do not**
prefix it with `// Stable.`. The v1.0 epic (#27) is the single place
those markers get added all at once.

---

## Working in the docs site

The guide is a hand-rolled static site. Every page has the same shape:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <title>... · grove</title>
    <link rel="stylesheet" href="./assets/docs.css">
  </head>
  <body>
    <article class="doc-content" data-doc-slug="my-slug">
      <!-- prose, headings, code samples, etc. -->
    </article>
    <script src="./assets/docs.js"></script>
  </body>
</html>
```

`docs.js` reads `data-doc-slug`, finds the matching entry in the
`MANIFEST`, and injects the sidebar, topbar, "on this page" TOC, and
prev/next nav. To add a page:

1. Create `docs/guide/my-page.html` with the skeleton above.
2. Write prose inside `<article>`. Use `<h2>` for sections (they
  become TOC entries), `<h3>` for sub-sections, `<pre><code>` for
   code samples with a `language-go` (or `language-bash`, etc.) class.
3. Add an entry to `MANIFEST` in `docs/guide/assets/docs.js` at the
  right position in reading order, with a `group` that matches a
   sensible sidebar heading.
4. Add a row to the docs-pointer table in `README.md`.
5. Preview with `python3 -m http.server 8000 -d docs` and visit the
  page; confirm sidebar highlight, TOC, and prev/next all render.

Syntax highlighting is injected via highlight.js from a CDN; no action
needed per page beyond the `language-*` class on `<code>`.

---

## When you get stuck

- Can't tell which guide page owns a topic? Look at the `MANIFEST` in
`docs/guide/assets/docs.js` — one source of truth for the page map.
- Solver math feels fuzzy? The `solver.go` comments reference sections
of Bertsimas & Tsitsiklis, *Introduction to Linear Optimization*.
That's the textbook grove models itself on.
- Unsure whether a change is breaking? Err on the side of "yes" and
flag it in the PR description; a reviewer can downgrade it.
- Unsure whether a new dependency is justified? The answer is almost
certainly no. Ask in the issue before adding it.
- Found speculative work not in any release? It probably belongs in
`ROADMAP.md`'s `## Beyond 1.0 — speculative` section. Those items
don't have issues yet on purpose — triage happens post-1.0.

