package grove

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// Solver is the swappable backend interface that [Problem.Solve] dispatches
// to. The default implementation is [SimplexSolver]. v0.3 will ship a
// HiGHS CGO backend that satisfies this same interface (see highs_stub.go).
//
// Implementations must:
//   - never mutate the supplied [Problem]
//   - return a non-nil [Result] even when an error is also returned
//   - set [Result.Status] meaningfully
type Solver interface {
	Solve(p *Problem) (*Result, error)
}

// SimplexSolver is the pure-Go two-phase revised simplex implementation.
//
// It implements Dantzig's simplex with Bland's anti-cycling rule for
// pivot selection. The standard reference is Bertsimas & Tsitsiklis,
// "Introduction to Linear Optimization", chapters 3 (geometry & simplex)
// and 4 (anti-cycling, two-phase method).
//
// Complexity: O(m^2 * n) per pivot in the dense tableau form used here;
// memory O(m * n). For models above ~10k constraints/variables you will
// want the HiGHS backend planned for v0.3.
type SimplexSolver struct {
	// Verbose causes the solver to print phase headers, pivot choices,
	// and the final tableau to Out (or stderr by default).
	Verbose bool

	// MaxIterations caps the total pivot count across both phases.
	// Zero defaults to 50 * (m + n).
	MaxIterations int

	// Tolerance is the slack used for "≈ 0" comparisons. Zero defaults
	// to 1e-9.
	Tolerance float64

	// Out is where Verbose output is written. Defaults to os.Stderr.
	Out io.Writer
}

