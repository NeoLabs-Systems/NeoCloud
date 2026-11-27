package signpdf

import (
	"context"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Field detection: where a document wants something written. It reads the
// AcroForm and the layout of each page (no AI, no OCR: a scan without a text
// layer has nothing to read) and finds
//
//   - labels such as "Signature:", "Unterschrift", "Date", "Datum", "Name",
//     "Initials", "Ort" beside, under or above a line one writes on (drawn, or
//     typed as underscores or dots), or inside, above or beside an empty box;
//   - several labels sharing one line ("Ort, Datum", "Datum / Unterschrift"),
//     which split it;
//   - a label with a colon and nothing after it ("Datum:" followed by space);
//   - check boxes, drawn or typed (☐, ❏, [ ]), named by the text beside them.
//
// Every find has a confidence; overlapping finds keep the most certain one and
// form fields always win. Equal input gives equal output.

// signingRoles are the roles a person signing for themselves fills in.
var signingRoles = []string{"signature", "initials", "date", "name", "place"}

type roleMatch struct {
	role string
	name string // the part of the label it came from
}

var (
	partSeparator = regexp.MustCompile(`\s*(?:[,/;&+|]|\bund\b|\band\b)\s*`)
	roleRules     = []struct {
		role string
		re   *regexp.Regexp
	}{
		{"date", regexp.MustCompile(`\b(date|dated|datum)\b|datum\b`)},
		{"initials", regexp.MustCompile(`\b(initials?|initial here|paraphe|initialen|namensk[üu]rzel|k[üu]rzel)\b`)},
		{"signature", regexp.MustCompile(`\b(signature|signed|sign here|signatory|signatur|unterzeichnet|unterzeichner(in)?)\b|unterschrift`)},
		{"name", regexp.MustCompile(`\b(name|printed name|print name|full name|nachname|vorname|druckbuchstaben|klarname)\b`)},
		{"place", regexp.MustCompile(`\b(ort|place|city|stadt)\b`)},
	}
	// Labels that name a date or a name, but not the signer's of today.
	otherFacts = regexp.MustCompile(`geburt|birth|firm|company|business|unternehmen|organi[sz]ation|stra(ß|ss)e|street|e-?mail|telefon|phone|ablauf|expir|g[üu]ltig|valid|beginn|start|ende\b|end\b|due|f[äa]llig`)
	camelBreak = regexp.MustCompile(`([a-zäöü])([A-ZÄÖÜ])`)
)

// cleanLabel trims what surrounds a label: colons, stars, dashes, and
// brackets around the whole of it.
func cleanLabel(s string) string {
	s = strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(":*–—-_.…", r)
	})
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") && strings.Count(s, "(") == 1 {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// fieldNameWords turns a form field name like "SignatureDate_1" into words.
func fieldNameWords(name string) string {
	name = camelBreak.ReplaceAllString(name, "$1 $2")
	return strings.Map(func(r rune) rune {
		if r == '_' || r == '.' || unicode.IsDigit(r) {
			return ' '
		}
		return r
	}, name)
}

// labelRoles reads what a short label asks for, left to right. A part that
// names both a date and a signature ("Datum der Unterschrift") is a date.
func labelRoles(label string) []roleMatch {
	label = cleanLabel(label)
	if label == "" || len([]rune(label)) > 60 || len(strings.Fields(label)) > 8 {
		return nil
	}
	var out []roleMatch
	for _, part := range partSeparator.Split(label, -1) {
		part = cleanLabel(part)
		lower := strings.ToLower(part)
		if lower == "" || len(strings.Fields(lower)) > 5 {
			continue
		}
		role := ""
		for _, rule := range roleRules {
			if rule.re.MatchString(lower) {
				role = rule.role
				break
			}
		}
		if role == "" {
			continue
		}
		if otherFacts.MatchString(lower) && role != "signature" && role != "initials" {
			role = "text"
		}
		if n := len(out); n > 0 && out[n-1].role == role && role != "text" {
			continue
		}
		out = append(out, roleMatch{role: role, name: part})
	}
	return out
}

func fieldType(role string) string {
	switch role {
	case "signature", "initials", "date", "checkbox":
		return role
	default:
		return "text"
	}
}

// phrase is a run of words on one line, the unit labels are read in.
type phrase struct {
	text                            string
	x0, x1, top, bottom, base, size float64
}

func (p phrase) centerY() float64 { return (p.top + p.bottom) / 2 }

// phrases groups the characters into rows and the rows into phrases. Runs of
// underscores or dots end a phrase and are not part of any.
func phrases(chars []textChar) []phrase {
	sorted := slices.Clone(chars)
	slices.SortStableFunc(sorted, func(a, b textChar) int {
		if a.base != b.base {
			return cmpFloat(a.base, b.base)
		}
		return cmpFloat(a.x0, b.x0)
	})
	var rows [][]textChar
	for _, c := range sorted {
		if n := len(rows); n > 0 {
			ref := rows[n-1][0]
			if math.Abs(c.base-ref.base) <= 0.3*math.Max(1, math.Min(c.sz, ref.sz)) {
				rows[n-1] = append(rows[n-1], c)
				continue
			}
		}
		rows = append(rows, []textChar{c})
	}
	var out []phrase
	for _, row := range rows {
		slices.SortStableFunc(row, func(a, b textChar) int { return cmpFloat(a.x0, b.x0) })
		var current *phrase
		var text []rune
		var last textChar
		space := false
		flush := func() {
			if current != nil {
				current.text = strings.TrimSpace(string(text))
				if current.text != "" {
					out = append(out, *current)
				}
			}
			current, text, space = nil, nil, false
		}
		for i := 0; i < len(row); i++ {
			c := row[i]
			if c.r == '_' || c.r == '.' || c.r == '…' {
				j := i
				for j < len(row) && (row[j].r == c.r || c.r != '_' && (row[j].r == '.' || row[j].r == '…')) {
					j++
				}
				if c.r == '_' || j-i >= 4 {
					flush()
					i = j - 1
					continue
				}
			}
			if unicode.IsSpace(c.r) {
				space = current != nil
				continue
			}
			if current != nil && c.x0-last.x1 > math.Max(1.1*c.sz, 5) {
				flush()
			}
			if current == nil {
				current = &phrase{x0: c.x0, x1: c.x1, top: c.top(), bottom: c.bottom(), base: c.base, size: c.sz}
			} else {
				if space || c.x0-last.x1 > 0.18*c.sz {
					text = append(text, ' ')
				}
				current.x1 = math.Max(current.x1, c.x1)
				current.top, current.bottom = math.Min(current.top, c.top()), math.Max(current.bottom, c.bottom())
				current.size = math.Max(current.size, c.sz)
			}
			text = append(text, c.r)
			last, space = c, false
		}
		flush()
	}
	return out
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// target is something one writes in: a rule, an empty frame or a check box.
type target struct {
	kind  string // rule, frame, square
	r     rule
	b     box
	glyph bool // a check box typed as a character
}

type pairing struct {
	target, phrase int
	kind           string // left, below, above, inside, right
	score, dist    float64
	roles          []roleMatch
}

type pageDetector struct {
	page    int
	lay     *layout
	phrases []phrase
	targets []target
}

// collectTargets merges the rules, drops those with text sitting on them or
// no room above, and adds empty frames and check boxes.
func (pd *pageDetector) collectTargets() {
	rules := slices.Clone(pd.lay.rules)
	slices.SortStableFunc(rules, func(a, b rule) int {
		if math.Abs(a.y-b.y) > 1.5 {
			return cmpFloat(a.y, b.y)
		}
		return cmpFloat(a.x0, b.x0)
	})
	var merged []rule
	for _, r := range rules {
		if n := len(merged); n > 0 {
			m := &merged[n-1]
			if math.Abs(m.y-r.y) <= 1.5 && r.x0 <= m.x1+2 {
				m.x1 = math.Max(m.x1, r.x1)
				m.typed = m.typed && r.typed
				continue
			}
		}
		merged = append(merged, r)
	}
	for _, r := range merged {
		if r.x1-r.x0 < 18 || pd.ceiling(r.x0, r.x1, r.y) < 8 {
			continue
		}
		pd.targets = append(pd.targets, target{kind: "rule", r: r})
	}
	for _, f := range pd.lay.frames {
		pd.targets = append(pd.targets, target{kind: "frame", b: f})
	}
	squares := slices.Clone(pd.lay.squares)
	squares = append(squares, pd.typedSquares()...)
	for i, s := range squares {
		comb, duplicate := false, false
		for j, o := range squares {
			if i == j {
				continue
			}
			if j < i && s.overlap(o) > 0.5*math.Min(s.area(), o.area()) {
				duplicate = true
			}
			sameRow := math.Abs(s.y-o.y) < 2 && math.Abs(s.h-o.h) < 2
			if sameRow && (math.Abs(s.right()-o.x) < 1.5 || math.Abs(o.right()-s.x) < 1.5) {
				comb = true
			}
		}
		if !comb && !duplicate {
			pd.targets = append(pd.targets, target{kind: "square", b: s, glyph: i >= len(pd.lay.squares)})
		}
	}
}

// typedSquares are check boxes written as characters: ☐ and its kin, or "[ ]".
func (pd *pageDetector) typedSquares() []box {
	var out []box
	chars := pd.lay.chars
	for i, c := range chars {
		side := math.Min(c.x1-c.x0, 0.75*c.sz)
		switch {
		case strings.ContainsRune("☐□▢❏❐❑❒◻", c.r):
			side = math.Max(side, 0.6*c.sz)
			out = append(out, box{c.x0, c.base - 0.72*c.sz, side, side})
		case c.r == '[':
			for j := i + 1; j < len(chars) && j < i+4; j++ {
				n := chars[j]
				if math.Abs(n.base-c.base) > 0.3*c.sz || n.r != ' ' && n.r != ']' {
					break
				}
				if n.r == ']' && n.x1-c.x0 >= 0.4*c.sz && n.x1-c.x0 <= 1.4*c.sz {
					out = append(out, box{c.x0, c.base - 0.75*c.sz, n.x1 - c.x0, 0.85 * c.sz})
					break
				}
			}
		}
	}
	return out
}

// ceiling is the free height above a rule before the nearest text.
func (pd *pageDetector) ceiling(x0, x1, y float64) float64 {
	gap := 60.0
	for _, c := range pd.lay.chars {
		if c.r == '_' || unicode.IsSpace(c.r) || c.x1 <= x0+1 || c.x0 >= x1-1 {
			continue
		}
		if c.base <= y+0.3 {
			gap = math.Min(gap, math.Max(0, y-c.bottom()))
		}
	}
	return gap
}

// pairings proposes the labels each target could have.
func (pd *pageDetector) pairings() []pairing {
	var out []pairing
	add := func(t, p int, kind string, score, dist float64, roles []roleMatch) {
		out = append(out, pairing{target: t, phrase: p, kind: kind, score: score, dist: dist, roles: roles})
	}
	for ti, t := range pd.targets {
		for pi, p := range pd.phrases {
			roles := labelRoles(p.text)
			switch t.kind {
			case "rule":
				r := t.r
				width := r.x1 - r.x0
				switch {
				case p.x1 <= r.x0+3 && r.x0-p.x1 <= 150 && math.Abs(p.base-r.y) <= math.Max(0.75*p.size, 5):
					dist := math.Max(0, r.x0-p.x1)
					if x := strings.TrimSpace(strings.ToLower(p.text)); x == "x" || x == "×" {
						add(ti, pi, "left", 0.75, dist, []roleMatch{{role: "signature"}})
					} else if len(roles) > 0 {
						add(ti, pi, "left", 0.9, dist, roles)
					} else if generic(p.text, !r.typed || strings.HasSuffix(strings.TrimSpace(p.text), ":")) {
						add(ti, pi, "left", 0.6, dist, []roleMatch{{role: "text", name: p.text}})
					}
				case len(roles) > 0 && p.top >= r.y-1.5 && p.top-r.y <= math.Max(1.6*p.size, 10) && span(p.x0, p.x1, r.x0, r.x1) >= 0.6*(p.x1-p.x0):
					add(ti, pi, "below", 0.85, p.top-r.y, roles)
				case len(roles) > 0 && p.bottom <= r.y && r.y-p.bottom <= 2.6*p.size+8 && p.x0 >= r.x0-8 && p.x0 <= r.x0+0.5*width:
					add(ti, pi, "above", 0.65, r.y-p.bottom, roles)
				}
			case "frame":
				b := t.b
				switch {
				case p.x0 >= b.x-1 && p.x1 <= b.right()+1 && p.top >= b.y-1 && p.bottom <= b.bottom()+1:
					if len(roles) > 0 && p.top-b.y < 0.45*b.h && p.size <= 11 {
						add(ti, pi, "inside", 0.75, p.top-b.y, roles)
					}
				case p.x1 <= b.x+3 && b.x-p.x1 <= 150 && p.centerY() >= b.y && p.centerY() <= b.bottom():
					if len(roles) > 0 {
						add(ti, pi, "left", 0.8, b.x-p.x1, roles)
					} else if generic(p.text, true) {
						add(ti, pi, "left", 0.55, b.x-p.x1, []roleMatch{{role: "text", name: p.text}})
					}
				case len(roles) > 0 && p.bottom <= b.y+1 && b.y-p.bottom <= math.Max(1.2*p.size, 8) && span(p.x0, p.x1, b.x, b.right()) >= 0.5*(p.x1-p.x0):
					add(ti, pi, "above", 0.75, b.y-p.bottom, roles)
				case len(roles) > 0 && p.top >= b.bottom()-1 && p.top-b.bottom() <= math.Max(1.4*p.size, 9) && span(p.x0, p.x1, b.x, b.right()) >= 0.6*(p.x1-p.x0):
					add(ti, pi, "below", 0.7, p.top-b.bottom(), roles)
				}
			case "square":
				b := t.b
				if p.x0 >= b.right()-1 && p.x0-b.right() <= 20 && math.Abs(p.centerY()-(b.y+b.h/2)) <= math.Max(0.6*p.size, b.h/2+2) {
					add(ti, pi, "right", 0.8, p.x0-b.right(), []roleMatch{{role: "checkbox", name: p.text}})
				}
			}
		}
	}
	slices.SortStableFunc(out, func(a, b pairing) int {
		if a.score != b.score {
			return cmpFloat(b.score, a.score)
		}
		return cmpFloat(a.dist, b.dist)
	})
	return out
}

// generic accepts a short label for a plain text field: "Firma:", "IBAN".
func generic(text string, loose bool) bool {
	clean := cleanLabel(text)
	if clean == "" || len([]rune(clean)) > 40 || len(strings.Fields(clean)) > 4 || !strings.ContainsFunc(clean, unicode.IsLetter) {
		return false
	}
	return loose || strings.HasSuffix(strings.TrimSpace(text), ":")
}

func span(a0, a1, b0, b1 float64) float64 { return math.Max(0, math.Min(a1, b1)-math.Max(a0, b0)) }

// fields turns the best pairing of each target, and the labels left with
// free space after them, into fields.
func (pd *pageDetector) fields() []ImportedField {
	var out []ImportedField
	usedTarget := map[int]bool{}
	usedPhrase := map[int]bool{}
	// A frame with words in it that are not its own caption is not empty.
	inside := map[int]int{}
	for ti, t := range pd.targets {
		if t.kind != "frame" {
			continue
		}
		for _, p := range pd.phrases {
			if p.x0 >= t.b.x-1 && p.x1 <= t.b.right()+1 && p.top >= t.b.y-1 && p.bottom <= t.b.bottom()+1 {
				inside[ti]++
			}
		}
	}
	for _, pr := range pd.pairings() {
		t := pd.targets[pr.target]
		if usedTarget[pr.target] || usedPhrase[pr.phrase] {
			continue
		}
		if t.kind == "frame" && (inside[pr.target] > 1 || inside[pr.target] == 1 && pr.kind != "inside") {
			continue
		}
		usedTarget[pr.target], usedPhrase[pr.phrase] = true, true
		out = append(out, pd.place(t, pd.phrases[pr.phrase], pr)...)
	}
	for ti, t := range pd.targets {
		if !usedTarget[ti] && t.kind == "square" {
			out = append(out, pd.checkbox(t, "", 0.55))
		}
	}
	for pi, p := range pd.phrases {
		if !usedPhrase[pi] {
			out = append(out, pd.freeSpace(p)...)
		}
	}
	return out
}

func (pd *pageDetector) area(b box) Area {
	w, h := pd.lay.width, pd.lay.height
	x0, y0 := math.Max(0, b.x/w), math.Max(0, b.y/h)
	x1, y1 := math.Min(1, b.right()/w), math.Min(1, b.bottom()/h)
	return Area{Page: pd.page, X: round5(x0), Y: round5(y0), W: round5(x1 - x0), H: round5(y1 - y0)}
}

func (pd *pageDetector) checkbox(t target, label string, score float64) ImportedField {
	b := t.b
	if t.glyph {
		score += 0.05
	}
	return ImportedField{Type: "checkbox", Name: truncateRunes(cleanLabel(label), 100), Required: false, Prefs: map[string]any{},
		Areas: []Area{pd.area(b)}, Role: "checkbox", Confidence: round2(score), Source: "layout"}
}

// Sizes by role in points: preferred height, widest box.
var roleSizes = map[string]struct{ height, width, weight float64 }{
	"signature": {32, 260, 1.6},
	"initials":  {24, 80, 0.6},
	"date":      {0, 140, 0.9},
	"name":      {0, 260, 1.4},
	"place":     {0, 200, 1},
	"text":      {0, 1000, 1},
}

func roleHeight(role string, labelSize float64) float64 {
	if h := roleSizes[role].height; h > 0 {
		return h
	}
	return math.Min(22, math.Max(14, 1.6*labelSize))
}

// place makes the fields of one labelled target, splitting it between the
// roles a combined label names.
func (pd *pageDetector) place(t target, p phrase, pr pairing) []ImportedField {
	if t.kind == "square" {
		return []ImportedField{pd.checkbox(t, p.text, pr.score)}
	}
	var x0, x1, bottom, room float64
	switch t.kind {
	case "rule":
		x0, x1, bottom = t.r.x0, t.r.x1, t.r.y
		room = pd.ceiling(x0, x1, bottom) - 1.5
	case "frame":
		x0, x1, bottom = t.b.x+1.5, t.b.right()-1.5, t.b.bottom()-1.5
		room = t.b.h - 3
		if pr.kind == "inside" {
			room = t.b.bottom() - p.bottom - 2
		}
	}
	score := pr.score
	if pr.dist > 60 && pr.kind == "left" {
		score -= 0.1
	}
	if t.kind == "rule" && x1-x0 > 0.75*pd.lay.width {
		score -= 0.15
	}
	var weights float64
	for _, r := range pr.roles {
		weights += roleSizes[r.role].weight
	}
	const gap = 6.0
	free := x1 - x0 - gap*float64(len(pr.roles)-1)
	var out []ImportedField
	at := x0
	for _, r := range pr.roles {
		size := roleSizes[r.role]
		width := free * size.weight / weights
		segment := width
		if t.kind == "rule" {
			width = math.Min(width, size.width)
		}
		height := roleHeight(r.role, p.size)
		if t.kind == "frame" {
			height = room
		} else {
			height = math.Max(8, math.Min(height, room))
		}
		confidence := score
		if (r.role == "signature" || r.role == "initials") && bottom > 0.5*pd.lay.height {
			confidence += 0.04
		}
		name := r.name
		if len(pr.roles) == 1 && name != "" {
			name = p.text
		}
		out = append(out, ImportedField{
			Type: fieldType(r.role), Name: truncateRunes(cleanLabel(name), 100), Required: true, Prefs: map[string]any{},
			Areas: []Area{pd.area(box{at, bottom - height, width, height})}, Role: r.role, Confidence: round2(math.Min(0.97, confidence)), Source: "layout",
		})
		at += segment + gap
	}
	return out
}

// freeSpace places a field after a label like "Datum:" with nothing written
// after it on its line.
func (pd *pageDetector) freeSpace(p phrase) []ImportedField {
	if !strings.HasSuffix(strings.TrimSpace(p.text), ":") {
		return nil
	}
	roles := labelRoles(p.text)
	if len(roles) != 1 || !slices.Contains(signingRoles, roles[0].role) {
		return nil
	}
	next := pd.lay.width - 30
	for _, c := range pd.lay.chars {
		if !unicode.IsSpace(c.r) && c.x0 > p.x1+1 && math.Abs(c.base-p.base) < 0.6*p.size {
			next = math.Min(next, c.x0)
		}
	}
	role := roles[0].role
	free := next - p.x1 - 12
	if free < 60 {
		return nil
	}
	width := math.Min(free, roleSizes[role].width)
	x0, bottom := p.x1+6, p.base+0.25*p.size
	height := math.Min(roleHeight(role, p.size), pd.ceiling(x0, x0+width, bottom)-1.5)
	if height < 10 {
		return nil
	}
	return []ImportedField{{
		Type: fieldType(role), Name: truncateRunes(cleanLabel(p.text), 100), Required: true, Prefs: map[string]any{},
		Areas: []Area{pd.area(box{x0, bottom - height, width, height})}, Role: role, Confidence: 0.55, Source: "layout",
	}}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// formRole reads a form text field's role from the label beside, above or
// below its widget when its name says nothing.
func (pd *pageDetector) formRole(a Area) string {
	b := box{a.X * pd.lay.width, a.Y * pd.lay.height, a.W * pd.lay.width, a.H * pd.lay.height}
	best, bestDist := "", math.Inf(1)
	for _, p := range pd.phrases {
		roles := labelRoles(p.text)
		if len(roles) != 1 {
			continue
		}
		var dist float64
		switch {
		case p.x1 <= b.x+3 && b.x-p.x1 <= 120 && p.centerY() >= b.y-2 && p.centerY() <= b.bottom()+2:
			dist = b.x - p.x1
		case p.bottom <= b.y+1 && b.y-p.bottom <= math.Max(1.4*p.size, 10) && span(p.x0, p.x1, b.x, b.right()) > 0:
			dist = b.y - p.bottom + 5
		case p.top >= b.bottom()-1 && p.top-b.bottom() <= math.Max(1.6*p.size, 10) && span(p.x0, p.x1, b.x, b.right()) > 0:
			dist = p.top - b.bottom() + 5
		default:
			continue
		}
		if dist < bestDist {
			best, bestDist = roles[0].role, dist
		}
	}
	return best
}

// DetectFields finds the fields of a PDF: its form fields, and what its pages
// show as places to sign, date, name or tick. Found fields that overlap keep
// the more certain one; the result is in reading order.
func DetectFields(ctx context.Context, data []byte) ([]ImportedField, error) {
	doc, err := load(ctx, data, MaxPDFBytes)
	if err != nil {
		return nil, err
	}
	fonts := map[int]*fontInfo{}
	detectors := map[int]*pageDetector{}
	detector := func(index int) *pageDetector {
		if pd, ok := detectors[index]; ok {
			return pd
		}
		lay := doc.layout(index, fonts)
		pd := &pageDetector{page: index, lay: lay, phrases: phrases(lay.chars)}
		detectors[index] = pd
		return pd
	}
	found := doc.importForm()
	for i := range found {
		f := &found[i]
		if f.Type == "text" && f.Role == "" {
			f.Role = detector(f.Areas[0].Page).formRole(f.Areas[0])
		}
		f.retype()
	}
	for index := range doc.pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pd := detector(index)
		pd.collectTargets()
		found = append(found, pd.fields()...)
	}
	out := pickFields(found)
	sortReading(out)
	return out, nil
}

// pickFields keeps, of fields that overlap, the most certain one.
func pickFields(found []ImportedField) []ImportedField {
	slices.SortStableFunc(found, func(a, b ImportedField) int { return cmpFloat(b.Confidence, a.Confidence) })
	var out []ImportedField
	for _, f := range found {
		if !Overlaps(f.Areas, out) {
			out = append(out, f)
		}
	}
	return out
}

// Overlaps tells whether any of areas covers a good part of a field's area.
func Overlaps(areas []Area, fields []ImportedField) bool {
	for _, other := range fields {
		if AreasOverlap(areas, other.Areas) {
			return true
		}
	}
	return false
}

// AreasOverlap: two boxes on the same page share at least 40% of the smaller.
func AreasOverlap(a, b []Area) bool {
	for _, x := range a {
		for _, y := range b {
			if x.Page != y.Page {
				continue
			}
			bx, by := box{x.X, x.Y, x.W, x.H}, box{y.X, y.Y, y.W, y.H}
			if smaller := math.Min(bx.area(), by.area()); smaller > 0 && bx.overlap(by) > 0.4*smaller {
				return true
			}
		}
	}
	return false
}
