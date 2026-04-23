package grove

import (
	"math"
	"strings"
	"testing"
)

// TestPresolveFixedVariable checks that a variable with low == high is
// substituted into the RHS of every constraint and into the objective
// constant, and its value is restored in the expanded result.
func TestPresolveFixedVariable(t *testing.T) {
	// min  2x + 3y + 10z
	// s.t. x + y + z >= 5
	//      y <= 4
	//      x ∈ [0, Inf], y ∈ [0, Inf], z ∈ [2, 2]   ← z is fixed
	//
	// Substituting z = 2: min 2x + 3y + 20, x + y >= 3, y <= 4.
	// Optimum: x = 3, y = 0, z = 2, objective = 26.
	p := NewProblem("fixed_var", Minimize)
	x := p.NewVar("x", Continuous)
	y := p.NewVar("y", Continuous)
	z := p.NewVar("z", Continuous, Bounds(2, 2))
	p.SetObjective(Expr{x: 2, y: 3, z: 10})
	cSum := p.AddConstraint("sum", Expr{x: 1, y: 1, z: 1}, GTE, 5)
	p.AddConstraint("y_cap", Expr{y: 1}, LTE, 4)

	// Check presolve output directly.
	rp, undo, err := p.Presolve(nil)
	if err != nil {
		t.Fatalf("Presolve: %v", err)
	}
	if undo == nil {
		t.Fatal("nil undo")
	}
	if got, want := undo.varValue[z], 2.0; got != want {
		t.Errorf("undo.varValue[z] = %g, want %g", got, want)
	}
	if rp == nil {
		t.Fatal("expected a reduced problem")
	}
	if len(rp.vars) != 2 {
		t.Errorf("reduced vars = %d, want 2", len(rp.vars))
	}
	// The reduced "sum" constraint should have rhs 3, not 5.
	rc := rp.ConstraintByName("sum")
	if rc == nil {
		t.Fatal("sum constraint missing from reduced problem")
	}
	if rc.rhs != 3 {
		t.Errorf("reduced sum rhs = %g, want 3", rc.rhs)
	}

	// End-to-end solve: Solve() should drive the presolve + expand path.
	res, err := p.Solve()
	if err != nil || res.Status != Optimal {
		t.Fatalf("Solve: status=%v err=%v", res.Status, err)
	}
	if !approxEq(res.Objective, 26) {
		t.Errorf("objective = %g, want 26", res.Objective)
	}
	if !approxEq(res.Value(z), 2) {
		t.Errorf("z value = %g, want 2", res.Value(z))
	}
	if !approxEq(res.Value(x), 3) {
		t.Errorf("x value = %g, want 3", res.Value(x))
	}
	if !approxEq(res.Value(y), 0) {
		t.Errorf("y value = %g, want 0", res.Value(y))
	}
	// Duals on the original constraints must still be addressable by
	// the original handles.
	if math.IsNaN(res.Dual(cSum)) {
		t.Errorf("dual(sum) is NaN")
	}
}

// TestPresolveEmptyRow: a constraint that is satisfied trivially by
// problem structure (LTE with RHS ≥ 0 and all fixed LHS terms) should
// be dropped by presolve and its dual reported as zero.
func TestPresolveEmptyRow(t *testing.T) {
	// min  x + y
	// s.t. x + y >= 3                (real constraint)
	//      z <= 100                  (empty-row once z is fixed)
	//      z ∈ [1, 1]                (z is fixed to 1)
	//      x, y >= 0
	p := NewProblem("empty_row", Minimize)
	x := p.NewVar("x", Continuous)
	y := p.NewVar("y", Continuous)
	z := p.NewVar("z", Continuous, Bounds(1, 1))
	p.SetObjective(Expr{x: 1, y: 1})
	p.AddConstraint("need", Expr{x: 1, y: 1}, GTE, 3)
	cTrivial := p.AddConstraint("trivial", Expr{z: 1}, LTE, 100)

	rp, undo, err := p.Presolve(nil)
	if err != nil {
		t.Fatalf("Presolve: %v", err)
	}
	if rp.ConstraintByName("trivial") != nil {
		t.Errorf("trivial constraint should have been dropped")
	}
	if len(undo.droppedCons) != 1 || undo.droppedCons[0] != cTrivial {
		t.Errorf("droppedCons = %v", undo.droppedCons)
	}

	// Solve end-to-end; dual of the dropped constraint must read as 0.
	res, err := p.Solve()
	if err != nil || res.Status != Optimal {
		t.Fatalf("Solve: status=%v err=%v", res.Status, err)
	}
	if !approxEq(res.Objective, 3) {
		t.Errorf("objective = %g, want 3", res.Objective)
	}
	if res.Dual(cTrivial) != 0 {
		t.Errorf("dual(trivial) = %g, want 0", res.Dual(cTrivial))
	}
}

// TestPresolveEmptyRowInfeasible: a row that becomes 0 > 5 after
// substitution must surface as Infeasible without ever touching the
// simplex.
func TestPresolveEmptyRowInfeasible(t *testing.T) {
	p := NewProblem("bad_row", Minimize)
	x := p.NewVar("x", Continuous)
	z := p.NewVar("z", Continuous, Bounds(0, 0))
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("ok", Expr{x: 1}, GTE, 1)
	// After z = 0 this row says 0 >= 5, which is infeasible.
	p.AddConstraint("bad", Expr{z: 1}, GTE, 5)

	res, err := p.Solve()
	if err != nil {
		t.Fatalf("Solve returned error: %v", err)
	}
	if res.Status != Infeasible {
		t.Errorf("status = %v, want Infeasible", res.Status)
	}
	if !strings.Contains(res.Message, "bad") {
		t.Errorf("message should mention the offending constraint; got %q", res.Message)
	}
}

