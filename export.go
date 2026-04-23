package grove

// File format I/O for grove.
//
// We emit two industry-standard formats:
//
//   - CPLEX LP format (.lp) — readable text, the de-facto sharing format
//     for small-to-medium models. Spec: IBM ILOG CPLEX file formats
//     reference, "LP file format".
//
//   - MPS format (.mps) — the original 1960s fixed-column format, still
//     the lingua franca for solver competitions and benchmark archives
//     such as MIPLIB. Spec: ILOG CPLEX MPS reference.
//
// v0.2 will round-trip through these formats; for v0.1 we only emit.

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// WriteLP writes p to w in CPLEX LP format.
func (p *Problem) WriteLP(w io.Writer) error {
	bw := newBufW(w)
	defer bw.Flush()

	switch p.sense {
	case Maximize:
		bw.WriteString("Maximize\n")
	default:
		bw.WriteString("Minimize\n")
	}
	bw.WriteString(" obj: ")
	bw.WriteString(formatLPExpr(p.objective))
	if p.objConst != 0 {
		fmt.Fprintf(bw, " %+g", p.objConst)
	}
	bw.WriteString("\n\nSubject To\n")

	for _, c := range p.constraints {
		fmt.Fprintf(bw, " %s: %s %s %g\n", sanitize(c.name), formatLPExpr(c.expr), c.ctype, c.rhs)
	}

	bw.WriteString("\nBounds\n")
	for _, v := range p.vars {
		switch {
		case math.IsInf(v.low, -1) && math.IsInf(v.high, 1):
			fmt.Fprintf(bw, " %s free\n", sanitize(v.name))
		case math.IsInf(v.high, 1):
			fmt.Fprintf(bw, " %g <= %s\n", v.low, sanitize(v.name))
		case math.IsInf(v.low, -1):
			fmt.Fprintf(bw, " %s <= %g\n", sanitize(v.name), v.high)
		default:
			fmt.Fprintf(bw, " %g <= %s <= %g\n", v.low, sanitize(v.name), v.high)
		}
	}

	// Variable kind sections.
	var ints, bins []*Var
	for _, v := range p.vars {
		switch v.kind {
		case Integer:
			ints = append(ints, v)
		case Binary:
			bins = append(bins, v)
		}
	}
	if len(ints) > 0 {
		bw.WriteString("\nGeneral\n ")
		for _, v := range ints {
			fmt.Fprintf(bw, "%s ", sanitize(v.name))
		}
		bw.WriteString("\n")
	}
	if len(bins) > 0 {
		bw.WriteString("\nBinary\n ")
		for _, v := range bins {
			fmt.Fprintf(bw, "%s ", sanitize(v.name))
		}
		bw.WriteString("\n")
	}

	bw.WriteString("\nEnd\n")
	return nil
}

