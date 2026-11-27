package signpdf

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"slices"
	"strings"
)

// renderer draws field values onto their pages. An unusable value draws
// nothing, so one bad value cannot block finalisation.
type renderer struct {
	ctx      context.Context
	doc      *document
	fonts    fonts
	locale   string
	canvases map[int]*canvas
	images   map[[32]byte]embedded
}

func (r *renderer) canvas(page int) *canvas {
	if r.canvases[page] == nil {
		r.canvases[page] = newCanvas()
	}
	return r.canvases[page]
}

func isImageField(kind string) bool {
	return kind == "signature" || kind == "initials" || kind == "stamp" || kind == "image"
}

func chosen(value any) []string {
	if list, ok := value.([]any); ok {
		out := make([]string, len(list))
		for i, item := range list {
			out[i] = scalar(item)
		}
		return out
	}
	if text := scalar(value); text != "" {
		return []string{text}
	}
	return nil
}

func hasContent(field Field, value *Value) bool {
	if field.Type == "heading" {
		return true
	}
	if value == nil {
		return false
	}
	switch {
	case isImageField(field.Type):
		return len(value.Image) > 0 || value.Typed != "" || scalar(value.Data) != ""
	case field.Type == "file":
		return len(value.File) > 0
	case field.Type == "checkbox":
		return true // an empty box documents "not ticked"
	case field.Type == "multiple":
		return len(chosen(value.Data)) > 0 || len(field.Areas) > 1
	default:
		return scalar(value.Data) != ""
	}
}

// optionOf is the option an area stands for, when it has one.
func optionOf(field Field, index int) (string, bool) {
	if area := field.Areas[index]; area.Option != nil {
		return *area.Option, true
	}
	if len(field.Areas) > 1 && len(field.Prefs.Options) == len(field.Areas) {
		return field.Prefs.Options[index], true
	}
	return "", false
}

func (r *renderer) field(field Field, value *Value, fileName string) error {
	if !hasContent(field, value) {
		return nil
	}
	if value == nil {
		value = &Value{}
	}
	prefs := field.Prefs
	for index, area := range field.Areas {
		if area.Page < 0 || area.Page >= len(r.doc.pages) || area.W <= 0 || area.H <= 0 {
			continue
		}
		c := r.canvas(area.Page)
		box := r.doc.pages[area.Page].displayed(area)
		text := scalar(value.Data)
		switch field.Type {
		case "signature", "initials", "stamp", "image":
			if len(value.Image) > 0 {
				if err := r.image(c, box, value.Image); err != nil {
					return err
				}
				continue
			}
			// A typed signature without its picture prints the name.
			typed := value.Typed
			if typed == "" {
				typed, _ = value.Data.(string)
			}
			r.textBox(c, box, typed, prefs, false, 0)
		case "file":
			r.fileLabel(c, box, fileName, prefs)
		case "text", "phone", "select":
			r.textBox(c, box, text, prefs, false, 0)
		case "number":
			r.textBox(c, box, formatNumber(text, prefs.Format), prefs, false, 0)
		case "date":
			r.textBox(c, box, formatDate(text, prefs.Format, r.locale), prefs, false, 0)
		case "cells":
			r.cells(c, box, text, prefs)
		case "heading":
			heading := field.Name
			if field.Default != nil {
				heading = scalar(field.Default)
			}
			r.textBox(c, box, heading, prefs, true, 14)
		case "checkbox":
			checked := value.Data == true || slices.Contains([]string{"true", "1", "on", "yes", "checked"}, strings.ToLower(text))
			checkbox(c, box, checked, prefs)
		case "radio":
			if option, ok := optionOf(field, index); ok {
				radio(c, box, text == option, prefs)
			} else {
				r.textBox(c, box, text, prefs, false, 0)
			}
		case "multiple":
			if option, ok := optionOf(field, index); ok {
				checkbox(c, box, slices.Contains(chosen(value.Data), option), prefs)
			} else {
				r.textBox(c, box, strings.Join(chosen(value.Data), ", "), prefs, false, 0)
			}
		}
	}
	return nil
}

