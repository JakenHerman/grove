// Package grove is a pure-Go linear programming library — the PuLP of Go.
//
// grove gives Go backend developers an ergonomic modeling layer and a
// dependency-free simplex solver. There is no CGO, no system library, no
// commercial license. `go get` and you are solving LPs.
//
// Quick start:
//
//	prob := grove.NewProblem("nurses", grove.Maximize)
//	day   := prob.NewVar("day",   grove.Continuous, grove.Bounds(0, grove.Inf))
//	night := prob.NewVar("night", grove.Continuous, grove.Bounds(0, grove.Inf))
//
//	prob.SetObjective(grove.Expr{day: 1, night: 1})
//	prob.AddConstraint("day_min",   grove.Expr{day: 1},               grove.GTE, 4)
//	prob.AddConstraint("night_min", grove.Expr{night: 1},             grove.GTE, 2)
//	prob.AddConstraint("budget",    grove.Expr{day: 1200, night: 1500}, grove.LTE, 18000)
//
//	res, err := prob.Solve()
//	fmt.Println(res.Status, res.Objective, res.Value(day), res.Value(night))
//
// The pure-Go solver implements the two-phase simplex method of Dantzig
// (Bertsimas & Tsitsiklis, "Introduction to Linear Optimization", chapters
// 3 and 4) with Bland's rule for anti-cycling. v0.2 adds dual values and
// reduced costs; v0.3 will plug in an optional HiGHS CGO backend through
// the [Solver] interface.
package grove

import (
	"fmt"
	"math"
	"strings"
)

// Inf is the canonical positive-infinity sentinel used for unbounded
// variable bounds. Use [grove.Inf] for an upper bound of +∞ and
// `-grove.Inf` for a lower bound of -∞.
var Inf = math.Inf(1)

// Sense describes whether the objective is to be minimized or maximized.
type Sense int

const (
	// Minimize the objective function.
	Minimize Sense = iota
	// Maximize the objective function.
	Maximize
)

func (s Sense) String() string {
	switch s {
	case Minimize:
		return "Minimize"
	case Maximize:
		return "Maximize"
	}
	return "UnknownSense"
}

// VarKind classifies a decision variable. Continuous variables are solved
// directly by the simplex method; Integer and Binary variables are accepted
// at the modeling layer and (for v0.1) solved as their LP relaxation. Full
// branch-and-bound MIP support is on the v0.4 roadmap.
type VarKind int

const (
	// Continuous variables take any real value within their bounds.
	Continuous VarKind = iota
	// Integer variables are constrained to whole numbers.
	// (Solved as LP relaxation in v0.1.)
	Integer
	// Binary variables are constrained to {0, 1}.
	// (Solved as LP relaxation with bounds [0,1] in v0.1.)
	Binary
)

func (k VarKind) String() string {
	switch k {
	case Continuous:
		return "Continuous"
	case Integer:
		return "Integer"
	case Binary:
		return "Binary"
	}
	return "UnknownKind"
}

// ConstraintType is the relation in a linear constraint a·x ⟂ b.
type ConstraintType int

const (
	// LTE: a·x ≤ b
	LTE ConstraintType = iota
	// GTE: a·x ≥ b
	GTE
	// EQ:  a·x = b
	EQ
)

func (c ConstraintType) String() string {
	switch c {
	case LTE:
		return "<="
	case GTE:
		return ">="
	case EQ:
		return "="
	}
	return "?"
}

// Status is the terminal state of a solve.
type Status int

const (
	// NotSolved: solver has not yet run.
	NotSolved Status = iota
	// Optimal: an optimal solution was found.
	Optimal
	// Infeasible: no feasible solution exists.
	Infeasible
	// Unbounded: the objective is unbounded along a feasible ray.
	Unbounded
	// IterationLimit: the solver hit the iteration cap without converging.
	IterationLimit
	// NumericalError: numerical breakdown (e.g. degenerate pivot) prevented a solution.
	NumericalError
)

func (s Status) String() string {
	switch s {
	case NotSolved:
		return "NotSolved"
	case Optimal:
		return "Optimal"
	case Infeasible:
		return "Infeasible"
	case Unbounded:
		return "Unbounded"
	case IterationLimit:
		return "IterationLimit"
	case NumericalError:
		return "NumericalError"
	}
	return "UnknownStatus"
}

