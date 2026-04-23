package grove

// This file implements grove's presolve pass. Presolve cheaply shrinks a
// problem before the simplex runs: it detects fixed variables (low ==
// high), eliminates empty columns (variables that never appear in any
// constraint), and drops empty rows (constraints whose LHS has collapsed
// after fixed-variable substitution). The textbook references are
// Bertsimas & Tsitsiklis §4.11 on "problem reduction" and Achterberg's
// 2007 PhD thesis "Constraint Integer Programming" §10.3 for a modern
// tour of the cheap reductions every serious LP/MIP solver ships with.
//
// v0.2 scope (tracked in #5) is deliberately narrow: only the three
// reductions above, no propagation of variable bounds through
// constraints, no singleton-row substitution, no duplicate-column
// detection. Future releases can pile more passes onto the same Presolve
// entry point without changing its signature.

import (
	"fmt"
	"math"
)

// PresolveOptions configures the presolve pass run by [Problem.Presolve]
// and, implicitly, by [Problem.Solve]. All fields are optional.
type PresolveOptions struct {
	// Tolerance is the slack used for feasibility and zero checks on
	// reduced rows (e.g. a row that collapses to 0 = rhs is infeasible
	// when |rhs| > Tolerance). Defaults to 1e-9.
	Tolerance float64
}

// PresolveUndo carries the mapping required to re-expand a
// reduced-problem [Result] back into the original index space. Callers
// rarely build one directly: [Problem.Presolve] returns it and
// [Problem.Solve] threads it through transparently.
//
// The zero value is not useful; always obtain a PresolveUndo from
// [Problem.Presolve].
type PresolveUndo struct {
	// orig is the Problem the presolve was run on.
	orig *Problem

	// varValue holds presolve-determined values: the union of fixed
	// variables (low == high) and empty-column variables whose
	// optimum is read off their objective coefficient and bounds.
	// Any original variable not in this map survived presolve and its
	// value is carried by the reduced [Result].
	varValue map[*Var]float64

	// varMap maps a surviving reduced *Var back to the original *Var.
	varMap map[*Var]*Var

	// consMap maps a surviving reduced *Constraint back to the original.
	consMap map[*Constraint]*Constraint

	// droppedCons records the original constraints the presolve
	// removed (empty-row eliminations). These carry a zero dual in the
	// expanded [Result], matching the fact that relaxing a redundant
	// row does not improve the objective.
	droppedCons []*Constraint

	// objShift is the constant piece of the objective folded out by
	// substituting fixed-variable values, reported in the user sense.
	objShift float64

	// Terminal, when non-zero (i.e. not NotSolved), signals that
	// presolve decided the problem's status on its own — an empty row
	// with an inconsistent RHS is [Infeasible]; a free variable that
	// only appears in the objective is [Unbounded]; an all-fixed model
	// is [Optimal]. The reduced Problem is nil in those cases and
	// [Problem.Solve] synthesises a Result directly from the undo map.
	Terminal    Status
	TerminalMsg string
}

