package grove

// CPLEX LP-format parser. The round-trip contract:
//
//	WriteLP  writes a grove *Problem to CPLEX LP format.
//	ReadLP   reads CPLEX LP format back into a grove *Problem.
//
// A model flowed through WriteLP → ReadLP is structurally equivalent
// to the original: same sense, same variable names/kinds/bounds, same
// objective coefficients and constant, same constraint coefficients,
// relations, and right-hand sides. Declaration order is preserved when
// every variable appears in the objective (which is how WriteLP orders
// its output).
//
// The grammar accepted here is the CPLEX LP section-based grammar
// (Minimize/Maximize, Subject To, Bounds, General, Binary, End), with
// the usual tolerances: section headers are case-insensitive and may be
// abbreviated (Min/Max, s.t./st/such that, Bound, Gen/Integer/Integers,
// Bin/Binaries). Comments start with "\" and run to end of line. Blank
// lines are allowed anywhere. Variable names that are never declared
// in a Bounds, General, or Binary section default to a non-negative
// continuous variable (lower bound 0, upper bound +Inf) — matching the
// CPLEX default.
//
// Parse errors carry a 1-based line number (and column, when available)
// to make mistakes easy to locate.

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// ReadLP parses a CPLEX LP-format problem from r. It is the inverse of
// [Problem.WriteLP]: any grove model written with WriteLP and read back
// with ReadLP produces an equivalent *Problem.
//
// The parser accepts the standard section-based LP grammar with common
// tolerances: case-insensitive keywords, abbreviated forms (Min/Max,
// s.t./st/such that, Bound, Gen/Integer/Integers, Bin/Binaries),
// backslash comments, and blank lines anywhere. Unknown sections (e.g.
// SOS) are skipped rather than rejected. Undeclared variables default
// to [0, +Inf] continuous, matching the CPLEX default.
func ReadLP(r io.Reader) (*Problem, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return parseLP(string(data))
}

// parseError carries a 1-based line and column so the caller can point
// the user at the offending spot.
type parseError struct {
	Line, Col int
	Msg       string
}

func (e *parseError) Error() string {
	switch {
	case e.Col > 0 && e.Line > 0:
		return fmt.Sprintf("grove: LP parse error at line %d, column %d: %s", e.Line, e.Col, e.Msg)
	case e.Line > 0:
		return fmt.Sprintf("grove: LP parse error at line %d: %s", e.Line, e.Msg)
	default:
		return "grove: LP parse error: " + e.Msg
	}
}

func errAt(line, col int, format string, args ...any) *parseError {
	return &parseError{Line: line, Col: col, Msg: fmt.Sprintf(format, args...)}
}

// srcLine is an input line with its original 1-based line number.
// Comments have already been stripped by the time one of these lands
// in a section body.
type srcLine struct {
	text string
	no   int
}

type sectionKind int

const (
	secNone sectionKind = iota
	secMin
	secMax
	secST
	secBounds
	secGeneral
	secBinary
	secSOS
	secEnd
)

type section struct {
	kind   sectionKind
	header int // 1-based line of the header keyword
	body   []srcLine
}

// splitSections walks the raw source and slices it at section headers.
// Each returned section carries the body lines (with comments stripped)
// between its own header and the next header. The initial (pre-header)
// slice is returned as a secNone section and is expected to be empty
// for well-formed input.
func splitSections(src string) []section {
	raw := strings.Split(src, "\n")
	lines := make([]srcLine, 0, len(raw))
	for i, s := range raw {
		if j := strings.IndexByte(s, '\\'); j >= 0 {
			s = s[:j]
		}
		s = strings.TrimRight(s, "\r")
		lines = append(lines, srcLine{text: s, no: i + 1})
	}

	var sections []section
	cur := section{kind: secNone}
	for _, l := range lines {
		trimmed := strings.TrimSpace(l.text)
		if trimmed == "" {
			cur.body = append(cur.body, l)
			continue
		}
		if kind, ok := matchSectionHeader(trimmed); ok {
			sections = append(sections, cur)
			cur = section{kind: kind, header: l.no}
			continue
		}
		cur.body = append(cur.body, l)
	}
	sections = append(sections, cur)
	return sections
}