// Var is a decision variable in an LP. Use [Problem.NewVar] to create one;
// the returned pointer is the canonical handle and is used as a key in
// [Expr] linear expressions.
type Var struct {
	name string
	kind VarKind
	low  float64
	high float64
	idx  int // position in Problem.vars, set on creation
	prob *Problem
}

// Name returns the user-supplied name of the variable.
func (v *Var) Name() string { return v.name }

// Kind returns the variable's kind.
func (v *Var) Kind() VarKind { return v.kind }

// Low returns the lower bound. -[Inf] indicates a free lower bound.
func (v *Var) Low() float64 { return v.low }

// High returns the upper bound. +[Inf] indicates a free upper bound.
func (v *Var) High() float64 { return v.high }

// VarOption tweaks a variable at construction time.
type VarOption func(*Var)

// Bounds sets the lower and upper bound of a variable. Use ±[Inf] for an
// unbounded side. Bounds(0, Inf) is the default for Continuous and Integer
// variables; Binary variables are pinned to [0, 1] regardless.
func Bounds(low, high float64) VarOption {
	return func(v *Var) {
		v.low = low
		v.high = high
	}
}

// Expr is a sparse linear expression: a map from variable to coefficient.
//
// Example: 3x + 2y is grove.Expr{x: 3, y: 2}.
//
// A variable absent from the map has coefficient zero. Maps are mutable;
// the modeling DSL deliberately keeps them lightweight rather than wrapping
// them in a builder type.
type Expr map[*Var]float64

// Add returns the sum of two expressions as a new Expr.
func (e Expr) Add(other Expr) Expr {
	out := make(Expr, len(e)+len(other))
	for v, c := range e {
		out[v] = c
	}
	for v, c := range other {
		out[v] += c
	}
	return out
}

// Scale returns a copy of e with every coefficient multiplied by k.
func (e Expr) Scale(k float64) Expr {
	out := make(Expr, len(e))
	for v, c := range e {
		out[v] = c * k
	}
	return out
}

// Constraint is a single linear (in)equality.
type Constraint struct {
	name  string
	expr  Expr
	ctype ConstraintType
	rhs   float64
	idx   int // position in Problem.constraints, set on creation
}

// Name returns the user-supplied name of the constraint.
func (c *Constraint) Name() string { return c.name }

// Expr returns the constraint's left-hand-side linear expression. The
// returned map is the live model — mutate at your own risk.
func (c *Constraint) Expr() Expr { return c.expr }

// Type returns the constraint relation (LTE / GTE / EQ).
func (c *Constraint) Type() ConstraintType { return c.ctype }

// RHS returns the right-hand-side scalar.
func (c *Constraint) RHS() float64 { return c.rhs }

// Problem is a linear program in modeling form. Build it with
// [NewProblem], [Problem.NewVar], [Problem.SetObjective], and
// [Problem.AddConstraint], then call [Problem.Solve].
type Problem struct {
	name        string
	sense       Sense
	vars        []*Var
	varNames    map[string]*Var
	constraints []*Constraint
	consNames   map[string]*Constraint
	objective   Expr
	objConst    float64

	// Solver is the backend used by [Problem.Solve]. Defaults to the
	// pure-Go simplex solver. Set to plug in HiGHS or another backend
	// once the v0.3 roadmap lands.
	Solver Solver

	// Verbose enables iteration logging from the simplex solver.
	Verbose bool

	// MaxIterations caps the simplex iteration count. Zero means
	// "use the solver default".
	MaxIterations int

	// SkipPresolve disables the pre-simplex reduction pass run by
	// [Problem.Solve]. The presolve pass (added in v0.2) removes
	// fixed variables, empty columns, and empty rows before the
	// solver sees the model; it is cheap and safe for well-formed
	// LPs, but callers debugging the solver or comparing against
	// external references may want to turn it off. See
	// [Problem.Presolve].
	SkipPresolve bool
}

// NewProblem returns an empty LP with the given name and optimization sense.
func NewProblem(name string, sense Sense) *Problem {
	return &Problem{
		name:      name,
		sense:     sense,
		objective: Expr{},
		varNames:  map[string]*Var{},
		consNames: map[string]*Constraint{},
	}
}