// Solve runs the two-phase simplex method on p.
func (s *SimplexSolver) Solve(p *Problem) (*Result, error) {
	tol := s.Tolerance
	if tol <= 0 {
		tol = 1e-9
	}
	out := s.Out
	if out == nil {
		out = os.Stderr
	}

	std, err := buildStandard(p)
	if err != nil {
		return &Result{Status: NotSolved, Message: err.Error()}, err
	}
	if s.Verbose {
		fmt.Fprintln(out, std.describe())
	}

	maxIter := s.MaxIterations
	if maxIter <= 0 {
		maxIter = 50 * (std.m + std.n)
		if maxIter < 500 {
			maxIter = 500
		}
	}

	res := &Result{values: map[*Var]float64{}, dual: map[*Constraint]float64{}, reduced: map[*Var]float64{}}

	// ---- Phase I: minimize sum of artificials -----------------------------
	// Bertsimas & Tsitsiklis §3.5: introduce artificial variables a_i ≥ 0
	// in every row that lacks an obvious basis column, then minimize Σ a_i.
	// If the optimal value is > 0, the original LP is infeasible.
	phaseICost := make([]float64, std.n)
	for _, j := range std.artificials {
		phaseICost[j] = 1
	}
	if s.Verbose {
		fmt.Fprintln(out, "── Phase I ─────────────────────────────────────")
	}
	status1, iters1 := simplexLoop(std, phaseICost, maxIter, tol, s.Verbose, out, "I ")
	res.Iterations += iters1
	switch status1 {
	case Unbounded:
		// Phase I cannot be unbounded if all artificials are bounded
		// below by 0 — this would indicate a numerical breakdown.
		res.Status = NumericalError
		res.Message = "phase I unexpectedly unbounded"
		return res, fmt.Errorf("grove: %s", res.Message)
	case IterationLimit:
		res.Status = IterationLimit
		res.Message = "phase I hit iteration limit"
		return res, fmt.Errorf("grove: %s", res.Message)
	case NumericalError:
		res.Status = NumericalError
		res.Message = "numerical breakdown in phase I"
		return res, fmt.Errorf("grove: %s", res.Message)
	}
	phaseIObj := dot(phaseICost, std.x)
	if phaseIObj > 1e-6 {
		res.Status = Infeasible
		res.Message = fmt.Sprintf("phase I residual = %g (problem is infeasible)", phaseIObj)
		return res, nil
	}

	// Drive artificials out of the basis if any remain at zero level.
	if err := std.expelArtificials(tol); err != nil {
		res.Status = NumericalError
		res.Message = err.Error()
		return res, err
	}

	// ---- Phase II: minimize the real (already-negated for max) objective --
	if s.Verbose {
		fmt.Fprintln(out, "── Phase II ────────────────────────────────────")
	}
	// Mask out artificial columns by costing them prohibitively (they were
	// already expelled, but a stray re-entry would be wrong).
	c2 := make([]float64, std.n)
	copy(c2, std.c)
	for _, j := range std.artificials {
		c2[j] = math.Inf(1) // forbid re-entry; we'll skip them in pricing
	}
	status2, iters2 := simplexLoop(std, c2, maxIter, tol, s.Verbose, out, "II")
	res.Iterations += iters2
	switch status2 {
	case Unbounded:
		res.Status = Unbounded
		res.Message = "objective is unbounded along a feasible ray"
		return res, nil
	case IterationLimit:
		res.Status = IterationLimit
		res.Message = "phase II hit iteration limit"
		return res, fmt.Errorf("grove: %s", res.Message)
	case NumericalError:
		res.Status = NumericalError
		res.Message = "numerical breakdown in phase II"
		return res, fmt.Errorf("grove: %s", res.Message)
	}

	// ---- Recover user values, objective, sensitivity ---------------------
	res.Status = Optimal
	internalObj := dot(std.c, std.x) // (already in min sense, with any sign flip)
	if std.maximized {
		res.Objective = -internalObj + std.objConst + p.objConst
	} else {
		res.Objective = internalObj + std.objConst + p.objConst
	}

	for _, uv := range p.vars {
		var val float64
		for _, slot := range std.slotsFor[uv] {
			val += slot.sign * std.x[slot.col]
		}
		val += std.shift[uv]
		res.values[uv] = val
	}

	// Dual values y = c_B^T B^{-1}. With our tableau form B has been reduced
	// to identity, so c_B^T B^{-1} equals the negated reduced costs of the
	// columns that started as identity (the original artificials). Each row
	// has an artificial in a known column → y_i = c_a_i - r_a_i where the
	// reduced cost is for the *true* objective (Phase II cost vector).
	// We compute y_i directly from the tableau.
	y := std.dualValuesFor(c2)

	for _, c := range p.constraints {
		ri, ok := std.rowOf[c]
		if !ok {
			continue
		}
		yi := y[ri]
		// Account for any row-negation we did to keep b ≥ 0 and for the
		// objective sense flip.
		shadow := yi * std.rowSign[ri]
		if std.maximized {
			shadow = -shadow
		}
		res.dual[c] = shadow
	}

	// Reduced costs for user variables. For each user variable we sum the
	// signed reduced costs of its internal split components (most user
	// vars have a single component).
	rc := std.reducedCosts(c2, y)
	for _, uv := range p.vars {
		var r float64
		for _, slot := range std.slotsFor[uv] {
			r += slot.sign * rc[slot.col]
		}
		if std.maximized {
			r = -r
		}
		res.reduced[uv] = r
	}

	if s.Verbose {
		fmt.Fprintf(out, "── Optimal: obj = %g (iters=%d) ────────────────\n", res.Objective, res.Iterations)
	}
	return res, nil
}

// ─── Standard form construction ──────────────────────────────────────────
//
// We convert the user model into:
//     minimize  c^T x
//     subject to A x = b,  x ≥ 0,  b ≥ 0
//
// This is the canonical form §3.1 of Bertsimas & Tsitsiklis.
//
// Variable transformation: for each user variable u with bounds [lo, hi]
//   lo = 0,  hi = +∞:                   x = u                     (1 slot, sign +1, shift 0)
//   lo finite,  hi = +∞:                u = x + lo                (1 slot, sign +1, shift lo)
//   lo finite,  hi finite:              u = x + lo, x ≤ hi-lo     (1 slot + upper bound row)
//   lo = -∞,  hi finite:                u = hi - x                (1 slot, sign -1, shift hi)
//   lo = -∞,  hi = +∞ (free):           u = x_pos - x_neg         (2 slots)

type slot struct {
	col  int     // column index in std.A
	sign float64 // contribution to user value
}