// matchSectionHeader returns the section kind for a line that is
// entirely a section header, case-insensitive and whitespace-insensitive.
// All the variant spellings the wild sees are accepted.
func matchSectionHeader(line string) (sectionKind, bool) {
	norm := strings.ToLower(strings.Join(strings.Fields(line), " "))
	switch norm {
	case "minimize", "minimise", "min":
		return secMin, true
	case "maximize", "maximise", "max":
		return secMax, true
	case "subject to", "such that", "s.t.", "st.", "st":
		return secST, true
	case "bounds", "bound":
		return secBounds, true
	case "general", "generals", "gen", "integer", "integers":
		return secGeneral, true
	case "binary", "binaries", "bin":
		return secBinary, true
	case "sos":
		return secSOS, true
	case "end":
		return secEnd, true
	}
	return secNone, false
}

// ── Tokenizer ─────────────────────────────────────────────────────────

type tokType int

const (
	tokEOF tokType = iota
	tokIdent
	tokNumber
	tokColon
	tokPlus
	tokMinus
	tokStar
	tokLE
	tokGE
	tokEQ
)

type token struct {
	typ       tokType
	text      string
	num       float64
	line, col int
}

type tokenizer struct {
	lines []srcLine
	i     int
	j     int
}

func newTokenizer(body []srcLine) *tokenizer {
	return &tokenizer{lines: body}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isStructural reports whether c is a character with its own syntactic
// role (whitespace or one of the LP operators).
func isStructural(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '+', '-', '*', '<', '>', '=', ':', '\\':
		return true
	}
	return false
}

// isIdentStart/isIdentPart follow the CPLEX LP convention: identifiers
// may use most punctuation except the structural operators and must not
// start with a digit or a period (which would collide with numeric
// literals).
func isIdentStart(c byte) bool {
	if isStructural(c) || isDigit(c) || c == '.' {
		return false
	}
	return true
}

func isIdentPart(c byte) bool { return !isStructural(c) }

func (t *tokenizer) next() (token, error) {
	for t.i < len(t.lines) {
		line := t.lines[t.i].text
		for t.j < len(line) && (line[t.j] == ' ' || line[t.j] == '\t' || line[t.j] == '\r') {
			t.j++
		}
		if t.j >= len(line) {
			t.i++
			t.j = 0
			continue
		}
		c := line[t.j]
		lno := t.lines[t.i].no
		col := t.j + 1
		switch {
		case c == ':':
			t.j++
			return token{typ: tokColon, line: lno, col: col}, nil
		case c == '+':
			t.j++
			return token{typ: tokPlus, line: lno, col: col}, nil
		case c == '-':
			t.j++
			return token{typ: tokMinus, line: lno, col: col}, nil
		case c == '*':
			t.j++
			return token{typ: tokStar, line: lno, col: col}, nil
		case c == '<':
			t.j++
			if t.j < len(line) && line[t.j] == '=' {
				t.j++
			}
			return token{typ: tokLE, line: lno, col: col}, nil
		case c == '>':
			t.j++
			if t.j < len(line) && line[t.j] == '=' {
				t.j++
			}
			return token{typ: tokGE, line: lno, col: col}, nil
		case c == '=':
			t.j++
			if t.j < len(line) {
				if line[t.j] == '<' {
					t.j++
					return token{typ: tokLE, line: lno, col: col}, nil
				}
				if line[t.j] == '>' {
					t.j++
					return token{typ: tokGE, line: lno, col: col}, nil
				}
			}
			return token{typ: tokEQ, line: lno, col: col}, nil
		case isDigit(c) || c == '.':
			return t.readNumber(lno, col)
		case isIdentStart(c):
			return t.readIdent(lno, col)
		default:
			t.j++
			return token{}, errAt(lno, col, "unexpected character %q", c)
		}
	}
	return token{typ: tokEOF}, nil
}