// Name returns the problem name supplied to [NewProblem].
func (p *Problem) Name() string { return p.name }

// Sense returns the optimization sense.
func (p *Problem) Sense() Sense { return p.sense }

// Vars returns the variables in declaration order. The slice is the live
// backing array; treat it as read-only.
func (p *Problem) Vars() []*Var { return p.vars }

// Constraints returns the constraints in declaration order.
func (p *Problem) Constraints() []*Constraint { return p.constraints }

// Objective returns the current objective expression.
func (p *Problem) Objective() Expr { return p.objective }

// ObjectiveConstant returns the constant offset added to the objective.
func (p *Problem) ObjectiveConstant() float64 { return p.objConst }

// NewVar registers a new variable on the problem and returns a pointer
// suitable for use as a key in an [Expr]. Names must be unique within a
// problem.
func (p *Problem) NewVar(name string, kind VarKind, opts ...VarOption) *Var {
	if _, dup := p.varNames[name]; dup {
		panic(fmt.Sprintf("grove: duplicate variable name %q", name))
	}
	v := &Var{
		name: name,
		kind: kind,
		low:  0,
		high: Inf,
		idx:  len(p.vars),
		prob: p,
	}
	if kind == Binary {
		v.low, v.high = 0, 1
	}
	for _, opt := range opts {
		opt(v)
	}
	if kind == Binary {
		// Re-pin in case Bounds was supplied for a binary; this matches
		// PuLP's behaviour of treating Binary as authoritative.
		if v.low < 0 {
			v.low = 0
		}
		if v.high > 1 {
			v.high = 1
		}
	}
	p.vars = append(p.vars, v)
	p.varNames[name] = v
	return v
}

// VarByName returns the variable with the given name, or nil if absent.
func (p *Problem) VarByName(name string) *Var { return p.varNames[name] }

// SetObjective sets the linear objective. A constant offset can be added
// with [Problem.SetObjectiveConstant].
func (p *Problem) SetObjective(e Expr) {
	p.objective = make(Expr, len(e))
	for v, c := range e {
		if v.prob != p {
			panic("grove: objective uses a variable from a different problem")
		}
		p.objective[v] = c
	}
}

// SetObjectiveConstant sets a constant added to the objective value
// reported in [Result.Objective].
func (p *Problem) SetObjectiveConstant(k float64) { p.objConst = k }

// AddConstraint adds a named constraint and returns a handle to it.
// Names must be unique within a problem.
func (p *Problem) AddConstraint(name string, e Expr, ctype ConstraintType, rhs float64) *Constraint {
	if _, dup := p.consNames[name]; dup {
		panic(fmt.Sprintf("grove: duplicate constraint name %q", name))
	}
	for v := range e {
		if v.prob != p {
			panic(fmt.Sprintf("grove: constraint %q uses a variable from a different problem", name))
		}
	}
	c := &Constraint{
		name:  name,
		expr:  e,
		ctype: ctype,
		rhs:   rhs,
		idx:   len(p.constraints),
	}
	p.constraints = append(p.constraints, c)
	p.consNames[name] = c
	return c
}

// ConstraintByName returns the constraint with the given name, or nil if absent.
func (p *Problem) ConstraintByName(name string) *Constraint { return p.consNames[name] }

// Validate checks the problem for obvious modeling errors and returns a
// non-nil error if any are found. [Problem.Solve] runs Validate
// implicitly; call it directly if you want to fail fast at build time.
func (p *Problem) Validate() error {
	if len(p.vars) == 0 {
		return fmt.Errorf("grove: problem %q has no variables", p.name)
	}
	for _, v := range p.vars {
		if math.IsNaN(v.low) || math.IsNaN(v.high) {
			return fmt.Errorf("grove: variable %q has NaN bound", v.name)
		}
		if v.low > v.high {
			return fmt.Errorf("grove: variable %q has empty domain [%g, %g]", v.name, v.low, v.high)
		}
	}
	if len(p.objective) == 0 {
		return fmt.Errorf("grove: problem %q has no objective (call SetObjective)", p.name)
	}
	for _, c := range p.constraints {
		if math.IsNaN(c.rhs) {
			return fmt.Errorf("grove: constraint %q has NaN right-hand side", c.name)
		}
		if math.IsInf(c.rhs, 0) {
			return fmt.Errorf("grove: constraint %q has infinite right-hand side", c.name)
		}
		if len(c.expr) == 0 {
			return fmt.Errorf("grove: constraint %q has empty left-hand side", c.name)
		}
		for v, coef := range c.expr {
			if math.IsNaN(coef) || math.IsInf(coef, 0) {
				return fmt.Errorf("grove: constraint %q has non-finite coefficient on %q", c.name, v.name)
			}
		}
	}
	return nil
}

