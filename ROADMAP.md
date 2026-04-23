# grove roadmap

The plan, in versions. Each tagged release follows semantic versioning;
pre-1.0 minor versions are allowed to break API as the design settles.

> Every in-scope bullet below is tracked as a GitHub issue and rolled
> up into a **release epic**. Link to the epic to watch progress:
> [v0.2 → #24](https://github.com/JakenHerman/grove/issues/24) ·
> [v0.3 → #25](https://github.com/JakenHerman/grove/issues/25) ·
> [v0.4 → #26](https://github.com/JakenHerman/grove/issues/26) ·
> [v1.0 → #27](https://github.com/JakenHerman/grove/issues/27).
> See [AGENTS.md](./AGENTS.md) for the workflow anyone picking up an
> issue should follow.

## v0.1 — Pure-Go simplex (this release)

Status: **shipped.**

- Modeling DSL: `Problem`, `Var`, `Expr`, `Constraint`, `Result`.
- Variable kinds: `Continuous`, `Integer`, `Binary` (the latter two solved
  as their LP relaxation; full MIP is on the v0.4 plan).
- Constraint relations: `LTE`, `GTE`, `EQ`.
- Variable bounds: free (`±Inf`), one-sided, two-sided. Bounded variables
  are handled by substitution + an explicit upper-bound row; free
  variables are split into positive/negative components.
- Two-phase simplex on a dense tableau with Bland's anti-cycling rule.
- Result statuses: `Optimal`, `Infeasible`, `Unbounded`, `IterationLimit`,
  `NumericalError`.
- Basic sensitivity outputs: shadow prices, reduced costs, slacks,
  binding flags via `SensitivityReport`.
- Verbose mode that streams every Phase I/II pivot.
- Unit tests covering: optimality, infeasibility, unboundedness, free
  variables, negative right-hand sides, Beale's degenerate cycling
  example, modeling-layer validation, exports.

## v0.2 — File I/O round-trip and richer sensitivity

Target: **Q3.** Backwards-compatible additions only. Epic: [#24](https://github.com/JakenHerman/grove/issues/24).

- **Reading** `.lp` and `.mps` files (writers ship in v0.1; readers come
  next so models can flow in from MIPLIB benchmarks).
  - [x] [#1](https://github.com/JakenHerman/grove/issues/1) CPLEX LP
  - [x] [#2](https://github.com/JakenHerman/grove/issues/2) MPS
- Range information for sensitivity: per-coefficient and per-RHS ranges
  over which the current basis stays optimal (Bertsimas & Tsitsiklis §5.2).
  ([#3](https://github.com/JakenHerman/grove/issues/3))
- Solution warm-starting: `Solver` interface gains `SolveWith(prob,
  startingBasis)` for re-solving after small model edits.
  ([#4](https://github.com/JakenHerman/grove/issues/4))
- [x] Presolve pass that drops empty rows/columns and detects fixed
  variables before the simplex starts.
  ([#5](https://github.com/JakenHerman/grove/issues/5))
- Constraint validation upgrades: detect duplicates, empty rows,
  zero-only objectives, NaN/Inf coefficients.
  ([#6](https://github.com/JakenHerman/grove/issues/6))

## v0.3 — HiGHS cgo backend

Target: **Q4.** Optional dependency, behind a build tag. Epic: [#25](https://github.com/JakenHerman/grove/issues/25).

- `grove.HiGHS` becomes a real solver behind `go build -tags highs`,
  cgo-linked against [HiGHS](https://highs.dev). Falls back to the
  pure-Go solver if the tag is absent.
  ([#7](https://github.com/JakenHerman/grove/issues/7))
- Sparse CSC matrix conversion; uses HiGHS' own duals/reduced costs.
  ([#8](https://github.com/JakenHerman/grove/issues/8))
- `BuildHiGHSPackage` doc page covering Linux/macOS/Windows linker flags
  and Docker base images.
  ([#9](https://github.com/JakenHerman/grove/issues/9))
- Side-by-side benchmarks: pure Go vs HiGHS on a curated MIPLIB subset.
  ([#10](https://github.com/JakenHerman/grove/issues/10))
- CI matrix that builds and tests `-tags highs` on Linux and macOS
  alongside the pure-Go path.
  ([#11](https://github.com/JakenHerman/grove/issues/11))

## v0.4 — Branch-and-bound MIP

Target: **next year.** The first feature that needs a meaningfully
different solver core. Epic: [#26](https://github.com/JakenHerman/grove/issues/26).

- Standalone branch-and-bound driver wrapping the v0.1 simplex as the
  LP-relaxation oracle.
  ([#12](https://github.com/JakenHerman/grove/issues/12))
- Best-first node selection with Most-Fractional / Strong / Pseudocost
  branching options.
  ([#13](https://github.com/JakenHerman/grove/issues/13))
- Cutting planes: Gomory cuts for integer rounds; cover cuts for binary
  knapsack rows.
  ([#14](https://github.com/JakenHerman/grove/issues/14))
- Optimality gap reporting in `Result` (`PrimalBound`, `DualBound`,
  `RelGap`).
  ([#15](https://github.com/JakenHerman/grove/issues/15))
- Time-limit and gap-tolerance solver options.
  ([#16](https://github.com/JakenHerman/grove/issues/16))
- Retire the v0.1 `WarnLPRelaxationOnly` path — the warning only
  existed because grove silently relaxed ILPs, and now it doesn't.
  ([#17](https://github.com/JakenHerman/grove/issues/17))
- Rewrite `docs/guide/integer-variables.html` from a "here's the
  caveat" page into a MIP tuning guide: branching rules, cut
  families, time/gap options.
  ([#18](https://github.com/JakenHerman/grove/issues/18))

## v1.0 — Stability and docs

Target: **after a couple of production users have weighed in.** Epic: [#27](https://github.com/JakenHerman/grove/issues/27).

- API freeze for the modeling layer (`Problem`, `Var`, `Expr`,
  `Constraint`, `Result`, `Solver`) — every frozen symbol tagged
  `// Stable.` in its GoDoc.
  ([#19](https://github.com/JakenHerman/grove/issues/19))
- Cookbook-style documentation site (mirroring PuLP's "case studies"
  page) with twenty-plus worked models.
  ([#20](https://github.com/JakenHerman/grove/issues/20))
- Long-running performance regression suite gating every release.
  ([#21](https://github.com/JakenHerman/grove/issues/21))
- 100% test coverage on the solver core; fuzz tests for the modeling
  layer and file-format readers/writers.
  ([#22](https://github.com/JakenHerman/grove/issues/22))
- Release engineering: `CHANGELOG.md`, install-verification Action
  across Linux/macOS/Windows on every tag, and a pkg.go.dev
  rendering pass on every exported symbol.
  ([#23](https://github.com/JakenHerman/grove/issues/23))

## Beyond 1.0 — speculative

These ideas do **not** have tracking issues yet. Post-1.0 triage
decides which become real milestones.

- Quadratic objectives (QP) — reuse simplex for active-set methods.
- Network simplex for transportation/assignment LPs.
- A WebAssembly build of the pure-Go solver for in-browser demos.
- Distributed cutting-plane MIP (Benders / Dantzig-Wolfe decomposition).
