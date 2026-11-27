package signpdf

import (
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Reading the text of a page needs, per font, how its codes map to Unicode and
// how wide each glyph is. This is the small part of font handling that field
// detection needs: simple fonts (one byte per code) with their encoding and
// widths, composite fonts (two bytes) with /W, and ToUnicode CMaps for both.

type glyph struct {
	code  int
	text  []rune
	width float64 // in text space units per unit of font size
}

type fontInfo struct {
	composite bool
	unicode   map[int][]rune
	encoding  *[256]rune
	widths    map[int]float64
	fallback  float64
	scale     float64 // glyph space to text space
}

func (d *document) fontInfo(ref types.Object) *fontInfo {
	dict := d.dict(ref)
	f := &fontInfo{fallback: 500, scale: 0.001, widths: map[int]float64{}}
	if dict == nil {
		f.encoding = &winAnsi
		return f
	}
	base := d.name(dict["BaseFont"])
	subtype := d.name(dict["Subtype"])
	if stream, ok := d.resolve(dict["ToUnicode"]).(types.StreamDict); ok {
		if data, ok := d.decoded(stream); ok {
			f.unicode = parseToUnicode(data)
		}
	}
	if subtype == "Type0" {
		f.composite = true
		f.fallback = 1000
		descendants := d.array(dict["DescendantFonts"])
		if len(descendants) > 0 {
			cid := d.dict(descendants[0])
			if v, ok := cid["DW"]; ok {
				f.fallback = d.number(v)
			}
			d.cidWidths(cid["W"], f.widths)
		}
		return f
	}
	if subtype == "Type3" {
		if m := d.array(dict["FontMatrix"]); len(m) == 6 {
			f.scale = d.number(m[0])
		}
	}
	f.encoding = d.simpleEncoding(dict, base)
	first := int(d.number(dict["FirstChar"]))
	for i, w := range d.array(dict["Widths"]) {
		f.widths[first+i] = d.number(w)
	}
	if len(f.widths) == 0 {
		f.fallback = standardWidth(base, -1)
		for code := 32; code < 127; code++ {
			f.widths[code] = standardWidth(base, code)
		}
	} else if descriptor := d.dict(dict["FontDescriptor"]); descriptor != nil {
		if v, ok := descriptor["MissingWidth"]; ok {
			f.fallback = d.number(v)
		}
	}
	return f
}

func (d *document) cidWidths(obj types.Object, into map[int]float64) {
	list := d.array(obj)
	for i := 0; i < len(list) && len(into) < 70000; {
		first := int(d.number(list[i]))
		if i+1 >= len(list) {
			return
		}
		if run := d.array(list[i+1]); run != nil {
			for j, w := range run {
				into[first+j] = d.number(w)
			}
			i += 2
			continue
		}
		if i+2 >= len(list) {
			return
		}
		last, w := int(d.number(list[i+1])), d.number(list[i+2])
		for c := first; c <= last && c-first < 70000; c++ {
			into[c] = w
		}
		i += 3
	}
}

func (d *document) simpleEncoding(dict types.Dict, base string) *[256]rune {
	table := winAnsi
	switch {
	case strings.Contains(base, "ZapfDingbats") || strings.Contains(base, "Dingbats"):
		table = dingbats
	case strings.Contains(base, "Wingdings"):
		table = wingdings
	}
	encoding := dict["Encoding"]
	var differences types.Array
	if name := d.name(encoding); name != "" {
		if name == "MacRomanEncoding" {
			table = macRoman
		}
	} else if enc := d.dict(encoding); enc != nil {
		if d.name(enc["BaseEncoding"]) == "MacRomanEncoding" {
			table = macRoman
		}
		differences = d.array(enc["Differences"])
	}
	code := 0
	for _, item := range differences {
		switch v := d.resolve(item).(type) {
		case types.Integer:
			code = v.Value()
		case types.Float:
			code = int(v.Value())
		case types.Name:
			if code >= 0 && code < 256 {
				if r := glyphRune(string(v)); r != 0 {
					table[code] = r
				}
			}
			code++
		}
	}
	return &table
}

// glyphs splits a shown string into glyphs.
func (f *fontInfo) glyphs(s []byte) []glyph {
	var out []glyph
	step := 1
	if f.composite {
		step = 2
	}
	for i := 0; i+step <= len(s); i += step {
		code := int(s[i])
		if step == 2 {
			code = code<<8 | int(s[i+1])
		}
		g := glyph{code: code}
		if text, ok := f.unicode[code]; ok {
			g.text = text
		} else if f.encoding != nil {
			if r := f.encoding[code]; r != 0 {
				g.text = []rune{r}
			}
		}
		w, ok := f.widths[code]
		if !ok {
			w = f.fallback
		}
		g.width = w * f.scale
		out = append(out, g)
	}
	return out
}

// parseToUnicode reads the bfchar and bfrange sections of a ToUnicode CMap.
func parseToUnicode(data []byte) map[int][]rune {
	out := map[int][]rune{}
	l := &lexer{data: data}
	var operands []any
	section := ""
	for len(out) < 70000 {
		value, ok := l.next()
		if !ok {
			break
		}
		op, isOp := value.(pdfOp)
		if !isOp {
			operands = append(operands, value)
			continue
		}
		switch op {
		case "beginbfchar", "beginbfrange":
			section = string(op)
			operands = operands[:0]
			continue
		case "endbfchar":
			for i := 0; i+1 < len(operands); i += 2 {
				src, ok1 := operands[i].(pdfString)
				dst, ok2 := operands[i+1].(pdfString)
				if ok1 && ok2 {
					out[codeOf(src)] = utf16Runes(dst)
				}
			}
			section = ""
		case "endbfrange":
			for i := 0; i+2 < len(operands); i += 3 {
				lo, ok1 := operands[i].(pdfString)
				hi, ok2 := operands[i+1].(pdfString)
				if !ok1 || !ok2 {
					continue
				}
				first, last := codeOf(lo), codeOf(hi)
				if last < first || last-first > 0xffff {
					continue
				}
				switch dst := operands[i+2].(type) {
				case pdfString:
					base := utf16Runes(dst)
					if len(base) == 0 {
						continue
					}
					for c := first; c <= last; c++ {
						text := append([]rune(nil), base...)
						text[len(text)-1] += rune(c - first)
						out[c] = text
					}
				case []any:
					for j, item := range dst {
						if s, ok := item.(pdfString); ok && first+j <= last {
							out[first+j] = utf16Runes(s)
						}
					}
				}
			}
			section = ""
		}
		if section == "" {
			operands = operands[:0]
		}
	}
	return out
}

func codeOf(s pdfString) int {
	code := 0
	for _, b := range s {
		code = code<<8 | int(b)
	}
	return code
}

func utf16Runes(s pdfString) []rune {
	units := make([]uint16, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		units = append(units, uint16(s[i])<<8|uint16(s[i+1]))
	}
	if len(s)%2 == 1 {
		units = append(units, uint16(s[len(s)-1]))
	}
	return utf16.Decode(units)
}

// glyphRune maps the glyph names of encoding differences that matter for
// reading labels: Latin letters, digits, punctuation, uniXXXX.
func glyphRune(name string) rune {
	if r, ok := glyphNames[name]; ok {
		return r
	}
	if len(name) == 1 {
		return rune(name[0])
	}
	for _, prefix := range []string{"uni", "u"} {
		if strings.HasPrefix(name, prefix) && len(name) >= len(prefix)+4 {
			if v, err := strconv.ParseUint(name[len(prefix):len(prefix)+4], 16, 32); err == nil {
				return rune(v)
			}
		}
	}
	return 0
}

var glyphNames = map[string]rune{
	"space": ' ', "exclam": '!', "quotedbl": '"', "numbersign": '#', "dollar": '$', "percent": '%', "ampersand": '&',
	"quotesingle": '\'', "quoteright": '’', "quoteleft": '‘', "parenleft": '(', "parenright": ')', "asterisk": '*', "plus": '+',
	"comma": ',', "hyphen": '-', "minus": '-', "period": '.', "slash": '/', "colon": ':', "semicolon": ';', "less": '<',
	"equal": '=', "greater": '>', "question": '?', "at": '@', "bracketleft": '[', "backslash": '\\', "bracketright": ']',
	"underscore": '_', "grave": '`', "braceleft": '{', "bar": '|', "braceright": '}', "asciitilde": '~', "endash": '–',
	"emdash": '—', "ellipsis": '…', "bullet": '•', "periodcentered": '·', "multiply": '×',
	"zero": '0', "one": '1', "two": '2', "three": '3', "four": '4', "five": '5', "six": '6', "seven": '7', "eight": '8', "nine": '9',
	"adieresis": 'ä', "odieresis": 'ö', "udieresis": 'ü', "Adieresis": 'Ä', "Odieresis": 'Ö', "Udieresis": 'Ü', "germandbls": 'ß',
	"eacute": 'é', "egrave": 'è', "agrave": 'à', "ccedilla": 'ç', "Eacute": 'É',
}

// standardWidth approximates the standard 14 fonts, which files may use
// without widths: Courier is fixed, the rest are measured like Helvetica.
func standardWidth(base string, code int) float64 {
	if strings.Contains(base, "Courier") {
		return 600
	}
	if code >= 32 && code < 127 {
		return float64(helveticaWidths[code-32])
	}
	return 556
}

var helveticaWidths = [95]int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584,
}

