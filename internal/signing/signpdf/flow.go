package signpdf

// flow is a small top-to-bottom layout engine for the certificate pages.
type flow struct {
	doc   *document
	fonts fonts
	pages []*flowPage
	y     float64
}

type flowPage struct {
	page   *page
	canvas *canvas
}

const (
	certMargin  = 48.0
	certBottom  = 56.0
	certContent = 595.28 - 2*certMargin
)

var (
	certInk    = colour{0.1, 0.1, 0.13}
	certMuted  = colour{0.42, 0.42, 0.47}
	certRule   = colour{0.8, 0.8, 0.83}
	certBand   = colour{0.955, 0.955, 0.965}
	certAccent = colour{0.62, 0.45, 0.1}
)

type paragraphStyle struct {
	size  float64
	bold  bool
	color *colour
	x     float64
}

func newFlow(doc *document, faces fonts) *flow {
	f := &flow{doc: doc, fonts: faces}
	f.newPage()
	return f
}

func (f *flow) current() *canvas { return f.pages[len(f.pages)-1].canvas }

func (f *flow) newPage() {
	f.pages = append(f.pages, &flowPage{f.doc.newPage(a4.short, a4.long), newCanvas()})
	f.y = a4.long - certMargin
}

// ensure starts a new page unless height still fits above the bottom margin.
func (f *flow) ensure(height float64) bool {
	if f.y-height < certBottom {
		f.newPage()
		return true
	}
	return false
}

func (f *flow) paragraph(raw string, style paragraphStyle) {
	if style.size == 0 {
		style.size = 9
	}
	if style.x == 0 {
		style.x = certMargin
	}
	fill := certInk
	if style.color != nil {
		fill = *style.color
	}
	face := f.fonts.pick(style.bold)
	height := style.size * lineHeight
	for _, line := range wrap(f.fonts.sanitize(raw), face, style.size, certContent-(style.x-certMargin)) {
		f.ensure(height)
		f.current().text(face, style.x, f.y-style.size, style.size, fill, line, 0)
		f.y -= height
	}
}

func (f *flow) heading(title string) {
	f.ensure(60)
	f.y -= 10
	f.paragraph(title, paragraphStyle{size: 12, bold: true})
	f.current().line(certMargin, f.y-1, certMargin+certContent, f.y-1, 0.8, certAccent)
	f.y -= 7
}

// keyValue draws "label  value" on one row; the value wraps in its column.
func (f *flow) keyValue(label, value string) {
	if value == "" {
		return
	}
	const labelWidth, size = 130.0, 8.5
	face := f.fonts.regular
	height := size * lineHeight
	lines := wrap(f.fonts.sanitize(value), face, size, certContent-labelWidth)
	f.ensure(height)
	f.current().text(face, certMargin, f.y-size, size, certMuted, f.fonts.sanitize(label), 0)
	for _, line := range lines {
		f.ensure(height)
		f.current().text(face, certMargin+labelWidth, f.y-size, size, certInk, line, 0)
		f.y -= height
	}
}

// table has a repeating header; cells wrap and a row never splits.
func (f *flow) table(titles []string, widths []float64, rows [][]string) {
	const size, padX, padY = 8.0, 3.0, 2.5
	height := size * lineHeight
	header := func() {
		f.ensure(height + 2*padY)
		f.current().fillRect(rect{certMargin, f.y - height - 2*padY, certContent, height + 2*padY}, certBand)
		x := certMargin
		for i, title := range titles {
			f.current().text(f.fonts.bold, x+padX, f.y-padY-size, size, certInk, f.fonts.sanitize(title), 0)
			x += widths[i]
		}
		f.y -= height + 2*padY
	}
	header()
	for _, row := range rows {
		cells := make([][]string, len(row))
		lines := 1
		for i, cell := range row {
			cells[i] = wrap(f.fonts.sanitize(cell), f.fonts.regular, size, widths[i]-2*padX)
			lines = max(lines, len(cells[i]))
		}
		rowHeight := float64(lines)*height + 2*padY
		if f.ensure(rowHeight) {
			header()
		}
		x := certMargin
		for i, cell := range cells {
			for n, line := range cell {
				f.current().text(f.fonts.regular, x+padX, f.y-padY-size-float64(n)*height, size, certInk, line, 0)
			}
			x += widths[i]
		}
		f.current().line(certMargin, f.y-rowHeight, certMargin+certContent, f.y-rowHeight, 0.4, certRule)
		f.y -= rowHeight
	}
}

// finish draws the footers and puts every page's marks onto it.
func (f *flow) finish(footer func(n, total int) (string, string)) {
	for i, page := range f.pages {
		left, right := footer(i+1, len(f.pages))
		face := f.fonts.regular
		right = f.fonts.sanitize(right)
		page.canvas.line(certMargin, 38, certMargin+certContent, 38, 0.4, certRule)
		page.canvas.text(face, certMargin, 26, 7.5, certMuted, f.fonts.sanitize(left), 0)
		page.canvas.text(face, certMargin+certContent-face.width(right, 7.5), 26, 7.5, certMuted, right, 0)
		f.doc.apply(page.page, page.canvas, true)
	}
}
