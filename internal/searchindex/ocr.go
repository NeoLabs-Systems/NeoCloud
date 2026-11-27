package searchindex

import (
	"context"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/imaging"
	"github.com/NeoLabs-Systems/neocloud/internal/ocr"
)

// Text recognition for the index: pictures, and PDF pages without a text
// layer. Both are read only while recognition is available; without it
// pictures are not indexable, so they wait for it instead of being recorded
// as empty.

// MaxImageSource is the largest picture read for its text.
const MaxImageSource = 40 << 20

// pageReserve is the extraction time one more page needs; a file's pages
// stop being read with less left, so the pages already read are kept.
const pageReserve = 15 * time.Second

var (
	pictureExtensions = []string{".jpg", ".jpeg", ".png", ".webp", ".heic", ".heif", ".tif", ".tiff"}
	pictureMIMEs      = []string{"image/jpeg", "image/png", "image/webp", "image/heic", "image/heif", "image/tiff"}
)

func isPicture(name, mimeType string) bool {
	return slices.Contains(pictureMIMEs, strings.ToLower(mimeType)) || slices.Contains(pictureExtensions, strings.ToLower(path.Ext(name)))
}

// recognizePicture reads the text of a picture, turned upright.
func recognizePicture(ctx context.Context, source []byte) (string, error) {
	picture, err := imaging.Decode(ctx, source)
	if err != nil {
		return "", err
	}
	page, err := ocr.Image(ctx, picture.Fit(ocr.MaxEdge))
	if err != nil {
		return "", err
	}
	return page.Text(), nil
}

// recognizeScannedPages adds what recognition reads to the pages whose text
// layer is (nearly) empty, up to ocr.MaxPDFPages of them. A page that cannot
// be read keeps its own text.
func recognizeScannedPages(ctx context.Context, source []byte, pages []string) error {
	if !ocr.PDFAvailable() {
		return nil
	}
	read := 0
	for index, text := range pages {
		if read >= ocr.MaxPDFPages {
			break
		}
		if !ocr.NeedsRecognition(text) {
			continue
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < pageReserve {
			break
		}
		read++
		page, err := ocr.PDFPage(ctx, source, index)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		if recognized := page.Text(); recognized != "" {
			pages[index] = strings.TrimSpace(text + "\n" + recognized)
		}
	}
	return nil
}
