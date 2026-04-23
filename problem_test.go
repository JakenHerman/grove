package grove

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestNewProblemBasics(t *testing.T) {
	p := NewProblem("p", Maximize)
	if p.Name() != "p" {
		t.Errorf("name=%q", p.Name())
	}
	if p.Sense() != Maximize {
		t.Errorf("sense=%v", p.Sense())
	}
	if len(p.Vars()) != 0 {
		t.Errorf("expected no vars")
	}
	if len(p.Constraints()) != 0 {
		t.Errorf("expected no constraints")
	}
}

func TestNewVarKindsAndDefaults(t *testing.T) {
	p := NewProblem("p", Minimize)

	c := p.NewVar("c", Continuous)
	if c.Low() != 0 || !math.IsInf(c.High(), 1) {
		t.Errorf("continuous default bounds (%g,%g)", c.Low(), c.High())
	}
	bin := p.NewVar("b", Binary)
	if bin.Low() != 0 || bin.High() != 1 {
		t.Errorf("binary bounds (%g,%g)", bin.Low(), bin.High())
	}
	bin2 := p.NewVar("b2", Binary, Bounds(-5, 99))
	if bin2.Low() != 0 || bin2.High() != 1 {
		t.Errorf("binary should clamp Bounds(-5,99) → (0,1), got (%g,%g)",
			bin2.Low(), bin2.High())
	}
	ranged := p.NewVar("r", Continuous, Bounds(-3, 5))
	if ranged.Low() != -3 || ranged.High() != 5 {
		t.Errorf("ranged bounds (%g,%g)", ranged.Low(), ranged.High())
	}
}

func TestDuplicateVarNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate variable name")
		}
	}()
	p := NewProblem("p", Minimize)
	p.NewVar("x", Continuous)
	p.NewVar("x", Continuous)
}

func TestDuplicateConstraintNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate constraint name")
		}
	}()
	p := NewProblem("p", Minimize)
	x := p.NewVar("x", Continuous)
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("c", Expr{x: 1}, GTE, 0)
	p.AddConstraint("c", Expr{x: 1}, LTE, 5)
}

func TestCrossProblemVarPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when using a Var from another Problem")
		}
	}()
	p1 := NewProblem("p1", Minimize)
	p2 := NewProblem("p2", Minimize)
	x := p1.NewVar("x", Continuous)
	p2.SetObjective(Expr{x: 1})
}

func TestExprAddAndScale(t *testing.T) {
	p := NewProblem("p", Minimize)
	x := p.NewVar("x", Continuous)
	y := p.NewVar("y", Continuous)
	a := Expr{x: 2, y: 3}
	b := Expr{x: -1, y: 4}
	c := a.Add(b)
	if c[x] != 1 || c[y] != 7 {
		t.Errorf("add: %v", c)
	}
	d := a.Scale(2)
	if d[x] != 4 || d[y] != 6 {
		t.Errorf("scale: %v", d)
	}
	// Originals unchanged.
	if a[x] != 2 || a[y] != 3 {
		t.Errorf("Add mutated a: %v", a)
	}
}

func TestValidateBoundsInverted(t *testing.T) {
	p := NewProblem("p", Minimize)
	x := p.NewVar("x", Continuous, Bounds(5, 1))
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("c", Expr{x: 1}, GTE, 0)
	errs := p.Validate()
	if len(errs) == 0 {
		t.Fatal("expected inverted-bounds error")
	}
	if !hasValidationKind(errs, ValidationInvertedBounds) {
		t.Fatalf("errs=%v want ValidationInvertedBounds", errs)
	}
	if !strings.Contains(errs[0].Error(), "empty domain") {
		t.Fatalf("message=%q", errs[0].Error())
	}
}

func TestValidateNaNCoefficient(t *testing.T) {
	p := NewProblem("p", Minimize)
	x := p.NewVar("x", Continuous)
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("c", Expr{x: math.NaN()}, GTE, 0)
	errs := p.Validate()
	if len(errs) == 0 {
		t.Fatal("expected NaN error")
	}
	if !hasValidationKind(errs, ValidationBadCoefficient) {
		t.Fatalf("errs=%v want ValidationBadCoefficient", errs)
	}
}

// hasValidationKind reports whether errs contains a *ValidationError
// with the given Kind.
func hasValidationKind(errs []error, k ValidationErrorKind) bool {
	for _, err := range errs {
		var ve *ValidationError
		if errors.As(err, &ve) && ve.Kind == k {
			return true
		}
	}
	return false
}

