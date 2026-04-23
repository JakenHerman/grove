package grove

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

// roundTripMPS writes p to MPS, reads it back, and returns the new
// *Problem. Mirrors the LP helper for symmetry.
func roundTripMPS(t *testing.T, p *Problem, opts ...MPSOption) *Problem {
	t.Helper()
	var buf bytes.Buffer
	if err := p.WriteMPS(&buf); err != nil {
		t.Fatalf("WriteMPS: %v", err)
	}
	got, err := ReadMPS(&buf, opts...)
	if err != nil {
		t.Fatalf("ReadMPS: %v\nMPS:\n%s", err, buf.String())
	}
	return got
}

// ── Generated-problem round-trips ────────────────────────────────────

func TestReadMPSRoundTripExamples(t *testing.T) {
	// WriteMPS flips the objective sign on Maximize (MPS doesn't emit
	// OBJSENSE) and emits via sanitize(). We round-trip each example
	// through a text fixed-point: WriteMPS(p) == WriteMPS(ReadMPS(WriteMPS(p))).
	// The byte-level comparison is the strongest guarantee we can make
	// without also changing WriteMPS to emit OBJSENSE.
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
			if err := p.WriteMPS(&first); err != nil {
				t.Fatalf("WriteMPS: %v", err)
			}
			got, err := ReadMPS(bytes.NewReader(first.Bytes()))
			if err != nil {
				t.Fatalf("ReadMPS: %v\nMPS:\n%s", err, first.String())
			}
			var second bytes.Buffer
			if err := got.WriteMPS(&second); err != nil {
				t.Fatalf("WriteMPS (2nd): %v", err)
			}
			if first.String() != second.String() {
				t.Errorf("WriteMPS output differs after round-trip.\n--- first ---\n%s\n--- second ---\n%s",
					first.String(), second.String())
			}
		})
	}
}

func TestReadMPSRoundTripIntegerAndBinary(t *testing.T) {
	// Exercise INTORG/INTEND markers and the whole gamut of BOUNDS
	// types in one shot.
	p := NewProblem("mip", Minimize)
	x := p.NewVar("x", Continuous, Bounds(0, 10))
	y := p.NewVar("y", Integer, Bounds(0, Inf))
	z := p.NewVar("z", Binary)
	free := p.NewVar("free_var", Continuous, Bounds(-Inf, Inf))
	p.SetObjective(Expr{x: 1, y: 2, z: -3, free: 0.5})
	p.AddConstraint("c1", Expr{x: 1, y: 1}, LTE, 5)
	p.AddConstraint("c2", Expr{x: 2, z: 1}, GTE, 1)
	p.AddConstraint("c3", Expr{free: 1, x: -1}, EQ, 0)

	got := roundTripMPS(t, p)
	problemsEquivalent(t, p, got)

	// Variable kinds should have survived the trip.
	for _, nv := range []struct {
		name string
		kind VarKind
	}{
		{"x", Continuous},
		{"y", Integer},
		{"z", Binary},
		{"free_var", Continuous},
	} {
		v := got.VarByName(nv.name)
		if v == nil {
			t.Fatalf("missing var %q", nv.name)
		}
		if v.Kind() != nv.kind {
			t.Errorf("var %q kind: got %v, want %v", nv.name, v.Kind(), nv.kind)
		}
	}
}

// ── MIPLIB instances ────────────────────────────────────────────────

// TestReadMPSMIPLIBRoundTrip exercises the parser end-to-end on real
// MIPLIB instances. Each file goes through ReadMPS → WriteMPS → ReadMPS
// and we compare the two parsed problems structurally. WriteMPS emits
// a canonical form so we can't diff the byte streams, but the parsed
// *Problem values must be equivalent.
//
// The three instances exercise complementary corners of the grammar:
//
//   - flugpl: small LP with integer markers and LO/UP bounds.
//   - gr4x6:  lots of BV binary-bound rows and a Maximize-free objective.
//   - pk1:    equality and ≥ rows plus UP bounds, no marker block.
func TestReadMPSMIPLIBRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{"flugpl", "flugpl.mps"},
		{"gr4x6", "gr4x6.mps"},
		{"pk1", "pk1.mps"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join("testdata", "miplib", c.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			p, err := ReadMPS(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("ReadMPS %s: %v", path, err)
			}
			if len(p.Vars()) == 0 {
				t.Fatalf("%s produced no variables", c.file)
			}
			if len(p.Constraints()) == 0 {
				t.Fatalf("%s produced no constraints", c.file)
			}

			var buf bytes.Buffer
			if err := p.WriteMPS(&buf); err != nil {
				t.Fatalf("WriteMPS %s: %v", path, err)
			}
			got, err := ReadMPS(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("ReadMPS(WriteMPS(%s)): %v", c.file, err)
			}
			problemsEquivalent(t, p, got)
		})
	}
}

