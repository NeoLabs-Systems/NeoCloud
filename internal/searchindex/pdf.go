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

// extractPDFText uses poppler's pdftotext when it is installed, else the
// built-in reader.
func extractPDFText(ctx context.Context, source []byte) (string, error) {
	if binary := pdftotextPath(); binary != "" {
		return runPdftotext(ctx, binary, source)
	}
	var out strings.Builder
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
		remaining := MaxTextBytes - out.Len()
		if remaining <= 0 {
			return errPDFTextLimit
		}
		if out.Len() > 0 && text != "" {
			out.WriteByte('\n')
			remaining--
		}
		if len(text) > remaining {
			text = text[:remaining]
			for len(text) > 0 && !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
			out.WriteString(text)
			return errPDFTextLimit
		}
		out.WriteString(text)
		return nil
	})
	if errors.Is(err, errPDFTextLimit) {
		err = nil
	}
	if err != nil {
		return "", err
	}
	return out.String(), nil
}
