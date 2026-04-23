package grove

import (
	"math"
	"strings"
	"testing"
)

const eps = 1e-6

func approxEq(a, b float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return false
	}
	return math.Abs(a-b) <= eps*(1+math.Abs(b))
}

// TestNurseSchedulingExample reproduces the README example end-to-end.
//
//	max  x + y
//	s.t. x ≥ 4
//	     y ≥ 2
//	     1200x + 1500y ≤ 18000
//	     x, y ≥ 0
//
// Optimum: x = 12.5, y = 2, objective = 14.5.
// The budget and y≥2 constraints are binding; x≥4 is slack.
func TestNurseSchedulingExample(t *testing.T) {
	prob := NewProblem("nurses", Maximize)
	x := prob.NewVar("nurses_day", Continuous, Bounds(0, Inf))
	y := prob.NewVar("nurses_night", Continuous, Bounds(0, Inf))

	prob.SetObjective(Expr{x: 1, y: 1})
	cDay := prob.AddConstraint("min_day_coverage", Expr{x: 1}, GTE, 4)
	cNight := prob.AddConstraint("min_night_coverage", Expr{y: 1}, GTE, 2)
	cBudget := prob.AddConstraint("total_budget", Expr{x: 1200, y: 1500}, LTE, 18000)

	res, err := prob.Solve()
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if res.Status != Optimal {
		t.Fatalf("status = %v, want Optimal (%s)", res.Status, res.Message)
	}
	if !approxEq(res.Objective, 14.5) {
		t.Fatalf("objective = %g, want 14.5", res.Objective)
	}
	if !approxEq(res.Value(x), 12.5) {
		t.Fatalf("x = %g, want 12.5", res.Value(x))
	}
	if !approxEq(res.Value(y), 2) {
		t.Fatalf("y = %g, want 2", res.Value(y))
	}

	// Sensitivity: x≥4 is slack so its dual = 0; y≥2 is binding (relaxing
	// it by 1 nurse would let us hire 1 more day-nurse worth, i.e. +1 to
	// objective if budget didn't bind, but budget binds so it's a trade…)
	// Hand-derived shadows for this exact LP: dual(y_min) = 1 - 1500/1200,
	// dual(budget) = 1/1200, dual(day_min) = 0.
	if !approxEq(res.Dual(cDay), 0) {
		t.Errorf("dual(day_min) = %g, want 0", res.Dual(cDay))
	}
	wantBudgetDual := 1.0 / 1200.0
	if !approxEq(res.Dual(cBudget), wantBudgetDual) {
		t.Errorf("dual(budget) = %g, want %g", res.Dual(cBudget), wantBudgetDual)
	}
	wantNightDual := 1 - 1500.0/1200.0
	if !approxEq(res.Dual(cNight), wantNightDual) {
		t.Errorf("dual(night_min) = %g, want %g", res.Dual(cNight), wantNightDual)
	}
}

// TestSimpleMin: min 3x + 2y s.t. x+y >= 10, x>=0, y>=0.  Optimum x=0, y=10, obj=20.
func TestSimpleMin(t *testing.T) {
	p := NewProblem("simple_min", Minimize)
	x := p.NewVar("x", Continuous, Bounds(0, Inf))
	y := p.NewVar("y", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{x: 3, y: 2})
	p.AddConstraint("c", Expr{x: 1, y: 1}, GTE, 10)
	r, err := p.Solve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Optimal {
		t.Fatalf("status %v: %s", r.Status, r.Message)
	}
	if !approxEq(r.Objective, 20) {
		t.Errorf("obj=%g, want 20", r.Objective)
	}
	if !approxEq(r.Value(x), 0) || !approxEq(r.Value(y), 10) {
		t.Errorf("(x,y)=(%g,%g), want (0,10)", r.Value(x), r.Value(y))
	}
}

// TestEqualityConstraint: max x+y s.t. x+y = 5, x<=3, x>=0, y>=0.
// Optimum: any (x, 5-x) with x in [0,3]; objective = 5.
func TestEqualityConstraint(t *testing.T) {
	p := NewProblem("eq", Maximize)
	x := p.NewVar("x", Continuous, Bounds(0, 3))
	y := p.NewVar("y", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{x: 1, y: 1})
	p.AddConstraint("sum", Expr{x: 1, y: 1}, EQ, 5)
	r, err := p.Solve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Optimal {
		t.Fatalf("status %v: %s", r.Status, r.Message)
	}
	if !approxEq(r.Objective, 5) {
		t.Errorf("obj=%g want 5", r.Objective)
	}
}

