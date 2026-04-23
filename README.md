# grove

**The PuLP of Go.** A pure-Go linear-programming library with an ergonomic
modeling DSL, a built-in two-phase simplex solver, and zero CGO. `go get`
and you are solving LPs.

```go
prob := grove.NewProblem("nurses", grove.Maximize)

day   := prob.NewVar("day",   grove.Continuous, grove.Bounds(0, grove.Inf))
night := prob.NewVar("night", grove.Continuous, grove.Bounds(0, grove.Inf))

prob.SetObjective(grove.Expr{day: 1, night: 1})
prob.AddConstraint("day_min",   grove.Expr{day: 1},                grove.GTE, 4)
prob.AddConstraint("night_min", grove.Expr{night: 1},              grove.GTE, 2)
prob.AddConstraint("budget",    grove.Expr{day: 1200, night: 1500}, grove.LTE, 18000)

res, err := prob.Solve()
fmt.Println(res.Status, res.Objective, res.Value(day), res.Value(night))
// Optimal 14.5 12.5 2
```

---

## Why grove exists

Go has had bits and pieces of LP tooling for years, but every existing
option asks you to make a sacrifice:

| Library   | Solver                       | Modeling DSL          | Pure Go? | License            | Status                |
|-----------|------------------------------|-----------------------|----------|--------------------|-----------------------|
| `gonum/optimize/lp` | Simplex on dense matrices    | None — pass `c`, `A`, `b` directly | yes      | BSD-3-Clause       | maintained            |
| `golp`    | LP_Solve via cgo             | Thin wrapper          | no (cgo) | LGPL (LP_Solve)    | abandoned 2017        |
| `goop`    | Pure Go simplex              | Operator-style DSL    | yes      | BSD-3-Clause       | abandoned 2017        |
| **grove** | Pure Go two-phase simplex; HiGHS later | Named-variable map DSL | yes      | MIT                | actively developed    |

If you've reached for `gonum/optimize/lp` you've probably written code
like this:

```go
c := []float64{-1, -1, 0, 0, 0}             // objective (negated for max)
A := [][]float64{{1, 0, -1, 0, 0}, /* … */ }// hand-built constraint matrix
b := []float64{4, 2, 18000}                 // right-hand-side
```

…and if you've spent more than ten minutes building a model that way you
already know the bug surface: column orderings, sign flips, slack
columns, reading back the right element of `x`. grove keeps the variables
as first-class values and constraints as named relations, then lowers
everything into standard form internally.

If you've reached for `golp`, you've shipped a production binary that
needs `apt-get install liblpsolve55-dev` on every host. grove never
shells out and never asks for a system library.

## Installation

```bash
go get github.com/jakenherman/grove
```

That's the entire setup. Go 1.22+, no cgo, no `LD_LIBRARY_PATH` dance.

## Documentation

Narrative docs live at <https://jakenherman.github.io/grove/guide/>, with
one page per topic (modeling, solving, sensitivity, integer variables,
file I/O, a full API reference, and the roadmap). The `README` you're
reading is the pitch; the site is the reference.

## ⚠️ Integer variables in v0.1 are solved as their LP relaxation

grove v0.1 has a pure-LP solver. If you declare a variable as `grove.Integer`
or `grove.Binary`, grove will still accept the model and return a result —
but internally it is solving the **LP relaxation** (the same model with the
integrality constraint dropped). Branch-and-bound MIP support is scheduled
for v0.4.

Two things follow from this, and both can bite you:

1. **A non-integer LP optimum is not the ILP optimum.** If the LP vertex
   lands at, say, `x = 0.75`, the true ILP optimum might be at `x = 1`
   with a strictly worse objective, or the ILP might be infeasible
   entirely.
2. **An LP-feasible ILP can actually be infeasible.** grove can't detect
   this in v0.1. You'll get `Status = Optimal` with a non-integer
   solution where CBC/HiGHS would correctly report `Infeasible`.

Example — this ILP is genuinely infeasible (no integer `x1, x2` satisfy
`0.5 ≤ x1 − x2 ≤ 0.75`):