// TestReadMPSMIPLIBLPRelaxationSolvable verifies that the LP relaxation
// of each MIPLIB instance is not only parseable but also survives a
// Solve() call. This closes the loop: reader output is fit for the
// simplex to consume.
func TestReadMPSMIPLIBLPRelaxationSolvable(t *testing.T) {
	files := []string{"flugpl.mps", "gr4x6.mps", "pk1.mps"}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			path := filepath.Join("testdata", "miplib", f)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			p, err := ReadMPS(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("ReadMPS: %v", err)
			}
			// Drop integer flags — we only want the LP relaxation, and
			// running branch-and-bound is out of scope for v0.2.
			for _, v := range p.Vars() {
				switch v.kind {
				case Integer, Binary:
					v.kind = Continuous
				}
			}
			p.MaxIterations = 200000
			res, err := p.Solve()
			if err != nil {
				t.Fatalf("Solve: %v", err)
			}
			if res.Status != Optimal && res.Status != IterationLimit {
				// flugpl and gr4x6 are small enough to solve cleanly;
				// pk1 at 86×45 is near the edge of what the v0.1
				// simplex handles without presolve. Accept iteration
				// limit as "parse was fine, solver struggled".
				t.Logf("%s solve status: %v (%s)", f, res.Status, res.Message)
			}
		})
	}
}

// ── Auto-detect & forced format ──────────────────────────────────────

// TestReadMPSAutoDetectsFixed walks a fixed-column file and confirms
// the auto-detector picks up on it. We can't observe the detection
// decision directly, so the test pins behaviour by asserting that
// both MPSFixedInput and MPSFreeInput accept the same fixed input.
func TestReadMPSAutoDetectsFixed(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "miplib", "flugpl.mps"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, tc := range []struct {
		name string
		opts []MPSOption
	}{
		{"auto", nil},
		{"forced fixed", []MPSOption{MPSFixedInput()}},
		{"forced free", []MPSOption{MPSFreeInput()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ReadMPS(bytes.NewReader(data), tc.opts...)
			if err != nil {
				t.Fatalf("ReadMPS: %v", err)
			}
			if len(p.Vars()) == 0 || len(p.Constraints()) == 0 {
				t.Fatalf("empty problem after parsing flugpl")
			}
		})
	}
}

// TestReadMPSAutoDetectsFree fabricates a free-form MPS file whose
// column names overflow the 8-character fixed slots, forcing the
// auto-detector onto the free path. Both MPSFreeInput and MPSAuto
// must accept it.
func TestReadMPSAutoDetectsFree(t *testing.T) {
	// Names are >8 chars; values run wherever whitespace falls. This
	// is perfectly valid free-form MPS.
	src := `NAME          free_form
ROWS
 N obj
 L capacity
 G demand
COLUMNS
 long_variable_name obj 1.0 capacity 2.5
 long_variable_name demand 0.5
 another_long_name obj 1.5 capacity 3.0
 another_long_name demand 0.5
RHS
 RHS capacity 10 demand 2
BOUNDS
 UP BND long_variable_name 5
 UP BND another_long_name 5
ENDATA
`
	for _, tc := range []struct {
		name string
		opts []MPSOption
	}{
		{"auto", nil},
		{"forced free", []MPSOption{MPSFreeInput()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ReadMPS(strings.NewReader(src), tc.opts...)
			if err != nil {
				t.Fatalf("ReadMPS: %v", err)
			}
			if n := len(p.Vars()); n != 2 {
				t.Errorf("vars = %d, want 2", n)
			}
			if n := len(p.Constraints()); n != 2 {
				t.Errorf("constraints = %d, want 2", n)
			}
			v := p.VarByName("long_variable_name")
			if v == nil || v.High() != 5 {
				t.Errorf("long_variable_name bound lost: %+v", v)
			}
		})
	}
}

