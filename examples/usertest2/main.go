package main

import (
	"fmt"

	"github.com/jakenherman/grove"
)

func build(kind grove.VarKind) *grove.Problem {
	p := grove.NewProblem("user_ilp2", grove.Minimize)
	x1 := p.NewVar("x1", kind, grove.Bounds(-1, 1))
	x2 := p.NewVar("x2", kind, grove.Bounds(-1, 1))
	x3 := p.NewVar("x3", kind, grove.Bounds(-1, 1))

	p.SetObjective(grove.Expr{x1: 2, x2: -3, x3: 1})

	p.AddConstraint("c1", grove.Expr{x1: 1, x2: -1}, grove.GTE, 0.5)
	p.AddConstraint("c2", grove.Expr{x1: 1, x2: -1}, grove.LTE, 0.75)
	p.AddConstraint("c3", grove.Expr{x2: 1, x3: -1}, grove.LTE, 1.25)
	p.AddConstraint("c4", grove.Expr{x2: 1, x3: -1}, grove.GTE, 0.95)
	return p
}

func run(label string, kind grove.VarKind) {
	p := build(kind)
	r, err := p.Solve()
	fmt.Printf("--- %s ---\n", label)
	fmt.Printf("Status:    %s\n", r.Status)
	if err != nil {
		fmt.Printf("Err:       %v\n", err)
	}
	if r.Status == grove.Optimal {
		fmt.Printf("Objective: %.4f\n", r.Objective)
		for _, v := range p.Vars() {
			fmt.Printf("  %s = %.4f\n", v.Name(), r.Value(v))
		}
	}
	if r.Message != "" {
		fmt.Printf("Message:   %s\n", r.Message)
	}
	for _, w := range r.Warnings {
		fmt.Printf("Warning:   %s\n", w)
	}
	fmt.Println()
}

func main() {
	// grove v0.1 treats Integer as LP relaxation, so both calls below solve
	// the same LP internally. We run them labelled separately for clarity.
	run("ILP (Integer kind — solved as LP relaxation in v0.1)", grove.Integer)
	run("LP relaxation (Continuous)", grove.Continuous)
}
