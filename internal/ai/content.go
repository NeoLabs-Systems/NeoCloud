package ai

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/imaging"
	"github.com/NeoLabs-Systems/neocloud/internal/searchindex"
)

// What a model gets to see of a file. Text first: the search index's extracted
// text when it has read this content, else the same extractor run in memory.
// Images go to the vision role, downscaled. Nothing here is written to disk.
const (
	// About 4k tokens of text is enough to name, tag and place a document.
	maxPromptRunes      = 12000
	maxImageSourceBytes = 40 << 20
	visionEdge          = 1024
	visionQuality       = 80
	// How long an upload waits for the indexer before reading the file itself.
	indexWait = 2 * time.Minute
)

type fileInput struct {
	// kind is "text", "image", "wait" (the indexer reads it soon) or "none".
	kind   string
	text   string
	image  []byte
	exif   imaging.Exif
	reason string
}

func nothing(reason string) fileInput { return fileInput{kind: "none", reason: reason} }

func isImage(n node) bool {
	return strings.HasPrefix(n.MIME, "image/") && n.MIME != "image/svg+xml"
}

func promptText(text string) string {
	return truncate(strings.Join(strings.Fields(text), " "), maxPromptRunes)
}

func truncate(text string, limit int) string {
	count := 0
	for index := range text {
		if count == limit {
			return text[:index]
		}
		count++
	}
	return text
}

func searchNode(n node) searchindex.Node {
	return searchindex.Node{ID: n.ID, Name: n.Name, MIME: n.MIME, Size: n.Size, BlobID: n.BlobID}
}

func (h *Handler) readImage(ctx context.Context, userID int64, n node) (fileInput, error) {
	if n.Size > maxImageSourceBytes {
		return nothing("TOO_LARGE"), nil
	}
	source, err := h.options.ReadBlob(ctx, userID, searchNode(n))
	if err != nil {
		return fileInput{}, err
	}
	picture, err := imaging.Decode(ctx, source)
	if err != nil {
		if ctx.Err() != nil {
			return fileInput{}, ctx.Err()
		}
		return nothing("UNREADABLE"), nil
	}
	var data bytes.Buffer
	if err = jpeg.Encode(&data, picture.Fit(visionEdge), &jpeg.Options{Quality: visionQuality}); err != nil {
		return nothing("UNREADABLE"), nil
	}
	return fileInput{kind: "image", image: data.Bytes(), exif: imaging.ReadExif(source)}, nil
}

// imageExif is the camera and date of an image, without decoding the picture.
func (h *Handler) imageExif(ctx context.Context, userID int64, n node) imaging.Exif {
	if !isImage(n) || n.Size > maxImageSourceBytes {
		return imaging.Exif{}
	}
	source, err := h.options.ReadBlob(ctx, userID, searchNode(n))
	if err != nil {
		return imaging.Exif{}
	}
	return imaging.ReadExif(source)
}

func (h *Handler) readFileInput(ctx context.Context, userID int64, n node, waitForIndex bool) (fileInput, error) {
	if n.BlobID == "" {
		return nothing("EMPTY"), nil
	}
	status, indexed := "", ""
	if h.options.Index != nil {
		var err error
		if status, indexed, err = h.options.Index.ReadIndexedText(ctx, userID, searchNode(n)); err != nil {
			return fileInput{}, err
		}
	}
	if status == "indexed" && strings.TrimSpace(indexed) != "" {
		return fileInput{kind: "text", text: promptText(indexed), exif: h.imageExif(ctx, userID, n)}, nil
	}
	if isImage(n) {
		return h.readImage(ctx, userID, n)
	}
	// An empty probe tells whether the extractor reads this kind of file at all.
	if _, err := searchindex.ExtractText(ctx, searchindex.Node{Name: n.Name, MIME: n.MIME}, nil); errors.Is(err, searchindex.ErrNotIndexable) {
		return nothing("UNSUPPORTED"), nil
	}
	if status == "failed" {
		return nothing("UNREADABLE"), nil
	}
	created, err := time.Parse(time.DateTime, n.CreatedAt)
	fresh := err == nil && time.Since(created) < indexWait
	if status == "" && waitForIndex && fresh && h.options.Index != nil && h.options.Index.IsEnabled() {
		return fileInput{kind: "wait"}, nil
	}
	if n.Size > searchindex.MaxPDFSource {
		return nothing("UNREADABLE"), nil
	}
	source, err := h.options.ReadBlob(ctx, userID, searchNode(n))
	if err != nil {
		return fileInput{}, err
	}
	text, err := searchindex.ExtractText(ctx, searchNode(n), source)
	if err != nil {
		if ctx.Err() != nil {
			return fileInput{}, ctx.Err()
		}
		return nothing("UNREADABLE"), nil
	}
	if text = promptText(text); text == "" {
		return nothing("NO_TEXT"), nil
	}
	return fileInput{kind: "text", text: text}, nil
}
