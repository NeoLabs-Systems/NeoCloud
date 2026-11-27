package signpdf

import (
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// The layout of a page as field detection sees it: the characters, the
// horizontal rules and the small and large boxes drawn on it, all in the
// displayed page (points, origin top-left, y down) whatever its /Rotate and
// whatever matrices the content used. Text that does not read upright on the
// displayed page is left out: no field is placed along it.

const (
	maxLayoutOps     = 400_000  // operators per page
	maxLayoutContent = 16 << 20 // decoded content bytes per page
	maxFormDepth     = 6
)

type textChar struct {
	r                rune
	x0, x1, base, sz float64
}

func (c textChar) top() float64    { return c.base - 0.78*c.sz }
func (c textChar) bottom() float64 { return c.base + 0.22*c.sz }

// box is a rectangle of the displayed page.
type box struct{ x, y, w, h float64 }

func (b box) right() float64  { return b.x + b.w }
func (b box) bottom() float64 { return b.y + b.h }
func (b box) area() float64   { return math.Max(0, b.w) * math.Max(0, b.h) }

func (b box) overlap(o box) float64 {
	w := math.Min(b.right(), o.right()) - math.Max(b.x, o.x)
	h := math.Min(b.bottom(), o.bottom()) - math.Max(b.y, o.y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// rule is a horizontal line one could write on: drawn, or typed as a run of
// underscores or dots.
type rule struct {
	x0, x1, y float64
	typed     bool
}

type layout struct {
	width, height float64
	chars         []textChar
	rules         []rule
	squares       []box // stroked boxes the size of a check box
	frames        []box // stroked boxes the size of a text field
}

type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

// times is m applied first, then n.
func (m matrix) times(n matrix) matrix {
	return matrix{
		m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

func (m matrix) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

type graphics struct {
	ctm                        matrix
	lineWidth                  float64
	font                       *fontInfo
	size, tc, tw, th, tl, rise float64
}

type scanner struct {
	doc     *document
	page    *page
	out     *layout
	fonts   map[int]*fontInfo
	ops     int
	content int

	state graphics
	stack []graphics
	tm    matrix
	tlm   matrix

	path   [][][2]float64 // subpaths of user space points
	rects  [][4][2]float64
	curved bool
}

// layout reads the content of one page.
func (d *document) layout(index int, fonts map[int]*fontInfo) *layout {
	p := d.pages[index]
	w, h := p.size()
	s := &scanner{doc: d, page: p, out: &layout{width: w, height: h}, fonts: fonts}
	s.state = graphics{ctm: identity, lineWidth: 1, th: 1}
	var streams []types.Object
	switch v := d.resolve(p.dict["Contents"]).(type) {
	case types.StreamDict:
		streams = []types.Object{p.dict["Contents"]}
	case types.Array:
		streams = v
	}
	var content []byte
	for _, ref := range streams {
		if stream, ok := d.resolve(ref).(types.StreamDict); ok {
			if data, ok := d.decoded(stream); ok && len(content)+len(data) <= maxLayoutContent {
				content = append(append(content, data...), '\n')
			}
		}
	}
	s.run(content, d.dict(p.dict["Resources"]), 0)
	s.out.rules = append(s.out.rules, typedRules(s.out.chars)...)
	return s.out
}

// decoded is a stream's content after its filters, within the load limits.
func (d *document) decoded(stream types.StreamDict) ([]byte, bool) {
	copied := stream
	if err := copied.DecodeWithLimit(maxDecodedStream); err != nil {
		return nil, false
	}
	return copied.Content, true
}

func (s *scanner) font(ref types.Object) *fontInfo {
	if r, ok := ref.(types.IndirectRef); ok {
		n := r.ObjectNumber.Value()
		if f, ok := s.fonts[n]; ok {
			return f
		}
		f := s.doc.fontInfo(ref)
		s.fonts[n] = f
		return f
	}
	return s.doc.fontInfo(ref)
}

func (s *scanner) run(content []byte, resources types.Dict, depth int) {
	s.content += len(content)
	if s.content > maxLayoutContent {
		return
	}
	l := &lexer{data: content}
	var operands []any
	for s.ops < maxLayoutOps {
		value, ok := l.next()
		if !ok {
			return
		}
		op, isOp := value.(pdfOp)
		if !isOp {
			if len(operands) < 64 {
				operands = append(operands, value)
			}
			continue
		}
		s.ops++
		s.operate(string(op), operands, resources, depth)
		operands = operands[:0]
	}
}

func numbers(operands []any, n int) ([]float64, bool) {
	if len(operands) < n {
		return nil, false
	}
	out := make([]float64, n)
	for i, v := range operands[len(operands)-n:] {
		f, ok := v.(float64)
		if !ok {
			return nil, false
		}
		out[i] = f
	}
	return out, true
}

func (s *scanner) operate(op string, operands []any, resources types.Dict, depth int) {
	g := &s.state
	switch op {
	case "q":
		if len(s.stack) < 256 {
			s.stack = append(s.stack, s.state)
		}
	case "Q":
		if n := len(s.stack); n > 0 {
			s.state, s.stack = s.stack[n-1], s.stack[:n-1]
		}
	case "cm":
		if v, ok := numbers(operands, 6); ok {
			g.ctm = matrix{v[0], v[1], v[2], v[3], v[4], v[5]}.times(g.ctm)
		}
	case "w":
		if v, ok := numbers(operands, 1); ok {
			g.lineWidth = v[0]
		}
	case "BT":
		s.tm, s.tlm = identity, identity
	case "Tf":
		if len(operands) >= 2 {
			if name, ok := operands[len(operands)-2].(pdfName); ok {
				g.font = s.font(s.doc.dict(resources["Font"])[string(name)])
			}
			if v, ok := operands[len(operands)-1].(float64); ok {
				g.size = v
			}
		}
	case "Tc", "Tw", "Tz", "TL", "Ts":
		if v, ok := numbers(operands, 1); ok {
			switch op {
			case "Tc":
				g.tc = v[0]
			case "Tw":
				g.tw = v[0]
			case "Tz":
				g.th = v[0] / 100
			case "TL":
				g.tl = v[0]
			case "Ts":
				g.rise = v[0]
			}
		}
	case "Td", "TD":
		if v, ok := numbers(operands, 2); ok {
			if op == "TD" {
				g.tl = -v[1]
			}
			s.tlm = matrix{1, 0, 0, 1, v[0], v[1]}.times(s.tlm)
			s.tm = s.tlm
		}
	case "Tm":
		if v, ok := numbers(operands, 6); ok {
			s.tlm = matrix{v[0], v[1], v[2], v[3], v[4], v[5]}
			s.tm = s.tlm
		}
	case "T*":
		s.newLine()
	case "Tj", "'", "\"":
		if op != "Tj" {
			if op == "\"" {
				if v, ok := numbers(operands[:max(0, len(operands)-1)], 2); ok {
					g.tw, g.tc = v[0], v[1]
				}
			}
			s.newLine()
		}
		if len(operands) > 0 {
			if text, ok := operands[len(operands)-1].(pdfString); ok {
				s.show(text)
			}
		}
	case "TJ":
		if len(operands) > 0 {
			items, _ := operands[len(operands)-1].([]any)
			for _, item := range items {
				switch v := item.(type) {
				case pdfString:
					s.show(v)
				case float64:
					s.advance(-v / 1000 * g.size * g.th)
				}
			}
		}
	case "m":
		if v, ok := numbers(operands, 2); ok {
			x, y := g.ctm.apply(v[0], v[1])
			s.path = append(s.path, [][2]float64{{x, y}})
		}
	case "l":
		if v, ok := numbers(operands, 2); ok && len(s.path) > 0 {
			x, y := g.ctm.apply(v[0], v[1])
			last := &s.path[len(s.path)-1]
			*last = append(*last, [2]float64{x, y})
		}
	case "c", "v", "y":
		s.curved = true
		if v, ok := numbers(operands, 2); ok && len(s.path) > 0 {
			x, y := g.ctm.apply(v[0], v[1])
			last := &s.path[len(s.path)-1]
			*last = append(*last, [2]float64{x, y})
		}
	case "h":
		if n := len(s.path); n > 0 && len(s.path[n-1]) > 1 {
			s.path[n-1] = append(s.path[n-1], s.path[n-1][0])
		}
	case "re":
		if v, ok := numbers(operands, 4); ok && len(s.rects) < 10000 {
			var corners [4][2]float64
			for i, c := range [4][2]float64{{v[0], v[1]}, {v[0] + v[2], v[1]}, {v[0] + v[2], v[1] + v[3]}, {v[0], v[1] + v[3]}} {
				corners[i][0], corners[i][1] = g.ctm.apply(c[0], c[1])
			}
			s.rects = append(s.rects, corners)
		}
	case "S", "s", "f", "F", "f*", "B", "B*", "b", "b*":
		stroke := op == "S" || op == "s" || op == "B" || op == "B*" || op == "b" || op == "b*"
		s.paint(stroke)
		s.clearPath()
	case "n":
		s.clearPath()
	case "Do":
		if len(operands) == 0 || depth >= maxFormDepth {
			return
		}
		name, _ := operands[len(operands)-1].(pdfName)
		ref := s.doc.dict(resources["XObject"])[string(name)]
		stream, ok := s.doc.resolve(ref).(types.StreamDict)
		if !ok || s.doc.name(stream.Dict["Subtype"]) != "Form" {
			return
		}
		data, ok := s.doc.decoded(stream)
		if !ok {
			return
		}
		saved, savedStack := s.state, append([]graphics(nil), s.stack...)
		if values := s.doc.array(stream.Dict["Matrix"]); len(values) == 6 {
			var m matrix
			for i := range m {
				m[i] = s.doc.number(values[i])
			}
			s.state.ctm = m.times(s.state.ctm)
		}
		inner := s.doc.dict(stream.Dict["Resources"])
		if inner == nil {
			inner = resources
		}
		s.run(data, inner, depth+1)
		s.state, s.stack = saved, savedStack
	}
}

func (s *scanner) newLine() {
	s.tlm = matrix{1, 0, 0, 1, 0, -s.state.tl}.times(s.tlm)
	s.tm = s.tlm
}

func (s *scanner) advance(tx float64) {
	s.tm = matrix{1, 0, 0, 1, tx, 0}.times(s.tm)
}

// displayed maps a user space point to the displayed page.
func (s *scanner) displayed(x, y float64) (float64, float64) { return s.page.topLeft(x, y) }

func (s *scanner) show(text pdfString) {
	g := &s.state
	if g.font == nil || len(s.out.chars) > 200_000 {
		return
	}
	for _, gl := range g.font.glyphs(text) {
		m := s.tm.times(g.ctm)
		width := gl.width * g.size * g.th
		ox, oy := m.apply(0, g.rise)
		ex, ey := m.apply(width, g.rise)
		ux, uy := m.apply(0, g.rise+g.size)
		x0, y0 := s.displayed(ox, oy)
		x1, y1 := s.displayed(ex, ey)
		_, yu := s.displayed(ux, uy)
		size := y0 - yu
		upright := size > 0.5 && (x1-x0 > 0 || width == 0) && math.Abs(y1-y0) <= 0.1*math.Max(x1-x0, size)
		if upright {
			for _, r := range gl.text {
				if r < ' ' {
					continue
				}
				s.out.chars = append(s.out.chars, textChar{r: r, x0: x0, x1: x1, base: y0, sz: size})
			}
		}
		tx := (gl.width*g.size + g.tc) * g.th
		if !g.font.composite && gl.code == 32 {
			tx += g.tw * g.th
		}
		s.advance(tx)
	}
}

func (s *scanner) clearPath() {
	s.path, s.rects, s.curved = s.path[:0], s.rects[:0], false
}

func (s *scanner) scale() float64 {
	m := s.state.ctm
	return math.Sqrt(math.Abs(m[0]*m[3] - m[1]*m[2]))
}

// paint records what the current path adds to the layout.
func (s *scanner) paint(stroke bool) {
	if len(s.out.rules) > 20000 {
		return
	}
	for _, corners := range s.rects {
		s.shape(corners[:], stroke)
	}
	if s.curved {
		return
	}
	for _, sub := range s.path {
		if len(sub) == 5 || len(sub) == 4 {
			if s.shape(sub, stroke) {
				continue
			}
		}
		if !stroke {
			continue
		}
		for i := 1; i < len(sub); i++ {
			x0, y0 := s.displayed(sub[i-1][0], sub[i-1][1])
			x1, y1 := s.displayed(sub[i][0], sub[i][1])
			if math.Abs(y1-y0) <= 0.8 && math.Abs(x1-x0) >= 15 && s.state.lineWidth*s.scale() <= 3 {
				s.out.rules = append(s.out.rules, rule{x0: math.Min(x0, x1), x1: math.Max(x0, x1), y: (y0 + y1) / 2})
			}
		}
	}
}

// shape classifies an axis-aligned rectangle; false when the points are not one.
func (s *scanner) shape(points [][2]float64, stroke bool) bool {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	var xs, ys []float64
	for _, pt := range points {
		x, y := s.displayed(pt[0], pt[1])
		xs, ys = append(xs, x), append(ys, y)
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	for i := range xs {
		onX := math.Abs(xs[i]-minX) < 0.5 || math.Abs(xs[i]-maxX) < 0.5
		onY := math.Abs(ys[i]-minY) < 0.5 || math.Abs(ys[i]-maxY) < 0.5
		if !onX || !onY {
			return false
		}
	}
	b := box{minX, minY, maxX - minX, maxY - minY}
	switch {
	case b.h <= 2.5 && b.w >= 15:
		s.out.rules = append(s.out.rules, rule{x0: b.x, x1: b.right(), y: b.y + b.h/2})
	case !stroke:
	case b.w >= 5 && b.w <= 24 && b.h >= 5 && b.h <= 24 && b.w/b.h > 0.75 && b.w/b.h < 1.33:
		s.out.squares = append(s.out.squares, b)
	case b.h >= 12 && b.h <= 60 && b.w >= 40:
		s.out.frames = append(s.out.frames, b)
	}
	return true
}

// typedRules finds runs of underscores and of dots in the text.
func typedRules(chars []textChar) []rule {
	var out []rule
	for i := 0; i < len(chars); {
		r := chars[i].r
		if r != '_' && r != '.' && r != '…' {
			i++
			continue
		}
		j := i + 1
		for j < len(chars) && (chars[j].r == r || r == '.' && chars[j].r == '…') && math.Abs(chars[j].base-chars[i].base) < 0.5 && chars[j].x0-chars[j-1].x1 < 0.5*chars[i].sz {
			j++
		}
		count := j - i
		if r == '_' && count >= 3 || r != '_' && count >= 6 {
			lift := 0.1 * chars[i].sz
			if r != '_' {
				lift = 0
			}
			out = append(out, rule{x0: chars[i].x0, x1: chars[j-1].x1, y: chars[i].base + lift, typed: true})
		}
		i = j
	}
	return out
}
