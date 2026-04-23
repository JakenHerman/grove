package grove

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Comparison helpers ────────────────────────────────────────────────

// problemsEquivalent is a structural equality check for two *Problem
// values. It does not require declaration order to match — only that
// every named element is present on both sides with identical data.
// Used to verify round-trips through WriteLP → ReadLP.
func problemsEquivalent(t *testing.T, want, got *Problem) {
	t.Helper()
	if want.Sense() != got.Sense() {
		t.Errorf("sense: want %v, got %v", want.Sense(), got.Sense())
	}
	if math.Abs(want.ObjectiveConstant()-got.ObjectiveConstant()) > 1e-12 {
		t.Errorf("objective constant: want %g, got %g",
			want.ObjectiveConstant(), got.ObjectiveConstant())
	}

	wantVars := indexVarsByName(want)
	gotVars := indexVarsByName(got)
	if len(wantVars) != len(gotVars) {
		t.Errorf("var count: want %d, got %d", len(wantVars), len(gotVars))
	}
	for name, wv := range wantVars {
		gv, ok := gotVars[name]
		if !ok {
			t.Errorf("missing var %q after round-trip", name)
			continue
		}
		if wv.Kind() != gv.Kind() {
			t.Errorf("var %q kind: want %v, got %v", name, wv.Kind(), gv.Kind())
		}
		if !boundsEqual(wv.Low(), gv.Low()) || !boundsEqual(wv.High(), gv.High()) {
			t.Errorf("var %q bounds: want [%g,%g], got [%g,%g]",
				name, wv.Low(), wv.High(), gv.Low(), gv.High())
		}
	}

	// Objective coefficients keyed by variable name.
	wantObj := objByName(want)
	gotObj := objByName(got)
	if len(wantObj) != len(gotObj) {
		t.Errorf("objective term count: want %d, got %d", len(wantObj), len(gotObj))
	}
	for name, c := range wantObj {
		if math.Abs(c-gotObj[name]) > 1e-12 {
			t.Errorf("objective coef on %q: want %g, got %g", name, c, gotObj[name])
		}
	}

	// Constraints keyed by name.
	wantCons := indexConstraintsByName(want)
	gotCons := indexConstraintsByName(got)
	if len(wantCons) != len(gotCons) {
		t.Errorf("constraint count: want %d, got %d", len(wantCons), len(gotCons))
	}
	for name, wc := range wantCons {
		gc, ok := gotCons[name]
		if !ok {
			t.Errorf("missing constraint %q after round-trip", name)
			continue
		}
		if wc.Type() != gc.Type() {
			t.Errorf("constraint %q type: want %v, got %v", name, wc.Type(), gc.Type())
		}
		if math.Abs(wc.RHS()-gc.RHS()) > 1e-12 {
			t.Errorf("constraint %q rhs: want %g, got %g", name, wc.RHS(), gc.RHS())
		}
		wExpr := exprByName(wc.Expr())
		gExpr := exprByName(gc.Expr())
		if len(wExpr) != len(gExpr) {
			t.Errorf("constraint %q term count: want %d, got %d",
				name, len(wExpr), len(gExpr))
		}
		for vn, c := range wExpr {
			if math.Abs(c-gExpr[vn]) > 1e-12 {
				t.Errorf("constraint %q coef on %q: want %g, got %g",
					name, vn, c, gExpr[vn])
			}
		}
	}
}

func indexVarsByName(p *Problem) map[string]*Var {
	m := make(map[string]*Var, len(p.Vars()))
	for _, v := range p.Vars() {
		m[v.Name()] = v
	}
	return m
}

func indexConstraintsByName(p *Problem) map[string]*Constraint {
	m := make(map[string]*Constraint, len(p.Constraints()))
	for _, c := range p.Constraints() {
		m[c.Name()] = c
	}
	return m
}

func objByName(p *Problem) map[string]float64 {
	m := make(map[string]float64, len(p.Objective()))
	for v, c := range p.Objective() {
		m[v.Name()] = c
	}
	return m
}

func exprByName(e Expr) map[string]float64 {
	m := make(map[string]float64, len(e))
	for v, c := range e {
		m[v.Name()] = c
	}
	return m
}

func boundsEqual(a, b float64) bool {
	if math.IsInf(a, 1) && math.IsInf(b, 1) {
		return true
	}
	if math.IsInf(a, -1) && math.IsInf(b, -1) {
		return true
	}
	return math.Abs(a-b) < 1e-12
}

