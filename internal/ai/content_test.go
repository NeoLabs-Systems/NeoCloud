package ai

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/searchindex"
)

type fixedIndex struct{ status, text string }

func (f fixedIndex) ReadIndexedText(context.Context, int64, searchindex.Node) (string, string, error) {
	return f.status, f.text, nil
}
func (fixedIndex) IsEnabled() bool { return true }

// An image the index recognised text in goes to the text model with that
// text (docs/ai.md); without any, the picture goes to the vision model.
func TestImageInputUsesRecognisedText(t *testing.T) {
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewGray(image.Rect(0, 0, 40, 30))); err != nil {
		t.Fatal(err)
	}
	source := picture.Bytes()
	h := &Handler{options: Options{
		Index:    fixedIndex{"indexed", "Rechnung  Nr. 42\nGesamt 19,99 EUR"},
		ReadBlob: func(context.Context, int64, searchindex.Node) ([]byte, error) { return source, nil },
	}}
	photo := node{ID: "n", Name: "beleg.png", MIME: "image/png", BlobID: "b", Size: int64(len(source))}
	input, err := h.readFileInput(context.Background(), 1, photo, false)
	if err != nil || input.kind != "text" || input.text != "Rechnung Nr. 42 Gesamt 19,99 EUR" {
		t.Fatalf("input = %q %q %v", input.kind, input.text, err)
	}
	h.options.Index = fixedIndex{"indexed", ""}
	if input, err = h.readFileInput(context.Background(), 1, photo, false); err != nil || input.kind != "image" || len(input.image) == 0 {
		t.Fatalf("no text = %q %v", input.kind, err)
	}
}
