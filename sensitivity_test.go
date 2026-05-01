package grove

import (
	"math"
	"strings"
	"testing"
)

// approxEqTight is a 1e-9 comparator. Range computations from a clean
// optimal basis come out to machine precision; the looser eps is for
// solver-iteration artifacts in problems that may have multiple optima.
func approxEqTight(a, b float64) bool {
	switch {
	case math.IsInf(a, 1) && math.IsInf(b, 1):
		return true
	case math.IsInf(a, -1) && math.IsInf(b, -1):
		return true
	case math.IsInf(a, 0) || math.IsInf(b, 0):
		return false
	case math.IsNaN(a) || math.IsNaN(b):
		return false
	}
	return math.Abs(a-b) <= 1e-9*(1+math.Abs(b))
}

// TestRangingTextbookMaxLP reproduces the classical 2D max-LP example
// worked through by hand against Bertsimas & Tsitsiklis §5.2.
//
//	max  5 x + 4 y
//	s.t. 6 x + 4 y ≤ 24       (c1)
//	     1 x + 2 y ≤ 6        (c2)
//	     x, y ≥ 0
//
// Optimum: x = 3, y = 1.5, obj = 21. Optimal basis is {x, y}.
//
// Hand-derived ranges (all closed intervals):
//   - obj coef x ∈ [2, 6]
//   - obj coef y ∈ [10/3, 10]
//   - rhs c1 ∈ [12, 36]
//   - rhs c2 ∈ [4, 12]
//
// The basis stays optimal across each interval.
func TestRangingTextbookMaxLP(t *testing.T) {
	p := NewProblem("range-max", Maximize)
	x := p.NewVar("x", Continuous, Bounds(0, Inf))
	y := p.NewVar("y", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{x: 5, y: 4})
	c1 := p.AddConstraint("c1", Expr{x: 6, y: 4}, LTE, 24)
	c2 := p.AddConstraint("c2", Expr{x: 1, y: 2}, LTE, 6)

	r, err := p.Solve()
	if err != nil || r.Status != Optimal {
		t.Fatalf("solve: status=%v err=%v", r.Status, err)
	}

	// Sanity-check the optimum so a basis change up-stack is caught early.
	if !approxEqTight(r.Objective, 21) {
		t.Fatalf("obj = %.12g, want 21", r.Objective)
	}
	if !approxEqTight(r.Value(x), 3) || !approxEqTight(r.Value(y), 1.5) {
		t.Fatalf("(x,y)=(%g, %g), want (3, 1.5)", r.Value(x), r.Value(y))
	}

	type expCoef struct {
		v      *Var
		lo, hi float64
	}
	for _, want := range []expCoef{
		{x, 2, 6},
		{y, 10.0 / 3.0, 10},
	} {
		got := r.ObjCoefRange(want.v)
		if !approxEqTight(got.Lo, want.lo) || !approxEqTight(got.Hi, want.hi) {
			t.Errorf("ObjCoefRange(%s) = [%.12g, %.12g], want [%.12g, %.12g]",
				want.v.Name(), got.Lo, got.Hi, want.lo, want.hi)
		}
	}

	type expRhs struct {
		c      *Constraint
		lo, hi float64
	}
	for _, want := range []expRhs{
		{c1, 12, 36},
		{c2, 4, 12},
	} {
		got := r.RHSRange(want.c)
		if !approxEqTight(got.Lo, want.lo) || !approxEqTight(got.Hi, want.hi) {
			t.Errorf("RHSRange(%s) = [%.12g, %.12g], want [%.12g, %.12g]",
				want.c.Name(), got.Lo, got.Hi, want.lo, want.hi)
		}
	}
}