// WriteMPS writes p to w in fixed-column MPS format.
//
// The MPS format expects all constraints to take the form "lhs op rhs"
// with rhs on the right; row types are N (objective), L (≤), G (≥), E (=).
func (p *Problem) WriteMPS(w io.Writer) error {
	bw := newBufW(w)
	defer bw.Flush()

	fmt.Fprintf(bw, "NAME          %s\n", sanitize(p.name))

	// ROWS
	bw.WriteString("ROWS\n")
	bw.WriteString(" N  COST\n")
	for _, c := range p.constraints {
		var t byte
		switch c.ctype {
		case LTE:
			t = 'L'
		case GTE:
			t = 'G'
		case EQ:
			t = 'E'
		}
		fmt.Fprintf(bw, " %c  %s\n", t, sanitize(c.name))
	}

	// COLUMNS — emit each variable's column entries together.
	bw.WriteString("COLUMNS\n")
	// MIP markers for integer/binary variables.
	inIntSection := false
	for _, v := range p.vars {
		needInt := v.kind == Integer || v.kind == Binary
		if needInt && !inIntSection {
			fmt.Fprintf(bw, "    MARKER                 'MARKER'                 'INTORG'\n")
			inIntSection = true
		}
		if !needInt && inIntSection {
			fmt.Fprintf(bw, "    MARKER                 'MARKER'                 'INTEND'\n")
			inIntSection = false
		}
		// Objective coefficient.
		coefObj, hasObj := p.objective[v]
		if hasObj && coefObj != 0 {
			signed := coefObj
			if p.sense == Maximize {
				// MPS conventionally minimizes; we flip the sign so a
				// reader that minimizes will produce the same optimum.
				// (Explicit OBJSENSE sections are non-standard.)
				signed = -coefObj
			}
			fmt.Fprintf(bw, "    %-9s %-9s %15.8g\n", sanitize(v.name), "COST", signed)
		}
		// Constraint coefficients (sorted by row idx for stable output).
		var rows []*Constraint
		for _, c := range p.constraints {
			if _, ok := c.expr[v]; ok {
				rows = append(rows, c)
			}
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].idx < rows[j].idx })
		for _, c := range rows {
			fmt.Fprintf(bw, "    %-9s %-9s %15.8g\n", sanitize(v.name), sanitize(c.name), c.expr[v])
		}
	}
	if inIntSection {
		fmt.Fprintf(bw, "    MARKER                 'MARKER'                 'INTEND'\n")
	}

	// RHS
	bw.WriteString("RHS\n")
	for _, c := range p.constraints {
		if c.rhs == 0 {
			continue
		}
		fmt.Fprintf(bw, "    RHS       %-9s %15.8g\n", sanitize(c.name), c.rhs)
	}

	// BOUNDS
	bw.WriteString("BOUNDS\n")
	for _, v := range p.vars {
		switch {
		case math.IsInf(v.low, -1) && math.IsInf(v.high, 1):
			fmt.Fprintf(bw, " FR BND       %s\n", sanitize(v.name))
		case math.IsInf(v.high, 1):
			if v.low != 0 {
				fmt.Fprintf(bw, " LO BND       %-9s %15.8g\n", sanitize(v.name), v.low)
			}
		case math.IsInf(v.low, -1):
			fmt.Fprintf(bw, " MI BND       %s\n", sanitize(v.name))
			fmt.Fprintf(bw, " UP BND       %-9s %15.8g\n", sanitize(v.name), v.high)
		default:
			if v.low != 0 {
				fmt.Fprintf(bw, " LO BND       %-9s %15.8g\n", sanitize(v.name), v.low)
			}
			fmt.Fprintf(bw, " UP BND       %-9s %15.8g\n", sanitize(v.name), v.high)
		}
	}

	bw.WriteString("ENDATA\n")
	return nil
}

func formatLPExpr(e Expr) string {
	if len(e) == 0 {
		return "0"
	}
	type term struct {
		v *Var
		c float64
	}
	terms := make([]term, 0, len(e))
	for v, c := range e {
		if c == 0 {
			continue
		}
		terms = append(terms, term{v, c})
	}
	sort.Slice(terms, func(i, j int) bool { return terms[i].v.idx < terms[j].v.idx })
	var b strings.Builder
	for i, t := range terms {
		switch {
		case i == 0 && t.c == 1:
			fmt.Fprintf(&b, "%s", sanitize(t.v.name))
		case i == 0 && t.c == -1:
			fmt.Fprintf(&b, "- %s", sanitize(t.v.name))
		case i == 0:
			fmt.Fprintf(&b, "%g %s", t.c, sanitize(t.v.name))
		case t.c == 1:
			fmt.Fprintf(&b, " + %s", sanitize(t.v.name))
		case t.c == -1:
			fmt.Fprintf(&b, " - %s", sanitize(t.v.name))
		case t.c < 0:
			fmt.Fprintf(&b, " - %g %s", -t.c, sanitize(t.v.name))
		default:
			fmt.Fprintf(&b, " + %g %s", t.c, sanitize(t.v.name))
		}
	}
	return b.String()
}

// sanitize replaces characters that are illegal in LP/MPS identifiers
// with underscores. The CPLEX LP format forbids several punctuation
// characters; MPS treats names as 8-char identifiers — but most modern
// solvers accept long names.
func sanitize(name string) string {
	if name == "" {
		return "_"
	}
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if c := out[0]; c >= '0' && c <= '9' {
		out = "_" + out
	}
	return out
}

// newBufW returns a flushable writer that buffers output. Implemented in
// a tiny standalone helper to avoid importing bufio purely for one use.
type flushWriter struct{ io.Writer }

func (flushWriter) Flush() error { return nil }

func newBufW(w io.Writer) interface {
	io.Writer
	io.StringWriter
	Flush() error
} {
	if sw, ok := w.(interface {
		io.Writer
		io.StringWriter
		Flush() error
	}); ok {
		return sw
	}
	return &lineBuf{w: w}
}

// lineBuf is a minimal Writer + StringWriter that flushes on demand. We
// don't actually need to buffer — both LP and MPS files are small enough
// that pass-through is fine — but we expose Flush so the API matches a
// future bufio-backed implementation.
type lineBuf struct{ w io.Writer }

func (l *lineBuf) Write(p []byte) (int, error)       { return l.w.Write(p) }
func (l *lineBuf) WriteString(s string) (int, error) { return l.w.Write([]byte(s)) }
func (l *lineBuf) Flush() error                      { return nil }
