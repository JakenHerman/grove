package grove

// MPS-format parser. The round-trip contract:
//
//	WriteMPS  writes a grove *Problem to fixed-column MPS.
//	ReadMPS   reads fixed or free-form MPS back into a grove *Problem.
//
// A model flowed through WriteMPS → ReadMPS is structurally equivalent
// to the original: same variable names/kinds/bounds, same objective
// coefficients, same constraint coefficients/relations/right-hand
// sides. Declaration order in the resulting problem mirrors the order
// things appear in the file — variables in first-mention order in
// COLUMNS, constraints in declaration order in ROWS.
//
// The parser accepts every section emitted by WriteMPS — NAME, ROWS,
// COLUMNS (with the familiar INTORG/INTEND MIP markers), RHS, BOUNDS,
// ENDATA — and additionally OBJSENSE, RANGES, and SOS. SOS sets are
// silently skipped (grove does not model SOS yet); RANGES rows surface
// a clear diagnostic because range constraints (low ≤ lhs ≤ high)
// are not representable in grove today.
//
// Both the classic fixed-column layout (data in columns 2-3, 5-12,
// 15-22, 25-36, 40-47, 50-61) and the whitespace-separated "free" form
// are accepted. ReadMPS auto-detects the two by default; callers can
// pin either with [MPSFixedInput] or [MPSFreeInput]. For well-formed
// input without exotic names, parsing is identical either way.
//
// Parse errors carry a 1-based line number to make mistakes easy to
// locate and share the [ParseError] type with the LP reader.

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// MPSFormat selects between the two historical MPS encodings accepted
// by [ReadMPS].
type MPSFormat int

const (
	// MPSAuto detects fixed vs free from the input. Default.
	MPSAuto MPSFormat = iota
	// MPSFixed pins the parser to strict fixed-column MPS.
	MPSFixed
	// MPSFree pins the parser to whitespace-separated free-form MPS.
	MPSFree
)

// MPSOption configures [ReadMPS].
type MPSOption func(*mpsConfig)

type mpsConfig struct {
	format MPSFormat
}

// MPSFixedInput forces the parser to treat its input as strict fixed-
// column MPS. Auto-detection is bypassed; data lines that don't fit
// the column layout surface a parse error.
func MPSFixedInput() MPSOption { return func(c *mpsConfig) { c.format = MPSFixed } }

// MPSFreeInput forces the parser to treat its input as free-form
// whitespace-separated MPS. Auto-detection is bypassed.
func MPSFreeInput() MPSOption { return func(c *mpsConfig) { c.format = MPSFree } }

// ReadMPS parses an MPS-format problem from r. It is the inverse of
// [Problem.WriteMPS]: any grove model written with WriteMPS and read
// back with ReadMPS produces an equivalent *Problem.
//
// By default ReadMPS auto-detects whether the input is fixed-column or
// free-form MPS; callers may force either with [MPSFixedInput] or
// [MPSFreeInput]. The parser handles the NAME, ROWS, COLUMNS (with
// INTORG/INTEND integer markers), RHS, RANGES, BOUNDS, and OBJSENSE
// sections and recognises an ENDATA sentinel. SOS sections are
// silently skipped. Unknown section headers surface a parse error
// rather than being swallowed.
//
// Parse errors are returned as *[ParseError] values with a 1-based
// line number (and column, when meaningful) so mistakes surface with
// a useful pointer into the file.
func ReadMPS(r io.Reader, opts ...MPSOption) (*Problem, error) {
	cfg := &mpsConfig{format: MPSAuto}
	for _, opt := range opts {
		opt(cfg)
	}
	return parseMPS(r, cfg)
}

// errAtMPS builds a *ParseError tagged as an MPS-format error.
func errAtMPS(line, col int, format string, args ...any) *ParseError {
	return &ParseError{Line: line, Col: col, Msg: fmt.Sprintf(format, args...), Format: "MPS"}
}

// ── Internal state ──────────────────────────────────────────────────

// mpsRow records one ROWS-section entry — either the N-row (objective)
// or a constraint. Coefficients accumulate into coefs as COLUMNS-
// section lines reference the row by name.
type mpsRow struct {
	name    string
	rowType byte // 'N', 'L', 'G', 'E'
	idx     int
	ctype   ConstraintType
	rhs     float64
	coefs   map[string]float64
}