func TestValidateDuplicateConstraintName(t *testing.T) {
	// AddConstraint panics on duplicates, so simulate the scenario a
	// file reader or direct slice manipulation could hit.
	p := NewProblem("p", Minimize)
	x := p.NewVar("x", Continuous)
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("c", Expr{x: 1}, GTE, 0)
	// Manually append a second constraint named "c" to bypass the
	// registration-time panic.
	p.constraints = append(p.constraints, &Constraint{
		name:  "c",
		expr:  Expr{x: 1},
		ctype: LTE,
		rhs:   5,
		idx:   len(p.constraints),
	})
	errs := p.Validate()
	if !hasValidationKind(errs, ValidationDuplicateConstraint) {
		t.Fatalf("errs=%v want ValidationDuplicateConstraint", errs)
	}
}

func TestValidateEmptyRow(t *testing.T) {
	p := NewProblem("p", Minimize)
	x := p.NewVar("x", Continuous)
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("empty", Expr{}, LTE, 0)
	errs := p.Validate()
	if !hasValidationKind(errs, ValidationEmptyRow) {
		t.Fatalf("errs=%v want ValidationEmptyRow", errs)
	}
}

func TestValidateZeroRow(t *testing.T) {
	p := NewProblem("p", Minimize)
	x := p.NewVar("x", Continuous)
	y := p.NewVar("y", Continuous)
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("zero", Expr{x: 0, y: 0}, LTE, 0)
	errs := p.Validate()
	if !hasValidationKind(errs, ValidationZeroRow) {
		t.Fatalf("errs=%v want ValidationZeroRow", errs)
	}
}

func TestValidateZeroObjective(t *testing.T) {
	p := NewProblem("p", Minimize)
	x := p.NewVar("x", Continuous)
	y := p.NewVar("y", Continuous)
	p.SetObjective(Expr{x: 0, y: 0})
	p.AddConstraint("c", Expr{x: 1}, GTE, 0)
	errs := p.Validate()
	if !hasValidationKind(errs, ValidationZeroObjective) {
		t.Fatalf("errs=%v want ValidationZeroObjective", errs)
	}
}

func TestValidateNaNObjectiveCoefficient(t *testing.T) {
	p := NewProblem("p", Minimize)
	x := p.NewVar("x", Continuous)
	p.SetObjective(Expr{x: math.NaN()})
	p.AddConstraint("c", Expr{x: 1}, GTE, 0)
	errs := p.Validate()
	if !hasValidationKind(errs, ValidationBadObjectiveCoef) {
		t.Fatalf("errs=%v want ValidationBadObjectiveCoef", errs)
	}
}

func TestValidateInfObjectiveCoefficient(t *testing.T) {
	p := NewProblem("p", Minimize)
	x := p.NewVar("x", Continuous)
	p.SetObjective(Expr{x: math.Inf(1)})
	p.AddConstraint("c", Expr{x: 1}, GTE, 0)
	errs := p.Validate()
	if !hasValidationKind(errs, ValidationBadObjectiveCoef) {
		t.Fatalf("errs=%v want ValidationBadObjectiveCoef", errs)
	}
}

func TestValidateInfCoefficient(t *testing.T) {
	p := NewProblem("p", Minimize)
	x := p.NewVar("x", Continuous)
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("c", Expr{x: math.Inf(-1)}, LTE, 1)
	errs := p.Validate()
	if !hasValidationKind(errs, ValidationBadCoefficient) {
		t.Fatalf("errs=%v want ValidationBadCoefficient", errs)
	}
}

// TestValidateCollectsAllErrors confirms Validate never short-circuits:
// a pathological problem should yield at least four independent errors
// from a single call.
func TestValidateCollectsAllErrors(t *testing.T) {
	p := NewProblem("p", Minimize)
	// Inverted bounds on x.
	x := p.NewVar("x", Continuous, Bounds(5, 1))
	// All-zero objective.
	p.SetObjective(Expr{x: 0})
	// Empty-row constraint.
	p.AddConstraint("empty", Expr{}, LTE, 0)
	// NaN coefficient.
	p.AddConstraint("nan", Expr{x: math.NaN()}, LTE, 0)
	// Duplicate constraint name (manually).
	p.constraints = append(p.constraints, &Constraint{
		name: "empty", expr: Expr{x: 1}, ctype: LTE, rhs: 0,
		idx: len(p.constraints),
	})
	errs := p.Validate()
	for _, want := range []ValidationErrorKind{
		ValidationInvertedBounds,
		ValidationZeroObjective,
		ValidationEmptyRow,
		ValidationBadCoefficient,
		ValidationDuplicateConstraint,
	} {
		if !hasValidationKind(errs, want) {
			t.Errorf("missing %s in %v", want, errs)
		}
	}
}

