// Package ocr reads printed text from pictures with the optional `tesseract`
// command, and from PDF pages that poppler's `pdftoppm` draws first. Both run
// on stdin and stdout: the picture goes in on stdin and the words come back on
// stdout, so nothing is written to disk. Every run is bounded (input size,
// pixels, time, concurrent runs) and gets a minimal environment; neither tool
// opens network connections for this. Without tesseract every caller carries
// on as before.
package ocr

import (
	"bytes"
	"context"
	"errors"
	"image"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/proc"
)

const (
	// DefaultLanguages is read when nothing is configured.
	DefaultLanguages = "eng+deu"
	// MaxPDFBytes caps the PDFs whose pages are drawn for recognition.
	MaxPDFBytes = 100 << 20
	// MaxPDFPages is how many pages of one PDF a caller has read at most.
	MaxPDFPages = 10
	// MaxEdge is the longest side, in pixels, of what tesseract reads.
	MaxEdge = 3000
	// MaxJobs is how many tools run at once, server-wide.
	MaxJobs = 2

	pageEdge        = 2400 // pdftoppm's longest side: about 200 dpi on A4
	pictureDPI      = 300  // what a picture without a resolution is taken for
	recognizeTime   = 45 * time.Second
	rasterTime      = 30 * time.Second
	maxRasterOutput = 32 << 20
	maxTSVOutput    = 8 << 20
)

var (
	ErrUnavailable = errors.New("ocr: text recognition is not available")
	ErrTooLarge    = errors.New("ocr: input exceeds the recognition limits")
	ErrFailed      = errors.New("ocr: text recognition failed")
)

// Settings is the server's configuration of text recognition.
type Settings struct {
	// Disabled turns recognition off even when tesseract is installed.
	Disabled bool
	// Languages are tesseract language codes joined by "+", such as
	// "eng+deu". Codes that are not installed are left out.
	Languages string
}

// Status is what the administrator is shown about recognition.
type Status struct {
	// Enabled: tesseract is installed and on, with at least one language.
	Enabled bool `json:"enabled"`
	// PDF: pdftoppm is installed too, so scanned PDF pages can be read.
	PDF       bool     `json:"pdf"`
	Languages []string `json:"languages"`
	// Missing are configured languages tesseract does not have.
	Missing []string `json:"missingLanguages,omitempty"`
}

type tools struct {
	tesseract, pdftoppm string
	languages, missing  []string
}

var (
	mu        sync.Mutex
	settings  Settings
	detectOne = &sync.Once{}
	found     tools
	slots     = make(chan struct{}, MaxJobs)
)

// Configure applies the settings. It is called once while the server starts;
// the tools are looked for on first use.
func Configure(configured Settings) {
	mu.Lock()
	defer mu.Unlock()
	settings = configured
	detectOne, found = &sync.Once{}, tools{}
}

var languageCode = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)

// requested are the configured language codes, in order, without duplicates
// or anything that is not a plain code.
func requested(configured string) []string {
	if strings.TrimSpace(configured) == "" {
		configured = DefaultLanguages
	}
	var out []string
	for _, code := range strings.Split(configured, "+") {
		code = strings.TrimSpace(code)
		if languageCode.MatchString(code) && !slices.Contains(out, code) {
			out = append(out, code)
		}
	}
	return out
}

// installedLanguages parses `tesseract --list-langs`: a heading line, then
// one code per line.
func installedLanguages(listing string) []string {
	var out []string
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		if languageCode.MatchString(line) {
			out = append(out, line)
		}
	}
	return out
}

func detect() tools {
	mu.Lock()
	once, configured := detectOne, settings
	mu.Unlock()
	once.Do(func() {
		var t tools
		if path, err := exec.LookPath("tesseract"); err == nil && !configured.Disabled {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			listing, err := run(ctx, 10*time.Second, path, []string{"--list-langs"}, nil, 64<<10)
			cancel()
			if err == nil {
				installed := installedLanguages(string(listing))
				for _, code := range requested(configured.Languages) {
					if slices.Contains(installed, code) {
						t.languages = append(t.languages, code)
					} else {
						t.missing = append(t.missing, code)
					}
				}
				if len(t.languages) > 0 {
					t.tesseract = path
				}
			}
		}
		if path, err := exec.LookPath("pdftoppm"); err == nil && t.tesseract != "" {
			t.pdftoppm = path
		}
		mu.Lock()
		found = t
		mu.Unlock()
	})
	mu.Lock()
	defer mu.Unlock()
	return found
}

// Available reports whether pictures can be read.
func Available() bool { return detect().tesseract != "" }

// PDFAvailable reports whether PDF pages can be drawn and read.
func PDFAvailable() bool { return detect().pdftoppm != "" }