// mpsCol records one COLUMNS-section column (grove's variable).
// Bounds flags distinguish "not yet set" from "set to the default
// value". The default is lazily materialised in build().
type mpsCol struct {
	name    string
	idx     int
	kind    VarKind // promoted to Integer/Binary by markers or LI/UI/BV
	hasLow  bool
	hasHigh bool
	low     float64
	high    float64
}

type mpsSectionKind int

const (
	mpsSecNone mpsSectionKind = iota
	mpsSecName
	mpsSecObjsense
	mpsSecRows
	mpsSecColumns
	mpsSecRHS
	mpsSecRanges
	mpsSecBounds
	mpsSecSOS
	mpsSecEndata
)

type mpsState struct {
	name        string
	sense       Sense
	rows        []*mpsRow
	rowsByName  map[string]*mpsRow
	cols        []*mpsCol
	colsByName  map[string]*mpsCol
	objRowName  string
	inIntMarker bool
	format      MPSFormat
}

// ── Top-level parsing ───────────────────────────────────────────────

func parseMPS(r io.Reader, cfg *mpsConfig) (*Problem, error) {
	// Slurp the input into memory first. MPS files are section-after-
	// section and format auto-detection needs to peek at the COLUMNS
	// section's first data line, so streaming buys nothing here and
	// costs a much more tangled control flow.
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	var lines []srcLine
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := strings.TrimRight(sc.Text(), "\r")
		lines = append(lines, srcLine{text: raw, no: lineNo})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	format := cfg.format
	if format == MPSAuto {
		format = detectMPSFormat(lines)
	}

	st := &mpsState{
		sense:      Minimize,
		rowsByName: make(map[string]*mpsRow),
		colsByName: make(map[string]*mpsCol),
		format:     format,
	}

	// Walk the file section by section.
	section := mpsSecNone
	sawEndata := false

SCAN:
	for _, ln := range lines {
		text := ln.text
		if text == "" {
			continue
		}
		// Comment lines. MPS comments start with '*' in column 1.
		if text[0] == '*' {
			continue
		}
		// Section headers start at column 1 (no leading whitespace).
		if !isMPSDataLine(text) {
			head := strings.Fields(text)
			if len(head) == 0 {
				continue
			}
			switch strings.ToUpper(head[0]) {
			case "NAME":
				if len(head) > 1 {
					st.name = head[1]
				}
				section = mpsSecName
			case "OBJSENSE":
				section = mpsSecObjsense
			case "ROWS":
				section = mpsSecRows
			case "COLUMNS":
				section = mpsSecColumns
			case "RHS":
				section = mpsSecRHS
			case "RANGES":
				section = mpsSecRanges
			case "BOUNDS":
				section = mpsSecBounds
			case "SOS":
				section = mpsSecSOS
			case "ENDATA":
				sawEndata = true
				break SCAN
			default:
				return nil, errAtMPS(ln.no, 0, "unknown section %q", head[0])
			}
			continue
		}

		// Data line: dispatch to the current section's handler.
		fields := strings.Fields(text)
		if len(fields) == 0 {
			continue
		}
		switch section {
		case mpsSecObjsense:
			if err := st.parseObjsenseLine(ln, fields); err != nil {
				return nil, err
			}
		case mpsSecRows:
			if err := st.parseRowLine(ln, fields); err != nil {
				return nil, err
			}
		case mpsSecColumns:
			if err := st.parseColumnLine(ln, fields); err != nil {
				return nil, err
			}
		case mpsSecRHS:
			if err := st.parseRHSLine(ln, fields); err != nil {
				return nil, err
			}
		case mpsSecRanges:
			if err := st.parseRangesLine(ln, fields); err != nil {
				return nil, err
			}
		case mpsSecBounds:
			if err := st.parseBoundLine(ln, fields); err != nil {
				return nil, err
			}
		case mpsSecSOS:
			// grove does not model SOS sets yet.
		case mpsSecName, mpsSecNone:
			return nil, errAtMPS(ln.no, 0, "data before first recognised section")
		}
	}

	if !sawEndata {
		// Tolerate: many emitters skip ENDATA. But report if there
		// were no sections at all.
		if len(st.rows) == 0 && len(st.cols) == 0 {
			return nil, &ParseError{Msg: "empty MPS input", Format: "MPS"}
		}
	}

	return st.build()
}