type stdForm struct {
	A [][]float64 // m × n (row-major)
	b []float64   // m
	c []float64   // n; phase II objective in *minimization* form

	m, n int

	basis []int // length m, basis[i] is the column basic in row i

	x []float64 // length n; current solution (filled by simplexLoop)

	// Mappings back to user model
	slotsFor map[*Var][]slot     // user var → internal columns
	shift    map[*Var]float64    // user var → constant offset (e.g. lo or hi)
	rowOf    map[*Constraint]int // user constraint → internal row
	rowSign  []float64           // length m; -1 if we negated this row, +1 otherwise

	// origIdentityCol[i] is the column that was a unit vector e_i in A
	// *before* any pivots — either the artificial we added for row i or
	// the slack of an LTE row with non-negative RHS. This is what the
	// dual extraction y^T = c_B^T B^{-1} keys off, since the i-th column
	// of B^{-1} equals the current-tableau entries of that original
	// identity column. (Bertsimas & Tsitsiklis §4.5.)
	origIdentityCol []int

	// Internal column metadata
	artificials []int // columns that are artificial vars (must end at 0)

	// Sense flip
	maximized bool // true if user said Maximize (we negated c internally)

	// Constant accumulated by variable shifts. user_obj = (±)internal_obj + objConst (+ p.objConst)
	objConst float64
}

