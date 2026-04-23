# grove roadmap

The plan, in versions. Each tagged release follows semantic versioning;
pre-1.0 minor versions are allowed to break API as the design settles.

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

Target: **Q3.** Backwards-compatible additions only.

- **Reading** `.lp` and `.mps` files (writers ship in v0.1; readers come
  next so models can flow in from MIPLIB benchmarks).
- Range information for sensitivity: per-coefficient and per-RHS ranges
  over which the current basis stays optimal (Bertsimas & Tsitsiklis §5.2).
- Solution warm-starting: `Solver` interface gains `SolveWith(prob,
  startingBasis)` for re-solving after small model edits.
- Presolve pass that drops empty rows/columns and detects fixed
  variables before the simplex starts.
- Constraint validation upgrades: detect duplicates, empty rows,
  zero-only objectives.

## v0.3 — HiGHS cgo backend

Target: **Q4.** Optional dependency, behind a build tag.

- `grove.HiGHS` becomes a real solver behind `go build -tags highs`,
  cgo-linked against [HiGHS](https://highs.dev). Falls back to the
  pure-Go solver if the tag is absent.
- Sparse CSC matrix conversion; uses HiGHS' own duals/reduced costs.
- `BuildHiGHSPackage` doc page covering Linux/macOS/Windows linker flags
  and Docker base images.
- Side-by-side benchmarks: pure Go vs HiGHS on a curated MIPLIB subset.

## v0.4 — Branch-and-bound MIP

Target: **next year.** The first feature that needs a meaningfully
different solver core.

- Standalone branch-and-bound driver wrapping the v0.1 simplex as the
  LP-relaxation oracle.
- Best-first node selection with Most-Fractional / Strong / Pseudocost
  branching options.
- Cutting planes: Gomory cuts for integer rounds; cover cuts for binary
  knapsack rows.
- Optimality gap reporting in `Result` (`PrimalBound`, `DualBound`,
  `RelGap`).
- Time-limit and gap-tolerance solver options.

## v1.0 — Stability and docs

Target: **after a couple of production users have weighed in.**

- API freeze for the modeling layer (`Problem`, `Var`, `Expr`,
  `Constraint`, `Result`, `Solver`).
- Cookbook-style documentation site (mirroring PuLP's "case studies"
  page) with twenty-plus worked models.
- Long-running performance regression suite gating every release.
- 100% test coverage on the solver core; fuzz tests for the modeling
  layer and file-format readers/writers.

## Beyond 1.0 — speculative

- Quadratic objectives (QP) — reuse simplex for active-set methods.
- Network simplex for transportation/assignment LPs.
- A WebAssembly build of the pure-Go solver for in-browser demos.
- Distributed cutting-plane MIP (Benders / Dantzig-Wolfe decomposition).
