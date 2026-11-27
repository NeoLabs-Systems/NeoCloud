// Package ocrtest draws test pages for text recognition: black text, rules
// and boxes on white, in pixels.
package ocrtest

import (
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Sheet is a white page to draw on.
type Sheet struct{ *image.Gray }

// NewSheet makes a white page of width by height pixels.
func NewSheet(width, height int) Sheet {
	picture := image.NewGray(image.Rect(0, 0, width, height))
	for i := range picture.Pix {
		picture.Pix[i] = 0xff
	}
	return Sheet{picture}
}

// Text writes s with its baseline at y, starting at x, size pixels high.
func (s Sheet) Text(x, y int, size float64, text string) {
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		panic(err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	defer face.Close()
	drawer := font.Drawer{Dst: s.Gray, Src: image.NewUniform(color.Black), Face: face, Dot: fixed.P(x, y)}
	drawer.DrawString(text)
}

// Line draws a horizontal rule from x0 to x1, thick pixels high, its top at y.
// rise moves its right end that many pixels up, like a page scanned askew.
func (s Sheet) Line(x0, x1, y, thick, rise int) {
	for x := x0; x <= x1; x++ {
		offset := 0
		if x1 > x0 {
			offset = rise * (x - x0) / (x1 - x0)
		}
		for t := range thick {
			s.SetGray(x, y-offset+t, color.Gray{})
		}
	}
}

// Box draws the outline of a rectangle, its strokes thick pixels wide.
func (s Sheet) Box(x0, y0, x1, y1, thick int) {
	s.Line(x0, x1, y0, thick, 0)
	s.Line(x0, x1, y1-thick+1, thick, 0)
	for y := y0; y <= y1; y++ {
		for t := range thick {
			s.SetGray(x0+t, y, color.Gray{})
			s.SetGray(x1-t, y, color.Gray{})
		}
	}
}
