package signpdf

import (
	"context"
	"errors"
	"image"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/NeoLabs-Systems/neocloud/internal/ocr"
)

// Scanned pages. A page whose text layer is (nearly) empty is drawn as
// readers show it (/Rotate and crop box applied) and read by text
// recognition. Because the layout already lives in the displayed page, the
// picture's pixels map to it by scaling alone: x·width/pixels across,
// y·height/pixels down. The recognised words become characters like those of
// a text layer, and the drawing's lines and boxes are found in the pixels:
// rows of ink at least as long as a rule and no thicker than a pen stroke,
// joined across neighbouring pixel rows so that a page scanned slightly
// askew still gives one rule. Four strokes meeting as a rectangle are a box
// (a check box or a text field), as drawn boxes are in vector content.
// Underscores the recogniser reads become typed rules as usual.

// scanPage draws and reads one page; tests replace it.
var scanPage = ocr.PDFPage

// text is the page's text layer, to tell whether it needs recognition.
func (l *layout) text() string {
	runes := make([]rune, 0, len(l.chars))
	for _, c := range l.chars {
		runes = append(runes, c.r)
	}
	return string(runes)
}

// addScan adds what recognition found on the drawn page. It reports false,
// adding nothing, when the picture is not the displayed page.
func (l *layout) addScan(page *ocr.Page) bool {
	if page == nil || page.Width < 1 || page.Height < 1 || page.Image == nil {
		return false
	}
	sx, sy := l.width/float64(page.Width), l.height/float64(page.Height)
	// Equal scales or the picture is turned or cut differently from the page.
	if math.Abs(sx/sy-1) > 0.03 {
		return false
	}
	chars := scanChars(page.Words, sx, sy, l.chars)
	l.chars = append(l.chars, chars...)
	l.rules = append(l.rules, typedRules(chars)...)
	rules, squares, frames := scanShapes(page.Image, sx, page.Words)
	l.rules = append(l.rules, rules...)
	l.squares = append(l.squares, squares...)
	l.frames = append(l.frames, frames...)
	return true
}

// scanChars spreads each word's letters evenly over its box. All words of a
// line share the line's top and bottom, so they read as one row. Words
// without a letter or digit are specks and strokes, unless they are
// underscores or check box glyphs; words over the text layer are left out.
func scanChars(words []ocr.Word, sx, sy float64, layer []textChar) []textChar {
	var out []textChar
	previous := -1
	for _, w := range words {
		runes := []rune(w.Text)
		if !strings.ContainsFunc(w.Text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) &&
			strings.Trim(w.Text, "_") != "" && !strings.ContainsAny(w.Text, "☐□▢❏❐❑❒◻") {
			continue
		}
		top, bottom := float64(w.Line.Min.Y)*sy, float64(w.Line.Max.Y)*sy
		size := bottom - top
		x0, x1 := float64(w.Box.Min.X)*sx, float64(w.Box.Max.X)*sx
		if size <= 0 || x1 <= x0 || len(runes) == 0 || overText(layer, box{x0, top, x1 - x0, size}) {
			continue
		}
		base := top + 0.78*size
		if n := len(out); n > 0 && w.LineIndex == previous && out[n-1].x1 < x0 {
			out = append(out, textChar{r: ' ', x0: out[n-1].x1, x1: x0, base: base, sz: size})
		}
		step := (x1 - x0) / float64(len(runes))
		for i, r := range runes {
			out = append(out, textChar{r: r, x0: x0 + float64(i)*step, x1: x0 + float64(i+1)*step, base: base, sz: size})
		}
		previous = w.LineIndex
	}
	return out
}

func overText(layer []textChar, b box) bool {
	for _, c := range layer {
		if !unicode.IsSpace(c.r) && b.overlap(box{c.x0, c.top(), c.x1 - c.x0, c.bottom() - c.top()}) > 0 {
			return true
		}
	}
	return false
}

// stroke is a run of ink in pixels: its bounding box and how much of its
// length the runs actually cover.
type stroke struct {
	x0, y0, x1, y1 int // inclusive
	cover          int
}

const (
	scanTime       = 2 * time.Minute // recognition per detection, all pages
	maxScanRuns    = 300_000         // a page with more runs is too noisy to read
	maxScanStrokes = 4000            // per direction, for the box search
)