// Result is the outcome of a solve.
type Result struct {
	Status    Status
	Objective float64
	values    map[*Var]float64

	// Sensitivity diagnostics (set when Status == Optimal).
	dual    map[*Constraint]float64 // shadow prices / dual values
	reduced map[*Var]float64        // reduced costs of variables

	// Iterations is the number of simplex pivots performed across both
	// phases.
	Iterations int

	// Message gives optional human-readable detail (e.g. why a solve
	// failed, or why an Optimal result may not mean what you think —
	// warnings emitted by the post-solve checker are folded in here as
	// well as being surfaced in Warnings).
	Message string

	// Warnings are advisory notes about the result. v0.1 emits exactly one
	// kind of warning today: [WarnLPRelaxationOnly], raised when a problem
	// has Integer or Binary variables but the solver returned a non-integer
	// optimum — meaning the reported solution is LP-feasible only and the
	// true ILP optimum (including possible infeasibility) is unknown.
	Warnings []string
}

// WarnLPRelaxationOnly is the canonical warning message for "you asked for
// an ILP but v0.1's solver is LP-relaxation only". The prefix is stable
// across releases so callers can do `strings.HasPrefix(w, grove.WarnLPRelaxationOnly)`
// in their own tooling.
const WarnLPRelaxationOnly = "grove: LP-relaxation only"

// Value returns the value of v in the optimal solution. If the solve
// did not reach Optimal, Value returns 0.
func (r *Result) Value(v *Var) float64 {
	if r == nil {
		return 0
	}
	return r.values[v]
}

// Values returns a copy of the variable→value map.
func (r *Result) Values() map[*Var]float64 {
	out := make(map[*Var]float64, len(r.values))
	for k, v := range r.values {
		out[k] = v
	}
	return out
}

// Dual returns the dual value (shadow price) of constraint c. Dual values
// are only populated when [Result.Status] is [Optimal].
func (r *Result) Dual(c *Constraint) float64 {
	if r == nil {
		return 0
	}
	return r.dual[c]
}

// Reduced returns the reduced cost of variable v at the optimum.
func (r *Result) Reduced(v *Var) float64 {
	if r == nil {
		return 0
	}
	return r.reduced[v]
}

// Solve runs the configured solver. If [Problem.Solver] is nil the pure-Go
// simplex implementation is used.
//
// Post-solve, Solve runs a single integrality check: if the problem
// declared any Integer or Binary variables and the returned optimum has
// any of those variables at a non-integer value, Solve appends a warning
// to [Result.Warnings] and folds the same text into [Result.Message].
// This makes it hard to miss the v0.1 behaviour of "Integer is solved as
// its LP relaxation". v0.4's branch-and-bound driver will tighten
// integrality before returning, and this warning will fall silent
// whenever the model is a pure LP or the ILP is solved exactly.
func (p *Problem) Solve() (*Result, error) {
	if err := p.Validate(); err != nil {
		return &Result{Status: NotSolved, Message: err.Error()}, err
	}

	// Presolve runs by default; SkipPresolve = true bypasses it.
	target := p
	var undo *PresolveUndo
	var res *Result
	var err error
	if !p.SkipPresolve {
		rp, u, perr := p.Presolve(nil)
		if perr != nil {
			return &Result{Status: NotSolved, Message: perr.Error()}, perr
		}
		undo = u
		if u.Terminal != NotSolved {
			// Presolve decided the problem's status on its own; build
			// the expanded Result from the undo map and fall through to
			// the shared post-solve section so checks like the ILP
			// warning apply uniformly.
			res = u.terminalResult()
		} else {
			target = rp
		}
	}

	if res == nil {
		solver := p.Solver
		if solver == nil {
			solver = &SimplexSolver{Verbose: p.Verbose, MaxIterations: p.MaxIterations}
		}
		res, err = solver.Solve(target)
		if undo != nil {
			res = undo.expand(res)
		}
	}
	if res != nil && res.Status == Optimal {
		if off := res.NonIntegerIntegerVars(p); len(off) > 0 {
			w := fmt.Sprintf("%s: %d integer/binary variable(s) came back non-integer "+
				"(problem has %d such var(s)). The reported optimum is LP-feasible "+
				"only; the true ILP optimum (or its infeasibility) is not determined. "+
				"Use branch-and-bound (v0.4) or an external solver like HiGHS/CBC.",
				WarnLPRelaxationOnly, len(off), countIntegerVars(p))
			res.Warnings = append(res.Warnings, w)
			if res.Message == "" {
				res.Message = w
			} else {
				res.Message = res.Message + "; " + w
			}
		}
	}
	return res, err
}