```go
p := grove.NewProblem("tricky", grove.Minimize)
x1 := p.NewVar("x1", grove.Integer, grove.Bounds(-1, 1))
x2 := p.NewVar("x2", grove.Integer, grove.Bounds(-1, 1))
x3 := p.NewVar("x3", grove.Integer, grove.Bounds(-1, 1))
p.SetObjective(grove.Expr{x1: 2, x2: -3, x3: 1})
p.AddConstraint("c1", grove.Expr{x1: 1, x2: -1}, grove.GTE, 0.5)
p.AddConstraint("c2", grove.Expr{x1: 1, x2: -1}, grove.LTE, 0.75)
p.AddConstraint("c3", grove.Expr{x2: 1, x3: -1}, grove.LTE, 1.25)
p.AddConstraint("c4", grove.Expr{x2: 1, x3: -1}, grove.GTE, 0.95)

r, _ := p.Solve()
// grove v0.1:    r.Status == Optimal,   obj = -0.25, (0.75, 0.25, -1)
// PuLP / CBC:    r.Status == Infeasible
```

### How grove protects you from this

Every `Solve` runs a post-solve integrality check:

- If the problem has any `Integer`/`Binary` variables and the returned
  optimum has any of them at a non-integer value, grove **appends a
  warning** to `Result.Warnings` and folds the same text into
  `Result.Message`:

  ```text
  grove: LP-relaxation only: 2 integer/binary variable(s) came back non-integer…
  ```

- `result.NonIntegerIntegerVars(p)` returns the offending `*Var`s so you
  can surface them programmatically.

- The warning prefix `grove.WarnLPRelaxationOnly` is a stable constant;
  you can `strings.HasPrefix` against it in a CI gate.

**Rule of thumb until v0.4 ships:**

- Pure-LP models (`Continuous` variables only) — grove is correct, fast,
  and warning-free.
- ILP models whose LP relaxation has an integer-valued optimum — grove
  gives you the correct ILP answer (this is a standard textbook result)
  and also emits no warning.
- Any other ILP — treat grove's output as an **upper bound** (for `Max`)
  or **lower bound** (for `Min`) on the true ILP optimum, and/or call
  into CBC/HiGHS. The v0.3 HiGHS backend lands as a drop-in `Solver`:

  ```go
  p.Solver = &grove.HiGHS{} // requires cgo + libhighs (ships in v0.3)
  ```

## Three worked examples

Every example below lives in [`examples/`](./examples) and is runnable
with `go run ./examples/<name>`.

### 1. Shift scheduling

Cover 7 days of nurse demand at minimum total wage cost, choosing how
many full-time and part-time shifts to staff each day.

```go
prob := grove.NewProblem("nurse_schedule", grove.Minimize)

full := make([]*grove.Var, 7)
part := make([]*grove.Var, 7)
for i, d := range days {
    full[i] = prob.NewVar("full_"+d, grove.Continuous, grove.Bounds(0, grove.Inf))
    part[i] = prob.NewVar("part_"+d, grove.Continuous, grove.Bounds(0, grove.Inf))
}

obj := grove.Expr{}
for i := range days {
    obj[full[i]] = 280  // $/shift
    obj[part[i]] = 160
}
prob.SetObjective(obj)

for i, d := range days {
    prob.AddConstraint("cover_"+d,
        grove.Expr{full[i]: 8, part[i]: 4}, // hours per shift
        grove.GTE, demand[i])
}
```

Run it:

```text
$ go run ./examples/scheduling
Status:  Optimal
Cost:    $8505.00
Pivots:  7
…
```

### 2. VM resource allocation

LP-relaxation of a multi-dimensional knapsack. Pick fractions of a set of
workloads to schedule on a fixed CPU/RAM/disk pool to maximise business
value.

```go
prob := grove.NewProblem("vm_allocation", grove.Maximize)

frac := make([]*grove.Var, len(workloads))
for i, w := range workloads {
    frac[i] = prob.NewVar(w.name, grove.Continuous, grove.Bounds(0, 1))
}

obj := grove.Expr{}
for i, w := range workloads {
    obj[frac[i]] = w.value
}
prob.SetObjective(obj)

prob.AddConstraint("cpu",  cpuExpr,  grove.LTE, cluster.cpu)
prob.AddConstraint("ram",  ramExpr,  grove.LTE, cluster.ram)
prob.AddConstraint("disk", diskExpr, grove.LTE, cluster.disk)
```

The dual on the binding resource is what an extra unit of that resource
is worth in the objective — exactly the number a capacity planner wants.

### 3. Diet optimisation (Stigler's diet)

The original LP. Hit a daily nutritional target at minimum dollar cost
across a basket of foods.

```go
prob := grove.NewProblem("diet", grove.Minimize)

x := make([]*grove.Var, len(foods))
for i, f := range foods {
    x[i] = prob.NewVar(f.name, grove.Continuous, grove.Bounds(0, 20))
}
// objective: minimise cost
// constraints: calories, protein, fat, carbs, sodium

res, _ := prob.Solve()
fmt.Println(grove.SensitivityReport(prob, res))
```

