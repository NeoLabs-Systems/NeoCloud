package signpdf

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Simple Markdown to an A4 PDF: headings, paragraphs, **bold**, *italic*,
// lists and rules. There is no italic font, so italic text is slanted.

const (
	mdMargin = 64.0
	mdBody   = 11.0
	mdLine   = 1.45
)

// MaxMarkdownBytes bounds the text of a rendered document.
const MaxMarkdownBytes = 200 * 1024

var (
	mdInk      = colour{0.11, 0.13, 0.09}
	mdMuted    = colour{0.45, 0.47, 0.42}
	mdRuleInk  = colour{0.75, 0.75, 0.72}
	mdHeadings = [6]float64{22, 18, 15, 13, 12, 11}
	italicSkew = math.Tan(12 * math.Pi / 180)

	mdInline    = regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__|\*([^\s*][^*]*?)\*|_([^\s_][^_]*?)_`)
	mdRuleLine  = regexp.MustCompile(`^(?:(?:-\s*){3,}|(?:\*\s*){3,}|(?:_\s*){3,})$`)
	mdHeading   = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	mdClosing   = regexp.MustCompile(`\s+#+$`)
	mdBullet    = regexp.MustCompile(`^[-*+]\s+(.*)$`)
	mdNumbered  = regexp.MustCompile(`^(\d{1,4})[.)]\s+(.*)$`)
	signLabels  = map[string][4]string{"en": {"Signatures", "Signature", "Name", "Date"}, "de": {"Unterschriften", "Unterschrift", "Name", "Datum"}}
	errNoSigner = errors.New("no signers")
)

type mdRun struct {
	text         string
	bold, italic bool
}

func inlineRuns(text string) []mdRun {
	var runs []mdRun
	last := 0
	for _, m := range mdInline.FindAllStringSubmatchIndex(text, -1) {
		if m[0] > last {
			runs = append(runs, mdRun{text: text[last:m[0]]})
		}
		for group := 1; group <= 4; group++ {
			if m[2*group] >= 0 {
				runs = append(runs, mdRun{text: text[m[2*group]:m[2*group+1]], bold: group <= 2, italic: group > 2})
				break
			}
		}
		last = m[1]
	}
	if last < len(text) {
		runs = append(runs, mdRun{text: text[last:]})
	}
	for i := range runs {
		runs[i].text = strings.ReplaceAll(runs[i].text, "`", "")
	}
	return runs
}

type mdBlock struct {
	kind, text, marker string
	level, indent      int
}