// CurrentStatus reports the tools found and the languages in use.
func CurrentStatus() Status {
	t := detect()
	status := Status{Enabled: t.tesseract != "", PDF: t.pdftoppm != "", Languages: slices.Clone(t.languages), Missing: slices.Clone(t.missing)}
	if status.Languages == nil {
		status.Languages = []string{}
	}
	return status
}

// NeedsRecognition tells whether a page's own text is too thin to stand for
// it: fewer than ten letters or digits means a scan, or a page whose words
// are drawn as shapes.
func NeedsRecognition(text string) bool {
	count := 0
	for _, r := range text {
		if isWordRune(r) {
			if count++; count >= 10 {
				return false
			}
		}
	}
	return true
}

// Image reads an upright picture. Larger pictures are scaled down to MaxEdge.
func Image(ctx context.Context, picture image.Image) (*Page, error) {
	t := detect()
	if t.tesseract == "" {
		return nil, ErrUnavailable
	}
	gray := grayscale(picture, MaxEdge)
	encoded, err := encodePNG(gray)
	if err != nil {
		return nil, ErrFailed
	}
	return recognize(ctx, t, encoded, gray, pictureDPI)
}

// PDFPage draws one page (index from 0) the way readers show it, /Rotate
// and crop box applied, and reads it. The page comes back with the drawn
// picture, so its pixels map to the displayed page by plain scaling.
func PDFPage(ctx context.Context, pdf []byte, index int) (*Page, error) {
	t := detect()
	if t.pdftoppm == "" {
		return nil, ErrUnavailable
	}
	if len(pdf) == 0 || len(pdf) > MaxPDFBytes || index < 0 {
		return nil, ErrTooLarge
	}
	number := itoa(index + 1)
	// "-" reads the PDF from stdin; with no output root the one page is
	// written to stdout.
	raster, err := run(ctx, rasterTime, t.pdftoppm, []string{"-q", "-f", number, "-l", number, "-cropbox", "-scale-to", itoa(pageEdge), "-gray", "-png", "-"}, pdf, maxRasterOutput)
	if err != nil {
		return nil, err
	}
	gray, err := decodeRaster(raster)
	if err != nil {
		return nil, err
	}
	// The PNG carries the resolution pdftoppm drew it at.
	return recognize(ctx, t, raster, gray, 0)
}

func recognize(ctx context.Context, t tools, encoded []byte, gray *image.Gray, dpi int) (*Page, error) {
	args := []string{"stdin", "stdout", "-l", strings.Join(t.languages, "+"), "--psm", "3"}
	if dpi > 0 {
		args = append(args, "--dpi", itoa(dpi))
	}
	args = append(args, "-c", "tessedit_create_tsv=1")
	tsv, err := run(ctx, recognizeTime, t.tesseract, args, encoded, maxTSVOutput)
	if err != nil {
		return nil, err
	}
	bounds := gray.Bounds()
	return &Page{Width: bounds.Dx(), Height: bounds.Dy(), Words: parseTSV(tsv), Image: gray}, nil
}

// environment is all the tools get: a search path, a UTF-8 locale, one
// thread each (the slots bound the work) and tesseract's data folder when the
// server was told where it is.
func environment() []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin"
	}
	env := []string{"PATH=" + path, "LANG=C.UTF-8", "OMP_THREAD_LIMIT=1"}
	if data := os.Getenv("TESSDATA_PREFIX"); data != "" {
		env = append(env, "TESSDATA_PREFIX="+data)
	}
	return env
}

// run starts one tool in its own process group once a slot is free, feeds it
// stdin, keeps at most limit bytes of stdout and kills the whole group when
// the time is up.
func run(ctx context.Context, timeout time.Duration, binary string, args []string, stdin []byte, limit int) ([]byte, error) {
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	limited, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out := &cappedBuffer{limit: limit}
	process, err := proc.Start(proc.Spec{Path: binary, Args: args, Env: environment(), Stdin: bytes.NewReader(stdin), Stdout: out})
	if err != nil {
		return nil, ErrUnavailable
	}
	select {
	case <-process.Done():
	case <-limited.Done():
		process.Kill()
		process.Wait()
		// The caller giving up is not the file's fault; the tool's own
		// time running out is.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, ErrFailed
	}
	if out.overflow {
		return nil, ErrTooLarge
	}
	if exit := process.Wait(); exit.Code != 0 || exit.Err != nil {
		return nil, ErrFailed
	}
	return out.buffer.Bytes(), nil
}

// cappedBuffer keeps the first limit bytes and refuses the rest, which ends
// the tool with a broken pipe. It has no ReadFrom, so io.Copy cannot get
// past Write.
type cappedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.buffer.Len()+len(p) > c.limit {
		c.overflow = true
		return 0, ErrTooLarge
	}
	return c.buffer.Write(p)
}
