package signpdf

import (
	"context"
	"errors"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Rotate turns pages clockwise by quarter turns (page index → turns, negative
// turns go the other way) by changing their /Rotate. The content stays as it
// is, so nothing on a page is redrawn; only the way readers show it changes.
// The result is written as a fresh file like every file the engine makes.
func Rotate(ctx context.Context, data []byte, turns map[int]int) ([]byte, Info, error) {
	doc, err := load(ctx, data, MaxPDFBytes)
	if err != nil {
		return nil, Info{}, err
	}
	for index, n := range turns {
		if index < 0 || index >= len(doc.pages) {
			return nil, Info{}, refuse(CodeInvalid, errors.New("no such page"))
		}
		p := doc.pages[index]
		p.rotate = normalizeRotation(float64(p.rotate + 90*n))
		p.dict["Rotate"] = types.Integer(p.rotate)
	}
	info := producerInfo()
	if source := doc.ctx.XRefTable.Info; source != nil {
		for key, value := range doc.dict(*source) {
			if info[key] == nil {
				info[key] = value
			}
		}
	}
	out, _, err := doc.serialize(info, nil)
	if err != nil {
		return nil, Info{}, err
	}
	return out, doc.info(), nil
}

// RotateArea moves a box with its page when the page turns clockwise by
// turns quarter turns. width and height are the displayed page size before the
// turn. The box keeps its size in points and stays upright: its centre follows
// the content it sat on, so a signature box still reads left to right.
func RotateArea(a Area, turns int, width, height float64) Area {
	turns = ((turns % 4) + 4) % 4
	w, h := a.W*width, a.H*height
	cx, cy := a.X+a.W/2, a.Y+a.H/2
	for range turns {
		cx, cy = 1-cy, cx
		width, height = height, width
	}
	fw, fh := math.Min(1, w/width), math.Min(1, h/height)
	place := func(center, size float64) float64 { return math.Min(1-size, math.Max(0, center-size/2)) }
	out := a
	out.X, out.Y = round5(place(cx, fw)), round5(place(cy, fh))
	out.W, out.H = round5(fw), round5(fh)
	return out
}

func round5(v float64) float64 { return math.Round(v*1e5) / 1e5 }