// TestInfeasible: x>=5, x<=3.
func TestInfeasible(t *testing.T) {
	p := NewProblem("inf", Minimize)
	x := p.NewVar("x", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("lo", Expr{x: 1}, GTE, 5)
	p.AddConstraint("hi", Expr{x: 1}, LTE, 3)
	r, err := p.Solve()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if r.Status != Infeasible {
		t.Fatalf("status=%v (%s), want Infeasible", r.Status, r.Message)
	}
}

// TestUnbounded: max x s.t. x >= 0.
func TestUnbounded(t *testing.T) {
	p := NewProblem("unb", Maximize)
	x := p.NewVar("x", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("trivial", Expr{x: 1}, GTE, 0)
	r, err := p.Solve()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if r.Status != Unbounded {
		t.Fatalf("status=%v (%s), want Unbounded", r.Status, r.Message)
	}
}

// TestSingleVariable: max x s.t. x <= 7, x >= 2.
func TestSingleVariable(t *testing.T) {
	p := NewProblem("one", Maximize)
	x := p.NewVar("x", Continuous, Bounds(2, 7))
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("dummy", Expr{x: 1}, GTE, 0)
	r, err := p.Solve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Optimal {
		t.Fatalf("status=%v: %s", r.Status, r.Message)
	}
	if !approxEq(r.Value(x), 7) {
		t.Errorf("x=%g want 7", r.Value(x))
	}
}

// TestNegativeBounds: free variable.
//
// min 2x + y, s.t. x + y = 4, x in (-inf, +inf), y >= 0.
// Setting x = 4 - y, objective = 2(4-y) + y = 8 - y. Minimize: y → +∞,
// then x → -∞. So this should be unbounded.
func TestFreeVariableUnbounded(t *testing.T) {
	p := NewProblem("free", Minimize)
	x := p.NewVar("x", Continuous, Bounds(-Inf, Inf))
	y := p.NewVar("y", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{x: 2, y: 1})
	p.AddConstraint("c", Expr{x: 1, y: 1}, EQ, 4)
	r, _ := p.Solve()
	if r.Status != Unbounded {
		t.Fatalf("status=%v, want Unbounded", r.Status)
	}
}

// TestFreeVariableOptimal: bounded objective on a free variable.
//
// min x + y, s.t. x + y >= 3, x in (-inf, +inf), y in (-inf, +inf).
// Both vars free; the constraint is the lower bound on x+y. min(x+y)=3.
func TestFreeVariableOptimal(t *testing.T) {
	p := NewProblem("free_ok", Minimize)
	x := p.NewVar("x", Continuous, Bounds(-Inf, Inf))
	y := p.NewVar("y", Continuous, Bounds(-Inf, Inf))
	p.SetObjective(Expr{x: 1, y: 1})
	p.AddConstraint("c", Expr{x: 1, y: 1}, GTE, 3)
	r, err := p.Solve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Optimal {
		t.Fatalf("status=%v (%s)", r.Status, r.Message)
	}
	if !approxEq(r.Objective, 3) {
		t.Errorf("obj=%g want 3", r.Objective)
	}
}

// TestNegativeRHS: x - y >= -5 with min y.
//
// min y s.t. x - y >= -5, 0 <= x <= 10, y >= 0. y = max(0, x-(-5)) ... hmm.
// Actually constraint is y <= x + 5. Min y=0 (since y>=0). At y=0 and any x in [0,10] this is satisfied as long as x+5 >= 0, always true.
func TestNegativeRHS(t *testing.T) {
	p := NewProblem("negrhs", Minimize)
	x := p.NewVar("x", Continuous, Bounds(0, 10))
	y := p.NewVar("y", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{y: 1})
	p.AddConstraint("c", Expr{x: 1, y: -1}, GTE, -5)
	r, err := p.Solve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Optimal {
		t.Fatalf("status=%v (%s)", r.Status, r.Message)
	}
	if !approxEq(r.Objective, 0) {
		t.Errorf("obj=%g want 0", r.Objective)
	}
}

// TestDegenerateBland: a known degenerate LP that cycles under naive
// largest-coefficient pivoting. Beale's example, slightly adapted, with
// Bland's rule it must terminate finitely. We don't check the exact
// optimum here; we just want to confirm the solver returns Optimal.
func TestDegenerateBland(t *testing.T) {
	// Beale's cycling example (Bertsimas & Tsitsiklis Example 3.6):
	//   max  3/4 x1 - 150 x2 + 1/50 x3 - 6 x4
	//   s.t. 1/4 x1 -  60 x2 - 1/25 x3 + 9 x4 <= 0
	//        1/2 x1 -  90 x2 - 1/50 x3 + 3 x4 <= 0
	//        x3 <= 1
	//        x_i >= 0
	p := NewProblem("beale", Maximize)
	x1 := p.NewVar("x1", Continuous, Bounds(0, Inf))
	x2 := p.NewVar("x2", Continuous, Bounds(0, Inf))
	x3 := p.NewVar("x3", Continuous, Bounds(0, Inf))
	x4 := p.NewVar("x4", Continuous, Bounds(0, Inf))
	p.SetObjective(Expr{x1: 0.75, x2: -150, x3: 1.0 / 50, x4: -6})
	p.AddConstraint("c1", Expr{x1: 0.25, x2: -60, x3: -1.0 / 25, x4: 9}, LTE, 0)
	p.AddConstraint("c2", Expr{x1: 0.5, x2: -90, x3: -1.0 / 50, x4: 3}, LTE, 0)
	p.AddConstraint("c3", Expr{x3: 1}, LTE, 1)
	r, err := p.Solve()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if r.Status != Optimal {
		t.Fatalf("status=%v (%s)", r.Status, r.Message)
	}
	// Best known objective value for this LP is 1/20 = 0.05 (achieved by
	// pushing x3 up to its bound while keeping the other vars at 0).
	if !approxEq(r.Objective, 0.05) {
		t.Errorf("obj=%g want 0.05", r.Objective)
	}
}

// TestValidateNoVars: build a problem with no variables.
func TestValidateNoVars(t *testing.T) {
	p := NewProblem("empty", Minimize)
	if err := p.Validate(); err == nil {
		t.Fatal("want error")
	}
}

// TestValidateNoObjective: variables but no objective.
func TestValidateNoObjective(t *testing.T) {
	p := NewProblem("noobj", Minimize)
	p.NewVar("x", Continuous)
	if err := p.Validate(); err == nil {
		t.Fatal("want error")
	}
}

// TestBoundedVariable: x in [3, 5], min x + 2y, y >= 1.
func TestBoundedVariable(t *testing.T) {
	p := NewProblem("bounded", Minimize)
	x := p.NewVar("x", Continuous, Bounds(3, 5))
	y := p.NewVar("y", Continuous, Bounds(1, Inf))
	p.SetObjective(Expr{x: 1, y: 2})
	p.AddConstraint("c", Expr{x: 1, y: 1}, GTE, 0) // trivial
	r, err := p.Solve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Optimal {
		t.Fatalf("status=%v (%s)", r.Status, r.Message)
	}
	if !approxEq(r.Value(x), 3) || !approxEq(r.Value(y), 1) {
		t.Errorf("(x,y)=(%g,%g) want (3,1)", r.Value(x), r.Value(y))
	}
}

// TestUpperBoundConstraintEnforced: confirm finite-upper-bound vars are
// actually constrained.
func TestUpperBoundConstraintEnforced(t *testing.T) {
	p := NewProblem("ub", Maximize)
	x := p.NewVar("x", Continuous, Bounds(0, 7))
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("dummy", Expr{x: 1}, GTE, 0)
	r, _ := p.Solve()
	if r.Status != Optimal || !approxEq(r.Value(x), 7) {
		t.Fatalf("got status=%v, x=%g", r.Status, r.Value(x))
	}
}

// TestLPRelaxationOnInfeasibleILP documents a known v0.1 limitation:
// grove currently solves the LP relaxation even when the user declares
// Integer variables. On this particular model PuLP+CBC (real B&B) reports
// Infeasible, because the constraints
//
//	x1 - x2 in [0.5, 0.75],  x2 - x3 in [0.95, 1.25]
//
// have no integer solution (no integer lies in [0.5, 0.75]). grove v0.1,
// lacking branch-and-bound, instead returns Optimal at a non-integer
// vertex (0.75, 0.25, -1) with objective -0.25.
//
// This test pins the current behaviour AND asserts that [Result.Warnings]
// surfaces the LP-relaxation-only caveat, so users can't silently accept
// a wrong answer on an integer problem.
//
// When v0.4's branch-and-bound driver ships, this test will flip:
//   - Status should become Infeasible
//   - Warnings should be empty
//   - A companion test should check the LP-relaxation variant returns
//     Optimal unchanged.
//
// Source of the model: user-supplied ILP, cross-checked against
// CBC via examples/usertest2/verify_pulp.py.
func TestLPRelaxationOnInfeasibleILP(t *testing.T) {
	p := NewProblem("lp_relax_vs_infeasible_ilp", Minimize)
	x1 := p.NewVar("x1", Integer, Bounds(-1, 1))
	x2 := p.NewVar("x2", Integer, Bounds(-1, 1))
	x3 := p.NewVar("x3", Integer, Bounds(-1, 1))
	p.SetObjective(Expr{x1: 2, x2: -3, x3: 1})
	p.AddConstraint("c1", Expr{x1: 1, x2: -1}, GTE, 0.5)
	p.AddConstraint("c2", Expr{x1: 1, x2: -1}, LTE, 0.75)
	p.AddConstraint("c3", Expr{x2: 1, x3: -1}, LTE, 1.25)
	p.AddConstraint("c4", Expr{x2: 1, x3: -1}, GTE, 0.95)

	r, err := p.Solve()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// Current v0.1 behaviour: Optimal with the LP-vertex values.
	if r.Status != Optimal {
		t.Fatalf("v0.1 behaviour: expected Optimal (LP relaxation), got %v; "+
			"if v0.4 has shipped, flip this test to expect Infeasible", r.Status)
	}
	if !approxEq(r.Objective, -0.25) {
		t.Errorf("LP-relaxation objective = %g, want -0.25", r.Objective)
	}

	// The whole point of this test: grove must surface the LP-only caveat.
	offenders := r.NonIntegerIntegerVars(p)
	if len(offenders) == 0 {
		t.Fatal("expected NonIntegerIntegerVars to report at least one non-integer variable; " +
			"if v0.4 branch-and-bound is in, the test above should have caught infeasibility first")
	}
	if len(r.Warnings) == 0 {
		t.Fatal("expected Result.Warnings to contain the LP-relaxation caveat")
	}
	foundCaveat := false
	for _, w := range r.Warnings {
		if strings.HasPrefix(w, WarnLPRelaxationOnly) {
			foundCaveat = true
			break
		}
	}
	if !foundCaveat {
		t.Errorf("Warnings missing %q-prefixed entry; got %v", WarnLPRelaxationOnly, r.Warnings)
	}
	if !strings.Contains(r.Message, WarnLPRelaxationOnly) {
		t.Errorf("Result.Message should mention %q; got %q", WarnLPRelaxationOnly, r.Message)
	}

	// Sanity: the two offending variables are x1 and x2 (0.75 and 0.25);
	// x3 comes out at -1.0 which is integer.
	if got := vars(offenders); got != "x1,x2" {
		t.Errorf("offenders = %s, want x1,x2", got)
	}

	// And: the LP-relaxation sibling (Continuous) must NOT emit a warning.
	pLP := NewProblem("lp_only", Minimize)
	y1 := pLP.NewVar("x1", Continuous, Bounds(-1, 1))
	y2 := pLP.NewVar("x2", Continuous, Bounds(-1, 1))
	y3 := pLP.NewVar("x3", Continuous, Bounds(-1, 1))
	pLP.SetObjective(Expr{y1: 2, y2: -3, y3: 1})
	pLP.AddConstraint("c1", Expr{y1: 1, y2: -1}, GTE, 0.5)
	pLP.AddConstraint("c2", Expr{y1: 1, y2: -1}, LTE, 0.75)
	pLP.AddConstraint("c3", Expr{y2: 1, y3: -1}, LTE, 1.25)
	pLP.AddConstraint("c4", Expr{y2: 1, y3: -1}, GTE, 0.95)
	rLP, _ := pLP.Solve()
	if rLP.Status != Optimal || !approxEq(rLP.Objective, -0.25) {
		t.Errorf("LP-relaxation sibling: status=%v obj=%g", rLP.Status, rLP.Objective)
	}
	if len(rLP.Warnings) != 0 {
		t.Errorf("LP-relaxation (Continuous) should not emit warnings; got %v", rLP.Warnings)
	}
}

// TestIntegerLPReturnsIntegerNoWarning pins the converse: when the LP
// relaxation happens to return an all-integer optimum for an ILP, no
// warning fires (because the integer vertex is a valid ILP optimum too —
// this is the standard LP-relaxation-implies-ILP-optimality result used
// in branch-and-bound).
func TestIntegerLPReturnsIntegerNoWarning(t *testing.T) {
	p := NewProblem("integer_lp", Maximize)
	x1 := p.NewVar("x1", Integer, Bounds(-15, 15))
	x2 := p.NewVar("x2", Integer, Bounds(-15, 15))
	x3 := p.NewVar("x3", Integer, Bounds(-15, 15))
	p.SetObjective(Expr{x1: 2, x2: -3, x3: 1})
	p.AddConstraint("c1", Expr{x1: 1, x2: -1, x3: 1}, LTE, 5)
	p.AddConstraint("c2", Expr{x1: 1, x2: -1, x3: 4}, LTE, 7)

	r, err := p.Solve()
	if err != nil || r.Status != Optimal {
		t.Fatalf("status=%v err=%v", r.Status, err)
	}
	if len(r.Warnings) != 0 {
		t.Errorf("unexpected warnings on all-integer LP optimum: %v", r.Warnings)
	}
	if len(r.NonIntegerIntegerVars(p)) != 0 {
		t.Errorf("expected no non-integer offenders, got %v", vars(r.NonIntegerIntegerVars(p)))
	}
	_ = x3
}

// vars is a test helper that joins variable names with commas, in order.
func vars(vs []*Var) string {
	names := make([]string, len(vs))
	for i, v := range vs {
		names[i] = v.Name()
	}
	return strings.Join(names, ",")
}
