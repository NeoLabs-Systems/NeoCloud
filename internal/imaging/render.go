package imaging

import (
	"bytes"
	"image"
	"math"

	"github.com/gen2brain/vpx/webp"
	"golang.org/x/image/draw"
)

// turned reports whether the orientation swaps width and height.
func turned(orientation int) bool { return orientation >= 5 && orientation <= 8 }

// Size is the picture's size as shown.
func (p *Picture) Size() (int, int) {
	bounds := p.pixels.Bounds()
	if turned(p.orientation) {
		return bounds.Dy(), bounds.Dx()
	}
	return bounds.Dx(), bounds.Dy()
}

// scale resizes the stored pixels of source to a picture that is width by
// height once turned upright. Resizing first keeps the turn cheap.
func (p *Picture) scale(source image.Rectangle, width, height int) *image.RGBA {
	if turned(p.orientation) {
		width, height = height, width
	}
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(out, out.Bounds(), p.pixels, source, draw.Src, nil)
	return orient(out, p.orientation)
}

// Fit scales the picture to lie within an edge by edge box, never enlarging.
func (p *Picture) Fit(edge int) *image.RGBA {
	width, height := p.Size()
	ratio := math.Min(1, math.Min(float64(edge)/float64(width), float64(edge)/float64(height)))
	return p.scale(p.pixels.Bounds(), max(1, int(math.Round(float64(width)*ratio))), max(1, int(math.Round(float64(height)*ratio))))
}

// Cover crops the centre square and scales it to edge by edge.
func (p *Picture) Cover(edge int) *image.RGBA {
	bounds := p.pixels.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	left, top := bounds.Min.X+(bounds.Dx()-side)/2, bounds.Min.Y+(bounds.Dy()-side)/2
	return p.scale(image.Rect(left, top, left+side, top+side), edge, edge)
}

func orient(source *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return source
	}
	width, height := source.Bounds().Dx(), source.Bounds().Dy()
	outWidth, outHeight := width, height
	if turned(orientation) {
		outWidth, outHeight = height, width
	}
	out := image.NewRGBA(image.Rect(0, 0, outWidth, outHeight))
	for y := range outHeight {
		for x := range outWidth {
			var sx, sy int
			switch orientation {
			case 2:
				sx, sy = width-1-x, y
			case 3:
				sx, sy = width-1-x, height-1-y
			case 4:
				sx, sy = x, height-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, height-1-x
			case 7:
				sx, sy = width-1-y, height-1-x
			case 8:
				sx, sy = width-1-y, x
			}
			copy(out.Pix[out.PixOffset(x, y):out.PixOffset(x, y)+4], source.Pix[source.PixOffset(sx, sy):])
		}
	}
	return out
}

// EncodeWebP encodes a rendition, keeping transparency.
func EncodeWebP(picture image.Image, quality int) ([]byte, error) {
	var out bytes.Buffer
	if err := webp.Encode(&out, picture, webp.EncodeOptions{Quality: quality, Method: 4, Threads: 1}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// DHash is the difference hash of the picture as shown. The turn is applied
// to the whole picture first, so that it matches what the phone hashes.
func (p *Picture) DHash() string {
	if p.orientation == 1 {
		return DHash(p.pixels)
	}
	bounds := p.pixels.Bounds()
	stored := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(stored, stored.Bounds(), p.pixels, bounds.Min, draw.Src)
	return DHash(orient(stored, p.orientation))
}