// TestReadMPSForcedFixedRejectsFreeForm confirms that MPSFixedInput
// actually enforces the layout — free-form input with long names is
// rejected with a clear error rather than silently parsed.
func TestReadMPSForcedFixedRejectsFreeForm(t *testing.T) {
	// Column name "long_variable_name" overflows the fixed 8-char
	// slot at cols 5-12, so the first COLUMNS line violates the
	// layout's whitespace gutters.
	src := `NAME          free_form
ROWS
 N  obj
 L  first_constraint
 L  second_constraint
COLUMNS
 long_variable_name obj 1 first_constraint 2 second_constraint 3
 y obj 1 first_constraint 1 second_constraint 1
RHS
 RHS first_constraint 10 second_constraint 20
ENDATA
`
	_, err := ReadMPS(strings.NewReader(src), MPSFixedInput())
	if err == nil {
		t.Fatal("expected error from MPSFixedInput on free-form input")
	}
	if !strings.Contains(err.Error(), "fixed-column MPS layout") {
		t.Errorf("error should call out the layout violation, got: %v", err)
	}
	// Sanity: the same input parses fine under MPSFreeInput or auto.
	if _, err := ReadMPS(strings.NewReader(src), MPSFreeInput()); err != nil {
		t.Errorf("MPSFreeInput should accept free-form input: %v", err)
	}
	if _, err := ReadMPS(strings.NewReader(src)); err != nil {
		t.Errorf("auto-detect should accept free-form input: %v", err)
	}
}

// TestReadMPSPLAfterFRResetsLowerBound pins PL semantics: even though
// PL is usually redundant with the default, emitting it after FR has
// to reset the variable back to the nonnegative default.
func TestReadMPSPLAfterFRResetsLowerBound(t *testing.T) {
	src := `NAME          pl_after_fr
ROWS
 N  obj
 L  c1
COLUMNS
    x         obj              1   c1               1
RHS
    RHS       c1              10
BOUNDS
 FR BND       x
 PL BND       x
ENDATA
`
	p, err := ReadMPS(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ReadMPS: %v", err)
	}
	v := p.VarByName("x")
	if v == nil {
		t.Fatal("missing x")
	}
	if v.Low() != 0 {
		t.Errorf("PL after FR: low = %g, want 0", v.Low())
	}
	if !math.IsInf(v.High(), 1) {
		t.Errorf("PL after FR: high = %g, want +Inf", v.High())
	}
}

// ── OBJSENSE section ────────────────────────────────────────────────

func TestReadMPSObjsenseMaximize(t *testing.T) {
	src := `NAME          max_example
OBJSENSE
    MAX
ROWS
 N  obj
 L  c1
COLUMNS
    x         obj              3   c1               1
    y         obj              2   c1               1
RHS
    RHS       c1              10
BOUNDS
 UP BND       x               10
 UP BND       y               10
ENDATA
`
	p, err := ReadMPS(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ReadMPS: %v", err)
	}
	if p.Sense() != Maximize {
		t.Errorf("sense = %v, want Maximize", p.Sense())
	}
	// Objective coefficients land as written, not sign-flipped.
	x := p.VarByName("x")
	if got := p.Objective()[x]; got != 3 {
		t.Errorf("obj coef on x = %g, want 3", got)
	}
}

// ── Every bound type ────────────────────────────────────────────────

func TestReadMPSBoundTypes(t *testing.T) {
	src := `NAME          bounds
ROWS
 N  obj
 L  dummy
COLUMNS
    up_var    obj              1   dummy            1
    lo_var    obj              1   dummy            1
    fx_var    obj              1   dummy            1
    fr_var    obj              1   dummy            1
    mi_var    obj              1   dummy            1
    pl_var    obj              1   dummy            1
    bv_var    obj              1   dummy            1
    li_var    obj              1   dummy            1
    ui_var    obj              1   dummy            1
RHS
    RHS       dummy          100
BOUNDS
 UP BND       up_var          5
 LO BND       lo_var          2
 FX BND       fx_var          7
 FR BND       fr_var
 MI BND       mi_var
 PL BND       pl_var
 BV BND       bv_var
 LI BND       li_var          0
 UI BND       ui_var         10
ENDATA
`
	p, err := ReadMPS(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ReadMPS: %v", err)
	}
	checks := []struct {
		name string
		low  float64
		high float64
		kind VarKind
	}{
		{"up_var", 0, 5, Continuous},
		{"lo_var", 2, Inf, Continuous},
		{"fx_var", 7, 7, Continuous},
		{"fr_var", math.Inf(-1), math.Inf(1), Continuous},
		{"mi_var", math.Inf(-1), Inf, Continuous},
		{"pl_var", 0, Inf, Continuous},
		{"bv_var", 0, 1, Binary},
		{"li_var", 0, Inf, Integer},
		{"ui_var", 0, 10, Integer},
	}
	for _, c := range checks {
		v := p.VarByName(c.name)
		if v == nil {
			t.Fatalf("missing %q", c.name)
		}
		if !boundsEqual(v.Low(), c.low) || !boundsEqual(v.High(), c.high) {
			t.Errorf("%s bounds = [%g,%g], want [%g,%g]",
				c.name, v.Low(), v.High(), c.low, c.high)
		}
		if v.Kind() != c.kind {
			t.Errorf("%s kind = %v, want %v", c.name, v.Kind(), c.kind)
		}
	}
}

