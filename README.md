# grove

**The PuLP of Go.** A pure-Go linear-programming library with an ergonomic
modeling DSL, a built-in two-phase simplex solver, and zero CGO. `go get`
and you are solving LPs.

```go
prob := grove.NewProblem("nurses", grove.Maximize)

day   := prob.NewVar("day",   grove.Continuous, grove.Bounds(0, grove.Inf))
night := prob.NewVar("night", grove.Continuous, grove.Bounds(0, grove.Inf))

prob.SetObjective(grove.Expr{day: 1, night: 1})
prob.AddConstraint("day_min",   grove.Expr{day: 1},                 grove.GTE, 4)
prob.AddConstraint("night_min", grove.Expr{night: 1},               grove.GTE, 2)
prob.AddConstraint("budget",    grove.Expr{day: 1200, night: 1500}, grove.LTE, 18000)

res, err := prob.Solve()
fmt.Println(res.Status, res.Objective, res.Value(day), res.Value(night))
// Optimal 14.5 12.5 2
```

## Installation

```bash
go get github.com/jakenherman/grove
```

Go 1.22+, no cgo, no `LD_LIBRARY_PATH` dance.

## Why grove exists

Go has had bits and pieces of LP tooling for years, but every existing
option asks you to make a sacrifice:

| Library   | Solver                                 | Modeling DSL                       | Pure Go? | License         | Status             |
|-----------|----------------------------------------|------------------------------------|----------|-----------------|--------------------|
| `gonum/optimize/lp` | Simplex on dense matrices    | None — pass `c`, `A`, `b` directly | yes      | BSD-3-Clause    | maintained         |
| `golp`    | LP_Solve via cgo                       | Thin wrapper                       | no (cgo) | LGPL (LP_Solve) | abandoned 2017     |
| `goop`    | Pure Go simplex                        | Operator-style DSL                 | yes      | BSD-3-Clause    | abandoned 2017     |
| **grove** | Pure Go two-phase simplex; HiGHS later | Named-variable map DSL             | yes      | MIT             | actively developed |

grove keeps variables as first-class values and constraints as named
relations, then lowers everything into standard form internally — no
column-index bookkeeping, no sign flips, no `apt-get install
liblpsolve55-dev` on every host.

## Documentation

The full narrative docs live at <https://jakenherman.github.io/grove/guide/>.
This README is the pitch; the site is the reference.

| Topic                                  | Link                                                                                     |
|----------------------------------------|------------------------------------------------------------------------------------------|
| Modeling problems                      | <https://jakenherman.github.io/grove/guide/modeling.html>                                |
| Solving & the `Result` object          | <https://jakenherman.github.io/grove/guide/solving.html>                                 |
| Sensitivity analysis                   | <https://jakenherman.github.io/grove/guide/sensitivity.html>                             |
| **Integer variables in v0.1** (read this before shipping an ILP) | <https://jakenherman.github.io/grove/guide/integer-variables.html> |
| File I/O and swappable solvers         | <https://jakenherman.github.io/grove/guide/file-io.html>                                 |
| Worked examples                        | <https://jakenherman.github.io/grove/guide/examples.html>                                |
| Full API reference                     | <https://jakenherman.github.io/grove/guide/api-reference.html>                           |
| Roadmap                                | [ROADMAP.md](./ROADMAP.md) · <https://jakenherman.github.io/grove/guide/roadmap.html>    |

## One thing to know before shipping an integer model

grove v0.1 has a **pure-LP solver.** If you declare a variable as
`grove.Integer` or `grove.Binary`, grove will accept the model and
return a result — but internally it is solving the **LP relaxation.**
The LP optimum may be non-integer, worse than the true ILP optimum, or
belong to an ILP that is actually infeasible (grove can't detect that
case in v0.1).

grove protects you from being surprised:

- `Result.Warnings` includes `grove.WarnLPRelaxationOnly` whenever an
  `Integer`/`Binary` variable comes back non-integer.
- `Result.NonIntegerIntegerVars(p)` returns the offending `*Var`s so
  you can gate on them in CI.

Branch-and-bound MIP lands in **v0.4**, and a cgo-backed HiGHS
`Solver` in **v0.3**. See the [integer variables
guide](https://jakenherman.github.io/grove/guide/integer-variables.html)
for the full discussion, a worked failure case, and rules of thumb.

## Examples

Three runnable examples live under [`examples/`](./examples):

- [`scheduling`](./examples/scheduling) — 7-day nurse shift scheduling.
- [`allocation`](./examples/allocation) — multi-resource VM allocation.
- [`diet`](./examples/diet) — Stigler's diet problem.

Run any of them with `go run ./examples/<name>`.

## Contributing

Issues and PRs welcome. Please run `go test ./...` before sending a
patch. The library is small enough that a couple of hours with
Bertsimas & Tsitsiklis, *Introduction to Linear Optimization* plus the
test suite is enough to feel comfortable in `solver.go`.

## License

MIT.