func (t *tokenizer) readNumber(lno, col int) (token, error) {
	line := t.lines[t.i].text
	start := t.j
	sawDot := false
	for t.j < len(line) {
		c := line[t.j]
		if isDigit(c) {
			t.j++
			continue
		}
		if c == '.' && !sawDot {
			sawDot = true
			t.j++
			continue
		}
		break
	}
	if t.j < len(line) && (line[t.j] == 'e' || line[t.j] == 'E') {
		t.j++
		if t.j < len(line) && (line[t.j] == '+' || line[t.j] == '-') {
			t.j++
		}
		for t.j < len(line) && isDigit(line[t.j]) {
			t.j++
		}
	}
	txt := line[start:t.j]
	n, err := strconv.ParseFloat(txt, 64)
	if err != nil {
		return token{}, errAt(lno, col, "invalid number %q", txt)
	}
	return token{typ: tokNumber, text: txt, num: n, line: lno, col: col}, nil
}

func (t *tokenizer) readIdent(lno, col int) (token, error) {
	line := t.lines[t.i].text
	start := t.j
	for t.j < len(line) && isIdentPart(line[t.j]) {
		t.j++
	}
	return token{typ: tokIdent, text: line[start:t.j], line: lno, col: col}, nil
}

// ── Parser ────────────────────────────────────────────────────────────

type parser struct {
	tok *tokenizer
	buf []token // fixed-size-ish peek buffer
}

func (p *parser) peekN(n int) (token, error) {
	for len(p.buf) <= n {
		t, err := p.tok.next()
		if err != nil {
			return token{}, err
		}
		p.buf = append(p.buf, t)
		if t.typ == tokEOF {
			break
		}
	}
	if n >= len(p.buf) {
		return p.buf[len(p.buf)-1], nil
	}
	return p.buf[n], nil
}

func (p *parser) peek() (token, error) { return p.peekN(0) }

func (p *parser) consume() (token, error) {
	if len(p.buf) > 0 {
		t := p.buf[0]
		p.buf = p.buf[1:]
		return t, nil
	}
	return p.tok.next()
}

// expectEOF fails if the stream isn't at EOF. Used after parsing a
// bound statement to flag stray trailing tokens.
func (p *parser) expectEOF() error {
	t, err := p.consume()
	if err != nil {
		return err
	}
	if t.typ != tokEOF {
		text := t.text
		if text == "" {
			text = symbolName(t.typ)
		}
		return errAt(t.line, t.col, "unexpected trailing %q", text)
	}
	return nil
}

// symbolName is a display helper for tokens that carry no literal text.
func symbolName(t tokType) string {
	switch t {
	case tokPlus:
		return "+"
	case tokMinus:
		return "-"
	case tokStar:
		return "*"
	case tokColon:
		return ":"
	case tokLE:
		return "<="
	case tokGE:
		return ">="
	case tokEQ:
		return "="
	case tokEOF:
		return "<eof>"
	}
	return "<token>"
}

// isInfinityWord recognises the various spellings of infinity that
// appear in the Bounds section.
func isInfinityWord(s string) bool {
	switch strings.ToLower(s) {
	case "inf", "infinity", "infty":
		return true
	}
	return false
}

// parseSignedNumber reads an optional +/- and a numeric literal (or an
// infinity keyword). Used for right-hand sides and bound values.
func (p *parser) parseSignedNumber() (float64, error) {
	sign := 1.0
	t, err := p.peek()
	if err != nil {
		return 0, err
	}
	switch t.typ {
	case tokPlus:
		_, _ = p.consume()
	case tokMinus:
		_, _ = p.consume()
		sign = -1
	}
	t, err = p.consume()
	if err != nil {
		return 0, err
	}
	switch t.typ {
	case tokNumber:
		return sign * t.num, nil
	case tokIdent:
		if isInfinityWord(t.text) {
			if sign < 0 {
				return math.Inf(-1), nil
			}
			return math.Inf(1), nil
		}
	}
	return 0, errAt(t.line, t.col, "expected number, got %q", t.text)
}