// Presolve returns a reduced copy of p plus the undo map required to
// re-expand a [Result]. It never mutates p.
//
// The reduced Problem has its own fresh *Var and *Constraint handles —
// passing them to the original Problem's solver would be nonsense. In
// normal use, call Presolve via [Problem.Solve], which owns both the
// reduced Problem and the undo map for you.
//
// The v0.2 pass performs three reductions:
//
//   - Fixed variables. A variable with a finite two-sided bound where
//     low == high is substituted with that value everywhere (objective
//     constant and each constraint's RHS).
//   - Empty columns. A variable that — after fixed-variable substitution
//     — appears in no constraint is settled at its lower or upper bound
//     according to the sign of its objective coefficient. If the
//     settling bound is infinite the problem is unbounded.
//   - Empty rows. A constraint whose LHS has no surviving variables
//     after substitution is checked for feasibility (0 ⟂ rhs) and
//     dropped when feasible.
//
// Presolve returns a non-nil [PresolveUndo] even on the terminal paths
// (all-fixed / infeasible / unbounded). When it cannot return a
// reduced Problem (terminal path, or a malformed model) the reduced
// Problem pointer is nil and [PresolveUndo.Terminal] is set. Callers
// that want to synthesize a [Result] from a terminal undo directly —
// without going back through [Problem.Solve] — can pass the undo to
// [Problem.Solve] (which handles this path transparently) or inspect
// [PresolveUndo.Terminal] and [PresolveUndo.TerminalMsg] themselves.
func (p *Problem) Presolve(opts *PresolveOptions) (*Problem, *PresolveUndo, error) {
	tol := 1e-9
	if opts != nil && opts.Tolerance > 0 {
		tol = opts.Tolerance
	}
	undo := &PresolveUndo{
		orig:     p,
		varValue: map[*Var]float64{},
		varMap:   map[*Var]*Var{},
		consMap:  map[*Constraint]*Constraint{},
	}

	// ── Step 1: detect fixed variables (finite, low == high). ──────────
	for _, v := range p.vars {
		if math.IsInf(v.low, 0) || math.IsInf(v.high, 0) {
			continue
		}
		if v.low == v.high {
			undo.varValue[v] = v.low
		}
	}

	// ── Step 2: walk the constraints, substituting fixed variables into
	// the RHS. Collect surviving rows; flag empty rows for feasibility
	// checking.
	type redRow struct {
		owner *Constraint
		terms map[*Var]float64 // keyed by original *Var
		rhs   float64
	}
	reducedRows := make([]redRow, 0, len(p.constraints))
	for _, c := range p.constraints {
		rhs := c.rhs
		terms := map[*Var]float64{}
		for v, k := range c.expr {
			if val, fixed := undo.varValue[v]; fixed {
				rhs -= k * val
				continue
			}
			terms[v] = k
		}
		if len(terms) == 0 {
			if !emptyRowFeasible(c.ctype, rhs, tol) {
				undo.Terminal = Infeasible
				undo.TerminalMsg = fmt.Sprintf(
					"presolve: constraint %q reduces to 0 %s %g after fixed-variable substitution",
					c.name, c.ctype, rhs)
				return nil, undo, nil
			}
			undo.droppedCons = append(undo.droppedCons, c)
			continue
		}
		reducedRows = append(reducedRows, redRow{owner: c, terms: terms, rhs: rhs})
	}

	// ── Step 3: empty-column detection. A variable that has no fixed
	// value and does not appear in any surviving row is optimised on its
	// own — pin it to whichever bound the objective prefers, or declare
	// the problem unbounded.
	appears := map[*Var]bool{}
	for _, r := range reducedRows {
		for v := range r.terms {
			appears[v] = true
		}
	}
	for _, v := range p.vars {
		if _, fixed := undo.varValue[v]; fixed {
			continue
		}
		if appears[v] {
			continue
		}
		coef := p.objective[v]
		val, unbounded := emptyColumnValue(p.sense, v, coef, tol)
		if unbounded {
			undo.Terminal = Unbounded
			undo.TerminalMsg = fmt.Sprintf(
				"presolve: variable %q only appears in the objective and is unbounded in the improving direction",
				v.name)
			return nil, undo, nil
		}
		undo.varValue[v] = val
	}

	// If every variable has been settled, presolve has solved the model.
	// Compute the user-sense objective and short-circuit.
	if len(undo.varValue) == len(p.vars) {
		obj := p.objConst
		for v, k := range p.objective {
			obj += k * undo.varValue[v]
		}
		undo.objShift = obj - p.objConst
		undo.Terminal = Optimal
		undo.TerminalMsg = "presolve: all variables fixed"
		// Solve() synthesizes the terminal Result by calling
		// undo.terminalResult(), which recomputes the user-sense
		// objective directly from undo.varValue and the original
		// problem's objective coefficients / constant.
		return nil, undo, nil
	}

	// ── Step 4: build the reduced Problem.
	rp := newReducedProblem(p)

	// Surviving original vars → fresh vars on rp, in declaration order
	// so the reduced model's Vars() stays intuitive.
	revVarMap := map[*Var]*Var{}
	for _, v := range p.vars {
		if _, fixed := undo.varValue[v]; fixed {
			continue
		}
		nv := rp.NewVar(v.name, v.kind, Bounds(v.low, v.high))
		revVarMap[v] = nv
		undo.varMap[nv] = v
	}

	// Objective: drop fixed-variable terms, fold their contribution into
	// the objective constant.
	redObj := Expr{}
	var objShift float64
	for v, k := range p.objective {
		if val, fixed := undo.varValue[v]; fixed {
			objShift += k * val
			continue
		}
		redObj[revVarMap[v]] = k
	}
	// If every original objective term was on a fixed variable, redObj
	// is empty and the entire linear part lives in objConst. The
	// standard-form builder only ranges over p.objective, so an empty
	// map is fine; Validate allows it on reduced problems via
	// allowEmptyObjective.
	rp.SetObjective(redObj)
	rp.SetObjectiveConstant(p.objConst + objShift)
	undo.objShift = objShift

	// Constraints: copy the surviving rows with rewritten var keys.
	for _, r := range reducedRows {
		redExpr := make(Expr, len(r.terms))
		for v, k := range r.terms {
			redExpr[revVarMap[v]] = k
		}
		nc := rp.AddConstraint(r.owner.name, redExpr, r.owner.ctype, r.rhs)
		undo.consMap[nc] = r.owner
	}

	return rp, undo, nil
}