// TestRangingTextbookMinLP exercises a min-sense problem with a GTE
// constraint and a binding lower-bound row (B&T §5.2 conventions).
//
//	min 3 x + 2 y
//	s.t. x + y ≥ 3            (c1)
//	     x     ≥ 1            (c2)
//	     x, y ≥ 0
//
// Optimum: x = 1, y = 2, obj = 7. Both constraints binding; basis = {x, y}.
//
// Hand-derived ranges:
//   - obj coef x ∈ [2, +∞)
//   - obj coef y ∈ [0, 3]
//   - rhs c1 ∈ [1, +∞)
//   - rhs c2 ∈ [0, 3]
func TestRangingTextbookMinLP(t *testing.T) {
	p := NewProblem("range-min", Minimize)
	x := p.NewVar("x", Continuous, Bounds(0, Inf))
	y := p.NewVar("y", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{x: 3, y: 2})
	c1 := p.AddConstraint("c1", Expr{x: 1, y: 1}, GTE, 3)
	c2 := p.AddConstraint("c2", Expr{x: 1}, GTE, 1)

	r, err := p.Solve()
	if err != nil || r.Status != Optimal {
		t.Fatalf("solve: status=%v err=%v", r.Status, err)
	}
	if !approxEqTight(r.Objective, 7) {
		t.Fatalf("obj = %.12g, want 7", r.Objective)
	}
	if !approxEqTight(r.Value(x), 1) || !approxEqTight(r.Value(y), 2) {
		t.Fatalf("(x,y) = (%g, %g), want (1, 2)", r.Value(x), r.Value(y))
	}

	tests := []struct {
		name   string
		got    [2]float64
		wantLo float64
		wantHi float64
	}{
		{"coef x", [2]float64{r.ObjCoefRange(x).Lo, r.ObjCoefRange(x).Hi}, 2, math.Inf(1)},
		{"coef y", [2]float64{r.ObjCoefRange(y).Lo, r.ObjCoefRange(y).Hi}, 0, 3},
		{"rhs c1", [2]float64{r.RHSRange(c1).Lo, r.RHSRange(c1).Hi}, 1, math.Inf(1)},
		{"rhs c2", [2]float64{r.RHSRange(c2).Lo, r.RHSRange(c2).Hi}, 0, 3},
	}
	for _, tc := range tests {
		if !approxEqTight(tc.got[0], tc.wantLo) || !approxEqTight(tc.got[1], tc.wantHi) {
			t.Errorf("%s = [%.12g, %.12g], want [%.12g, %.12g]",
				tc.name, tc.got[0], tc.got[1], tc.wantLo, tc.wantHi)
		}
	}
}

// TestRangingNonBindingConstraint pins the slack-LTE invariant: a
// non-binding upper-bound constraint can grow without limit, and can
// shrink down to its current LHS before the slack hits zero.
//
//	max 2 x + y
//	s.t. x + y ≤ 10          (c_slack)
//	     x     ≤ 5
//	     y     ≤ 4
//	     x, y ≥ 0
//
// Optimum: x = 5, y = 4, obj = 14, LHS(c_slack) = 9.
func TestRangingNonBindingConstraint(t *testing.T) {
	p := NewProblem("nonbinding", Maximize)
	x := p.NewVar("x", Continuous, Bounds(0, Inf))
	y := p.NewVar("y", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{x: 2, y: 1})
	cSlack := p.AddConstraint("cap", Expr{x: 1, y: 1}, LTE, 10)
	p.AddConstraint("xcap", Expr{x: 1}, LTE, 5)
	p.AddConstraint("ycap", Expr{y: 1}, LTE, 4)

	r, err := p.Solve()
	if err != nil || r.Status != Optimal {
		t.Fatalf("solve: %v %v", r.Status, err)
	}
	if !approxEqTight(r.Objective, 14) {
		t.Fatalf("obj=%g, want 14", r.Objective)
	}

	got := r.RHSRange(cSlack)
	if !approxEqTight(got.Lo, 9) {
		t.Errorf("non-binding RHS lo = %.12g, want 9 (current LHS)", got.Lo)
	}
	if !math.IsInf(got.Hi, 1) {
		t.Errorf("non-binding RHS hi = %g, want +Inf", got.Hi)
	}
}