// parseExprAndConst reads a linear expression (a sum of signed terms)
// and returns the sparse coefficient map along with any constant terms.
// Parsing stops at a relation operator (<=, >=, =), a colon, or EOF.
//
// First term's sign is optional; subsequent terms must be separated by
// an explicit + or -. A "term" is either `[coef] [*] ident` (a linear
// term) or a bare number (a constant contribution). The `*` between
// coefficient and variable is optional, matching the CPLEX LP grammar.
func (p *parser) parseExprAndConst(b *builder) (Expr, float64, error) {
	expr := Expr{}
	constant := 0.0
	first := true

	for {
		sign := 1.0
		hasSign := false
		t, err := p.peek()
		if err != nil {
			return nil, 0, err
		}
		switch t.typ {
		case tokPlus:
			_, _ = p.consume()
			hasSign = true
		case tokMinus:
			_, _ = p.consume()
			sign = -1
			hasSign = true
		}

		t, err = p.peek()
		if err != nil {
			return nil, 0, err
		}

		if !first && !hasSign {
			switch t.typ {
			case tokLE, tokGE, tokEQ, tokColon, tokEOF:
				return expr, constant, nil
			default:
				return nil, 0, errAt(t.line, t.col, "expected '+' or '-' between terms")
			}
		}
		if hasSign {
			switch t.typ {
			case tokLE, tokGE, tokEQ, tokColon, tokEOF:
				return nil, 0, errAt(t.line, t.col, "dangling sign")
			}
		} else {
			switch t.typ {
			case tokLE, tokGE, tokEQ, tokColon, tokEOF:
				return expr, constant, nil
			}
		}

		coef := 1.0
		haveCoef := false
		if t.typ == tokNumber {
			coef = t.num
			haveCoef = true
			_, _ = p.consume()
			t2, err := p.peek()
			if err != nil {
				return nil, 0, err
			}
			if t2.typ == tokStar {
				_, _ = p.consume()
			}
		}

		t3, err := p.peek()
		if err != nil {
			return nil, 0, err
		}
		switch {
		case t3.typ == tokIdent:
			_, _ = p.consume()
			v := b.getVar(t3.text)
			expr[v] += sign * coef
		case haveCoef:
			constant += sign * coef
		default:
			return nil, 0, errAt(t3.line, t3.col, "expected number or identifier")
		}
		first = false
	}
}

// ── builder ───────────────────────────────────────────────────────────

// builder tracks the problem under construction, creating variables on
// first mention with default bounds (0, +Inf) and continuous kind. The
// Bounds, General, and Binary sections later adjust bounds and kinds
// in place.
type builder struct {
	prob *Problem
	vars map[string]*Var
}

func (b *builder) getVar(name string) *Var {
	if v, ok := b.vars[name]; ok {
		return v
	}
	v := b.prob.NewVar(name, Continuous)
	b.vars[name] = v
	return v
}

// ── Section parsers ───────────────────────────────────────────────────

func (b *builder) parseObjective(sec section) error {
	if !hasContent(sec.body) {
		return errAt(sec.header, 0, "missing objective expression")
	}
	p := &parser{tok: newTokenizer(sec.body)}

	// Optional "label:" prefix.
	t1, err := p.peek()
	if err != nil {
		return err
	}
	if t1.typ == tokIdent {
		t2, err := p.peekN(1)
		if err != nil {
			return err
		}
		if t2.typ == tokColon {
			_, _ = p.consume() // label ident
			_, _ = p.consume() // colon
		}
	}

	expr, constant, err := p.parseExprAndConst(b)
	if err != nil {
		return err
	}
	t, err := p.consume()
	if err != nil {
		return err
	}
	if t.typ != tokEOF {
		return errAt(t.line, t.col, "unexpected token %q in objective", t.text)
	}
	// Overwrite the objective directly — SetObjective would re-copy and
	// re-validate, but we know every *Var was just produced by b.getVar.
	b.prob.objective = expr
	b.prob.objConst = constant
	return nil
}