func TestSolveReturnsErrorOnInvalid(t *testing.T) {
	p := NewProblem("invalid", Minimize)
	r, err := p.Solve()
	if err == nil {
		t.Fatal("expected error")
	}
	if r == nil || r.Status != NotSolved {
		t.Errorf("status=%v", r.Status)
	}
}

func TestSensitivityReport(t *testing.T) {
	prob := NewProblem("nurses", Maximize)
	x := prob.NewVar("nurses_day", Continuous, Bounds(0, Inf))
	y := prob.NewVar("nurses_night", Continuous, Bounds(0, Inf))
	prob.SetObjective(Expr{x: 1, y: 1})
	prob.AddConstraint("min_day", Expr{x: 1}, GTE, 4)
	prob.AddConstraint("min_night", Expr{y: 1}, GTE, 2)
	prob.AddConstraint("budget", Expr{x: 1200, y: 1500}, LTE, 18000)

	r, err := prob.Solve()
	if err != nil || r.Status != Optimal {
		t.Fatalf("solve: status=%v err=%v", r.Status, err)
	}
	rep := SensitivityReport(prob, r)
	if rep == nil {
		t.Fatal("nil report")
	}
	if len(rep.Constraints) != 3 || len(rep.Variables) != 2 {
		t.Fatalf("rep: %+v", rep)
	}
	// budget binding, min_day not binding, min_night binding.
	for _, c := range rep.Constraints {
		switch c.Constraint.name {
		case "min_day":
			if c.Binding {
				t.Errorf("min_day should be slack")
			}
		case "min_night", "budget":
			if !c.Binding {
				t.Errorf("%s should be binding", c.Constraint.name)
			}
		}
	}
	// String renders without panicking.
	if s := rep.String(); !strings.Contains(s, "budget") {
		t.Errorf("rep.String missing 'budget':\n%s", s)
	}
}

func TestWriteLP(t *testing.T) {
	p := NewProblem("simple", Maximize)
	x := p.NewVar("x", Continuous, Bounds(0, 10))
	y := p.NewVar("y", Integer, Bounds(0, Inf))
	z := p.NewVar("z", Binary)
	p.SetObjective(Expr{x: 1, y: 2, z: -3})
	p.AddConstraint("c1", Expr{x: 1, y: 1}, LTE, 5)
	p.AddConstraint("c2", Expr{x: 2, z: 1}, GTE, 1)

	var buf bytes.Buffer
	if err := p.WriteLP(&buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, want := range []string{"Maximize", "obj:", "Subject To", "c1:", "c2:",
		"Bounds", "General", "Binary", "End"} {
		if !strings.Contains(s, want) {
			t.Errorf("LP missing %q in:\n%s", want, s)
		}
	}
}

func TestWriteMPS(t *testing.T) {
	p := NewProblem("simple", Minimize)
	x := p.NewVar("x", Continuous, Bounds(0, 10))
	y := p.NewVar("y", Integer, Bounds(0, Inf))
	p.SetObjective(Expr{x: 1, y: 2})
	p.AddConstraint("c1", Expr{x: 1, y: 1}, LTE, 5)
	p.AddConstraint("c2", Expr{x: 2}, GTE, 1)

	var buf bytes.Buffer
	if err := p.WriteMPS(&buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, want := range []string{"NAME", "ROWS", "COLUMNS", "RHS", "BOUNDS", "ENDATA",
		"INTORG", "INTEND", " L  c1", " G  c2", " N  COST"} {
		if !strings.Contains(s, want) {
			t.Errorf("MPS missing %q in:\n%s", want, s)
		}
	}
}

func TestHiGHSStubReturnsSentinel(t *testing.T) {
	p := NewProblem("p", Minimize)
	x := p.NewVar("x", Continuous)
	p.SetObjective(Expr{x: 1})
	p.AddConstraint("c", Expr{x: 1}, GTE, 0)
	p.Solver = &HiGHS{}
	r, err := p.Solve()
	if !errors.Is(err, ErrHiGHSNotBuilt) {
		t.Fatalf("err=%v want ErrHiGHSNotBuilt", err)
	}
	if r == nil || r.Status != NotSolved {
		t.Errorf("status=%v", r.Status)
	}
}

func TestStringRendering(t *testing.T) {
	p := NewProblem("rendered", Minimize)
	x := p.NewVar("x", Continuous, Bounds(0, 10))
	y := p.NewVar("y", Continuous, Bounds(-Inf, Inf))
	p.SetObjective(Expr{x: 2, y: -1})
	p.AddConstraint("c", Expr{x: 1, y: 1}, LTE, 5)
	out := p.String()
	for _, want := range []string{"Minimize", "obj:", "subject to", "c:", "bounds", "+inf"} {
		if !strings.Contains(out, want) {
			t.Errorf("String missing %q:\n%s", want, out)
		}
	}
}