// TestRangingDualBoundedRanges checks the symmetric LP from the maximize
// test in min form (manual sense flip): the same primal optimum, the
// same ranges, but stated from the minimization side.
func TestRangingDualBoundedRangesMin(t *testing.T) {
	// min -5 x - 4 y s.t. same constraints. Same optimum (x=3, y=1.5),
	// objective = -21. Ranges are reported in user-objective sense, so
	// they invert relative to the max formulation: a coefficient of -5
	// on x can move within [-6, -2] (i.e. the inverted [2, 6]).
	p := NewProblem("range-min-mirror", Minimize)
	x := p.NewVar("x", Continuous, Bounds(0, Inf))
	y := p.NewVar("y", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{x: -5, y: -4})
	c1 := p.AddConstraint("c1", Expr{x: 6, y: 4}, LTE, 24)
	c2 := p.AddConstraint("c2", Expr{x: 1, y: 2}, LTE, 6)

	r, err := p.Solve()
	if err != nil || r.Status != Optimal {
		t.Fatalf("solve: %v %v", r.Status, err)
	}
	if !approxEqTight(r.Objective, -21) {
		t.Fatalf("obj=%.12g, want -21", r.Objective)
	}
	rx := r.ObjCoefRange(x)
	if !approxEqTight(rx.Lo, -6) || !approxEqTight(rx.Hi, -2) {
		t.Errorf("ObjCoefRange(x) = [%.12g, %.12g], want [-6, -2]", rx.Lo, rx.Hi)
	}
	ry := r.ObjCoefRange(y)
	if !approxEqTight(ry.Lo, -10) || !approxEqTight(ry.Hi, -10.0/3.0) {
		t.Errorf("ObjCoefRange(y) = [%.12g, %.12g], want [-10, -10/3]", ry.Lo, ry.Hi)
	}
	r1 := r.RHSRange(c1)
	if !approxEqTight(r1.Lo, 12) || !approxEqTight(r1.Hi, 36) {
		t.Errorf("RHSRange(c1) = [%.12g, %.12g], want [12, 36]", r1.Lo, r1.Hi)
	}
	r2 := r.RHSRange(c2)
	if !approxEqTight(r2.Lo, 4) || !approxEqTight(r2.Hi, 12) {
		t.Errorf("RHSRange(c2) = [%.12g, %.12g], want [4, 12]", r2.Lo, r2.Hi)
	}
}

// TestRangingPresolveSentinels verifies the half-line sentinel returned
// for variables and constraints that did not survive presolve.
func TestRangingPresolveSentinels(t *testing.T) {
	// p has a fixed variable (low == high) that presolve will substitute
	// out, and an empty-row constraint that presolve will drop.
	p := NewProblem("presolve-sentinel", Minimize)
	a := p.NewVar("a", Continuous, Bounds(2, 2)) // fixed at 2
	b := p.NewVar("b", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{a: 1, b: 1})
	dropped := p.AddConstraint("dropped", Expr{a: 1}, LTE, 5) // 2 ≤ 5, redundant
	live := p.AddConstraint("live", Expr{b: 1}, GTE, 3)

	r, err := p.Solve()
	if err != nil || r.Status != Optimal {
		t.Fatalf("solve: %v %v", r.Status, err)
	}
	if got := r.ObjCoefRange(a); !math.IsInf(got.Lo, -1) || !math.IsInf(got.Hi, 1) {
		t.Errorf("fixed var coef range = [%g, %g], want (-Inf, +Inf)", got.Lo, got.Hi)
	}
	if got := r.RHSRange(dropped); !math.IsInf(got.Lo, -1) || !math.IsInf(got.Hi, 1) {
		t.Errorf("dropped constraint RHS range = [%g, %g], want (-Inf, +Inf)", got.Lo, got.Hi)
	}
	// live variable / constraint should report a real interval.
	if got := r.ObjCoefRange(b); math.IsInf(got.Lo, -1) && math.IsInf(got.Hi, 1) {
		t.Errorf("live var coef range came back unbounded; want a finite Lo")
	}
	if got := r.RHSRange(live); math.IsInf(got.Lo, -1) && math.IsInf(got.Hi, 1) {
		t.Errorf("live constraint RHS range came back unbounded; want a finite endpoint")
	}
}

// TestSensitivityStringRenderRanges checks the v0.2 String() output
// includes both the rhs-lo/rhs-hi columns for constraints and the
// coef-lo/coef-hi columns for variables — without breaking the v0.1
// "two tables" shape.
func TestSensitivityStringRenderRanges(t *testing.T) {
	p := NewProblem("render", Maximize)
	x := p.NewVar("x", Continuous, Bounds(0, Inf))
	y := p.NewVar("y", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{x: 5, y: 4})
	p.AddConstraint("c1", Expr{x: 6, y: 4}, LTE, 24)
	p.AddConstraint("c2", Expr{x: 1, y: 2}, LTE, 6)
	r, err := p.Solve()
	if err != nil || r.Status != Optimal {
		t.Fatalf("solve: %v %v", r.Status, err)
	}
	rep := SensitivityReport(p, r)
	out := rep.String()
	for _, want := range []string{
		"Constraints",
		"rhs-lo",
		"rhs-hi",
		"Variables",
		"coef-lo",
		"coef-hi",
		"x", "y", "c1", "c2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rep.String() missing %q\n%s", want, out)
		}
	}
	// Must contain exactly two table headers.
	if strings.Count(out, "Constraints\n") != 1 || strings.Count(out, "Variables\n") != 1 {
		t.Errorf("expected exactly two table headers; got\n%s", out)
	}
}