func (b *builder) parseConstraints(sec section) error {
	if !hasContent(sec.body) {
		return nil
	}
	p := &parser{tok: newTokenizer(sec.body)}
	anon := 0
	for {
		t, err := p.peek()
		if err != nil {
			return err
		}
		if t.typ == tokEOF {
			return nil
		}

		// Optional "name:" prefix.
		name := ""
		if t.typ == tokIdent {
			t2, err := p.peekN(1)
			if err != nil {
				return err
			}
			if t2.typ == tokColon {
				_, _ = p.consume() // ident
				_, _ = p.consume() // colon
				name = t.text
			}
		}
		if name == "" {
			anon++
			name = fmt.Sprintf("c%d", anon)
		}

		expr, lhsConst, err := p.parseExprAndConst(b)
		if err != nil {
			return err
		}

		opTok, err := p.consume()
		if err != nil {
			return err
		}
		var ctype ConstraintType
		switch opTok.typ {
		case tokLE:
			ctype = LTE
		case tokGE:
			ctype = GTE
		case tokEQ:
			ctype = EQ
		default:
			return errAt(opTok.line, opTok.col, "expected '<=', '>=', or '=' in constraint %q", name)
		}

		rhs, err := p.parseSignedNumber()
		if err != nil {
			return err
		}

		// Range constraints ("3 <= expr <= 10") are not modeled by grove
		// today — surface a clear diagnostic rather than silently drop
		// the second half.
		if tNext, err := p.peek(); err == nil {
			if tNext.typ == tokLE || tNext.typ == tokGE {
				return errAt(tNext.line, tNext.col, "range constraints are not supported in constraint %q", name)
			}
		}

		if _, exists := b.prob.consNames[name]; exists {
			return errAt(opTok.line, opTok.col, "duplicate constraint name %q", name)
		}
		b.prob.AddConstraint(name, expr, ctype, rhs-lhsConst)
	}
}

func (b *builder) parseBounds(sec section) error {
	for _, line := range sec.body {
		if strings.TrimSpace(line.text) == "" {
			continue
		}
		if err := b.parseBoundLine(line); err != nil {
			return err
		}
	}
	return nil
}

// parseBoundLine accepts any of the CPLEX LP bound statement shapes:
//
//	x free                           -> (-∞, +∞)
//	x <= u                           -> upper bound u
//	x >= l   (or  x > l)             -> lower bound l
//	x = v                            -> x fixed at v
//	l <= x                           -> lower bound l
//	l <= x <= u                      -> two-sided
//	-inf <= x <= +inf                -> free (via infinity keywords)
//
// The reverse form "u >= x" is also accepted and treated as "x <= u".
func (b *builder) parseBoundLine(line srcLine) error {
	p := &parser{tok: newTokenizer([]srcLine{line})}

	t, err := p.peek()
	if err != nil {
		return err
	}
	switch t.typ {
	case tokIdent:
		_, _ = p.consume()
		v := b.getVar(t.text)

		t2, err := p.consume()
		if err != nil {
			return err
		}
		if t2.typ == tokIdent && strings.EqualFold(t2.text, "free") {
			v.low = math.Inf(-1)
			v.high = math.Inf(1)
			return p.expectEOF()
		}
		switch t2.typ {
		case tokLE:
			num, err := p.parseSignedNumber()
			if err != nil {
				return err
			}
			v.high = num
		case tokGE:
			num, err := p.parseSignedNumber()
			if err != nil {
				return err
			}
			v.low = num
		case tokEQ:
			num, err := p.parseSignedNumber()
			if err != nil {
				return err
			}
			v.low = num
			v.high = num
		default:
			return errAt(t2.line, t2.col, "expected '<=', '>=', '=', or 'free' after variable %q", t.text)
		}
		return p.expectEOF()

	case tokNumber, tokPlus, tokMinus:
		lo, err := p.parseSignedNumber()
		if err != nil {
			return err
		}
		opTok, err := p.consume()
		if err != nil {
			return err
		}
		var flipped bool
		switch opTok.typ {
		case tokLE:
			flipped = false
		case tokGE:
			flipped = true
		default:
			return errAt(opTok.line, opTok.col, "expected '<=' or '>=' after bound value")
		}
		idTok, err := p.consume()
		if err != nil {
			return err
		}
		if idTok.typ != tokIdent {
			return errAt(idTok.line, idTok.col, "expected variable name")
		}
		v := b.getVar(idTok.text)

		t3, err := p.peek()
		if err != nil {
			return err
		}
		if t3.typ == tokLE || t3.typ == tokGE {
			_, _ = p.consume()
			hi, err := p.parseSignedNumber()
			if err != nil {
				return err
			}
			if flipped {
				v.high = lo
				v.low = hi
			} else {
				v.low = lo
				v.high = hi
			}
		} else {
			if flipped {
				v.high = lo
			} else {
				v.low = lo
			}
		}
		return p.expectEOF()
	}
	return errAt(t.line, t.col, "unexpected token %q in bounds", t.text)
}