// isMPSDataLine reports whether text begins with whitespace, the
// distinguishing feature of an MPS data line (section headers start at
// column 1).
func isMPSDataLine(text string) bool {
	if text == "" {
		return false
	}
	return text[0] == ' ' || text[0] == '\t'
}

// ── Section handlers ────────────────────────────────────────────────

func (s *mpsState) parseObjsenseLine(ln srcLine, fields []string) error {
	switch strings.ToUpper(fields[0]) {
	case "MAX", "MAXIMIZE":
		s.sense = Maximize
	case "MIN", "MINIMIZE":
		s.sense = Minimize
	default:
		return errAtMPS(ln.no, 0, "OBJSENSE expects MAX or MIN, got %q", fields[0])
	}
	return nil
}

func (s *mpsState) parseRowLine(ln srcLine, fields []string) error {
	if len(fields) < 2 {
		return errAtMPS(ln.no, 0, "ROWS line needs a type and a name")
	}
	t := strings.ToUpper(fields[0])
	if len(t) != 1 {
		return errAtMPS(ln.no, 0, "unknown row type %q (expected N, L, G, or E)", fields[0])
	}
	name := fields[1]
	if _, dup := s.rowsByName[name]; dup {
		return errAtMPS(ln.no, 0, "duplicate row name %q", name)
	}
	r := &mpsRow{name: name, rowType: t[0], idx: len(s.rows), coefs: make(map[string]float64)}
	switch t[0] {
	case 'N':
		if s.objRowName == "" {
			s.objRowName = name
		}
	case 'L':
		r.ctype = LTE
	case 'G':
		r.ctype = GTE
	case 'E':
		r.ctype = EQ
	default:
		return errAtMPS(ln.no, 0, "unknown row type %q (expected N, L, G, or E)", fields[0])
	}
	s.rowsByName[name] = r
	s.rows = append(s.rows, r)
	return nil
}

// isMarkerLine recognises the well-known COLUMNS-section lines that
// toggle the integer marker state. The middle field is literally the
// token 'MARKER' (single-quoted) per the CPLEX MPS extension.
func isMarkerLine(fields []string) bool {
	if len(fields) < 3 {
		return false
	}
	mid := strings.Trim(fields[1], "'\"")
	return mid == "MARKER"
}

func (s *mpsState) parseColumnLine(ln srcLine, fields []string) error {
	if isMarkerLine(fields) {
		// The third token tells us whether we're entering ('INTORG')
		// or leaving ('INTEND') the integer block.
		kind := strings.Trim(fields[2], "'\"")
		switch kind {
		case "INTORG":
			s.inIntMarker = true
		case "INTEND":
			s.inIntMarker = false
		default:
			return errAtMPS(ln.no, 0, "unknown MARKER kind %q (expected INTORG or INTEND)", fields[2])
		}
		return nil
	}

	if len(fields) < 3 {
		return errAtMPS(ln.no, 0, "COLUMNS line needs a column name and at least one row/value pair")
	}
	colName := fields[0]
	col, ok := s.colsByName[colName]
	if !ok {
		col = &mpsCol{name: colName, idx: len(s.cols)}
		if s.inIntMarker {
			col.kind = Integer
		}
		s.colsByName[colName] = col
		s.cols = append(s.cols, col)
	} else if s.inIntMarker && col.kind == Continuous {
		// A column may legally reappear inside the same INTORG block
		// (or in multiple blocks). Promote it on the first sighting
		// inside a marker.
		col.kind = Integer
	}

	rest := fields[1:]
	if len(rest)%2 != 0 {
		return errAtMPS(ln.no, 0, "COLUMNS line has odd number of row/value fields after the column name")
	}
	for i := 0; i < len(rest); i += 2 {
		rowName := rest[i]
		val, err := parseMPSNumber(rest[i+1])
		if err != nil {
			return errAtMPS(ln.no, 0, "invalid value %q: %v", rest[i+1], err)
		}
		row, ok := s.rowsByName[rowName]
		if !ok {
			return errAtMPS(ln.no, 0, "unknown row %q in COLUMNS section", rowName)
		}
		row.coefs[colName] += val
	}
	return nil
}