func buildStandard(p *Problem) (*stdForm, error) {
	std := &stdForm{
		slotsFor:  map[*Var][]slot{},
		shift:     map[*Var]float64{},
		rowOf:     map[*Constraint]int{},
		maximized: p.sense == Maximize,
	}

	// ── Step 1: allocate internal columns for user variables ────────────
	// Track per-user-variable the rhs adjustment that the shift introduces
	// for any constraint that mentions the variable, plus any explicit
	// upper-bound row we need to add.
	type ubRow struct {
		col   int     // internal column to bound
		bound float64 // rhs (must be ≥ 0)
	}
	var upperBounds []ubRow
	addCol := func() int {
		std.A = appendCol(std.A) // grow each row by one zero column (rows added later use n)
		std.c = append(std.c, 0)
		std.n++
		return std.n - 1
	}

	for _, v := range p.vars {
		switch {
		case math.IsInf(v.low, -1) && math.IsInf(v.high, 1):
			// Free: split into positive and negative parts.
			cp := addCol()
			cn := addCol()
			std.slotsFor[v] = []slot{{col: cp, sign: +1}, {col: cn, sign: -1}}
			std.shift[v] = 0
		case math.IsInf(v.low, -1):
			// (-∞, hi]: u = hi - x, x ≥ 0, no upper bound on x.
			c := addCol()
			std.slotsFor[v] = []slot{{col: c, sign: -1}}
			std.shift[v] = v.high
		case math.IsInf(v.high, 1):
			// [lo, +∞): u = x + lo.
			c := addCol()
			std.slotsFor[v] = []slot{{col: c, sign: +1}}
			std.shift[v] = v.low
		default:
			// [lo, hi]: u = x + lo, x ≤ hi - lo.
			c := addCol()
			std.slotsFor[v] = []slot{{col: c, sign: +1}}
			std.shift[v] = v.low
			width := v.high - v.low
			if width < 0 {
				return nil, fmt.Errorf("grove: variable %q has empty domain", v.name)
			}
			upperBounds = append(upperBounds, ubRow{col: c, bound: width})
		}
	}

	// ── Step 2: translate the objective into internal column space ──────
	for v, coef := range p.objective {
		actualCoef := coef
		if std.maximized {
			actualCoef = -coef
		}
		// Subtract the constant contribution from the variable shift.
		// E.g. u = x + lo  →  coef * u = coef*x + coef*lo, the constant
		// piece is added back to the objective when reporting to the user.
		std.objConst += coef * std.shift[v]
		for _, sl := range std.slotsFor[v] {
			std.c[sl.col] += sl.sign * actualCoef
		}
	}

	// ── Step 3: convert each user constraint into Ax = b form ───────────
	// For now we just add rows to A (and zero-extend later for added slacks
	// and artificials).
	type rowSpec struct {
		coeffs map[int]float64 // internal col -> coef
		ctype  ConstraintType
		rhs    float64
		owner  *Constraint // nil for upper-bound rows
	}
	var rows []rowSpec
	addRow := func(spec rowSpec) {
		rows = append(rows, spec)
	}

	for _, c := range p.constraints {
		coeffs := map[int]float64{}
		rhs := c.rhs
		for v, k := range c.expr {
			rhs -= k * std.shift[v]
			for _, sl := range std.slotsFor[v] {
				coeffs[sl.col] += sl.sign * k
			}
		}
		addRow(rowSpec{coeffs: coeffs, ctype: c.ctype, rhs: rhs, owner: c})
	}
	for _, ub := range upperBounds {
		addRow(rowSpec{
			coeffs: map[int]float64{ub.col: 1},
			ctype:  LTE,
			rhs:    ub.bound,
			owner:  nil,
		})
	}

	// ── Step 4: introduce slacks/surplus and possibly negate rows so b ≥ 0
	// Then add artificials where the row has no obvious basis column.
	std.m = len(rows)
	if std.m == 0 {
		return nil, fmt.Errorf("grove: problem has no constraints")
	}
	// Re-shape A so it has m rows of length n now (we'll grow n further).
	std.A = make([][]float64, std.m)
	for i := range std.A {
		std.A[i] = make([]float64, std.n)
	}
	std.b = make([]float64, std.m)
	std.rowSign = make([]float64, std.m)
	std.basis = make([]int, std.m)

	for i, r := range rows {
		for col, k := range r.coeffs {
			std.A[i][col] = k
		}
		std.b[i] = r.rhs
		std.rowSign[i] = 1
		if r.owner != nil {
			std.rowOf[r.owner] = i
		}
	}

	// Add slack/surplus per row first (one column per inequality row).
	rowSlackCol := make([]int, std.m)
	for i := range rowSlackCol {
		rowSlackCol[i] = -1
	}
	for i, r := range rows {
		switch r.ctype {
		case LTE:
			col := std.addColumn(0)
			std.setEntry(i, col, +1)
			rowSlackCol[i] = col
		case GTE:
			col := std.addColumn(0)
			std.setEntry(i, col, -1)
			rowSlackCol[i] = col
		case EQ:
			// no slack
		}
	}

	// Negate any row with negative b and flip the sign of any slack that
	// turned into "-1 * slack on a positive RHS".
	for i := range rows {
		if std.b[i] < 0 {
			negateRow(std.A, i)
			std.b[i] = -std.b[i]
			std.rowSign[i] = -1
		}
	}

	// Now choose initial basis. For each row we want a column that has
	// +1 in this row and 0 elsewhere (an identity column). After the
	// negation step, an LTE row whose original b ≥ 0 still has a +1
	// slack; an LTE row with original b < 0 (now negated) has -1 slack.
	// In the latter case we need an artificial.
	std.origIdentityCol = make([]int, std.m)
	for i := range rows {
		basisCol := -1
		if rowSlackCol[i] >= 0 && std.A[i][rowSlackCol[i]] == 1 && isIdentityColumn(std.A, rowSlackCol[i], i) {
			basisCol = rowSlackCol[i]
		}
		if basisCol < 0 {
			art := std.addColumn(0)
			std.setEntry(i, art, +1)
			std.artificials = append(std.artificials, art)
			basisCol = art
		}
		std.basis[i] = basisCol
		std.origIdentityCol[i] = basisCol
	}

	// Initialise x as the BFS implied by the basis.
	std.x = make([]float64, std.n)
	for i, col := range std.basis {
		std.x[col] = std.b[i]
	}

	return std, nil
}

// addColumn extends every row of A with the given default value and grows c
// by 0. Returns the new column index.
func (s *stdForm) addColumn(defaultVal float64) int {
	for i := range s.A {
		s.A[i] = append(s.A[i], defaultVal)
	}
	s.c = append(s.c, 0)
	s.n++
	return s.n - 1
}

func (s *stdForm) setEntry(row, col int, v float64) { s.A[row][col] = v }

