package grove

import (
	"math/rand"
	"testing"
)

// BenchmarkSmallLP is the README example, sized for fast iteration.
func BenchmarkSmallLP(b *testing.B) {
	for i := 0; i < b.N; i++ {
		p := NewProblem("nurses", Maximize)
		x := p.NewVar("d", Continuous, Bounds(0, Inf))
		y := p.NewVar("n", Continuous, Bounds(0, Inf))
		p.SetObjective(Expr{x: 1, y: 1})
		p.AddConstraint("c1", Expr{x: 1}, GTE, 4)
		p.AddConstraint("c2", Expr{y: 1}, GTE, 2)
		p.AddConstraint("c3", Expr{x: 1200, y: 1500}, LTE, 18000)
		_, _ = p.Solve()
	}
}

// BenchmarkRandomLP_50x100 builds a random feasible LP with 50 LTE
// constraints, 100 continuous variables, and a positive-coefficient
// objective. This is the lowest-hanging "is grove competitive?" benchmark
// versus gonum.org/v1/gonum/optimize/lp; the gonum comparison is a
// follow-up benchmark to be added once gonum is added as a dev dependency.
func BenchmarkRandomLP_50x100(b *testing.B) {
	const m, n = 50, 100
	rng := rand.New(rand.NewSource(42))

	// Pre-generate a single problem instance so the benchmark times only
	// the solve, not problem construction.
	A := make([][]float64, m)
	for i := range A {
		A[i] = make([]float64, n)
		for j := range A[i] {
			A[i][j] = rng.Float64()
		}
	}
	bs := make([]float64, m)
	for i := range bs {
		bs[i] = float64(n) * 0.5 // generous RHS so the LP is feasible
	}
	cObj := make([]float64, n)
	for j := range cObj {
		cObj[j] = rng.Float64()
	}

	build := func() *Problem {
		p := NewProblem("rand", Maximize)
		vars := make([]*Var, n)
		for j := 0; j < n; j++ {
			vars[j] = p.NewVar("x"+itoa(j), Continuous, Bounds(0, Inf))
		}
		obj := Expr{}
		for j := 0; j < n; j++ {
			obj[vars[j]] = cObj[j]
		}
		p.SetObjective(obj)
		for i := 0; i < m; i++ {
			row := Expr{}
			for j := 0; j < n; j++ {
				row[vars[j]] = A[i][j]
			}
			p.AddConstraint("c"+itoa(i), row, LTE, bs[i])
		}
		return p
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = build().Solve()
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
