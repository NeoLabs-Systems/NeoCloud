package searchindex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/NeoLabs-Systems/neocloud/internal/ocr"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/unicode"
)

var ErrUnsupportedExtraction = errors.New("searchindex: this document format needs an optional extractor")
var sourceCharset = regexp.MustCompile(`(?i)(?:charset|encoding)\s*=\s*["']?([A-Za-z0-9._:-]+)`)

// ExtractText converts supported plain-text, HTML, PDF and Office/OpenDocument
// files, and pictures while text recognition is available, into bounded UTF-8
// text. The caller should decrypt the source blob
// before calling it and keep that plaintext only in memory.
func ExtractText(ctx context.Context, node Node, source []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	kind := extractionKind(node.Name, node.MIME)
	if kind == "" {
		return "", ErrNotIndexable
	}
	node.Size = max(node.Size, int64(len(source)))
	if err := checkSourceSize(node); err != nil {
		return "", err
	}
	if kind == "pdf" {
		text, err := extractPDFText(ctx, source)
		if err != nil {
			return "", err
		}
		return capText(text), nil
	}
	if kind == "image" {
		text, err := recognizePicture(ctx, source)
		if err != nil {
			return "", err
		}
		return capText(text), nil
	}
	if kind == "office" {
		text, err := extractOffice(ctx, source, strings.ToLower(path.Ext(node.Name)))
		if err != nil {
			return "", err
		}
		return capText(text), nil
	}
	text, err := decodeSource(source, kind)
	if err != nil {
		return "", err
	}
	if kind == "html" {
		text, err = htmlText(ctx, text)
		if err != nil {
			return "", err
		}
	}
	return capText(text), nil
}

// checkSourceSize refuses files that are too large to be read at all.
func checkSourceSize(node Node) error {
	limit := int64(MaxSourceText)
	switch extractionKind(node.Name, node.MIME) {
	case "pdf":
		limit = MaxPDFSource
	case "office":
		limit = MaxOfficeSource
	case "image":
		limit = MaxImageSource
	}
	if node.Size > limit {
		return fmt.Errorf("searchindex: source exceeds %d-byte extraction limit", limit)
	}
	return nil
}

func extractionKind(name, mimeType string) string {
	name = strings.ToLower(name)
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	switch {
	case strings.HasSuffix(name, ".pdf") || mimeType == "application/pdf":
		return "pdf"
	case strings.HasSuffix(name, ".docx"), strings.HasSuffix(name, ".xlsx"), strings.HasSuffix(name, ".pptx"), strings.HasSuffix(name, ".odt"), strings.HasSuffix(name, ".ods"), strings.HasSuffix(name, ".odp"):
		return "office"
	case isPicture(name, mimeType):
		if ocr.Available() {
			return "image"
		}
		return ""
	case strings.HasSuffix(name, ".html"), strings.HasSuffix(name, ".htm"), strings.HasSuffix(name, ".xhtml"), mimeType == "text/html":
		return "html"
	case strings.HasPrefix(mimeType, "text/") || isIndexable(name, mimeType):
		return "text"
	default:
		return ""
	}
}

func decodeSource(source []byte, kind string) (string, error) {
	if bytes.HasPrefix(source, []byte{0xef, 0xbb, 0xbf}) {
		return string(source[3:]), nil
	}
	if bytes.HasPrefix(source, []byte{0xff, 0xfe}) || bytes.HasPrefix(source, []byte{0xfe, 0xff}) {
		reader := unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder().Reader(bytes.NewReader(source))
		decoded, err := io.ReadAll(io.LimitReader(reader, MaxSourceText+1))
		if err != nil {
			return "", err
		}
		return string(decoded), nil
	}
	if endianness, ok := detectUTF16ByteOrder(source); ok {
		reader := unicode.UTF16(endianness, unicode.IgnoreBOM).NewDecoder().Reader(bytes.NewReader(source))
		decoded, err := io.ReadAll(io.LimitReader(reader, MaxSourceText+1))
		if err != nil {
			return "", err
		}
		if len(decoded) > MaxSourceText {
			decoded = decoded[:MaxSourceText]
		}
		return string(decoded), nil
	}
	// Binary data has no text.
	if bytes.Contains(source[:min(len(source), 8192)], []byte{0}) {
		return "", nil
	}
	if utf8.Valid(source) {
		return string(source), nil
	}
	contentType := "text/plain"
	if kind == "html" {
		contentType = "text/html"
	}
	if head := source[:min(len(source), 2048)]; len(head) > 0 {
		if found := sourceCharset.FindSubmatch(head); len(found) == 2 {
			contentType += "; charset=" + string(found[1])
		}
	}
	reader, err := charset.NewReader(bytes.NewReader(source), contentType)
	if err != nil {
		return "", err
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, MaxSourceText+1))
	if err != nil {
		return "", err
	}
	if len(decoded) > MaxSourceText {
		decoded = decoded[:MaxSourceText]
	}
	return string(decoded), nil
}

// detectUTF16ByteOrder recognizes BOM-less UTF-16 from the alternating zero
// bytes common in ASCII-heavy text. It uses only the first 4096 bytes and
// requires a strong signal to avoid misclassifying binary or UTF-8 input.
func detectUTF16ByteOrder(source []byte) (unicode.Endianness, bool) {
	sample := source[:min(len(source), 4096)]
	if len(sample) < 4 {
		return unicode.LittleEndian, false
	}
	var evenZeros, oddZeros int
	for i, value := range sample {
		if value != 0 {
			continue
		}
		if i%2 == 0 {
			evenZeros++
		} else {
			oddZeros++
		}
	}
	half := len(sample) / 2
	if oddZeros*10 > half*3 && evenZeros*20 < half {
		return unicode.LittleEndian, true
	}
	if evenZeros*10 > half*3 && oddZeros*20 < half {
		return unicode.BigEndian, true
	}
	return unicode.LittleEndian, false
}

func htmlText(ctx context.Context, source string) (string, error) {
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", err
	}
	var out strings.Builder
	var walk func(*html.Node) error
	walk = func(node *html.Node) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if node.Type == html.ElementNode {
			switch strings.ToLower(node.Data) {
			case "script", "style", "noscript", "template":
				return nil
			case "p", "div", "br", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "section", "article", "header", "footer", "table", "ul", "ol", "pre", "blockquote":
				out.WriteByte('\n')
			}
		}
		if node.Type == html.TextNode {
			out.WriteString(node.Data)
			out.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(document); err != nil {
		return "", err
	}
	return out.String(), nil
}
