package signpdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"math"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func flateStream(dict types.Dict, content []byte) types.StreamDict {
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	_, _ = w.Write(content)
	_ = w.Close()
	if dict == nil {
		dict = types.Dict{}
	}
	dict["Filter"] = types.Name("FlateDecode")
	return types.StreamDict{Dict: dict, Raw: compressed.Bytes()}
}

type colour struct{ r, g, b float64 }

var (
	ink     = colour{0.07, 0.07, 0.1}
	gridInk = colour{0.62, 0.62, 0.66}
	outline = colour{0.25, 0.25, 0.3}
)

// parseColour reads "#RGB" or "#RRGGBB".
func parseColour(s string) (colour, bool) {
	hex := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	var r, g, b int
	if len(hex) != 6 {
		return colour{}, false
	}
	if _, err := fmt.Sscanf(strings.ToLower(hex), "%02x%02x%02x", &r, &g, &b); err != nil {
		return colour{}, false
	}
	return colour{float64(r) / 255, float64(g) / 255, float64(b) / 255}, true
}

func colourOr(s string, fallback colour) colour {
	if c, ok := parseColour(s); ok {
		return c
	}
	return fallback
}

// canvas collects the drawing operators of one page, with the resources they
// name.
type canvas struct {
	content  bytes.Buffer
	fonts    map[string]types.IndirectRef
	xobjects map[string]types.IndirectRef
}

func newCanvas() *canvas {
	return &canvas{fonts: map[string]types.IndirectRef{}, xobjects: map[string]types.IndirectRef{}}
}

func (c *canvas) op(operator string, values ...float64) {
	for _, v := range values {
		c.content.WriteString(formatReal(v))
		c.content.WriteByte(' ')
	}
	c.content.WriteString(operator)
	c.content.WriteByte('\n')
}

func (c *canvas) save()    { c.op("q") }
func (c *canvas) restore() { c.op("Q") }

func (c *canvas) clip(r rect) {
	c.op("re", r.x, r.y, r.w, r.h)
	c.op("W")
	c.op("n")
}

func (c *canvas) fillRect(r rect, fill colour) {
	c.save()
	c.op("rg", fill.r, fill.g, fill.b)
	c.op("re", r.x, r.y, r.w, r.h)
	c.op("f")
	c.restore()
}

func (c *canvas) strokeRect(r rect, line colour, width float64) {
	c.save()
	c.op("RG", line.r, line.g, line.b)
	c.op("w", width)
	c.op("re", r.x, r.y, r.w, r.h)
	c.op("S")
	c.restore()
}

func (c *canvas) line(x1, y1, x2, y2, width float64, line colour) {
	c.save()
	c.op("RG", line.r, line.g, line.b)
	c.op("w", width)
	c.op("m", x1, y1)
	c.op("l", x2, y2)
	c.op("S")
	c.restore()
}

// circle draws with four Bézier arcs; paint is "f" or "S".
func (c *canvas) circle(x, y, radius, width float64, ink colour, paint string) {
	k := radius * 4 * (math.Sqrt2 - 1) / 3
	c.save()
	if paint == "f" {
		c.op("rg", ink.r, ink.g, ink.b)
	} else {
		c.op("RG", ink.r, ink.g, ink.b)
		c.op("w", width)
	}
	c.op("m", x+radius, y)
	c.op("c", x+radius, y+k, x+k, y+radius, x, y+radius)
	c.op("c", x-k, y+radius, x-radius, y+k, x-radius, y)
	c.op("c", x-radius, y-k, x-k, y-radius, x, y-radius)
	c.op("c", x+k, y-radius, x+radius, y-k, x+radius, y)
	c.op(paint)
	c.restore()
}

