package ocr

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/image/draw"
)

// minConfidence drops the words tesseract is least sure of: specks, rules
// and stamps read as letters.
const minConfidence = 35

// Word is one recognised word with its box in the pixels of Page.Image.
type Word struct {
	Text string
	Box  image.Rectangle
	// Line is the box of the line the word sits on, so words of one line
	// share a top and a bottom whatever their letters reach.
	Line image.Rectangle
	// LineIndex counts the page's lines from 0 in reading order.
	LineIndex  int
	Confidence float64
}

// Page is what was read of one picture or PDF page.
type Page struct {
	Width, Height int
	Words         []Word
	// Image is the upright greyscale picture that was read.
	Image *image.Gray
}

// Text is the page's words, a line of text per recognised line.
func (p *Page) Text() string {
	var out strings.Builder
	for i, w := range p.Words {
		if i > 0 {
			if w.LineIndex != p.Words[i-1].LineIndex {
				out.WriteByte('\n')
			} else {
				out.WriteByte(' ')
			}
		}
		out.WriteString(w.Text)
	}
	return out.String()
}

// parseTSV reads tesseract's TSV: a header, then one row per page, block,
// paragraph, line (level 4) and word (level 5), each with its box in pixels.
// Rows that do not parse are skipped.
func parseTSV(data []byte) []Word {
	type key struct{ block, par, line int }
	lines := map[key]image.Rectangle{}
	index := map[key]int{}
	var words []Word
	for _, row := range strings.Split(string(data), "\n") {
		cols := strings.Split(strings.TrimRight(row, "\r"), "\t")
		if len(cols) < 11 {
			continue
		}
		var n [10]int
		ok := true
		for i := range 10 {
			if n[i], ok = number(cols[i]); !ok {
				break
			}
		}
		if !ok || n[8] <= 0 || n[9] <= 0 || n[6] < 0 || n[7] < 0 {
			continue
		}
		k := key{n[2], n[3], n[4]}
		box := image.Rect(n[6], n[7], n[6]+n[8], n[7]+n[9])
		switch n[0] {
		case 4:
			lines[k] = box
		case 5:
			text := strings.TrimSpace(strings.Join(cols[11:], "\t"))
			confidence, err := strconv.ParseFloat(strings.TrimSpace(cols[10]), 64)
			if text == "" || err != nil || confidence < minConfidence {
				continue
			}
			if _, seen := index[k]; !seen {
				index[k] = len(index)
			}
			words = append(words, Word{Text: strings.ToValidUTF8(text, ""), Box: box, LineIndex: index[k], Confidence: confidence})
		}
	}
	keys := make(map[int]key, len(index))
	for k, i := range index {
		keys[i] = k
	}
	for i := range words {
		line, ok := lines[keys[words[i].LineIndex]]
		if !ok {
			line = words[i].Box
		}
		words[i].Line = line.Union(words[i].Box)
	}
	return words
}

func number(s string) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	return v, err == nil
}

func itoa(n int) string { return strconv.Itoa(n) }

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// grayscale draws a picture in grey on white (transparency becomes paper),
// scaled down so that its longest side is at most edge.
func grayscale(picture image.Image, edge int) *image.Gray {
	bounds := picture.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	ratio := math.Min(1, float64(edge)/float64(max(width, height, 1)))
	width, height = max(1, int(math.Round(float64(width)*ratio))), max(1, int(math.Round(float64(height)*ratio)))
	out := image.NewGray(image.Rect(0, 0, width, height))
	for i := range out.Pix {
		out.Pix[i] = 0xff
	}
	if ratio == 1 {
		draw.Draw(out, out.Bounds(), picture, bounds.Min, draw.Over)
	} else {
		draw.ApproxBiLinear.Scale(out, out.Bounds(), picture, bounds, draw.Over, nil)
	}
	return out
}

func encodePNG(picture image.Image) ([]byte, error) {
	var out bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&out, picture); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// decodeRaster reads pdftoppm's PNG after checking its size.
func decodeRaster(data []byte) (*image.Gray, error) {
	header, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, ErrFailed
	}
	if header.Width < 1 || header.Height < 1 || header.Width > 2*pageEdge || header.Height > 2*pageEdge {
		return nil, ErrTooLarge
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrFailed
	}
	if gray, ok := decoded.(*image.Gray); ok && gray.Bounds().Min == (image.Point{}) {
		return gray, nil
	}
	return grayscale(decoded, 2*pageEdge), nil
}