// TestPresolveObjectiveOnlyVariable: a variable that appears only in
// the objective is pinned to its best bound by presolve.
//
//	max  5x - 2y + 3k     (k only appears in the objective)
//	s.t. x + y <= 10
//	     x, y >= 0
//	     k ∈ [0, 7]
//
// Presolve settles k = 0 (we're maximising; coefficient on k is +3;
// maximising 3k with bounds [0, 7] wants the upper bound 7 though).
// Wait — for Maximize with +3 coefficient we want k at its upper
// bound. Optimum: k = 7, objective piece from k = 21. Then the LP
// degenerates to max 5x - 2y with x+y <= 10 → x=10, y=0. Total = 71.
func TestPresolveObjectiveOnlyVariable(t *testing.T) {
	p := NewProblem("obj_only", Maximize)
	x := p.NewVar("x", Continuous)
	y := p.NewVar("y", Continuous)
	k := p.NewVar("k", Continuous, Bounds(0, 7))
	p.SetObjective(Expr{x: 5, y: -2, k: 3})
	p.AddConstraint("cap", Expr{x: 1, y: 1}, LTE, 10)

	rp, undo, err := p.Presolve(nil)
	if err != nil {
		t.Fatalf("Presolve: %v", err)
	}
	if got, want := undo.varValue[k], 7.0; got != want {
		t.Errorf("presolved k value = %g, want %g", got, want)
	}
	// k must not appear in the reduced model.
	for _, rv := range rp.vars {
		if rv.name == "k" {
			t.Errorf("k should have been eliminated from the reduced model")
		}
	}

	res, err := p.Solve()
	if err != nil || res.Status != Optimal {
		t.Fatalf("Solve: status=%v err=%v", res.Status, err)
	}
	if !approxEq(res.Objective, 5*10+3*7) {
		t.Errorf("objective = %g, want %g", res.Objective, 5*10+3*7.0)
	}
	if !approxEq(res.Value(k), 7) {
		t.Errorf("k = %g, want 7", res.Value(k))
	}
	if !approxEq(res.Value(x), 10) {
		t.Errorf("x = %g, want 10", res.Value(x))
	}
}

// TestPresolveObjectiveOnlyUnbounded: a free variable that only
// appears in the objective proves unboundedness without touching the
// simplex.
func TestPresolveObjectiveOnlyUnbounded(t *testing.T) {
	p := NewProblem("unbounded_k", Maximize)
	x := p.NewVar("x", Continuous)
	k := p.NewVar("k", Continuous, Bounds(-Inf, Inf))
	p.SetObjective(Expr{x: 1, k: 1})
	p.AddConstraint("cap", Expr{x: 1}, LTE, 5)

	res, err := p.Solve()
	if err != nil {
		t.Fatalf("Solve returned error: %v", err)
	}
	if res.Status != Unbounded {
		t.Errorf("status = %v, want Unbounded", res.Status)
	}
	if !strings.Contains(res.Message, "k") {
		t.Errorf("message should mention the offending variable; got %q", res.Message)
	}
}

// TestSkipPresolve: setting SkipPresolve = true should bypass the
// presolve pass entirely and still yield a correct optimum. This
// keeps the option honest as a debugging dial.
func TestSkipPresolve(t *testing.T) {
	p := NewProblem("skip_presolve", Minimize)
	x := p.NewVar("x", Continuous)
	y := p.NewVar("y", Continuous)
	z := p.NewVar("z", Continuous, Bounds(2, 2))
	p.SetObjective(Expr{x: 2, y: 3, z: 10})
	p.AddConstraint("sum", Expr{x: 1, y: 1, z: 1}, GTE, 5)
	p.AddConstraint("y_cap", Expr{y: 1}, LTE, 4)
	p.SkipPresolve = true

	res, err := p.Solve()
	if err != nil || res.Status != Optimal {
		t.Fatalf("Solve: status=%v err=%v", res.Status, err)
	}
	if !approxEq(res.Objective, 26) {
		t.Errorf("objective = %g, want 26", res.Objective)
	}
	if !approxEq(res.Value(z), 2) {
		t.Errorf("z = %g, want 2", res.Value(z))
	}
}

// TestPresolveAllVariablesFixed: a model where every variable is
// pinned by its bounds (low == high) is fully solved by presolve; the
// simplex never runs.
func TestPresolveAllVariablesFixed(t *testing.T) {
	p := NewProblem("all_fixed", Minimize)
	x := p.NewVar("x", Continuous, Bounds(3, 3))
	y := p.NewVar("y", Continuous, Bounds(-1, -1))
	p.SetObjective(Expr{x: 2, y: 5})
	p.SetObjectiveConstant(1)
	// Redundant constraint that survives substitution as an empty row.
	p.AddConstraint("ok", Expr{x: 1, y: 1}, LTE, 100)

	res, err := p.Solve()
	if err != nil || res.Status != Optimal {
		t.Fatalf("Solve: status=%v err=%v", res.Status, err)
	}
	if !approxEq(res.Objective, 2*3+5*-1+1) {
		t.Errorf("objective = %g, want %g", res.Objective, 2*3+5*-1+1.0)
	}
	if res.Iterations != 0 {
		t.Errorf("iterations = %d, want 0 (simplex should not run)", res.Iterations)
	}
}