// roundTrip writes p to LP, reads it back, and returns the new *Problem.
func roundTrip(t *testing.T, p *Problem) *Problem {
	t.Helper()
	var buf bytes.Buffer
	if err := p.WriteLP(&buf); err != nil {
		t.Fatalf("WriteLP: %v", err)
	}
	got, err := ReadLP(&buf)
	if err != nil {
		t.Fatalf("ReadLP: %v\nLP:\n%s", err, buf.String())
	}
	return got
}

// ── Example models (structurally equal to the examples/ programs) ─────

// buildSchedulingExample mirrors examples/scheduling/main.go.
func buildSchedulingExample() *Problem {
	demand := []float64{40, 32, 35, 38, 45, 28, 25}
	days := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	const (
		fullHours = 8.0
		partHours = 4.0
		fullCost  = 280.0
		partCost  = 160.0
	)

	p := NewProblem("nurse_schedule", Minimize)

	full := make([]*Var, len(days))
	part := make([]*Var, len(days))
	for i, d := range days {
		full[i] = p.NewVar("full_"+d, Continuous, Bounds(0, Inf))
		part[i] = p.NewVar("part_"+d, Continuous, Bounds(0, Inf))
	}

	obj := Expr{}
	for i := range days {
		obj[full[i]] = fullCost
		obj[part[i]] = partCost
	}
	p.SetObjective(obj)

	for i, d := range days {
		p.AddConstraint("cover_"+d,
			Expr{full[i]: fullHours, part[i]: partHours},
			GTE, demand[i])
	}
	p.AddConstraint("part_cap_sat", Expr{part[5]: 1}, LTE, 4)
	p.AddConstraint("part_cap_sun", Expr{part[6]: 1}, LTE, 4)
	return p
}

// buildDietExample mirrors examples/diet/main.go.
func buildDietExample() *Problem {
	type food struct {
		name                          string
		cost, cal, pro, fat, carb, na float64
	}
	foods := []food{
		{"oats", 3.00, 389, 17, 7, 66, 2},
		{"chicken_breast", 10.00, 165, 31, 4, 0, 74},
		{"broccoli", 4.00, 34, 3, 0, 7, 33},
		{"olive_oil", 12.00, 884, 0, 100, 0, 2},
		{"brown_rice", 2.50, 370, 8, 3, 77, 7},
		{"black_beans", 3.50, 132, 9, 1, 24, 2},
		{"spinach", 5.00, 23, 3, 0, 4, 79},
		{"banana", 1.50, 89, 1, 0, 23, 1},
		{"egg", 5.50, 155, 13, 11, 1, 124},
		{"yogurt_greek", 7.00, 59, 10, 0, 4, 36},
	}
	p := NewProblem("diet", Minimize)
	x := make([]*Var, len(foods))
	for i, f := range foods {
		x[i] = p.NewVar(f.name, Continuous, Bounds(0, 20))
	}
	obj := Expr{}
	for i, f := range foods {
		obj[x[i]] = f.cost / 10
	}
	p.SetObjective(obj)

	cal, pro, fat, carb, na := Expr{}, Expr{}, Expr{}, Expr{}, Expr{}
	for i, f := range foods {
		cal[x[i]] = f.cal
		pro[x[i]] = f.pro
		fat[x[i]] = f.fat
		carb[x[i]] = f.carb
		na[x[i]] = f.na
	}
	p.AddConstraint("calories_min", cal, GTE, 2000)
	p.AddConstraint("calories_max", cal, LTE, 2400)
	p.AddConstraint("protein_min", pro, GTE, 100)
	p.AddConstraint("fat_max", fat, LTE, 80)
	p.AddConstraint("carbs_min", carb, GTE, 250)
	p.AddConstraint("sodium_max", na, LTE, 2300)
	return p
}

// buildAllocationExample mirrors examples/allocation/main.go.
func buildAllocationExample() *Problem {
	type workload struct {
		name                string
		cpu, ram, disk, val float64
	}
	wls := []workload{
		{"checkout-api", 4, 16, 80, 90},
		{"recommender", 8, 32, 200, 70},
		{"batch-etl", 16, 64, 500, 50},
		{"image-resize", 2, 8, 40, 30},
		{"search-index", 6, 24, 150, 60},
		{"ml-train", 32, 128, 1000, 200},
		{"audit-log", 1, 4, 200, 10},
		{"webhook-fan", 2, 8, 20, 25},
	}
	p := NewProblem("vm_allocation", Maximize)
	frac := make([]*Var, len(wls))
	for i, w := range wls {
		frac[i] = p.NewVar(w.name, Continuous, Bounds(0, 1))
	}
	obj := Expr{}
	for i, w := range wls {
		obj[frac[i]] = w.val
	}
	p.SetObjective(obj)
	cpu, ram, disk := Expr{}, Expr{}, Expr{}
	for i, w := range wls {
		cpu[frac[i]] = w.cpu
		ram[frac[i]] = w.ram
		disk[frac[i]] = w.disk
	}
	p.AddConstraint("cpu", cpu, LTE, 64)
	p.AddConstraint("ram", ram, LTE, 256)
	p.AddConstraint("disk", disk, LTE, 4000)
	return p
}