// expelArtificials drives any artificial column still in the basis at zero
// level out of the basis via a degenerate pivot. If the pivot row is all
// zero on the non-artificial columns the row is redundant and we skip it.
// Bertsimas & Tsitsiklis §3.5 ("artificial variables and the two-phase
// method") describes this clean-up step.
func (s *stdForm) expelArtificials(tol float64) error {
	artSet := make(map[int]bool, len(s.artificials))
	for _, j := range s.artificials {
		artSet[j] = true
	}
	for i, b := range s.basis {
		if !artSet[b] {
			continue
		}
		// Try to pivot some non-artificial nonbasic column into row i.
		pivotCol := -1
		for j := 0; j < s.n; j++ {
			if artSet[j] {
				continue
			}
			if math.Abs(s.A[i][j]) > tol {
				pivotCol = j
				break
			}
		}
		if pivotCol < 0 {
			// All non-artificial coefficients in row i are zero → row is
			// linearly dependent on the others. We'll leave the artificial
			// in the basis at value zero; pricing in phase II will simply
			// never select it because its cost is +∞.
			continue
		}
		s.pivot(i, pivotCol)
	}
	return nil
}

// pivot performs an elementary row operation: scale row pr so A[pr][pc]=1,
// then eliminate column pc from every other row.
func (s *stdForm) pivot(pr, pc int) {
	piv := s.A[pr][pc]
	if piv == 0 {
		return
	}
	inv := 1 / piv
	rowPR := s.A[pr]
	for j := 0; j < s.n; j++ {
		rowPR[j] *= inv
	}
	s.b[pr] *= inv
	for i := 0; i < s.m; i++ {
		if i == pr {
			continue
		}
		f := s.A[i][pc]
		if f == 0 {
			continue
		}
		rowI := s.A[i]
		for j := 0; j < s.n; j++ {
			rowI[j] -= f * rowPR[j]
		}
		s.b[i] -= f * s.b[pr]
	}
	s.basis[pr] = pc
	// Refresh x.
	for j := 0; j < s.n; j++ {
		s.x[j] = 0
	}
	for i, col := range s.basis {
		s.x[col] = s.b[i]
	}
}

// dualValuesFor returns y, the dual vector y^T = c_B^T B^{-1}, evaluated
// against the supplied cost vector c (which may differ between phases).
//
// Because we kept A in tableau form (A_current = B^{-1} A_orig), the i-th
// column of B^{-1} is the current entries of whichever column was the
// unit vector e_i in A_orig — that's std.origIdentityCol[i]. Therefore
//
//	y_i = c_B^T A_current[:, origIdentityCol[i]].
func (s *stdForm) dualValuesFor(c []float64) []float64 {
	cB := s.basicCosts(c)
	y := make([]float64, s.m)
	for i := 0; i < s.m; i++ {
		col := s.origIdentityCol[i]
		var sum float64
		for k := 0; k < s.m; k++ {
			sum += cB[k] * s.A[k][col]
		}
		y[i] = sum
	}
	return y
}

// reducedCosts computes r_j = c_j - z_j with z_j = c_B^T A_current[:, j].
// y is unused but kept in the signature for symmetry with classic
// presentations of the simplex method.
func (s *stdForm) reducedCosts(c, _ []float64) []float64 {
	cB := s.basicCosts(c)
	r := make([]float64, s.n)
	for j := 0; j < s.n; j++ {
		zj := 0.0
		for i := 0; i < s.m; i++ {
			zj += cB[i] * s.A[i][j]
		}
		cj := c[j]
		if math.IsInf(cj, 1) {
			cj = 0
		}
		r[j] = cj - zj
	}
	return r
}

// basicCosts returns the cost-of-basics vector c_B with any "+∞" forbidden
// entries (used to mask artificials in Phase II) treated as zero. Returning
// 0 here is safe because such columns cannot be in the basis once Phase I
// has expelled them.
func (s *stdForm) basicCosts(c []float64) []float64 {
	cB := make([]float64, s.m)
	for i, col := range s.basis {
		ci := c[col]
		if math.IsInf(ci, 1) {
			ci = 0
		}
		cB[i] = ci
	}
	return cB
}