// textBox draws text inside a box: it shrinks to fit, wraps when the field is
// multi-line or tall enough, truncates as a last resort and is clipped.
func (r *renderer) textBox(c *canvas, box rect, raw string, prefs Prefs, boldByDefault bool, defaultSize float64) {
	bold := strings.Contains(strings.ToLower(prefs.FontType), "bold") || boldByDefault && prefs.FontType == ""
	f := r.fonts.pick(bold)
	text := strings.Trim(r.fonts.sanitize(raw), "\n")
	if strings.TrimSpace(text) == "" {
		return
	}
	pad := math.Min(math.Min(3, box.h*0.12), box.w*0.05)
	maxWidth, maxHeight := box.w-2*pad, box.h-2*pad
	multiline := prefs.Multiline || strings.Contains(text, "\n")
	auto := defaultSize
	if auto == 0 {
		auto = math.Min(12, maxHeight/lineHeight)
		if multiline {
			auto = 10
		}
	}
	base := auto
	if prefs.FontSize > 0 {
		base = prefs.FontSize
	}
	base = math.Max(base, 4)
	least := math.Min(base, 5)
	if prefs.FontSize > 0 {
		least = math.Max(4, prefs.FontSize*0.6)
	}
	size, lines := fitText(text, f, maxWidth, maxHeight, base, least, multiline || maxHeight >= 2*lineHeight*base)
	height := float64(len(lines)) * size * lineHeight
	valign := prefs.Valign
	if valign == "" {
		valign = "center"
		if len(lines) > 1 || multiline {
			valign = "top"
		}
	}
	top := box.y + box.h - pad
	switch valign {
	case "center", "middle":
		top = box.y + box.h/2 + height/2
	case "bottom":
		top = box.y + pad + height
	}
	if background, ok := parseColour(prefs.Background); ok {
		c.fillRect(box, background)
	}
	c.save()
	c.clip(box)
	for i, line := range lines {
		x := box.x + pad
		switch prefs.Align {
		case "center":
			x = box.x + (box.w-f.width(line, size))/2
		case "right":
			x = box.x + box.w - pad - f.width(line, size)
		}
		c.text(f, x, top-float64(i)*size*lineHeight-baseline*size, size, colourOr(prefs.Color, ink), line, 0)
	}
	c.restore()
}

// cells draws one character per box.
func (r *renderer) cells(c *canvas, box rect, raw string, prefs Prefs) {
	text := []rune(strings.ReplaceAll(r.fonts.sanitize(raw), "\n", " "))
	count := prefs.Cells
	if count <= 0 {
		count = max(1, int(math.Round(box.w/box.h)))
	}
	count = min(count, 200)
	width := box.w / float64(count)
	f := r.fonts.pick(strings.Contains(strings.ToLower(prefs.FontType), "bold"))
	size := prefs.FontSize
	if size <= 0 {
		size = 14
	}
	size = math.Max(4, math.Min(math.Min(size, box.h*0.6), width*0.75))
	for i := range count {
		x := box.x + float64(i)*width
		c.strokeRect(rect{x, box.y, width, box.h}, gridInk, 0.5)
		if i >= len(text) || text[i] == ' ' {
			continue
		}
		char := string(text[i])
		c.text(f, x+(width-f.width(char, size))/2, box.y+(box.h-size*0.72)/2, size, colourOr(prefs.Color, ink), char, 0)
	}
}

func squareIn(box rect) (float64, float64, float64) {
	side := math.Min(box.w, box.h)
	return box.x + (box.w-side)/2, box.y + (box.h-side)/2, side
}

func checkbox(c *canvas, box rect, checked bool, prefs Prefs) {
	x, y, side := squareIn(box)
	c.strokeRect(rect{x, y, side, side}, outline, math.Max(0.6, side*0.05))
	if !checked {
		return
	}
	mark, thickness := colourOr(prefs.Color, ink), math.Max(1, side*0.11)
	c.line(x+0.2*side, y+0.52*side, x+0.42*side, y+0.28*side, thickness, mark)
	c.line(x+0.42*side, y+0.28*side, x+0.8*side, y+0.76*side, thickness, mark)
}

func radio(c *canvas, box rect, selected bool, prefs Prefs) {
	x, y, side := squareIn(box)
	radius := side/2 - math.Max(0.3, side*0.03)
	c.circle(x+side/2, y+side/2, radius, math.Max(0.6, side*0.05), outline, "S")
	if selected {
		c.circle(x+side/2, y+side/2, radius*0.52, 0, colourOr(prefs.Color, ink), "f")
	}
}

// image draws a picture scaled to fit, never distorted, centred in the box.
// Equal pictures share one XObject.
func (r *renderer) image(c *canvas, box rect, data []byte) error {
	key := sha256.Sum256(data)
	img, ok := r.images[key]
	if !ok {
		var err error
		if img, err = r.doc.embedImage(r.ctx, fmt.Sprintf("NCI%d", len(r.images)+1), data); err != nil {
			return err
		}
		r.images[key] = img
	}
	pad := math.Min(box.w, box.h) * 0.04
	scale := math.Min((box.w-2*pad)/img.width, (box.h-2*pad)/img.height)
	w, h := img.width*scale, img.height*scale
	c.xobject(img.name, img.ref, [6]float64{w, 0, 0, h, box.x + (box.w-w)/2, box.y + (box.h-h)/2})
	return nil
}

// fileLabel draws a small page icon followed by the file name.
func (r *renderer) fileLabel(c *canvas, box rect, name string, prefs Prefs) {
	icon := math.Min(box.h*0.7, 14)
	x, y := box.x+2, box.y+(box.h-icon)/2
	grey := colour{0.35, 0.35, 0.4}
	c.strokeRect(rect{x, y, icon * 0.75, icon}, grey, 0.7)
	c.line(x+icon*0.15, y+icon*0.55, x+icon*0.6, y+icon*0.55, 0.6, grey)
	prefs.Valign = "center"
	r.textBox(c, rect{box.x + icon, box.y, math.Max(0, box.w-icon), box.h}, name, prefs, false, 0)
}