var winAnsi = func() [256]rune {
	var t [256]rune
	for c := 32; c < 256; c++ {
		t[c] = rune(c)
	}
	for i, r := range []rune("€\x00‚ƒ„…†‡ˆ‰Š‹Œ\x00Ž\x00\x00‘’“”•–—˜™š›œ\x00žŸ") {
		t[0x80+i] = r
	}
	return t
}()

var macRoman = func() [256]rune {
	t := winAnsi
	for i, r := range []rune("ÄÅÇÉÑÖÜáàâäãåçéèêëíìîïñóòôöõúùûü†°¢£§•¶ß®©™´¨≠ÆØ∞±≤≥¥µ∂∑∏π∫ªºΩæø¿¡¬√ƒ≈∆«»… ÀÃÕŒœ–—“”‘’÷◊ÿŸ⁄€‹›ﬁﬂ‡·‚„‰ÂÊÁËÈÍÎÏÌÓÔÒÚÛÙıˆ˜¯˘˙˚¸˝˛ˇ") {
		t[0x80+i] = r
	}
	return t
}()

// The check box glyphs of the two symbol fonts forms use most.
var dingbats = func() [256]rune {
	var t [256]rune
	for code, r := range map[byte]rune{'n': '■', 'o': '❏', 'p': '❐', 'q': '❑', 'r': '❒', '3': '✓', '4': '✔', '7': '✗', '8': '✘'} {
		t[code] = r
	}
	return t
}()

var wingdings = func() [256]rune {
	var t [256]rune
	for code, r := range map[byte]rune{'o': '□', 'q': '❑', 'n': '■', 0xA8: '◻', 0xFD: '☒', 0xFE: '☑', 0xFC: '✓', 0x78: '☒'} {
		t[code] = r
	}
	return t
}()
