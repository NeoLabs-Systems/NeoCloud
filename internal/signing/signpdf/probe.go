package signpdf

import (
	"bytes"
	"context"
	"image/png"
	"math"
)

// PNG re-encodes an uploaded picture, at most maxW×maxH: only pixels
// survive, no metadata.
func PNG(ctx context.Context, data []byte, maxW, maxH int) (out []byte, width, height int, err error) {
	img, err := picture(ctx, data, maxW, maxH)
	if err != nil {
		return nil, 0, 0, refuse(CodeImage, err)
	}
	var buffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err = encoder.Encode(&buffer, img); err != nil {
		return nil, 0, 0, refuse(CodeImage, err)
	}
	return buffer.Bytes(), img.Bounds().Dx(), img.Bounds().Dy(), nil
}

var a4 = struct{ short, long float64 }{595.28, 841.89}

// ImagePDF puts one picture on one A4 page (landscape for wide pictures),
// fitted inside 36 pt margins.
func ImagePDF(ctx context.Context, data []byte) ([]byte, error) {
	doc := emptyDocument()
	img, err := doc.embedImage(ctx, "NCI1", data)
	if err != nil {
		return nil, err
	}
	width, height := a4.short, a4.long
	if img.width > img.height {
		width, height = a4.long, a4.short
	}
	const margin = 36
	scale := min((width-2*margin)/img.width, (height-2*margin)/img.height)
	w, h := img.width*scale, img.height*scale
	page := doc.newPage(width, height)
	c := newCanvas()
	c.xobject(img.name, img.ref, [6]float64{w, 0, 0, h, (width - w) / 2, (height - h) / 2})
	doc.apply(page, c, true)
	out, _, err := doc.serialize(producerInfo(), nil)
	return out, err
}

// Page is one page as the reader sees it, in points.
type Page struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Rotate int     `json:"rotate"`
}

// Info is what Probe learns about a PDF.
type Info struct {
	Pages       []Page
	HasAcroForm bool
}

// Probe reads the page sizes of a PDF and whether it carries form fields.
func Probe(ctx context.Context, data []byte) (Info, error) {
	doc, err := load(ctx, data, MaxPDFBytes)
	if err != nil {
		return Info{}, err
	}
	return doc.info(), nil
}

func (d *document) info() Info {
	info := Info{}
	for _, p := range d.pages {
		w, h := p.size()
		info.Pages = append(info.Pages, Page{Width: round2(w), Height: round2(h), Rotate: p.rotate})
	}
	form := d.dict(d.root["AcroForm"])
	info.HasAcroForm = form != nil && len(d.array(form["Fields"])) > 0
	return info
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
