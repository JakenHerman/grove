// Shift scheduling: cover required nurse demand at minimum total wage cost
// across a 7-day week, choosing how many full-time and part-time nurses to
// staff on each day.
//
// Run with:
//
//	go run ./examples/scheduling
package main

import (
	"fmt"
	"log"

	"github.com/jakenherman/grove"
)

func main() {
	// Demand for nurse-hours per day (Mon..Sun).
	demand := []float64{40, 32, 35, 38, 45, 28, 25}
	days := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

	// Two staff types:
	//   - full: 8h shift, $35/hr × 8h = $280 per shift
	//   - part: 4h shift, $40/hr × 4h = $160 per shift
	const (
		fullHours = 8.0
		partHours = 4.0
		fullCost  = 280.0
		partCost  = 160.0
	)

	prob := grove.NewProblem("nurse_schedule", grove.Minimize)

	full := make([]*grove.Var, len(days))
	part := make([]*grove.Var, len(days))
	for i, d := range days {
		full[i] = prob.NewVar("full_"+d, grove.Continuous, grove.Bounds(0, grove.Inf))
		part[i] = prob.NewVar("part_"+d, grove.Continuous, grove.Bounds(0, grove.Inf))
	}

	// Objective: minimize total cost across the week.
	obj := grove.Expr{}
	for i := range days {
		obj[full[i]] = fullCost
		obj[part[i]] = partCost
	}
	prob.SetObjective(obj)

	// Demand coverage: full*8 + part*4 >= demand for each day.
	for i, d := range days {
		prob.AddConstraint("cover_"+d,
			grove.Expr{full[i]: fullHours, part[i]: partHours},
			grove.GTE,
			demand[i])
	}

	// Cap part-time on weekends (a labor-rules constraint).
	prob.AddConstraint("part_cap_sat", grove.Expr{part[5]: 1}, grove.LTE, 4)
	prob.AddConstraint("part_cap_sun", grove.Expr{part[6]: 1}, grove.LTE, 4)

	res, err := prob.Solve()
	if err != nil {
		log.Fatalf("solve: %v", err)
	}

	fmt.Printf("Status:  %s\n", res.Status)
	fmt.Printf("Cost:    $%.2f\n", res.Objective)
	fmt.Printf("Pivots:  %d\n\n", res.Iterations)

	fmt.Printf("%-5s %8s %8s %8s\n", "day", "full", "part", "demand")
	for i, d := range days {
		fmt.Printf("%-5s %8.2f %8.2f %8.0f\n", d, res.Value(full[i]), res.Value(part[i]), demand[i])
	}

	fmt.Println("\nShadow prices (per nurse-hour of additional demand):")
	for _, c := range prob.Constraints() {
		fmt.Printf("  %-15s  %.4f\n", c.Name(), res.Dual(c))
	}
}
