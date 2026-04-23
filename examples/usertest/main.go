package main

import (
	"fmt"
	"log"

	"github.com/jakenherman/grove"
)

func main() {
	p := grove.NewProblem("user_ilp", grove.Maximize)

	x1 := p.NewVar("x1", grove.Continuous, grove.Bounds(-15, 15))
	x2 := p.NewVar("x2", grove.Continuous, grove.Bounds(-15, 15))
	x3 := p.NewVar("x3", grove.Continuous, grove.Bounds(-15, 15))
	x4 := p.NewVar("x4", grove.Continuous, grove.Bounds(-15, 15))
	x5 := p.NewVar("x5", grove.Continuous, grove.Bounds(-15, 15))

	p.SetObjective(grove.Expr{x1: 2, x2: -3, x3: 1})

	p.AddConstraint("c1", grove.Expr{x1: 1, x2: -1, x3: 1}, grove.LTE, 5)
	p.AddConstraint("c2", grove.Expr{x1: 1, x2: -1, x3: 4}, grove.LTE, 7)
	p.AddConstraint("c3", grove.Expr{x1: 1, x2: 2, x3: -1, x4: 1}, grove.LTE, 14)
	p.AddConstraint("c4", grove.Expr{x3: 1, x4: -1, x5: 1}, grove.LTE, 7)

	r, err := p.Solve()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Status:    %s\n", r.Status)
	fmt.Printf("Objective: %.4f\n", r.Objective)
	fmt.Printf("x1=%.4f  x2=%.4f  x3=%.4f  x4=%.4f  x5=%.4f\n",
		r.Value(x1), r.Value(x2), r.Value(x3), r.Value(x4), r.Value(x5))

	isInt := func(v float64) bool {
		diff := v - float64(int(v+0.5))
		if diff < 0 {
			diff = -diff
		}
		return diff < 1e-6
	}
	allInt := isInt(r.Value(x1)) && isInt(r.Value(x2)) && isInt(r.Value(x3)) &&
		isInt(r.Value(x4)) && isInt(r.Value(x5))
	fmt.Printf("All integer at LP optimum? %v\n", allInt)
}