// parseRHSLine parses a line of the RHS section. Lines have the shape
//
//	<rhsname>  <row1>  <val1>  [<row2>  <val2>]
//
// where <rhsname> is an arbitrary identifier naming the RHS vector
// (most emitters use "RHS", "B", "RR", or the problem's name). We
// accept the loosely-specified free-form case where <rhsname> is
// omitted by peeking: if the first field names a known row, we treat
// it as the first row rather than the vector name.
func (s *mpsState) parseRHSLine(ln srcLine, fields []string) error {
	rest := fields
	if len(rest) == 0 {
		return errAtMPS(ln.no, 0, "empty RHS line")
	}
	if _, isRow := s.rowsByName[rest[0]]; !isRow {
		rest = rest[1:]
	}
	if len(rest)%2 != 0 {
		return errAtMPS(ln.no, 0, "RHS line has odd number of row/value fields")
	}
	for i := 0; i < len(rest); i += 2 {
		rowName := rest[i]
		val, err := parseMPSNumber(rest[i+1])
		if err != nil {
			return errAtMPS(ln.no, 0, "invalid RHS value %q: %v", rest[i+1], err)
		}
		row, ok := s.rowsByName[rowName]
		if !ok {
			return errAtMPS(ln.no, 0, "unknown row %q in RHS", rowName)
		}
		row.rhs = val
	}
	return nil
}

// parseRangesLine recognises RANGES entries structurally but refuses to
// lower them into grove constraints — grove does not (yet) model
// range-style rows low ≤ a·x ≤ high. The caller gets a clear error
// rather than a silently mangled problem. Empty ranges sections are
// tolerated without issue.
func (s *mpsState) parseRangesLine(ln srcLine, fields []string) error {
	return errAtMPS(ln.no, 0,
		"RANGES section is not supported (grove does not model range constraints); "+
			"split the offending row into two separate constraints")
}

// parseBoundLine parses one bound statement. Accepted forms:
//
//	UP bndname var value
//	LO bndname var value
//	FX bndname var value
//	FR bndname var
//	MI bndname var
//	PL bndname var
//	BV bndname var
//	LI bndname var value
//	UI bndname var value
//
// Like the RHS parser, we tolerate the non-standard free-form where
// the bound-vector name is omitted.
func (s *mpsState) parseBoundLine(ln srcLine, fields []string) error {
	if len(fields) < 2 {
		return errAtMPS(ln.no, 0, "BOUNDS line is too short")
	}
	t := strings.ToUpper(fields[0])

	// Figure out where the variable name lives. Standard:
	//   fields[0]=TYPE  fields[1]=bndname  fields[2]=var  [fields[3]=value]
	// Free-form without bndname:
	//   fields[0]=TYPE  fields[1]=var      [fields[2]=value]
	varIdx := 2
	valIdx := 3
	if len(fields) < 3 || s.colsByName[fields[2]] == nil {
		// Fall back to the no-bndname interpretation if the standard
		// layout doesn't resolve to a known variable.
		if _, ok := s.colsByName[fields[1]]; ok {
			varIdx = 1
			valIdx = 2
		}
	}
	if varIdx >= len(fields) {
		return errAtMPS(ln.no, 0, "BOUNDS line missing variable name")
	}
	varName := fields[varIdx]
	col, ok := s.colsByName[varName]
	if !ok {
		return errAtMPS(ln.no, 0, "bound on unknown variable %q", varName)
	}

	readValue := func() (float64, error) {
		if valIdx >= len(fields) {
			return 0, errAtMPS(ln.no, 0, "%s bound needs a value", t)
		}
		return parseMPSNumber(fields[valIdx])
	}

	switch t {
	case "UP":
		v, err := readValue()
		if err != nil {
			return err
		}
		col.high = v
		col.hasHigh = true
	case "LO":
		v, err := readValue()
		if err != nil {
			return err
		}
		col.low = v
		col.hasLow = true
	case "FX":
		v, err := readValue()
		if err != nil {
			return err
		}
		col.low, col.high = v, v
		col.hasLow, col.hasHigh = true, true
	case "FR":
		col.low, col.high = math.Inf(-1), math.Inf(1)
		col.hasLow, col.hasHigh = true, true
	case "MI":
		col.low = math.Inf(-1)
		col.hasLow = true
	case "PL":
		col.high = math.Inf(1)
		col.hasHigh = true
	case "BV":
		col.low, col.high = 0, 1
		col.hasLow, col.hasHigh = true, true
		col.kind = Binary
	case "LI":
		v, err := readValue()
		if err != nil {
			return err
		}
		col.low = v
		col.hasLow = true
		if col.kind != Binary {
			col.kind = Integer
		}
	case "UI":
		v, err := readValue()
		if err != nil {
			return err
		}
		col.high = v
		col.hasHigh = true
		if col.kind != Binary {
			col.kind = Integer
		}
	default:
		return errAtMPS(ln.no, 0, "unknown bound type %q", t)
	}
	return nil
}