// text draws one line at a baseline; skew slants it (a synthetic italic).
func (c *canvas) text(f *font, x, y, size float64, fill colour, s string, skew float64) {
	if s == "" {
		return
	}
	c.fonts[f.resource] = f.ref
	c.save()
	c.op("rg", fill.r, fill.g, fill.b)
	c.op("BT")
	fmt.Fprintf(&c.content, "/%s %s Tf\n", f.resource, formatReal(size))
	c.op("Tm", 1, 0, skew, 1, x, y)
	fmt.Fprintf(&c.content, "<%s> Tj\n", f.encode(s))
	c.op("ET")
	c.restore()
}

func (c *canvas) xobject(name string, ref types.IndirectRef, m [6]float64) {
	c.xobjects[name] = ref
	c.save()
	c.op("cm", m[:]...)
	fmt.Fprintf(&c.content, "/%s Do\n", name)
	c.restore()
}

// apply puts the canvas on top of a page. The page's own content is wrapped in
// q/Q so that a source leaving its graphics state changed cannot shift the
// marks. displayed draws in the coordinates of the page as the reader sees it.
func (d *document) apply(p *page, c *canvas, displayed bool) {
	if c.content.Len() == 0 {
		return
	}
	var body bytes.Buffer
	body.WriteString("Q\nq\n")
	if displayed {
		m := p.matrix()
		for _, v := range m {
			body.WriteString(formatReal(v) + " ")
		}
		body.WriteString("cm\n")
	}
	body.Write(c.content.Bytes())
	body.WriteString("Q\n")

	contents := types.Array{d.stream(nil, []byte("q\n"))}
	switch v := d.resolve(p.dict["Contents"]).(type) {
	case types.Array:
		contents = append(contents, v...)
	case types.StreamDict:
		contents = append(contents, p.dict["Contents"])
	}
	p.dict["Contents"] = append(contents, d.stream(nil, body.Bytes()))

	// Resources may be shared between pages: copy before adding to them.
	resources := copyDict(d.dict(p.dict["Resources"]))
	for category, entries := range map[string]map[string]types.IndirectRef{"Font": c.fonts, "XObject": c.xobjects} {
		if len(entries) == 0 {
			continue
		}
		merged := copyDict(d.dict(resources[category]))
		for name, ref := range entries {
			merged[name] = ref
		}
		resources[category] = merged
	}
	p.dict["Resources"] = resources
}

func copyDict(source types.Dict) types.Dict {
	out := types.Dict{}
	for key, value := range source {
		out[key] = value
	}
	return out
}

// newPage appends an empty page of the given size to the page tree.
func (d *document) newPage(width, height float64) *page {
	pagesRef := d.root["Pages"].(types.IndirectRef)
	pages := d.dict(pagesRef)
	dict := types.Dict{
		"Type": types.Name("Page"), "Parent": pagesRef,
		"MediaBox":  types.Array{types.Integer(0), types.Integer(0), types.Float(width), types.Float(height)},
		"CropBox":   types.Array{types.Integer(0), types.Integer(0), types.Float(width), types.Float(height)},
		"Rotate":    types.Integer(0),
		"Resources": types.Dict{},
	}
	ref := d.add(dict)
	pages["Kids"] = append(append(types.Array{}, d.array(pages["Kids"])...), ref)
	pages["Count"] = types.Integer(int(d.number(pages["Count"])) + 1)
	d.set(pagesRef, pages)
	p := &page{ref: ref, dict: dict, box: rect{0, 0, width, height}}
	d.pages = append(d.pages, p)
	return p
}

// emptyDocument is a PDF without pages, for documents NeoCloud writes itself.
func emptyDocument() *document {
	doc := &document{ctx: newContext(), next: 1}
	pages := types.Dict{"Type": types.Name("Pages"), "Kids": types.Array{}, "Count": types.Integer(0)}
	pagesRef := doc.add(pages)
	doc.root = types.Dict{"Type": types.Name("Catalog"), "Pages": pagesRef}
	rootRef := doc.add(doc.root)
	doc.ctx.XRefTable.Root = &rootRef
	return doc
}