Run it and grove will faithfully reproduce the historic Stigler result
(modulo modern food prices): one ingredient does almost all the work.

## API reference

### Building a problem

```go
p := grove.NewProblem(name string, sense grove.Sense)
```

`sense` is `grove.Maximize` or `grove.Minimize`.

### Variables

```go
v := p.NewVar(name string, kind grove.VarKind, opts ...grove.VarOption)
```

`kind` is one of `grove.Continuous`, `grove.Integer`, `grove.Binary`. The
only `VarOption` today is `grove.Bounds(low, high float64)`. Use
`grove.Inf` (or `-grove.Inf`) for unbounded sides.

> Integer and Binary variables are accepted at the modeling layer and
> solved as their LP relaxation in v0.1. Branch-and-bound MIP support is
> planned for v0.4.

### Expressions

`grove.Expr` is `map[*grove.Var]float64`. The map is the linear
expression directly:

```go
e := grove.Expr{x: 3, y: -2, z: 1}
e2 := e.Add(grove.Expr{z: 1}).Scale(0.5)
```

### Objective

```go
p.SetObjective(grove.Expr{x: 1, y: 1})
p.SetObjectiveConstant(5) // optional fixed offset
```

### Constraints

```go
c := p.AddConstraint(name string, lhs grove.Expr, op grove.ConstraintType, rhs float64)
```

`op` is one of `grove.LTE`, `grove.GTE`, `grove.EQ`. The returned handle
is what you pass to `result.Dual(c)` later.

### Solve

```go
res, err := p.Solve()
```

`err` is non-nil only on configuration errors (no objective, no
variables, etc.). Logical outcomes — including `Infeasible` and
`Unbounded` — return `(res, nil)`; check `res.Status`.

```go
res.Status      // Optimal / Infeasible / Unbounded / IterationLimit / NumericalError
res.Objective   // user-sense objective at the optimum
res.Value(v)    // value of variable v
res.Dual(c)     // shadow price of constraint c (∂z*/∂rhs)
res.Reduced(v)  // reduced cost of variable v
res.Iterations  // total simplex pivots across both phases
res.Message     // human-readable detail (failures, LP-relaxation caveat, etc.)
res.Warnings    // advisory notes; v0.1 emits WarnLPRelaxationOnly when an ILP was silently relaxed
res.NonIntegerIntegerVars(p)  // Integer/Binary vars whose LP optimum is non-integer
```

For a structured report:

```go
rep := grove.SensitivityReport(p, res)
fmt.Println(rep) // pretty-prints binding/slack constraints + reduced costs
```

### Verbose mode

```go
p.Verbose = true
```

Prints the standard-form tableau and every Phase I / Phase II pivot to
`stderr`.

### File I/O

```go
p.WriteLP(io.Writer)  // CPLEX-LP format
p.WriteMPS(io.Writer) // fixed-column MPS format
```

### Swapping in HiGHS (v0.3 preview)

```go
p.Solver = &grove.HiGHS{} // returns ErrHiGHSNotBuilt today; cgo bindings land in v0.3
```

## How grove works

grove implements Dantzig's two-phase simplex with Bland's anti-cycling
rule, on a dense tableau. References below are to the standard textbook,
Bertsimas & Tsitsiklis, *Introduction to Linear Optimization*.

1. **Standard-form construction** (§3.1). Variables are shifted/split so
   every internal variable is non-negative; inequalities are augmented
   with slack/surplus columns; rows are negated where needed to keep `b ≥ 0`.
2. **Initial basis** (§3.5). Wherever no obvious identity column exists
   we add an artificial variable.
3. **Phase I** (§3.5). Minimise the sum of artificials. If the residual
   is positive, the original LP is infeasible.
4. **Phase II** (§3.4, §4). Optimise the original objective from the
   feasible basis; pivot with Bland's rule for guaranteed termination.
5. **Sensitivity** (§4.5, §5). Dual values are extracted from the
   original identity columns: `y_i = c_B · A_current[:, origIdent[i]]`,
   adjusted for sense and any row negations.

## Roadmap

See [ROADMAP.md](./ROADMAP.md). Highlights: MPS/LP I/O round-trip
(v0.2), HiGHS cgo backend (v0.3), branch-and-bound MIP (v0.4), 1.0
stability (v1.0).

## Contributing

Issues and PRs welcome. The library is small enough that a couple of
hours with the textbook plus the test suite is enough to feel
comfortable in `solver.go`. Please run `go test ./...` before sending a
patch.

## License

MIT.
