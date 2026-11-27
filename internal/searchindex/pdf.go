package searchindex

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	pdftext "github.com/giraffesyo/pdf"
)

const maxPDFPages = 2000

var errPDFTextLimit = errors.New("searchindex: PDF text limit reached")

// extractPDFText reads the text layer, with poppler's pdftotext when it is
// installed, else the built-in reader; pages without one are read by text
// recognition when it is available.
func extractPDFText(ctx context.Context, source []byte) (string, error) {
	pages, err := pdfPages(ctx, source)
	if err != nil {
		return "", err
	}
	if err = recognizeScannedPages(ctx, source, pages); err != nil {
		return "", err
	}
	return strings.Join(pages, "\n"), nil
}

// pdfPages is the text layer of each page, as far as the limits reach.
func pdfPages(ctx context.Context, source []byte) ([]string, error) {
	if binary := pdftotextPath(); binary != "" {
		text, err := runPdftotext(ctx, binary, source)
		if err != nil {
			return nil, err
		}
		// pdftotext ends every page with a form feed.
		pages := strings.Split(text, "\f")
		if len(pages) > 1 && pages[len(pages)-1] == "" {
			pages = pages[:len(pages)-1]
		}
		return pages, nil
	}
	var pages []string
	size := 0
	// Keep parser work bounded even for valid but pathological documents. The
	// source and output limits are enforced by ExtractText and capText.
	_, err := pdftext.ExtractPages(ctx, bytes.NewReader(source), int64(len(source)), pdftext.Options{
		Pages:       []pdftext.PageRange{{First: 1, Last: maxPDFPages}},
		Concurrency: 1,
		Strict:      true,
		Limits: pdftext.Limits{
			MaxStreamBytes:      16 << 20,
			MaxOperatorsPerPage: 200_000,
			MaxGlyphsPerPage:    50_000,
			MaxFormDepth:        8,
			MaxImagesPerPage:    10_000,
		},
	}, func(page pdftext.Page) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		text := page.Text()
		remaining := MaxTextBytes - size
		if remaining <= 0 {
			return errPDFTextLimit
		}
		if len(text) > remaining {
			text = text[:remaining]
			for len(text) > 0 && !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
			pages = append(pages, text)
			return errPDFTextLimit
		}
		pages, size = append(pages, text), size+len(text)+1
		return nil
	})
	if errors.Is(err, errPDFTextLimit) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return pages, nil
}