// scanShapes finds rules, check boxes and frames in the picture. scale is
// points per pixel. Strokes inside recognised words are their letters.
func scanShapes(picture *image.Gray, scale float64, words []ocr.Word) ([]rule, []box, []box) {
	ink, ok := inkMask(picture)
	if !ok {
		return nil, nil, nil
	}
	width, height := picture.Bounds().Dx(), picture.Bounds().Dy()
	px := func(points float64) int { return max(1, int(math.Round(points/scale))) }
	horizontal := outsideWords(strokes(ink, width, height, false, px(5), px(3)), words)
	vertical := outsideWords(strokes(ink, width, height, true, px(5), px(3)), words)
	used := make([]bool, len(horizontal))
	var squares, frames []box
	if len(horizontal) <= maxScanStrokes && len(vertical) <= maxScanStrokes {
		for _, found := range findBoxes(horizontal, vertical, px(1.5), px(4), used) {
			b := box{float64(found.Min.X) * scale, float64(found.Min.Y) * scale, float64(found.Dx()) * scale, float64(found.Dy()) * scale}
			switch {
			case b.w >= 5 && b.w <= 24 && b.h >= 5 && b.h <= 24 && b.w/b.h > 0.75 && b.w/b.h < 1.33:
				squares = append(squares, b)
			case b.h >= 12 && b.h <= 60 && b.w >= 40:
				frames = append(frames, b)
			}
		}
	}
	var rules []rule
	for i, s := range horizontal {
		if used[i] || float64(s.x1-s.x0+1)*scale < 15 {
			continue
		}
		rules = append(rules, rule{x0: float64(s.x0) * scale, x1: float64(s.x1+1) * scale, y: float64(s.y0+s.y1+1) / 2 * scale})
	}
	return rules, squares, frames
}

// outsideWords drops the strokes that lie within a word of two or more
// letters (a lone character may be a check box read as "O" or "D").
func outsideWords(list []stroke, words []ocr.Word) []stroke {
	out := list[:0]
	for _, s := range list {
		inside := false
		for _, w := range words {
			if len([]rune(w.Text)) > 1 && image.Rect(s.x0, s.y0, s.x1+1, s.y1+1).In(w.Box.Inset(-2)) {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, s)
		}
	}
	return out
}

// inkMask marks the dark pixels, split from the paper by Otsu's threshold
// (kept between 60 and 180 so a blank or grey page does not turn to ink).
// A page that is mostly dark is not a form on paper.
func inkMask(picture *image.Gray) ([]bool, bool) {
	var histogram [256]int
	bounds := picture.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for _, v := range picture.Pix[picture.PixOffset(bounds.Min.X, y) : picture.PixOffset(bounds.Min.X, y)+bounds.Dx()] {
			histogram[v]++
		}
	}
	total := bounds.Dx() * bounds.Dy()
	if total == 0 {
		return nil, false
	}
	var sum float64
	for v, n := range histogram {
		sum += float64(v * n)
	}
	threshold, best, below, belowSum := 128, -1.0, 0, 0.0
	for v, n := range histogram {
		below += n
		belowSum += float64(v * n)
		above := total - below
		if below == 0 || above == 0 {
			continue
		}
		mean0, mean1 := belowSum/float64(below), (sum-belowSum)/float64(above)
		if between := float64(below) * float64(above) * (mean0 - mean1) * (mean0 - mean1); between > best {
			threshold, best = v, between
		}
	}
	threshold = min(180, max(60, threshold))
	mask := make([]bool, total)
	dark := 0
	for y := range bounds.Dy() {
		row := picture.Pix[picture.PixOffset(bounds.Min.X, bounds.Min.Y+y):]
		for x := range bounds.Dx() {
			if int(row[x]) <= threshold {
				mask[y*bounds.Dx()+x] = true
				dark++
			}
		}
	}
	return mask, dark*2 < total
}

