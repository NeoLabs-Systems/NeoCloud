package ocr

import (
	"context"
	"errors"
	"image"
	"image/color"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/ocr/ocrtest"
)

const sampleTSV = "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
	"1\t1\t0\t0\t0\t0\t0\t0\t1000\t800\t-1\t\n" +
	"2\t1\t1\t0\t0\t0\t40\t50\t600\t120\t-1\t\n" +
	"4\t1\t1\t1\t1\t0\t40\t50\t420\t30\t-1\t\n" +
	"5\t1\t1\t1\t1\t1\t40\t52\t120\t26\t96.5\tDatum:\n" +
	"5\t1\t1\t1\t1\t2\t180\t50\t60\t30\t12.0\t~~\n" +
	"5\t1\t1\t1\t1\t3\t260\t55\t200\t20\t91\tUnterschrift\n" +
	"4\t1\t1\t1\t2\t0\t40\t140\t300\t28\t-1\t\n" +
	"5\t1\t1\t1\t2\t1\t40\t140\t300\t28\t88\tÖffnungszeiten\n" +
	"5\t1\tx\t1\t2\t1\t40\t140\t300\t28\t88\tbroken\n" +
	"5\t1\t1\t1\t2\t2\t40\t140\t0\t28\t88\tzero\n" +
	"5\t1\t1\t1\t2\t3\t40\t140\t20\t28\t88\t   \n" +
	"short\trow\n"

func TestParseTSV(t *testing.T) {
	words := parseTSV([]byte(sampleTSV))
	var texts []string
	for _, w := range words {
		texts = append(texts, w.Text)
	}
	if !slices.Equal(texts, []string{"Datum:", "Unterschrift", "Öffnungszeiten"}) {
		t.Fatalf("words = %q", texts)
	}
	if words[0].Box != image.Rect(40, 52, 160, 78) || words[0].Line != image.Rect(40, 50, 460, 80) || words[0].Confidence != 96.5 {
		t.Fatalf("first word = %+v", words[0])
	}
	if words[1].LineIndex != 0 || words[2].LineIndex != 1 || words[2].Line != image.Rect(40, 140, 340, 168) {
		t.Fatalf("lines = %+v", words)
	}
	page := &Page{Words: words}
	if got := page.Text(); got != "Datum: Unterschrift\nÖffnungszeiten" {
		t.Fatalf("text = %q", got)
	}
	if len(parseTSV(nil)) != 0 || len(parseTSV([]byte("garbage\n\t\t\n"))) != 0 {
		t.Fatal("junk gave words")
	}
}

func TestLanguages(t *testing.T) {
	if got := requested(""); !slices.Equal(got, []string{"eng", "deu"}) {
		t.Fatalf("default = %q", got)
	}
	if got := requested(" deu + eng+deu+../x+script/Latin+chi_sim "); !slices.Equal(got, []string{"deu", "eng", "chi_sim"}) {
		t.Fatalf("requested = %q", got)
	}
	listing := "List of available languages in \"/usr/share/tesseract-ocr/5/tessdata/\" (3):\neng\nosd\ndeu\n"
	if got := installedLanguages(listing); !slices.Equal(got, []string{"eng", "osd", "deu"}) {
		t.Fatalf("installed = %q", got)
	}
}

func TestNeedsRecognition(t *testing.T) {
	for text, want := range map[string]bool{
		"":                           true,
		"  \n\f ":                    true,
		"Seite 1":                    true,
		"_____ ..... ----":           true,
		"Mietvertrag zwischen A":     false,
		"Rechnung Nr. 2026-0042 vom": false,
	} {
		if got := NeedsRecognition(text); got != want {
			t.Errorf("NeedsRecognition(%q) = %v", text, got)
		}
	}
}