// NonIntegerIntegerVars returns the [Var]s declared as [Integer] or
// [Binary] whose value in r is not (within 1e-6) an integer. On an empty
// or non-Optimal result it returns nil. The returned slice is in
// declaration order.
func (r *Result) NonIntegerIntegerVars(p *Problem) []*Var {
	if r == nil || r.Status != Optimal || p == nil {
		return nil
	}
	const tol = 1e-6
	var offenders []*Var
	for _, v := range p.vars {
		if v.kind != Integer && v.kind != Binary {
			continue
		}
		val := r.values[v]
		if math.Abs(val-math.Round(val)) > tol {
			offenders = append(offenders, v)
		}
	}
	return offenders
}

func countIntegerVars(p *Problem) int {
	n := 0
	for _, v := range p.vars {
		if v.kind == Integer || v.kind == Binary {
			n++
		}
	}
	return n
}

// String returns a human-readable rendering of the model in CPLEX-LP-ish
// form. Useful for debugging.
func (p *Problem) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", p.sense, p.name)
	fmt.Fprintf(&b, "  obj: %s", formatExpr(p.objective))
	if p.objConst != 0 {
		fmt.Fprintf(&b, " %+g", p.objConst)
	}
	b.WriteString("\nsubject to\n")
	for _, c := range p.constraints {
		fmt.Fprintf(&b, "  %s: %s %s %g\n", c.name, formatExpr(c.expr), c.ctype, c.rhs)
	}
	b.WriteString("bounds\n")
	for _, v := range p.vars {
		fmt.Fprintf(&b, "  %s in [%s, %s] (%s)\n", v.name, formatBound(v.low), formatBound(v.high), v.kind)
	}
	return b.String()
}

func formatExpr(e Expr) string {
	if len(e) == 0 {
		return "0"
	}
	// Stable order: by variable index.
	type term struct {
		v *Var
		c float64
	}
	terms := make([]term, 0, len(e))
	for v, c := range e {
		terms = append(terms, term{v, c})
	}
	// Tiny insertion sort by idx to avoid pulling in sort for a hot path.
	for i := 1; i < len(terms); i++ {
		for j := i; j > 0 && terms[j-1].v.idx > terms[j].v.idx; j-- {
			terms[j-1], terms[j] = terms[j], terms[j-1]
		}
	}
	var b strings.Builder
	for i, t := range terms {
		switch {
		case i == 0 && t.c < 0:
			fmt.Fprintf(&b, "-%g %s", -t.c, t.v.name)
		case i == 0:
			fmt.Fprintf(&b, "%g %s", t.c, t.v.name)
		case t.c < 0:
			fmt.Fprintf(&b, " - %g %s", -t.c, t.v.name)
		default:
			fmt.Fprintf(&b, " + %g %s", t.c, t.v.name)
		}
	}
	return b.String()
}

func formatBound(x float64) string {
	switch {
	case math.IsInf(x, 1):
		return "+inf"
	case math.IsInf(x, -1):
		return "-inf"
	}
	return fmt.Sprintf("%g", x)
}