func (s *stdForm) describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "standard form: m=%d rows, n=%d cols (max=%v)\n", s.m, s.n, s.maximized)
	for i, row := range s.A {
		fmt.Fprintf(&b, "  row %d: ", i)
		for _, v := range row {
			fmt.Fprintf(&b, "%7.3f ", v)
		}
		fmt.Fprintf(&b, " | %g  (basis=%d)\n", s.b[i], s.basis[i])
	}
	fmt.Fprintf(&b, "  c   : ")
	for _, v := range s.c {
		fmt.Fprintf(&b, "%7.3f ", v)
	}
	fmt.Fprintln(&b)
	return b.String()
}

// ─── Simplex iteration loop ──────────────────────────────────────────────

// simplexLoop runs simplex iterations on std using the supplied cost vector
// (Phase I or Phase II). Returns the terminal Status (Optimal/Unbounded/
// IterationLimit) and the iteration count.
//
// Pivot rules: Bland's rule — among columns with negative reduced cost
// pick the one with the smallest index; among rows tied on the minimum
// ratio test pick the one whose basic variable has the smallest index.
// This guarantees finite termination even on degenerate problems
// (Bertsimas & Tsitsiklis §3.4).
func simplexLoop(std *stdForm, c []float64, maxIter int, tol float64, verbose bool, out io.Writer, tag string) (Status, int) {
	iters := 0
	for iters < maxIter {
		// Compute reduced costs r_j = c_j - z_j with z_j = c_B^T A_j.
		// Bland's entering rule: smallest j with r_j < -tol.
		entering := -1
		for j := 0; j < std.n; j++ {
			cj := c[j]
			if math.IsInf(cj, 1) {
				continue // forbidden column (e.g. artificial in phase II)
			}
			// In-basis columns have reduced cost 0 by definition; skip.
			isBasic := false
			for _, b := range std.basis {
				if b == j {
					isBasic = true
					break
				}
			}
			if isBasic {
				continue
			}
			zj := 0.0
			for i := 0; i < std.m; i++ {
				cb := c[std.basis[i]]
				if math.IsInf(cb, 1) {
					cb = 0
				}
				zj += cb * std.A[i][j]
			}
			rj := cj - zj
			if rj < -tol {
				entering = j
				break
			}
		}
		if entering < 0 {
			return Optimal, iters
		}

		// Min-ratio test (Bland: smallest basic-variable index on ties).
		leaving := -1
		minRatio := math.Inf(1)
		for i := 0; i < std.m; i++ {
			a := std.A[i][entering]
			if a <= tol {
				continue
			}
			ratio := std.b[i] / a
			switch {
			case ratio < minRatio-tol:
				minRatio = ratio
				leaving = i
			case math.Abs(ratio-minRatio) <= tol:
				// Tie-break by smallest basic-variable index (Bland).
				if leaving < 0 || std.basis[i] < std.basis[leaving] {
					leaving = i
				}
			}
		}
		if leaving < 0 {
			return Unbounded, iters
		}

		if verbose {
			fmt.Fprintf(out, "[%s] iter %3d: enter col %d, leave row %d (basis %d → %d), ratio=%g\n",
				tag, iters, entering, leaving, std.basis[leaving], entering, minRatio)
		}
		std.pivot(leaving, entering)
		iters++
	}
	return IterationLimit, iters
}

// ─── tiny helpers ───────────────────────────────────────────────────────

func dot(a, b []float64) float64 {
	s := 0.0
	for i, x := range a {
		if math.IsInf(x, 1) {
			continue // forbidden; treat as zero for the inner product
		}
		s += x * b[i]
	}
	return s
}

// appendCol grows every row of A by one zero entry. Used during the
// "allocate internal columns for user variables" pass before any rows are
// in place (so it's a no-op on an empty A).
func appendCol(A [][]float64) [][]float64 {
	for i := range A {
		A[i] = append(A[i], 0)
	}
	return A
}

func negateRow(A [][]float64, r int) {
	row := A[r]
	for j := range row {
		row[j] = -row[j]
	}
}

// isIdentityColumn reports whether column col of A equals e_row, i.e. has
// 1 at A[row][col] and 0 elsewhere.
func isIdentityColumn(A [][]float64, col, row int) bool {
	for i := range A {
		if i == row {
			continue
		}
		if A[i][col] != 0 {
			return false
		}
	}
	return A[row][col] == 1
}
