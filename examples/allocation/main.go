// VM resource allocation: pack workloads onto a fixed pool of CPU/RAM/disk
// to maximise total business value. Each workload yields some value if
// scheduled, with continuous "fraction scheduled" variables — this is the
// LP relaxation of a knapsack and a useful upper bound for the MIP version
// (planned for v0.4).
//
// Run with:
//
//	go run ./examples/allocation
package main

import (
	"fmt"
	"log"

	"github.com/jakenherman/grove"
)

type workload struct {
	name     string
	cpu, ram float64 // vCPU, GB
	disk     float64 // GB
	value    float64 // dollars/hour of business value
}

func main() {
	cluster := struct {
		cpu, ram, disk float64
	}{cpu: 64, ram: 256, disk: 4000}

	workloads := []workload{
		{"checkout-api", 4, 16, 80, 90},
		{"recommender", 8, 32, 200, 70},
		{"batch-etl", 16, 64, 500, 50},
		{"image-resize", 2, 8, 40, 30},
		{"search-index", 6, 24, 150, 60},
		{"ml-train", 32, 128, 1000, 200},
		{"audit-log", 1, 4, 200, 10},
		{"webhook-fan", 2, 8, 20, 25},
	}

	prob := grove.NewProblem("vm_allocation", grove.Maximize)

	frac := make([]*grove.Var, len(workloads))
	for i, w := range workloads {
		// Continuous in [0,1]: fraction of the workload to schedule.
		// Replace with grove.Binary for an exact (MIP) version once v0.4 lands.
		frac[i] = prob.NewVar(w.name, grove.Continuous, grove.Bounds(0, 1))
	}

	// Objective: maximise total business value.
	obj := grove.Expr{}
	for i, w := range workloads {
		obj[frac[i]] = w.value
	}
	prob.SetObjective(obj)

	// Resource constraints.
	cpuExpr := grove.Expr{}
	ramExpr := grove.Expr{}
	diskExpr := grove.Expr{}
	for i, w := range workloads {
		cpuExpr[frac[i]] = w.cpu
		ramExpr[frac[i]] = w.ram
		diskExpr[frac[i]] = w.disk
	}
	prob.AddConstraint("cpu", cpuExpr, grove.LTE, cluster.cpu)
	prob.AddConstraint("ram", ramExpr, grove.LTE, cluster.ram)
	prob.AddConstraint("disk", diskExpr, grove.LTE, cluster.disk)

	res, err := prob.Solve()
	if err != nil {
		log.Fatalf("solve: %v", err)
	}

	fmt.Printf("Status: %s\n", res.Status)
	fmt.Printf("Value:  $%.2f / hr\n", res.Objective)
	fmt.Printf("Pivots: %d\n\n", res.Iterations)

	fmt.Printf("%-15s %8s\n", "workload", "fraction")
	for i, w := range workloads {
		fmt.Printf("%-15s %8.2f\n", w.name, res.Value(frac[i]))
	}

	fmt.Println("\nResource shadow prices (extra value per added unit):")
	for _, c := range prob.Constraints() {
		fmt.Printf("  %-6s %.4f\n", c.Name(), res.Dual(c))
	}
}