// strokes finds the straight strokes of ink in one direction (vertical reads
// columns as rows): runs of ink joined across adjacent rows when they
// overlap. A stroke must be at least minLength long, covered
// for 90% of it, and no thicker than thick plus the drift of a 2° skew.
func strokes(ink []bool, width, height int, vertical bool, minLength, thick int) []stroke {
	rows, length := height, width
	at := func(row, i int) bool { return ink[row*width+i] }
	if vertical {
		rows, length = width, height
		at = func(row, i int) bool { return ink[i*width+row] }
	}
	type run struct{ row, a, b int }
	var runs []run
	// Runs shorter than a stroke can be thick are letters' stems and are
	// left out; a gap of two pixels is scanner noise.
	minRun := max(3, 2*thick)
	starts := make([]int, rows+1)
	for row := range rows {
		starts[row] = len(runs)
		for i := 0; i < length; {
			if !at(row, i) {
				i++
				continue
			}
			a, b := i, i
			for j := i + 1; j < length && j <= b+3; j++ {
				if at(row, j) {
					b = j
				}
			}
			if b-a+1 >= minRun {
				runs = append(runs, run{row, a, b})
				if len(runs) > maxScanRuns {
					return nil
				}
			}
			i = b + 1
		}
	}
	starts[rows] = len(runs)
	parent := make([]int, len(runs))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	// Runs of a row are in order and apart, so one pass over both rows
	// finds every overlap.
	for row := 1; row < rows; row++ {
		j := starts[row-1]
		for k := starts[row]; k < starts[row+1]; k++ {
			for j < starts[row] && runs[j].b < runs[k].a {
				j++
			}
			for m := j; m < starts[row] && runs[m].a <= runs[k].b; m++ {
				parent[find(k)] = find(m)
			}
		}
	}
	groups := map[int][]run{}
	for i, r := range runs {
		root := find(i)
		groups[root] = append(groups[root], r)
	}
	var out []stroke
	for _, group := range groups {
		slices.SortFunc(group, func(p, q run) int { return p.a - q.a })
		a, b, top, bottom, cover, reach := group[0].a, group[0].b, group[0].row, group[0].row, 0, -1
		for _, r := range group {
			a, b = min(a, r.a), max(b, r.b)
			top, bottom = min(top, r.row), max(bottom, r.row)
			if r.b > reach {
				cover += r.b - max(r.a, reach+1) + 1
				reach = r.b
			}
		}
		span := b - a + 1
		if span < minLength || cover*10 < span*9 || bottom-top+1 > thick+span*35/1000 {
			continue
		}
		s := stroke{x0: a, x1: b, y0: top, y1: bottom, cover: cover}
		if vertical {
			s = stroke{x0: top, x1: bottom, y0: a, y1: b, cover: cover}
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(p, q stroke) int {
		if p.y0 != q.y0 {
			return p.y0 - q.y0
		}
		return p.x0 - q.x0
	})
	return out
}

// findBoxes pairs each horizontal stroke with the nearest one below that
// spans the same width, when vertical strokes close both sides. Edges of
// boxes are marked used: they are not rules.
func findBoxes(horizontal, vertical []stroke, tolerance, minHeight int, used []bool) []image.Rectangle {
	near := func(a, b int) bool { return a-b <= tolerance && b-a <= tolerance }
	side := func(x, top, bottom int) bool {
		for _, v := range vertical {
			if v.x0 <= x+tolerance && v.x1 >= x-tolerance && v.y0 <= top+tolerance && v.y1 >= bottom-tolerance {
				return true
			}
		}
		return false
	}
	var out []image.Rectangle
	for i, top := range horizontal {
		for j := i + 1; j < len(horizontal); j++ {
			bottom := horizontal[j]
			if bottom.y0-top.y1 < minHeight || !near(top.x0, bottom.x0) || !near(top.x1, bottom.x1) {
				continue
			}
			if side(top.x0, top.y0, bottom.y1) && side(top.x1, top.y0, bottom.y1) {
				used[i], used[j] = true, true
				out = append(out, image.Rect(min(top.x0, bottom.x0), top.y0, max(top.x1, bottom.x1)+1, bottom.y1+1))
				break
			}
		}
	}
	return out
}

// scanBudget reads the pages without a text layer while the page limit and
// the time allow; recognition being unavailable ends it quietly.
type scanBudget struct {
	left     int
	deadline time.Time
	stopped  bool
}

func newScanBudget() *scanBudget {
	return &scanBudget{left: ocr.MaxPDFPages, deadline: time.Now().Add(scanTime)}
}

// read recognises the page when it needs it, and reports whether it did.
func (s *scanBudget) read(ctx context.Context, data []byte, index int, lay *layout) (bool, error) {
	if s.stopped || s.left <= 0 || time.Now().After(s.deadline) || !ocr.NeedsRecognition(lay.text()) {
		return false, nil
	}
	s.left--
	page, err := scanPage(ctx, data, index)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		s.stopped = errors.Is(err, ocr.ErrUnavailable)
		return false, nil
	}
	return lay.addScan(page), nil
}
