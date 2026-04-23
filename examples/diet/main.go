// Diet optimisation: choose grams of each food to minimise cost while
// meeting daily nutritional requirements. This is the classic Stigler diet
// problem (1945) — and the LP that motivated Dantzig to invent the simplex
// method.
//
// Run with:
//
//	go run ./examples/diet
package main

import (
	"fmt"
	"log"

	"github.com/jakenherman/grove"
)

type food struct {
	name            string
	costPerKG       float64
	caloriesPer100g float64
	proteinPer100g  float64 // grams
	fatPer100g      float64
	carbsPer100g    float64
	sodiumPer100g   float64 // mg
}

func main() {
	foods := []food{
		// name              $/kg  cal   pro   fat  carb  sodium
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

	// Daily nutritional targets (typical adult).
	const (
		minCalories = 2000.0
		maxCalories = 2400.0
		minProtein  = 100.0  // g
		maxFat      = 80.0   // g
		minCarbs    = 250.0  // g
		maxSodium   = 2300.0 // mg
	)

	prob := grove.NewProblem("diet", grove.Minimize)

	// Decision variable: hectograms (100g units) of each food. Capped at
	// 20 hectograms (2 kg) per food per day so the solver doesn't propose
	// "eat 8 kg of olive oil".
	x := make([]*grove.Var, len(foods))
	for i, f := range foods {
		x[i] = prob.NewVar(f.name, grove.Continuous, grove.Bounds(0, 20))
	}

	// Objective: minimise daily cost. costPerKG / 10 = cost per hectogram.
	obj := grove.Expr{}
	for i, f := range foods {
		obj[x[i]] = f.costPerKG / 10
	}
	prob.SetObjective(obj)

	// Build the nutrient expressions.
	calExpr := grove.Expr{}
	proExpr := grove.Expr{}
	fatExpr := grove.Expr{}
	carbExpr := grove.Expr{}
	naExpr := grove.Expr{}
	for i, f := range foods {
		calExpr[x[i]] = f.caloriesPer100g
		proExpr[x[i]] = f.proteinPer100g
		fatExpr[x[i]] = f.fatPer100g
		carbExpr[x[i]] = f.carbsPer100g
		naExpr[x[i]] = f.sodiumPer100g
	}

	prob.AddConstraint("calories_min", calExpr, grove.GTE, minCalories)
	prob.AddConstraint("calories_max", calExpr, grove.LTE, maxCalories)
	prob.AddConstraint("protein_min", proExpr, grove.GTE, minProtein)
	prob.AddConstraint("fat_max", fatExpr, grove.LTE, maxFat)
	prob.AddConstraint("carbs_min", carbExpr, grove.GTE, minCarbs)
	prob.AddConstraint("sodium_max", naExpr, grove.LTE, maxSodium)

	res, err := prob.Solve()
	if err != nil {
		log.Fatalf("solve: %v", err)
	}

	fmt.Printf("Status: %s\n", res.Status)
	fmt.Printf("Cost:   $%.2f / day\n", res.Objective)
	fmt.Printf("Pivots: %d\n\n", res.Iterations)

	fmt.Printf("%-16s %10s %10s\n", "food", "grams", "cost")
	totalGrams := 0.0
	for i, f := range foods {
		hg := res.Value(x[i])
		if hg < 1e-6 {
			continue
		}
		totalGrams += hg * 100
		fmt.Printf("%-16s %10.1f %10.2f\n", f.name, hg*100, hg*f.costPerKG/10)
	}
	fmt.Printf("\nTotal mass: %.0f g\n", totalGrams)

	rep := grove.SensitivityReport(prob, res)
	fmt.Println()
	fmt.Println(rep)
}
