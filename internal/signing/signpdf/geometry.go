package signpdf

import "math"

// Clients and the database speak in the displayed page: the visible box with
// /Rotate applied, origin top-left, y down, fractions 0..1. PDF content lives
// in unrotated user space (origin bottom left, y up, offset by the box
// corner). This file is the only place that converts between the two.

type rect struct{ x, y, w, h float64 }

func (r rect) intersect(o rect) (rect, bool) {
	x, y := math.Max(r.x, o.x), math.Max(r.y, o.y)
	w := math.Min(r.x+r.w, o.x+o.w) - x
	h := math.Min(r.y+r.h, o.y+o.h) - y
	return rect{x, y, w, h}, w > 0 && h > 0
}

func normalizeRotation(angle float64) int {
	return ((int(math.Round(angle/90))*90)%360 + 360) % 360
}

// size is the displayed size of a page.
func (p *page) size() (float64, float64) {
	if p.rotate == 90 || p.rotate == 270 {
		return p.box.h, p.box.w
	}
	return p.box.w, p.box.h
}

// matrix maps displayed coordinates (origin bottom-left, y up) to user space,
// so content drawn after it appears upright whatever the rotation.
func (p *page) matrix() [6]float64 {
	b := p.box
	switch p.rotate {
	case 90:
		return [6]float64{0, 1, -1, 0, b.x + b.w, b.y}
	case 180:
		return [6]float64{-1, 0, 0, -1, b.x + b.w, b.y + b.h}
	case 270:
		return [6]float64{0, -1, 1, 0, b.x, b.y + b.h}
	default:
		return [6]float64{1, 0, 0, 1, b.x, b.y}
	}
}

// displayed turns a fractional top-left area into points of the displayed page
// measured from its bottom-left.
func (p *page) displayed(a Area) rect {
	width, height := p.size()
	w, h := a.W*width, a.H*height
	return rect{a.X * width, height - a.Y*height - h, w, h}
}

// topLeft maps a user-space point to the displayed page, y down.
func (p *page) topLeft(x, y float64) (float64, float64) {
	b := p.box
	switch p.rotate {
	case 90:
		return y - b.y, x - b.x
	case 180:
		return b.x + b.w - x, y - b.y
	case 270:
		return b.y + b.h - y, b.x + b.w - x
	default:
		return x - b.x, b.y + b.h - y
	}
}

// area converts a user-space rectangle to a clamped fractional area.
func (p *page) area(r rect) Area {
	ax, ay := p.topLeft(r.x, r.y)
	bx, by := p.topLeft(r.x+r.w, r.y+r.h)
	width, height := p.size()
	round := func(n float64) float64 { return math.Round(math.Min(1, math.Max(0, n))*1e5) / 1e5 }
	x, y := round(math.Min(ax, bx)/width), round(math.Min(ay, by)/height)
	x2, y2 := round(math.Max(ax, bx)/width), round(math.Max(ay, by)/height)
	return Area{X: x, Y: y, W: round(x2 - x), H: round(y2 - y)}
}