func parseMarkdown(markdown string) []mdBlock {
	var blocks []mdBlock
	var paragraph []string
	flush := func() {
		if len(paragraph) > 0 {
			blocks = append(blocks, mdBlock{kind: "paragraph", text: strings.Join(paragraph, " ")})
			paragraph = nil
		}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(strings.ReplaceAll(markdown, "\r\n", "\n"), "\r", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		indent := 0
		if len(raw)-len(strings.TrimLeftFunc(raw, unicode.IsSpace)) >= 2 {
			indent = 1
		}
		switch {
		case line == "":
			flush()
		case mdRuleLine.MatchString(line):
			flush()
			blocks = append(blocks, mdBlock{kind: "rule"})
		case mdHeading.MatchString(line):
			flush()
			m := mdHeading.FindStringSubmatch(line)
			blocks = append(blocks, mdBlock{kind: "heading", level: len(m[1]), text: mdClosing.ReplaceAllString(m[2], "")})
		case mdBullet.MatchString(line):
			flush()
			blocks = append(blocks, mdBlock{kind: "item", marker: "•", text: mdBullet.FindStringSubmatch(line)[1], indent: indent})
		case mdNumbered.MatchString(line):
			flush()
			m := mdNumbered.FindStringSubmatch(line)
			blocks = append(blocks, mdBlock{kind: "item", marker: m[1] + ".", text: m[2], indent: indent})
		default:
			paragraph = append(paragraph, line)
		}
	}
	flush()
	return blocks
}

type mdWriter struct {
	doc    *document
	fonts  fonts
	pages  []*page
	canvas []*canvas
	y      float64
}

func (w *mdWriter) newPage() {
	w.pages = append(w.pages, w.doc.newPage(a4.short, a4.long))
	w.canvas = append(w.canvas, newCanvas())
	w.y = a4.long - mdMargin
}

func (w *mdWriter) ensure(height float64) {
	if len(w.pages) == 0 || w.y-height < mdMargin {
		w.newPage()
	}
}

func (w *mdWriter) current() *canvas { return w.canvas[len(w.canvas)-1] }

func (w *mdWriter) clean(text string) string {
	return strings.ReplaceAll(w.fonts.sanitize(text), "\n", " ")
}

// paragraph wraps and draws styled text from x to the right margin.
func (w *mdWriter) paragraph(runs []mdRun, size float64, bold bool, x float64) {
	type word struct {
		mdRun
		width float64
	}
	maxWidth := a4.short - mdMargin - x
	height := size * mdLine
	space := w.fonts.regular.width(" ", size)
	var line []word
	width := 0.0
	draw := func() {
		w.ensure(height)
		cursor := x
		for _, item := range line {
			skew := 0.0
			if item.italic {
				skew = italicSkew
			}
			w.current().text(w.fonts.pick(item.bold), cursor, w.y-size, size, mdInk, item.text, skew)
			cursor += item.width + space
		}
		w.y -= height
		line, width = line[:0], 0
	}
	for _, run := range runs {
		for _, text := range strings.Fields(w.clean(run.text)) {
			item := word{mdRun: mdRun{text: text, bold: bold || run.bold, italic: run.italic}}
			item.width = w.fonts.pick(item.bold).width(text, size)
			if len(line) > 0 && width+space+item.width > maxWidth {
				draw()
			}
			if len(line) > 0 {
				width += space
			}
			line = append(line, item)
			width += item.width
		}
	}
	if len(line) > 0 {
		draw()
	}
}

func (w *mdWriter) block(b mdBlock) {
	switch b.kind {
	case "rule":
		w.ensure(18)
		w.y -= 8
		w.current().line(mdMargin, w.y, a4.short-mdMargin, w.y, 0.8, mdRuleInk)
		w.y -= 10
	case "heading":
		size := mdHeadings[b.level-1]
		w.ensure(size * mdLine * 2)
		w.y -= size * 0.5
		w.paragraph(inlineRuns(b.text), size, true, mdMargin)
		w.y -= size * 0.25
	case "item":
		x := mdMargin + 14 + float64(b.indent)*16
		w.ensure(mdBody * mdLine)
		w.current().text(w.fonts.regular, x-14, w.y-mdBody, mdBody, mdInk, w.clean(b.marker), 0)
		if len(b.marker) > 2 && b.marker != "•" {
			x += 6
		}
		w.paragraph(inlineRuns(b.text), mdBody, false, x)
		w.y -= 2
	default:
		w.paragraph(inlineRuns(b.text), mdBody, false, mdMargin)
		w.y -= mdBody * 0.6
	}
}

// SignatureBoxes are the field areas drawn for one signer.
type SignatureBoxes struct{ Signature, Name, Date Area }

// signatureBlock draws a signature, printed name and date line per signer.
func (w *mdWriter) signatureBlock(signers []string, locale string) []SignatureBoxes {
	labels := signLabels[language(locale)]
	const block = 118.0
	w.ensure(30 + block)
	w.y -= 12
	w.paragraph([]mdRun{{text: labels[0], bold: true}}, 13, false, mdMargin)
	w.y -= 6
	area := func(x, top, width, height float64) Area {
		return Area{Page: len(w.doc.pages) - 1, X: x / a4.short, Y: (a4.long - top) / a4.long, W: width / a4.short, H: height / a4.long}
	}
	caption := func(text string, x, y float64) {
		w.current().text(w.fonts.regular, x, y, 8.5, mdMuted, w.clean(text), 0)
	}
	out := make([]SignatureBoxes, 0, len(signers))
	for _, signer := range signers {
		w.ensure(block)
		top := w.y
		sigTop := top - 6
		w.current().line(mdMargin, sigTop-52, mdMargin+240, sigTop-52, 0.8, mdInk)
		caption(labels[1]+" · "+signer, mdMargin, sigTop-63)
		rowTop := sigTop - 74
		dateX := mdMargin + 260
		w.current().line(mdMargin, rowTop-22, mdMargin+230, rowTop-22, 0.6, mdRuleInk)
		w.current().line(dateX, rowTop-22, dateX+140, rowTop-22, 0.6, mdRuleInk)
		caption(labels[2], mdMargin, rowTop-32)
		caption(labels[3], dateX, rowTop-32)
		w.y = top - block
		out = append(out, SignatureBoxes{
			Signature: area(mdMargin, sigTop, 240, 48), Name: area(mdMargin, rowTop, 230, 20), Date: area(dateX, rowTop, 140, 20),
		})
	}
	return out
}

func (w *mdWriter) finish(title string) ([]byte, error) {
	for i, p := range w.pages {
		w.doc.apply(p, w.canvas[i], true)
	}
	if err := w.fonts.finish(w.doc); err != nil {
		return nil, err
	}
	info := producerInfo()
	if source := w.doc.ctx.XRefTable.Info; source != nil {
		for key, value := range w.doc.dict(*source) {
			if info[key] == nil {
				info[key] = value
			}
		}
	}
	if title != "" {
		info["Title"] = textString(title)
	}
	out, _, err := w.doc.serialize(info, nil)
	return out, err
}

// RenderMarkdown renders markdown with title as a heading above it. With
// signers a signature block is drawn at the end and its boxes returned.
func RenderMarkdown(title, markdown string, signers []string, locale string) ([]byte, []SignatureBoxes, error) {
	if len(markdown) > MaxMarkdownBytes {
		return nil, nil, refuse(CodeTooLarge, nil)
	}
	doc := emptyDocument()
	w := &mdWriter{doc: doc, fonts: newFonts(doc)}
	w.newPage()
	if title != "" {
		w.block(mdBlock{kind: "heading", level: 1, text: title})
	}
	for _, block := range parseMarkdown(markdown) {
		// A page count past what can be signed is refused while it grows.
		if len(w.pages) > MaxPages {
			return nil, nil, refuse(CodeTooManyPages, errors.New(strconv.Itoa(len(w.pages))))
		}
		w.block(block)
	}
	var boxes []SignatureBoxes
	if signers != nil {
		boxes = w.signatureBlock(signers, locale)
	}
	out, err := w.finish(w.fonts.sanitize(title))
	return out, boxes, err
}

// AppendSignaturePage adds a page with a signature block to an existing PDF.
func AppendSignaturePage(ctx context.Context, data []byte, signers []string, locale string) ([]byte, []SignatureBoxes, error) {
	if len(signers) == 0 {
		return nil, nil, errNoSigner
	}
	doc, err := load(ctx, data, MaxPDFBytes)
	if err != nil {
		return nil, nil, err
	}
	w := &mdWriter{doc: doc, fonts: newFonts(doc)}
	w.newPage()
	boxes := w.signatureBlock(signers, locale)
	out, err := w.finish("")
	return out, boxes, err
}
