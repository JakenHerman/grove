package grove

// This file documents grove's sensitivity-analysis surface and adds a
// post-solve helper that summarises duals, reduced costs, and slacks per
// constraint. The actual numbers are computed inside the simplex solver
// (see solver.go's dualValuesFor and reducedCosts) — at the user's
// request grove keeps the public API thin: a *Result already exposes
// Dual(c) and Reduced(v).
//
// The textbook reference for everything in this file is Bertsimas &
// Tsitsiklis, "Introduction to Linear Optimization", chapter 5
// (sensitivity analysis) and chapter 4 (duality theory).

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Sensitivity is a structured snapshot of dual values, reduced costs, and
// slacks at the optimal basis. Build one from a [Result] with
// [SensitivityReport].
type Sensitivity struct {
	// Constraints, in declaration order, with their LHS value, slack, and
	// shadow price (dual).
	Constraints []ConstraintSensitivity
	// Variables, in declaration order, with their value and reduced cost.
	Variables []VarSensitivity
}

// ConstraintSensitivity is the per-constraint diagnostics block.
//
// Slack convention:
//   - LTE (a·x ≤ b): Slack = b - a·x ≥ 0; Slack=0 means binding.
//   - GTE (a·x ≥ b): Slack = a·x - b ≥ 0; Slack=0 means binding.
//   - EQ:           Slack is always 0.
//
// Dual is the shadow price ∂z*/∂b: how much the (user-sense) objective
// improves per unit increase of the constraint's right-hand side.
type ConstraintSensitivity struct {
	Constraint *Constraint
	LHS        float64
	Slack      float64
	Dual       float64
	Binding    bool
}

// VarSensitivity is the per-variable diagnostics block. Reduced cost
// follows the user objective sense: for a maximization problem a positive
// reduced cost on a non-basic variable at its lower bound says "raising
// this variable's coefficient by Δ raises the objective by Δ·value once
// the variable enters the basis"; for minimization the sign convention is
// reversed.
type VarSensitivity struct {
	Var     *Var
	Value   float64
	Reduced float64
	AtLower bool
	AtUpper bool
}

// SensitivityReport assembles a [Sensitivity] from a problem and its
// solved [Result]. It returns nil if the result is not Optimal.
func SensitivityReport(p *Problem, r *Result) *Sensitivity {
	if r == nil || r.Status != Optimal {
		return nil
	}
	tol := 1e-7
	out := &Sensitivity{}
	for _, c := range p.constraints {
		var lhs float64
		for v, k := range c.expr {
			lhs += k * r.Value(v)
		}
		var slack float64
		switch c.ctype {
		case LTE:
			slack = c.rhs - lhs
		case GTE:
			slack = lhs - c.rhs
		case EQ:
			slack = 0
		}
		out.Constraints = append(out.Constraints, ConstraintSensitivity{
			Constraint: c,
			LHS:        lhs,
			Slack:      slack,
			Dual:       r.Dual(c),
			Binding:    math.Abs(slack) <= tol,
		})
	}
	for _, v := range p.vars {
		val := r.Value(v)
		out.Variables = append(out.Variables, VarSensitivity{
			Var:     v,
			Value:   val,
			Reduced: r.Reduced(v),
			AtLower: !math.IsInf(v.low, -1) && math.Abs(val-v.low) <= tol,
			AtUpper: !math.IsInf(v.high, 1) && math.Abs(val-v.high) <= tol,
		})
	}
	return out
}

// String renders a Sensitivity report as a human-readable table.
func (s *Sensitivity) String() string {
	var b strings.Builder
	b.WriteString("Constraints\n")
	b.WriteString("  name                       lhs        rhs        slack       dual    binding\n")
	cons := append([]ConstraintSensitivity(nil), s.Constraints...)
	sort.SliceStable(cons, func(i, j int) bool { return cons[i].Constraint.idx < cons[j].Constraint.idx })
	for _, c := range cons {
		fmt.Fprintf(&b, "  %-22s %10.4g %3s %10.4g %10.4g %10.4g %8v\n",
			c.Constraint.name, c.LHS, c.Constraint.ctype, c.Constraint.rhs,
			c.Slack, c.Dual, c.Binding)
	}
	b.WriteString("Variables\n")
	b.WriteString("  name                        value     reduced    bound-status\n")
	vars := append([]VarSensitivity(nil), s.Variables...)
	sort.SliceStable(vars, func(i, j int) bool { return vars[i].Var.idx < vars[j].Var.idx })
	for _, v := range vars {
		status := "interior"
		switch {
		case v.AtLower && v.AtUpper:
			status = "fixed"
		case v.AtLower:
			status = "at lower"
		case v.AtUpper:
			status = "at upper"
		}
		fmt.Fprintf(&b, "  %-24s %10.4g %10.4g    %s\n", v.Var.name, v.Value, v.Reduced, status)
	}
	return b.String()
}