// newReducedProblem returns a fresh *Problem that mirrors p's
// configuration (name, sense, verbosity, iteration cap) but without any
// variables or constraints. Solver is intentionally not copied: the
// reduced problem is plugged back into whatever solver the caller is
// running, not re-dispatched.
func newReducedProblem(p *Problem) *Problem {
	name := p.name
	if name == "" {
		name = "presolved"
	} else {
		name = name + "_presolved"
	}
	rp := NewProblem(name, p.sense)
	rp.Verbose = p.Verbose
	rp.MaxIterations = p.MaxIterations
	rp.SkipPresolve = true // don't recursively presolve the reduced model
	rp.allowEmptyObjective = true
	return rp
}

// emptyRowFeasible reports whether a constraint of type ctype with LHS
// identically zero is feasible given the (possibly reduced) RHS.
func emptyRowFeasible(ctype ConstraintType, rhs, tol float64) bool {
	switch ctype {
	case EQ:
		return math.Abs(rhs) <= tol
	case LTE:
		return rhs >= -tol
	case GTE:
		return rhs <= tol
	}
	return true
}

// emptyColumnValue returns the value at which an empty-column variable
// v should be pinned, given its objective coefficient and the problem
// sense. The bool return is true when the improving direction is
// unbounded (so the caller must surface [Unbounded]).
func emptyColumnValue(sense Sense, v *Var, coef, tol float64) (float64, bool) {
	// Internally we always minimise; flip the coefficient on Maximize.
	minCoef := coef
	if sense == Maximize {
		minCoef = -coef
	}
	switch {
	case math.Abs(minCoef) <= tol:
		// Indifferent. Prefer lower bound, then upper, then zero.
		switch {
		case !math.IsInf(v.low, -1):
			return v.low, false
		case !math.IsInf(v.high, 1):
			return v.high, false
		default:
			return 0, false
		}
	case minCoef > 0:
		if math.IsInf(v.low, -1) {
			return 0, true
		}
		return v.low, false
	default: // minCoef < 0
		if math.IsInf(v.high, 1) {
			return 0, true
		}
		return v.high, false
	}
}

// expand re-projects a [Result] computed on the reduced problem back
// into the original index space described by u. It is a no-op when u
// is nil (presolve was disabled).
//
// The expanded Result keeps the reduced Result's Status, Objective,
// Iterations, Message, and Warnings. Variable values and reduced costs
// are keyed on the original *Var handles; constraint duals are keyed
// on the original *Constraint handles (dropped rows get a zero dual).
func (u *PresolveUndo) expand(res *Result) *Result {
	if u == nil || res == nil {
		return res
	}
	out := &Result{
		Status:     res.Status,
		Objective:  res.Objective,
		Iterations: res.Iterations,
		Message:    res.Message,
		Warnings:   append([]string(nil), res.Warnings...),
		values:     map[*Var]float64{},
		dual:       map[*Constraint]float64{},
		reduced:    map[*Var]float64{},
	}
	// Variables settled by presolve.
	for v, val := range u.varValue {
		out.values[v] = val
	}
	// Variables carried through from the reduced solve.
	for rv, ov := range u.varMap {
		out.values[ov] = res.values[rv]
		out.reduced[ov] = res.reduced[rv]
	}
	// Constraints carried through from the reduced solve.
	for rc, oc := range u.consMap {
		out.dual[oc] = res.dual[rc]
	}
	// Dropped constraints: zero dual.
	for _, oc := range u.droppedCons {
		out.dual[oc] = 0
	}
	return out
}

// terminalResult synthesises a [Result] for the cases where Presolve
// decided the problem's status unilaterally (all variables fixed,
// infeasibility, or unboundedness). Returns nil for non-terminal undo
// maps.
func (u *PresolveUndo) terminalResult() *Result {
	if u == nil || u.Terminal == NotSolved {
		return nil
	}
	res := &Result{
		Status:  u.Terminal,
		Message: u.TerminalMsg,
		values:  map[*Var]float64{},
		dual:    map[*Constraint]float64{},
		reduced: map[*Var]float64{},
	}
	// Variable values we know (populated on any terminal path).
	for v, val := range u.varValue {
		res.values[v] = val
	}
	// Dropped constraints have zero dual on every terminal path.
	for _, oc := range u.droppedCons {
		res.dual[oc] = 0
	}
	if u.Terminal == Optimal && u.orig != nil {
		// Compute the final objective from the original problem's
		// coefficients.
		obj := u.orig.objConst
		for v, k := range u.orig.objective {
			obj += k * u.varValue[v]
		}
		res.Objective = obj
	}
	return res
}