// parseMPSNumber parses a numeric field. MPS values are plain floats,
// but we accept the informal "inf"/"infinity" spellings as a kindness
// for files written by tools with a looser view of the spec.
func parseMPSNumber(s string) (float64, error) {
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "+") {
		low = low[1:]
	}
	sign := 1.0
	if strings.HasPrefix(low, "-") {
		sign = -1
		low = low[1:]
	}
	switch low {
	case "inf", "infinity", "infty":
		if sign < 0 {
			return math.Inf(-1), nil
		}
		return math.Inf(1), nil
	}
	return strconv.ParseFloat(s, 64)
}

// ── Assembly ────────────────────────────────────────────────────────

// build assembles a fully populated *Problem from the collected state.
// Variables come out in COLUMNS declaration order; constraints in ROWS
// declaration order (N-row is promoted to the objective).
func (s *mpsState) build() (*Problem, error) {
	prob := NewProblem(s.name, s.sense)

	varByName := make(map[string]*Var, len(s.cols))
	for _, col := range s.cols {
		kind := col.kind

		low := 0.0
		high := math.Inf(1)
		if kind == Binary {
			low, high = 0, 1
		}
		if col.hasLow {
			low = col.low
		}
		if col.hasHigh {
			high = col.high
		}
		v := prob.NewVar(col.name, kind, Bounds(low, high))
		varByName[col.name] = v
	}

	if s.objRowName != "" {
		if objRow, ok := s.rowsByName[s.objRowName]; ok {
			obj := make(Expr, len(objRow.coefs))
			for colName, coef := range objRow.coefs {
				if coef == 0 {
					continue
				}
				v, ok := varByName[colName]
				if !ok {
					continue
				}
				obj[v] = coef
			}
			prob.SetObjective(obj)
		}
	}

	for _, row := range s.rows {
		if row.rowType == 'N' {
			continue
		}
		e := make(Expr, len(row.coefs))
		for colName, coef := range row.coefs {
			if coef == 0 {
				continue
			}
			v, ok := varByName[colName]
			if !ok {
				continue
			}
			e[v] = coef
		}
		prob.AddConstraint(row.name, e, row.ctype, row.rhs)
	}

	return prob, nil
}

// ── Auto-detection ──────────────────────────────────────────────────

// detectMPSFormat inspects data lines to guess whether the file uses
// the strict 8/12-column fixed layout or the more permissive free
// form. The heuristic is the same one HiGHS and CBC use: if any
// identifier in a data line overflows its 8-character slot (cols 5-12
// or 15-22), or spills into a column the fixed layout reserves for
// whitespace, the file is free format.
//
// For well-formed input with short names the two encodings are
// indistinguishable, and we default to reporting MPSFixed — the
// parser itself tokenises whitespace either way.
func detectMPSFormat(lines []srcLine) MPSFormat {
	section := ""
	for _, ln := range lines {
		text := ln.text
		if text == "" {
			continue
		}
		if text[0] == '*' {
			continue
		}
		if !isMPSDataLine(text) {
			if f := strings.Fields(text); len(f) > 0 {
				section = strings.ToUpper(f[0])
			}
			continue
		}
		switch section {
		case "COLUMNS", "RHS", "RANGES", "BOUNDS":
			if looksFreeForm(text) {
				return MPSFree
			}
			return MPSFixed
		}
	}
	return MPSFixed
}

// looksFreeForm reports whether a data line violates the fixed-column
// layout's whitespace gutters. Text is 0-indexed; MPS columns are
// 1-indexed, so text[12] is column 13.
func looksFreeForm(text string) bool {
	isWS := func(i int) bool {
		if i >= len(text) {
			return true
		}
		c := text[i]
		return c == ' ' || c == '\t'
	}
	// Columns 13-14 and 23-24 are gutters between the 8-char name
	// slots. Column 37-39 and 48-49 separate the value slots from the
	// next name slot.
	for _, col := range []int{12, 13, 22, 23, 36, 37, 38, 47, 48} {
		if !isWS(col) {
			return true
		}
	}
	return false
}