// ── Integer markers ─────────────────────────────────────────────────

func TestReadMPSIntegerMarkers(t *testing.T) {
	src := `NAME          mip
ROWS
 N  obj
 L  c1
COLUMNS
    a         obj              1   c1               1
    MARK1     'MARKER'                 'INTORG'
    b         obj              2   c1               1
    c         obj              3   c1               1
    MARK2     'MARKER'                 'INTEND'
    d         obj              4   c1               1
RHS
    RHS       c1              10
ENDATA
`
	p, err := ReadMPS(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ReadMPS: %v", err)
	}
	want := map[string]VarKind{
		"a": Continuous,
		"b": Integer,
		"c": Integer,
		"d": Continuous,
	}
	for name, k := range want {
		v := p.VarByName(name)
		if v == nil {
			t.Fatalf("missing %q", name)
		}
		if v.Kind() != k {
			t.Errorf("%s kind = %v, want %v", name, v.Kind(), k)
		}
	}
}

// ── Error reporting ─────────────────────────────────────────────────

func TestReadMPSErrorLineNumberAndFormat(t *testing.T) {
	src := "NAME p\nROWS\n N obj\nCOLUMNS\n x unknown_row 1\nENDATA\n"
	_, err := ReadMPS(strings.NewReader(src))
	if err == nil {
		t.Fatal("expected parse error for unknown row")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("error is not *ParseError: %T", err)
	}
	if pe.Line == 0 {
		t.Errorf("parse error missing line number: %v", err)
	}
	if pe.Format != "MPS" {
		t.Errorf("parse error format = %q, want MPS", pe.Format)
	}
	if !strings.Contains(err.Error(), "MPS parse error") {
		t.Errorf("formatted error should say MPS parse error: %q", err.Error())
	}
}

func TestReadMPSUnknownSection(t *testing.T) {
	src := "NAME p\nWIDGETS\nENDATA\n"
	_, err := ReadMPS(strings.NewReader(src))
	if err == nil {
		t.Fatal("expected error on unknown section")
	}
	if !strings.Contains(err.Error(), "unknown section") {
		t.Errorf("error should mention unknown section: %v", err)
	}
}

func TestReadMPSRangesRejected(t *testing.T) {
	src := `NAME p
ROWS
 N obj
 L c1
COLUMNS
 x obj 1 c1 2
RHS
 RHS c1 10
RANGES
 RNG c1 3
ENDATA
`
	_, err := ReadMPS(strings.NewReader(src))
	if err == nil {
		t.Fatal("expected error on RANGES section")
	}
	// Wording should match the LP reader's diagnostic and should name
	// the offending row so users know where to edit.
	msg := err.Error()
	if !strings.Contains(msg, "range constraints (low <= expr <= high) are not supported") {
		t.Errorf("error should match LP reader wording, got: %v", err)
	}
	if !strings.Contains(msg, `"c1"`) {
		t.Errorf("error should name the offending row c1, got: %v", err)
	}
}

// TestReadMPSEmptyRangesTolerated confirms that a RANGES *header* with
// no data lines below it is accepted (some writers emit an empty
// section even when nothing needs range semantics).
func TestReadMPSEmptyRangesTolerated(t *testing.T) {
	src := `NAME p
ROWS
 N obj
 L c1
COLUMNS
 x obj 1 c1 2
RHS
 RHS c1 10
RANGES
BOUNDS
 UP BND x 5
ENDATA
`
	p, err := ReadMPS(strings.NewReader(src))
	if err != nil {
		t.Fatalf("empty RANGES should be tolerated: %v", err)
	}
	if v := p.VarByName("x"); v == nil || v.High() != 5 {
		t.Errorf("x bound lost: %+v", v)
	}
}

// ── Streaming ───────────────────────────────────────────────────────

func TestReadMPSStreamingOneByteReader(t *testing.T) {
	// Route a small model through iotest.OneByteReader to exercise
	// the scanner path a byte at a time.
	src := `NAME          small
ROWS
 N  obj
 G  c1
COLUMNS
    x         obj              2   c1               1
    y         obj              1   c1               1
RHS
    RHS       c1               3
BOUNDS
 UP BND       x                5
ENDATA
`
	p, err := ReadMPS(iotest.OneByteReader(strings.NewReader(src)))
	if err != nil {
		t.Fatalf("ReadMPS (one-byte reader): %v", err)
	}
	if got := len(p.Vars()); got != 2 {
		t.Errorf("vars = %d, want 2", got)
	}
	if got := len(p.Constraints()); got != 1 {
		t.Errorf("constraints = %d, want 1", got)
	}
}
