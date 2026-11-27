package searchindex

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"golang.org/x/text/encoding/unicode"
)

func TestExtractTextBoundsAndDecodesPlainText(t *testing.T) {
	got, err := ExtractText(context.Background(), Node{Name: "readme.md", MIME: "text/markdown", Size: 8}, []byte("hello\x00world"))
	// Binary data has no text.
	if err != nil || got != "" {
		t.Fatalf("extract text = %q, %v", got, err)
	}
	encoded := []byte("charset=windows-1252\ncaf\xe9")
	got, err = ExtractText(context.Background(), Node{Name: "legacy.txt", MIME: "text/plain", Size: int64(len(encoded))}, encoded)
	if err != nil || !strings.Contains(got, "café") {
		t.Fatalf("declared charset extraction = %q, %v", got, err)
	}
	if _, err = ExtractText(context.Background(), Node{Name: "readme.txt", MIME: "text/plain", Size: MaxSourceText + 1}, nil); err == nil {
		t.Fatal("oversized source was accepted")
	}
	if _, err = ExtractText(context.Background(), Node{Name: "archive.zip", MIME: "application/zip"}, []byte("not text")); !errors.Is(err, ErrNotIndexable) {
		t.Fatalf("unsupported file error = %v", err)
	}
	if _, err = ExtractText(context.Background(), Node{Name: "manual.pdf", MIME: "application/pdf"}, []byte("pdf")); err == nil {
		t.Fatal("invalid PDF was accepted")
	}
}

func TestExtractTextSniffsBOMlessUTF16(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order unicode.Endianness
	}{
		{name: "little endian", order: unicode.LittleEndian},
		{name: "big endian", order: unicode.BigEndian},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := unicode.UTF16(tc.order, unicode.IgnoreBOM).NewEncoder().Bytes([]byte("Résumé 2026"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := ExtractText(context.Background(), Node{Name: "memo.txt", MIME: "text/plain", Size: int64(len(encoded))}, encoded)
			if err != nil || got != "Résumé 2026" {
				t.Fatalf("BOM-less UTF-16 extraction = %q, %v", got, err)
			}
		})
	}
}

func TestExtractHTMLExcludesExecutableAndHiddenText(t *testing.T) {
	source := []byte(`<html><head><title>Allowed title</title><style>hidden-css</style></head><body><h1>Guide &amp; notes</h1><script>secret-token</script><p>Searchable body</p><template>hidden-template</template></body></html>`)
	got, err := ExtractText(context.Background(), Node{Name: "guide.html", MIME: "text/html", Size: int64(len(source))}, source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Guide & notes") || !strings.Contains(got, "Searchable body") || strings.Contains(got, "secret-token") || strings.Contains(got, "hidden-css") || strings.Contains(got, "hidden-template") {
		t.Fatalf("HTML text extraction = %q", got)
	}
}

func TestExtractChecksCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ExtractText(ctx, Node{Name: "a.txt", MIME: "text/plain", Size: 1}, []byte("a")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled extraction error = %v", err)
	}
}

func TestExtractPDFTextBounded(t *testing.T) {
	var source bytes.Buffer
	template := `{"pages":{"1":{"content":{"textfield":[{"id":"report","value":"NeoCloud report","pos":[10,10],"width":240,"height":24,"font":{"name":"Helvetica","size":12}},{"id":"summary","value":"Quarterly summary","pos":[10,40],"width":240,"height":24,"font":{"name":"Helvetica","size":12}}]}}}}`
	if err := api.Create(context.Background(), nil, strings.NewReader(template), &source, model.NewStatelessConfiguration()); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractText(context.Background(), Node{Name: "report.pdf", MIME: "application/pdf", Size: int64(source.Len())}, source.Bytes())
	if err != nil || !strings.Contains(text, "NeoCloud report") || !strings.Contains(text, "Quarterly summary") {
		t.Fatalf("PDF text extraction = %q, %v", text, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = ExtractText(ctx, Node{Name: "report.pdf", MIME: "application/pdf", Size: int64(source.Len())}, source.Bytes()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled PDF extraction error = %v", err)
	}
}

func TestExtractOfficeDocumentsWithinZipLimits(t *testing.T) {
	docx := makeOfficeFixture(t, map[string]string{
		"word/document.xml":     `<w:document xmlns:w="urn:w"><w:body><w:p><w:r><w:t>Quarterly report</w:t></w:r></w:p><w:p><w:r><w:t>Revenue outlook</w:t></w:r></w:p></w:body></w:document>`,
		"word/header1.xml":      `<w:hdr xmlns:w="urn:w"><w:p><w:r><w:t>Internal plan</w:t></w:r></w:p></w:hdr>`,
		"customXml/private.xml": `<data>ignored</data>`,
	})
	text, err := ExtractText(context.Background(), Node{Name: "report.docx", MIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Size: int64(len(docx))}, docx)
	if err != nil || !strings.Contains(text, "Quarterly report") || !strings.Contains(text, "Revenue outlook") || !strings.Contains(text, "Internal plan") || strings.Contains(text, "ignored") {
		t.Fatalf("DOCX extraction = %q, %v", text, err)
	}

	xlsx := makeOfficeFixture(t, map[string]string{
		"xl/sharedStrings.xml":     `<sst><si><t>Budget</t></si><si><r><t>North</t></r><r><t> region</t></r></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c t="s"><v>0</v></c><c t="s"><v>1</v></c><c t="inlineStr"><is><t>2026</t></is></c><c><v>42</v></c></row></sheetData></worksheet>`,
	})
	text, err = ExtractText(context.Background(), Node{Name: "forecast.xlsx", MIME: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Size: int64(len(xlsx))}, xlsx)
	if err != nil || !strings.Contains(text, "Budget") || !strings.Contains(text, "North region") || !strings.Contains(text, "2026") || !strings.Contains(text, "42") {
		t.Fatalf("XLSX extraction = %q, %v", text, err)
	}
}

func makeOfficeFixture(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
