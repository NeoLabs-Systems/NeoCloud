package signpdf

import (
	"context"
	"errors"
	"image"
	"math"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/ocr"
	"github.com/NeoLabs-Systems/neocloud/internal/ocr/ocrtest"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Scanned pages are tested without poppler: the page's picture is drawn
// here and handed to detection in place of pdftoppm's, with either the words
// tesseract would report or, when it is installed, tesseract's own.

// scannedPDF is one page of the given media size and /Rotate with nothing on
// it: no text layer, like a scan whose picture detection is handed.
func scannedPDF(t *testing.T, rotate int, width, height float64) []byte {
	t.Helper()
	doc := emptyDocument()
	p := doc.newPage(width, height)
	p.rotate, p.dict["Rotate"] = rotate, types.Integer(rotate)
	out, _, err := doc.serialize(producerInfo(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// scannedForm is a landscape form drawn at 2 pixels per point (842 × 595
// points displayed): a signature line with its label under it, a date label
// before a line and an empty check box with its label.
func scannedForm() ocrtest.Sheet {
	sheet := ocrtest.NewSheet(1684, 1190)
	sheet.Text(120, 160, 44, "Mietvertrag")
	sheet.Line(120, 720, 600, 3, 0)
	sheet.Text(120, 640, 26, "Unterschrift")
	sheet.Text(900, 600, 26, "Datum:")
	// Scanned 0.5° askew.
	sheet.Line(1010, 1500, 600, 3, 4)
	sheet.Box(120, 800, 150, 830, 3)
	sheet.Text(170, 826, 26, "Newsletter abonnieren")
	return sheet
}

func word(text string, x0, y0, x1, y1, line int) ocr.Word {
	return ocr.Word{Text: text, Box: image.Rect(x0, y0, x1, y1), Line: image.Rect(x0, y0-2, x1, y1+6), LineIndex: line, Confidence: 95}
}

func scannedFormWords() []ocr.Word {
	return []ocr.Word{
		word("Mietvertrag", 120, 128, 380, 160, 0),
		word("Unterschrift", 120, 620, 270, 640, 1),
		word("Datum:", 900, 580, 990, 600, 2),
		word("Newsletter", 170, 806, 300, 826, 3),
		word("abonnieren", 312, 806, 450, 826, 3),
		// A rule read as a word is a stroke, not text.
		word("—~—", 1010, 590, 1500, 604, 2),
	}
}

func withScan(t *testing.T, scan func(context.Context, []byte, int) (*ocr.Page, error)) {
	t.Helper()
	previous := scanPage
	scanPage = scan
	t.Cleanup(func() { scanPage = previous })
}

func findRole(fields []ImportedField, role string) *ImportedField {
	for i := range fields {
		if fields[i].Role == role {
			return &fields[i]
		}
	}
	return nil
}

// near checks an area against a box in points of the displayed page.
func near(t *testing.T, f *ImportedField, width, height, x, bottom, w float64) {
	t.Helper()
	if f == nil {
		t.Fatal("field not found")
	}
	a := f.Areas[0]
	gx, gb, gw := a.X*width, (a.Y+a.H)*height, a.W*width
	if math.Abs(gx-x) > 6 || math.Abs(gb-bottom) > 4 || math.Abs(gw-w) > 0.25*w {
		t.Fatalf("%s at x=%.1f bottom=%.1f w=%.1f, want %.1f %.1f %.1f", f.Role, gx, gb, gw, x, bottom, w)
	}
	if f.Source != "scan" {
		t.Fatalf("%s source = %q", f.Role, f.Source)
	}
}

func TestDetectScannedPage(t *testing.T) {
	sheet := scannedForm()
	// The page is portrait media turned a quarter: displayed landscape.
	for _, rotate := range []int{90, 270} {
		calls := 0
		withScan(t, func(_ context.Context, _ []byte, index int) (*ocr.Page, error) {
			calls++
			return &ocr.Page{Width: 1684, Height: 1190, Words: scannedFormWords(), Image: sheet.Gray}, nil
		})
		fields := detect(t, scannedPDF(t, rotate, 595, 842))
		if calls != 1 {
			t.Fatalf("scanned %d times", calls)
		}
		// Displayed 842 × 595: the signature line runs 60..360 at 300.5.
		near(t, findRole(fields, "signature"), 842, 595, 60, 300.5, 260)
		near(t, findRole(fields, "date"), 842, 595, 505, 300, 140)
		box := findRole(fields, "checkbox")
		near(t, box, 842, 595, 60, 415.5, 15.5)
		if box.Name != "Newsletter abonnieren" {
			t.Fatalf("check box name = %q", box.Name)
		}
		if findRole(fields, "text") != nil || len(fields) != 3 {
			t.Fatalf("rotate %d:\n%s", rotate, describe(fields))
		}
	}
}

func TestScanRefusesAPictureOfAnotherShape(t *testing.T) {
	withScan(t, func(context.Context, []byte, int) (*ocr.Page, error) {
		// Portrait picture of a landscape page: not the displayed page.
		return &ocr.Page{Width: 1190, Height: 1684, Words: scannedFormWords(), Image: image.NewGray(image.Rect(0, 0, 1190, 1684))}, nil
	})
	if fields := detect(t, scannedPDF(t, 90, 595, 842)); len(fields) != 0 {
		t.Fatalf("fields:\n%s", describe(fields))
	}
}

func TestScanOnlyPagesWithoutText(t *testing.T) {
	calls := 0
	withScan(t, func(context.Context, []byte, int) (*ocr.Page, error) {
		calls++
		return nil, ocr.ErrUnavailable
	})
	detect(t, typedForm())
	if calls != 0 {
		t.Fatalf("a page with text was scanned %d times", calls)
	}
	// Unavailable recognition is asked once, whatever the page count.
	doc := emptyDocument()
	for range 3 {
		doc.newPage(595, 842)
	}
	pages, _, err := doc.serialize(producerInfo(), nil)
	if err != nil {
		t.Fatal(err)
	}
	detect(t, pages)
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	pdf := scannedPDF(t, 0, 595, 842)
	withScan(t, func(context.Context, []byte, int) (*ocr.Page, error) { return nil, ocr.ErrFailed })
	if fields := detect(t, pdf); len(fields) != 0 {
		t.Fatalf("a failed scan gave fields")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	withScan(t, func(context.Context, []byte, int) (*ocr.Page, error) { cancel(); return nil, context.Canceled })
	if _, err := DetectFields(cancelled, pdf); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled = %v", err)
	}
}

func TestScanCharsMapToPoints(t *testing.T) {
	layer := []textChar{{r: 'S', x0: 10, x1: 16, base: 20, sz: 10}}
	chars := scanChars([]ocr.Word{
		word("Ort,", 100, 100, 140, 120, 0),
		word("Datum", 150, 100, 210, 120, 0),
		word("....", 220, 110, 300, 120, 0),
		word("_____", 220, 110, 300, 120, 0),
		word("Seite", 10, 18, 30, 30, 1), // over the text layer
	}, 0.5, 0.5, layer)
	var text strings.Builder
	for _, c := range chars {
		text.WriteRune(c.r)
	}
	if text.String() != "Ort, Datum _____" {
		t.Fatalf("text = %q", text.String())
	}
	d := chars[5] // D
	if d.r != 'D' || d.x0 != 75 || d.x1 != 81 || math.Abs(d.sz-14) > 1e-9 || math.Abs(d.base-(49+0.78*14)) > 1e-9 {
		t.Fatalf("D = %+v", d)
	}
}

func TestStrokesAndBoxesInPixels(t *testing.T) {
	sheet := ocrtest.NewSheet(1000, 600)
	sheet.Line(100, 900, 100, 2, 0)  // rule
	sheet.Line(100, 900, 200, 3, 12) // rule scanned askew (≈0.9°)
	sheet.Line(100, 400, 300, 30, 0) // a filled bar is not a rule
	sheet.Box(100, 400, 500, 470, 2) // a text field's frame
	sheet.Box(600, 400, 624, 424, 2) // a check box
	sheet.Line(700, 712, 500, 2, 0)  // too short for anything
	rules, squares, frames := scanShapes(sheet.Gray, 0.5, nil)
	if len(rules) != 2 || math.Abs(rules[0].y-50.5) > 0.6 || math.Abs(rules[0].x0-50) > 0.6 || math.Abs(rules[0].x1-450.5) > 0.6 {
		t.Fatalf("rules = %+v", rules)
	}
	if math.Abs(rules[1].y-97.5) > 4 || rules[1].x1-rules[1].x0 < 395 {
		t.Fatalf("askew rule = %+v", rules[1])
	}
	if len(frames) != 1 || math.Abs(frames[0].x-50) > 1 || math.Abs(frames[0].h-35.5) > 1 {
		t.Fatalf("frames = %+v", frames)
	}
	if len(squares) != 1 || math.Abs(squares[0].x-300) > 1 || math.Abs(squares[0].w-12.5) > 1 {
		t.Fatalf("squares = %+v", squares)
	}
	// Strokes inside a word are its letters.
	rules, _, _ = scanShapes(sheet.Gray, 0.5, []ocr.Word{word("underlined", 90, 90, 910, 110, 0)})
	if len(rules) != 1 {
		t.Fatalf("rules beside words = %+v", rules)
	}
	// A blank page and a black one have no shapes.
	if r, s, f := scanShapes(ocrtest.NewSheet(300, 300).Gray, 1, nil); len(r)+len(s)+len(f) != 0 {
		t.Fatal("shapes on a blank page")
	}
	if r, s, f := scanShapes(image.NewGray(image.Rect(0, 0, 300, 300)), 1, nil); len(r)+len(s)+len(f) != 0 {
		t.Fatal("shapes on a black page")
	}
}

// TestDetectScannedPageLive reads the drawn form with the installed
// tesseract; it skips without it.
func TestDetectScannedPageLive(t *testing.T) {
	ocr.Configure(ocr.Settings{})
	if !ocr.Available() {
		t.Skip("tesseract is not installed")
	}
	sheet := scannedForm()
	withScan(t, func(ctx context.Context, _ []byte, _ int) (*ocr.Page, error) { return ocr.Image(ctx, sheet) })
	fields := detect(t, scannedPDF(t, 90, 595, 842))
	near(t, findRole(fields, "signature"), 842, 595, 60, 300.5, 260)
	near(t, findRole(fields, "date"), 842, 595, 505, 300, 140)
	near(t, findRole(fields, "checkbox"), 842, 595, 60, 415.5, 15.5)
}
