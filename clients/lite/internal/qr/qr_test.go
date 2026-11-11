package qr

import (
	"encoding/base64"
	"strings"
	"testing"
)

const url = "https://cloud.example.com/app/device?code=ABCD-EFGH"

func TestTerminalUsesHalfBlocks(t *testing.T) {
	out, err := Terminal(url)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 10 {
		t.Fatalf("expected a QR of several lines, got %d", len(lines))
	}
	if !strings.ContainsAny(out, "▀▄█") {
		t.Fatalf("no half-block characters in %q", out)
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, ansiBlackOnWhite) || !strings.HasSuffix(line, ansiReset) {
			t.Fatalf("line %d is not forced to black on white", i)
		}
	}
	width := len([]rune(lines[0]))
	for i, line := range lines {
		if len([]rune(line)) != width {
			t.Fatalf("line %d has width %d, want %d", i, len([]rune(line)), width)
		}
	}
}

func TestSVGIsSquareAndSelfContained(t *testing.T) {
	svg, err := SVG(url)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(svg, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `) || !strings.HasSuffix(svg, "</svg>") {
		t.Fatalf("unexpected SVG: %.80s…", svg)
	}
	if strings.Contains(svg, "href") || strings.Contains(svg, "<script") {
		t.Fatal("SVG must not reference anything")
	}
	if !strings.Contains(svg, `d="M`) {
		t.Fatal("no dark modules drawn")
	}
	uri := DataURI(svg)
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, "data:image/svg+xml;base64,"))
	if err != nil || string(raw) != svg {
		t.Fatalf("data URI does not round-trip: %v", err)
	}
}

func TestEmptyTextFails(t *testing.T) {
	if _, err := SVG(""); err == nil {
		t.Fatal("expected an error for empty content")
	}
}
