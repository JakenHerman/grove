package grove

import (
	"errors"
)

// HiGHS is a placeholder for the v0.3 cgo-backed HiGHS solver.
//
// HiGHS is the open-source state-of-the-art LP/MIP/QP solver developed
// at the University of Edinburgh (https://highs.dev). It blows pure-Go
// implementations out of the water on dense and sparse models alike, and
// it's the right backend to reach for once a model gets nontrivial.
//
// The plan for v0.3 is:
//
//  1. Vendor or cgo-link against libhighs:
//
//     // #cgo LDFLAGS: -lhighs
//     // #include "interfaces/highs_c_api.h"
//     // #include <stdlib.h>
//     import "C"
//
//  2. Translate the [Problem] into HiGHS' column-major arrays:
//
//     - col_cost[]                   ← objective coefficients
//     - col_lower[], col_upper[]     ← variable bounds
//     - row_lower[], row_upper[]     ← derived from LTE/GTE/EQ + RHS
//     - a_start[], a_index[], a_value[]  ← CSC sparse constraint matrix
//     - integrality[]                ← 0/1 per variable
//
//  3. Call Highs_lpCall (or the MIP equivalent) with:
//
//     numCol := C.int(len(p.vars))
//     numRow := C.int(len(p.constraints))
//     status := C.Highs_lpCall(numCol, numRow, ..., out_col_value,
//     out_col_dual, out_row_value, out_row_dual,
//     &model_status)
//
//  4. Map model_status back to grove.Status (kHighsModelStatusOptimal →
//     Optimal, …Infeasible → Infeasible, etc.) and copy out_col_value,
//     out_col_dual into the [Result].
//
// To opt in once the bindings ship, the user will write:
//
//	prob.Solver = &grove.HiGHS{}     // requires CGO + libhighs
//
// Until then, calling Solve returns ErrHiGHSNotBuilt so callers don't
// silently get the pure-Go solver instead.
type HiGHS struct {
	// Threads is the number of parallel threads HiGHS may use. Zero
	// means "let HiGHS pick".
	Threads int
	// Presolve toggles HiGHS' presolve phase (default: on).
	Presolve bool
	// TimeLimit is a wall-clock cap in seconds. Zero means no limit.
	TimeLimit float64
}

// ErrHiGHSNotBuilt is returned by [HiGHS.Solve] until the v0.3 cgo
// bindings ship. Build with the future "highs" tag once available:
//
//	go build -tags highs
var ErrHiGHSNotBuilt = errors.New(
	"grove: HiGHS backend is not compiled in (planned for v0.3); " +
		"use the default pure-Go SimplexSolver, or stay tuned for " +
		"`go build -tags highs`")

// Solve always returns [ErrHiGHSNotBuilt] in the current release.
func (h *HiGHS) Solve(p *Problem) (*Result, error) {
	// TODO(v0.3): replace this stub with a real cgo binding. See the
	// type-level doc comment for the implementation outline.
	return &Result{Status: NotSolved, Message: ErrHiGHSNotBuilt.Error()}, ErrHiGHSNotBuilt
}