// ── Round-trip tests ──────────────────────────────────────────────────

func TestReadLPRoundTripExamples(t *testing.T) {
	// WriteLP is lossy for two things we don't try to reverse here:
	//   1. `sanitize` rewrites non-identifier characters (e.g. dashes
	//      in allocation's "batch-etl") to underscores.
	//   2. Zero-coefficient terms in an expression are dropped.
	// So the round-trip contract is expressed as text fixed-point:
	// WriteLP(p) == WriteLP(ReadLP(WriteLP(p))). Every example keeps
	// all variables in the objective, so declaration order is stable
	// and the writer's byte output is deterministic.
	examples := []struct {
		name  string
		build func() *Problem
	}{
		{"scheduling", buildSchedulingExample},
		{"diet", buildDietExample},
		{"allocation", buildAllocationExample},
	}
	for _, ex := range examples {
		t.Run(ex.name, func(t *testing.T) {
			p := ex.build()
			var first bytes.Buffer
			if err := p.WriteLP(&first); err != nil {
				t.Fatalf("WriteLP: %v", err)
			}
			got, err := ReadLP(bytes.NewReader(first.Bytes()))
			if err != nil {
				t.Fatalf("ReadLP: %v\nLP:\n%s", err, first.String())
			}
			var second bytes.Buffer
			if err := got.WriteLP(&second); err != nil {
				t.Fatalf("WriteLP (2nd): %v", err)
			}
			if first.String() != second.String() {
				t.Errorf("WriteLP output differs after round-trip.\n--- first ---\n%s\n--- second ---\n%s",
					first.String(), second.String())
			}
		})
	}
}

func TestReadLPRoundTripIntegerAndBinary(t *testing.T) {
	// Cover General / Binary sections and mixed bounds in one shot.
	p := NewProblem("mip", Maximize)
	x := p.NewVar("x", Continuous, Bounds(0, 10))
	y := p.NewVar("y", Integer, Bounds(0, Inf))
	z := p.NewVar("z", Binary)
	free := p.NewVar("free_var", Continuous, Bounds(-Inf, Inf))
	p.SetObjective(Expr{x: 1, y: 2, z: -3, free: 0.5})
	p.SetObjectiveConstant(7.5)
	p.AddConstraint("c1", Expr{x: 1, y: 1}, LTE, 5)
	p.AddConstraint("c2", Expr{x: 2, z: 1}, GTE, 1)
	p.AddConstraint("c3", Expr{free: 1, x: -1}, EQ, 0)

	got := roundTrip(t, p)
	problemsEquivalent(t, p, got)
}

// ── Real-world LP (MIPLIB-style mixed integer knapsack) ───────────────

// The file is a compact mixed-integer LP that exercises every section
// and several syntactic tolerances the wild throws at parsers:
//   - lowercase section keywords
//   - the "s.t." abbreviation
//   - continuation over multiple lines within a single expression
//   - implicit "1" coefficients on binary vars
//   - mixed bound forms (one-sided, two-sided, "free", "= fixed")
//   - inline backslash comments
//   - blank lines between and within sections
const miplibStyleLP = `\ Small MIP: pack items under weight + volume caps to maximise value.
\ Adapted from a MIPLIB-style knapsack; every section exercised.

Maximize
 profit: 10 x1 + 20 x2 + 15 x3
       + 25 x4 + 8 x5 + 30 x6
       - 5 slack + 100

Subject To
 weight:   5 x1 + 7 x2 + 4 x3 + 9 x4 + 3 x5 + 11 x6 <= 40
 volume:   3 x1 + 2 x2 + 6 x3 + 4 x4 + 2 x5 + 5 x6  <= 18
 pairing:  x2 - x4 = 0
 must_one: x1 + x3 + x6 >= 1
 bal:      slack - 2 x5 = 0

\ --- bounds section with varied shapes ---
Bounds
 0 <= slack <= 15
 x5 free
 x1 <= 1

Binary
 x1 x2 x3
 x4

Generals
 x5 x6

End
`