func TestGrayscaleBounds(t *testing.T) {
	big := image.NewNRGBA(image.Rect(0, 0, 6000, 1500))
	big.Set(10, 10, color.NRGBA{0, 0, 0, 255})
	gray := grayscale(big, MaxEdge)
	if gray.Bounds() != image.Rect(0, 0, MaxEdge, 750) {
		t.Fatalf("bounds = %v", gray.Bounds())
	}
	// Transparent pixels are paper.
	if gray.GrayAt(MaxEdge/2, 400).Y != 0xff {
		t.Fatalf("transparent = %v", gray.GrayAt(MaxEdge/2, 400))
	}
	small := image.NewNRGBA(image.Rect(5, 5, 105, 55))
	small.Set(5, 5, color.NRGBA{0, 0, 0, 255})
	gray = grayscale(small, MaxEdge)
	if gray.Bounds() != image.Rect(0, 0, 100, 50) || gray.GrayAt(0, 0).Y != 0 {
		t.Fatalf("small = %v %v", gray.Bounds(), gray.GrayAt(0, 0))
	}
}

func TestRunBounds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	ctx := context.Background()
	out, err := run(ctx, 5*time.Second, "/bin/sh", []string{"-c", "cat"}, []byte("hello"), 16)
	if err != nil || string(out) != "hello" {
		t.Fatalf("cat = %q, %v", out, err)
	}
	if _, err = run(ctx, 5*time.Second, "/bin/sh", []string{"-c", "yes"}, nil, 1<<10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("endless output = %v", err)
	}
	if _, err = run(ctx, 5*time.Second, "/bin/sh", []string{"-c", "exit 3"}, nil, 16); !errors.Is(err, ErrFailed) {
		t.Fatalf("exit status = %v", err)
	}
	// A child that keeps stdout open goes with its group when the time is up.
	started := time.Now()
	if _, err = run(ctx, 300*time.Millisecond, "/bin/sh", []string{"-c", "sleep 30 & sleep 30"}, nil, 16); !errors.Is(err, ErrFailed) {
		t.Fatalf("timeout = %v", err)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatal("the process group outlived its time")
	}
	cancelled, cancel := context.WithCancel(ctx)
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	if _, err = run(cancelled, 5*time.Second, "/bin/sh", []string{"-c", "sleep 30"}, nil, 16); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled = %v", err)
	}
}

func TestUnavailable(t *testing.T) {
	Configure(Settings{Disabled: true})
	defer Configure(Settings{})
	if Available() || PDFAvailable() || CurrentStatus().Enabled {
		t.Fatal("disabled recognition is available")
	}
	if _, err := Image(context.Background(), image.NewGray(image.Rect(0, 0, 10, 10))); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Image = %v", err)
	}
	if _, err := PDFPage(context.Background(), []byte("%PDF-1.4"), 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("PDFPage = %v", err)
	}
}

// The tests below run the installed tesseract and skip without it.

func form() ocrtest.Sheet {
	sheet := ocrtest.NewSheet(1600, 900)
	sheet.Text(100, 150, 48, "Mietvertrag Wohnung")
	sheet.Text(100, 400, 36, "Ort, Datum")
	sheet.Line(330, 900, 398, 3, 0)
	sheet.Text(100, 600, 36, "Signature of the tenant")
	return sheet
}

func TestImageLive(t *testing.T) {
	Configure(Settings{Languages: "eng+deu"})
	if !Available() {
		t.Skip("tesseract is not installed")
	}
	status := CurrentStatus()
	if !slices.Contains(status.Languages, "eng") {
		t.Skipf("tesseract has no English data: %+v", status)
	}
	page, err := Image(context.Background(), form())
	if err != nil {
		t.Fatal(err)
	}
	text := page.Text()
	for _, want := range []string{"Mietvertrag", "Datum", "Signature"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text %q lacks %q", text, want)
		}
	}
	for _, w := range page.Words {
		if strings.Contains(w.Text, "Datum") {
			// Drawn with its baseline at 400, starting at 100 + "Ort, " ≈ 180.
			if w.Box.Max.Y < 385 || w.Box.Max.Y > 415 || w.Box.Min.X < 150 || w.Box.Min.X > 260 {
				t.Fatalf("Datum at %v", w.Box)
			}
			if !w.Line.In(page.Image.Bounds()) || w.Line.Min.X > w.Box.Min.X {
				t.Fatalf("line box %v", w.Line)
			}
		}
	}
	if page.Width != 1600 || page.Height != 900 {
		t.Fatalf("size = %dx%d", page.Width, page.Height)
	}
}
