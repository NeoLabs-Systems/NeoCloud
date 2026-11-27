package signpdf

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Bundled Noto Sans subsets (SIL OFL 1.1, fonts/OFL.txt): Latin, Greek,
// Cyrillic, general punctuation and currency signs. Other characters and emoji
// print as "?".
var (
	//go:embed fonts/NotoSans-Regular.subset.ttf
	regularTTF []byte
	//go:embed fonts/NotoSans-Bold.subset.ttf
	boldTTF []byte
)

type face struct {
	name     string
	data     []byte
	sfnt     *sfnt.Font
	unitsPer float64
}

var loadFaces = sync.OnceValue(func() [2]*face {
	parse := func(name string, data []byte) *face {
		parsed, err := sfnt.Parse(data)
		if err != nil {
			panic("signpdf: bundled font is damaged: " + err.Error())
		}
		return &face{name: name, data: data, sfnt: parsed, unitsPer: float64(parsed.UnitsPerEm())}
	}
	return [2]*face{parse("NotoSans-Regular", regularTTF), parse("NotoSans-Bold", boldTTF)}
})

// font is one face as embedded into one document: only the glyphs that were
// drawn get widths and a Unicode mapping.
type font struct {
	face     *face
	resource string
	ref      types.IndirectRef
	buffer   sfnt.Buffer
	glyphs   map[rune]sfnt.GlyphIndex
	advances map[sfnt.GlyphIndex]float64
	used     map[sfnt.GlyphIndex]rune
}

type fonts struct{ regular, bold *font }

func newFonts(d *document) fonts {
	faces := loadFaces()
	make1 := func(i int, resource string) *font {
		return &font{face: faces[i], resource: resource, ref: d.add(types.Dict{}),
			glyphs: map[rune]sfnt.GlyphIndex{}, advances: map[sfnt.GlyphIndex]float64{}, used: map[sfnt.GlyphIndex]rune{}}
	}
	return fonts{regular: make1(0, "NCFr"), bold: make1(1, "NCFb")}
}

func (f fonts) pick(bold bool) *font {
	if bold {
		return f.bold
	}
	return f.regular
}

func (f *font) glyph(r rune) sfnt.GlyphIndex {
	if g, ok := f.glyphs[r]; ok {
		return g
	}
	g, err := f.face.sfnt.GlyphIndex(&f.buffer, r)
	if err != nil {
		g = 0
	}
	f.glyphs[r] = g
	return g
}

// supports reports whether both weights can draw a character.
func (f fonts) supports(r rune) bool { return f.regular.glyph(r) != 0 && f.bold.glyph(r) != 0 }

// advance is the width of a glyph in thousandths of the font size.
func (f *font) advance(g sfnt.GlyphIndex) float64 {
	if w, ok := f.advances[g]; ok {
		return w
	}
	ppem := fixed.Int26_6(f.face.sfnt.UnitsPerEm()) << 6
	units, err := f.face.sfnt.GlyphAdvance(&f.buffer, g, ppem, 0)
	w := 0.0
	if err == nil {
		w = float64(units) / 64 * 1000 / f.face.unitsPer
	}
	f.advances[g] = w
	return w
}

func (f *font) width(s string, size float64) float64 {
	total := 0.0
	for _, r := range s {
		total += f.advance(f.glyph(r))
	}
	return total * size / 1000
}

func (f *font) encode(s string) string {
	var out strings.Builder
	for _, r := range s {
		g := f.glyph(r)
		if _, ok := f.used[g]; !ok {
			f.used[g] = r
		}
		fmt.Fprintf(&out, "%04X", g)
	}
	return out.String()
}

// finish writes the font objects of the faces that were used.
func (f fonts) finish(d *document) error {
	for _, current := range []*font{f.regular, f.bold} {
		if len(current.used) == 0 {
			continue
		}
		if err := current.embed(d); err != nil {
			return err
		}
	}
	return nil
}

func (f *font) embed(d *document) error {
	ppem := fixed.Int26_6(f.face.sfnt.UnitsPerEm()) << 6
	scale := func(v fixed.Int26_6) types.Integer { return types.Integer(float64(v) / 64 * 1000 / f.face.unitsPer) }
	bounds, err := f.face.sfnt.Bounds(&f.buffer, ppem, 0)
	if err != nil {
		return err
	}
	metrics, err := f.face.sfnt.Metrics(&f.buffer, ppem, 0)
	if err != nil {
		return err
	}
	glyphs := make([]sfnt.GlyphIndex, 0, len(f.used))
	for g := range f.used {
		glyphs = append(glyphs, g)
	}
	slices.Sort(glyphs)
	widths := types.Array{}
	for _, g := range glyphs {
		widths = append(widths, types.Integer(g), types.Array{types.Float(f.advance(g))})
	}
	file := d.stream(types.Dict{"Length1": types.Integer(len(f.face.data))}, f.face.data)
	name := types.Name(f.face.name)
	descriptor := d.add(types.Dict{
		"Type": types.Name("FontDescriptor"), "FontName": name, "Flags": types.Integer(4),
		// sfnt reports y growing downwards.
		"FontBBox":    types.Array{scale(bounds.Min.X), -scale(bounds.Max.Y), scale(bounds.Max.X), -scale(bounds.Min.Y)},
		"ItalicAngle": types.Integer(0), "Ascent": scale(metrics.Ascent), "Descent": -scale(metrics.Descent),
		"CapHeight": scale(metrics.CapHeight), "StemV": types.Integer(80), "FontFile2": file,
	})
	descendant := d.add(types.Dict{
		"Type": types.Name("Font"), "Subtype": types.Name("CIDFontType2"), "BaseFont": name,
		"CIDSystemInfo":  types.Dict{"Registry": types.StringLiteral("Adobe"), "Ordering": types.StringLiteral("Identity"), "Supplement": types.Integer(0)},
		"FontDescriptor": descriptor, "CIDToGIDMap": types.Name("Identity"), "DW": types.Integer(1000), "W": widths,
	})
	var cmap strings.Builder
	cmap.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	for start := 0; start < len(glyphs); start += 100 {
		chunk := glyphs[start:min(start+100, len(glyphs))]
		fmt.Fprintf(&cmap, "%d beginbfchar\n", len(chunk))
		for _, g := range chunk {
			fmt.Fprintf(&cmap, "<%04X> <", g)
			for _, unit := range utf16.Encode([]rune{f.used[g]}) {
				fmt.Fprintf(&cmap, "%04X", unit)
			}
			cmap.WriteString(">\n")
		}
		cmap.WriteString("endbfchar\n")
	}
	cmap.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	d.set(f.ref, types.Dict{
		"Type": types.Name("Font"), "Subtype": types.Name("Type0"), "BaseFont": name, "Encoding": types.Name("Identity-H"),
		"DescendantFonts": types.Array{descendant}, "ToUnicode": d.stream(nil, []byte(cmap.String())),
	})
	return nil
}