func (b *builder) parseVarList(sec section, kind VarKind) error {
	p := &parser{tok: newTokenizer(sec.body)}
	for {
		t, err := p.consume()
		if err != nil {
			return err
		}
		if t.typ == tokEOF {
			return nil
		}
		if t.typ != tokIdent {
			return errAt(t.line, t.col, "expected variable name, got %q", t.text)
		}
		v := b.getVar(t.text)
		v.kind = kind
		// The LP convention: Binary variables are pinned to [0, 1]
		// regardless of any earlier Bounds row. (Mirrors NewVar.)
		if kind == Binary {
			v.low, v.high = 0, 1
		}
	}
}

func hasContent(body []srcLine) bool {
	for _, l := range body {
		if strings.TrimSpace(l.text) != "" {
			return true
		}
	}
	return false
}

// parseLP is the top-level routing function. It walks the section list,
// picks up the objective sense, then hands each section to the
// appropriate parser. The order mirrors the LP file format: objective,
// then Subject To, then Bounds, then General/Binary, then End.
func parseLP(src string) (*Problem, error) {
	sections := splitSections(src)

	var sense Sense
	var senseFound bool
	var objSection section
	var stSection section
	var boundsSection section
	var generalSections []section
	var binarySections []section

	for _, sec := range sections {
		switch sec.kind {
		case secNone:
			// Accept only empty preambles / trailing regions. A
			// non-empty sectionless block is a structural error.
			if hasContent(sec.body) && senseFound {
				continue // tolerate trailing junk after End
			}
			if hasContent(sec.body) {
				l := sec.body[0]
				return nil, errAt(l.no, 1, "content before Minimize/Maximize header")
			}
		case secMin:
			if senseFound {
				return nil, errAt(sec.header, 0, "duplicate objective section")
			}
			sense = Minimize
			senseFound = true
			objSection = sec
		case secMax:
			if senseFound {
				return nil, errAt(sec.header, 0, "duplicate objective section")
			}
			sense = Maximize
			senseFound = true
			objSection = sec
		case secST:
			stSection = sec
		case secBounds:
			boundsSection = sec
		case secGeneral:
			generalSections = append(generalSections, sec)
		case secBinary:
			binarySections = append(binarySections, sec)
		case secSOS:
			// Silently skip — grove does not model SOS sets (yet).
		case secEnd:
			// Stop looking at further content; LP format sanctions
			// whatever appears after End.
			goto done
		}
	}
done:

	if !senseFound {
		return nil, &parseError{Msg: "missing Minimize/Maximize section"}
	}

	prob := NewProblem("", sense)
	b := &builder{prob: prob, vars: map[string]*Var{}}

	if err := b.parseObjective(objSection); err != nil {
		return nil, err
	}
	if stSection.kind == secST {
		if err := b.parseConstraints(stSection); err != nil {
			return nil, err
		}
	}
	if boundsSection.kind == secBounds {
		if err := b.parseBounds(boundsSection); err != nil {
			return nil, err
		}
	}
	for _, g := range generalSections {
		if err := b.parseVarList(g, Integer); err != nil {
			return nil, err
		}
	}
	for _, bn := range binarySections {
		if err := b.parseVarList(bn, Binary); err != nil {
			return nil, err
		}
	}
	return prob, nil
}