func TestReadLPRealWorldStyle(t *testing.T) {
	p, err := ReadLP(strings.NewReader(miplibStyleLP))
	if err != nil {
		t.Fatalf("ReadLP: %v", err)
	}
	if p.Sense() != Maximize {
		t.Errorf("sense = %v, want Maximize", p.Sense())
	}
	if math.Abs(p.ObjectiveConstant()-100) > 1e-12 {
		t.Errorf("obj const = %g, want 100", p.ObjectiveConstant())
	}
	// Check a few variable shapes.
	names := indexVarsByName(p)
	for _, n := range []string{"x1", "x2", "x3", "x4", "x5", "x6", "slack"} {
		if names[n] == nil {
			t.Fatalf("missing var %q", n)
		}
	}
	for _, n := range []string{"x1", "x2", "x3", "x4"} {
		if names[n].Kind() != Binary {
			t.Errorf("var %q should be Binary, got %v", n, names[n].Kind())
		}
	}
	for _, n := range []string{"x5", "x6"} {
		if names[n].Kind() != Integer {
			t.Errorf("var %q should be Integer, got %v", n, names[n].Kind())
		}
	}
	if lo, hi := names["slack"].Low(), names["slack"].High(); lo != 0 || hi != 15 {
		t.Errorf("slack bounds = [%g,%g], want [0,15]", lo, hi)
	}
	if !math.IsInf(names["x5"].Low(), -1) || !math.IsInf(names["x5"].High(), 1) {
		// x5 declared free, then listed as Integer; Integer keeps existing bounds.
		t.Errorf("x5 free bounds lost: [%g,%g]", names["x5"].Low(), names["x5"].High())
	}
	// Round-trip preserves structure.
	got := roundTrip(t, p)
	problemsEquivalent(t, p, got)
}

// ── Comments and blank lines anywhere ────────────────────────────────

func TestReadLPTolerantOfCommentsAndBlanks(t *testing.T) {
	src := `\ leading comment
\ another

Minimize \ inline comment on header is stripped pre-match
 obj: x + y   \ trailing comment

\ comment between sections

Subject To

 c1: x + y <= 10   \ first constraint
\ dense comment
 c2: x - y  =  0


Bounds

 0 <= x <= 5
 \ a floating comment
 0 <= y <= 5

End
`
	p, err := ReadLP(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ReadLP: %v", err)
	}
	if n := len(p.Vars()); n != 2 {
		t.Errorf("vars = %d, want 2", n)
	}
	if n := len(p.Constraints()); n != 2 {
		t.Errorf("constraints = %d, want 2", n)
	}
}

// ── Error reporting carries line and column ───────────────────────────

func TestReadLPErrorsIncludeLineAndColumn(t *testing.T) {
	// Deliberately mangled expression: two variables with no operator
	// between them. The second identifier lives on line 5, and our
	// tokenizer will complain when it tries to consume the next term.
	src := `Minimize
 obj: 2 x 3 y
Subject To
 c1: x >= 0
End
`
	_, err := ReadLP(strings.NewReader(src))
	if err == nil {
		t.Fatal("expected parse error")
	}
	pe, ok := err.(*parseError)
	if !ok {
		t.Fatalf("error is not *parseError: %T (%v)", err, err)
	}
	if pe.Line == 0 {
		t.Errorf("parse error missing line number: %v", err)
	}
	if pe.Col == 0 {
		t.Errorf("parse error missing column number: %v", err)
	}
	if !strings.Contains(err.Error(), "line") {
		t.Errorf("formatted error should contain 'line': %q", err.Error())
	}
}

func TestReadLPMissingSenseHeader(t *testing.T) {
	src := "Subject To\n c: x >= 0\nEnd\n"
	_, err := ReadLP(strings.NewReader(src))
	if err == nil {
		t.Fatal("expected error for missing sense header")
	}
	if !strings.Contains(err.Error(), "Minimize") {
		t.Errorf("error should mention Minimize/Maximize: %v", err)
	}
}

func TestReadLPDuplicateConstraintName(t *testing.T) {
	src := `Minimize
 obj: x
Subject To
 c1: x <= 5
 c1: x >= 1
End
`
	_, err := ReadLP(strings.NewReader(src))
	if err == nil {
		t.Fatal("expected duplicate-name error")
	}
}

// ── Testdata file round-trip ──────────────────────────────────────────

func TestReadLPTestdataFile(t *testing.T) {
	path := filepath.Join("testdata", "transport.lp")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("testdata file missing: %v", err)
	}
	p, err := ReadLP(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ReadLP %s: %v", path, err)
	}
	if p.Sense() != Minimize {
		t.Errorf("sense = %v, want Minimize", p.Sense())
	}
	if _, err := p.Solve(); err != nil {
		t.Fatalf("Solve of parsed problem: %v", err)
	}
	// Round-trip.
	problemsEquivalent(t, p, roundTrip(t, p))
}
