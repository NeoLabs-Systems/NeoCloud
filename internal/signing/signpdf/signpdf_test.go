package signpdf

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	reader "github.com/giraffesyo/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

var ctx = context.Background()

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func nodeSigner(t *testing.T) *Signer {
	t.Helper()
	signer, err := LoadSigner(fixture(t, "node-seal.p12"), string(fixture(t, "node-seal.passphrase")), "Signed with NeoCloud", "NeoCloud")
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func texts(t *testing.T, data []byte) []string {
	t.Helper()
	doc, err := reader.Extract(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(doc.Pages))
	for i, p := range doc.Pages {
		out[i] = p.Text()
	}
	return out
}

// valid runs pdfcpu's validator over an output file.
func valid(t *testing.T, data []byte) {
	t.Helper()
	conf := model.NewDefaultConfiguration()
	conf.Offline = true
	if err := api.Validate(ctx, bytes.NewReader(data), conf, nil); err != nil {
		t.Fatalf("output is not a valid PDF: %v", err)
	}
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{20, 20, 90, uint8(128 + x%128)})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func area(x, y, w, h float64) []Area { return []Area{{Page: 0, X: x, Y: y, W: w, H: h}} }

func input(t *testing.T) FinalInput {
	return FinalInput{
		PDF: fixture(t, "form.pdf"),
		Parties: []Party{{ID: "p1", Name: "Alex Ünicode Łódź Ω Ж", Email: "alex@example.com", Color: "#D4A017",
			CompletedAt: "2026-11-02T10:00:00.000Z", IP: "192.0.2.1"}},
		Fields: []Field{
			{ID: "t", PartyID: "p1", Type: "text", Name: "Name", Areas: area(0.1, 0.5, 0.4, 0.04), Value: &Value{Data: "Alex Ünicode Łódź 漢"}},
			{ID: "d", PartyID: "p1", Type: "date", Name: "Date", Areas: area(0.1, 0.55, 0.3, 0.04), Value: &Value{Data: "2026-11-02"}},
			{ID: "n", PartyID: "p1", Type: "number", Name: "Sum", Prefs: Prefs{Format: "eur"}, Areas: area(0.1, 0.6, 0.3, 0.04), Value: &Value{Data: "1234567.5"}},
			{ID: "s", PartyID: "p1", Type: "signature", Name: "Signature", Areas: area(0.1, 0.65, 0.3, 0.08), Value: &Value{Image: pngBytes(t, 300, 100), Method: "draw"}},
			{ID: "c", PartyID: "p1", Type: "cells", Name: "Code", Prefs: Prefs{Cells: 4}, Areas: area(0.5, 0.5, 0.3, 0.04), Value: &Value{Data: "AB12"}},
			{ID: "x", PartyID: "p1", Type: "checkbox", Name: "Agree", Areas: area(0.5, 0.6, 0.03, 0.03), Value: &Value{Data: true}},
			{ID: "r", PartyID: "p1", Type: "radio", Name: "Pick", Prefs: Prefs{Options: []string{"a", "b"}},
				Areas: []Area{{X: 0.5, Y: 0.65, W: 0.03, H: 0.03}, {X: 0.6, Y: 0.65, W: 0.03, H: 0.03}}, Value: &Value{Data: "b"}},
			{ID: "h", Type: "heading", Name: "A heading", Areas: area(0.1, 0.45, 0.4, 0.03)},
			{ID: "f", PartyID: "p1", Type: "file", Name: "Proof", Areas: area(0.1, 0.8, 0.4, 0.04), Value: &Value{File: []byte("attached bytes"), FileName: "../proof.txt", Mime: "text/plain"}},
		},
		Events: []Event{{Type: "created", At: "2026-11-01T09:00:00.000Z", Hash: strings.Repeat("ab", 32)},
			{Type: "party_completed", At: "2026-11-02T10:00:00.000Z", PartyID: "p1", IP: "192.0.2.1", Hash: strings.Repeat("cd", 32)}},
		Document: Document{ID: "doc-1", Title: "Lease", SigningOrder: "preserved", CreatedAt: "2026-11-01T09:00:00.000Z",
			FinalizedAt: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC), ConsentVersion: 1},
		Locale: "en", Signer: nodeSigner(t),
	}
}

func TestNodeSealedFilesStayVerifiable(t *testing.T) {
	sealed := fixture(t, "node-sealed.pdf")
	if signed, intact := Verify(sealed); !signed || !intact {
		t.Fatalf("Node seal: signed=%v intact=%v", signed, intact)
	}
	changed := bytes.Replace(sealed, []byte("/Type /Page"), []byte("/Type /Pagf"), 1)
	if signed, intact := Verify(changed); !signed || intact {
		t.Fatalf("changed file: signed=%v intact=%v", signed, intact)
	}
	if _, intact := Verify(append(bytes.Clone(sealed), '\n')); intact {
		t.Fatal("appended bytes went unnoticed")
	}
	if signed, _ := Verify(fixture(t, "form.pdf")); signed {
		t.Fatal("an unsigned file reports a signature")
	}
}

func TestBuildIsSealedDeterministicAndReadable(t *testing.T) {
	in := input(t)
	first, hash, err := Build(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, err := Build(ctx, in)
	if err != nil || hash != secondHash || !bytes.Equal(first, second) {
		t.Fatalf("not deterministic: %v", err)
	}
	if signed, intact := Verify(first); !signed || !intact {
		t.Fatalf("signed=%v intact=%v", signed, intact)
	}
	valid(t, first)
	if out := os.Getenv("SIGNPDF_OUT"); out != "" {
		_ = os.WriteFile(out, first, 0o600)
	}
	pages := texts(t, first)
	if len(pages) < 3 {
		t.Fatalf("expected certificate pages, got %d", len(pages))
	}
	all := strings.Join(pages, "\n")
	for _, want := range []string{"Alex Ünicode Łódź ?", "2026-11-02", "1.234.567,5 €", "A heading", "Prefilled Value", "read only",
		"Signing certificate", "Certificate page 1 of", "proof.txt", "alex@example.com", "drawn", "Event log"} {
		if !strings.Contains(all, want) {
			t.Errorf("final file lacks %q", want)
		}
	}
	for _, gone := range []string{"app.alert", "/OpenAction", "/JavaScript", "NeedAppearances", "/FT /Tx"} {
		if bytes.Contains(first, []byte(gone)) {
			t.Errorf("final file still contains %q", gone)
		}
	}
	if probe, err := Probe(ctx, first); err != nil || len(probe.Pages) != len(pages) {
		t.Fatalf("probe of the output: %v", err)
	}
	if fields, err := ImportFields(ctx, first); err != nil || len(fields) != 1 || fields[0].Type != "signature" && len(fields) != 0 {
		// only the seal's own invisible widget remains, and it is too small to import
		if len(fields) != 0 {
			t.Fatalf("form fields survived: %+v %v", fields, err)
		}
	}

	in.NoCertificate = true
	clean, cleanHash, err := Build(ctx, in)
	if err != nil || cleanHash == hash {
		t.Fatalf("clean variant: %v", err)
	}
	if _, intact := Verify(clean); !intact {
		t.Fatal("clean variant is not sealed")
	}
	cleanText := strings.Join(texts(t, clean), "\n")
	if len(texts(t, clean)) != 2 || strings.Contains(cleanText, "Signing certificate") || !strings.Contains(cleanText, "1.234.567,5 €") {
		t.Fatal("clean variant content")
	}

	in.Document.OriginalSHA256 = strings.Repeat("0", 64)
	if _, _, err = Build(ctx, in); err == nil {
		t.Fatal("hash mismatch accepted")
	}
}

func TestGermanAndOwnCertificate(t *testing.T) {
	p12, passphrase, err := GenerateP12("NeoCloud document seal", 10)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := LoadSigner(p12, passphrase, "Signed with NeoCloud", "NeoCloud")
	if err != nil {
		t.Fatal(err)
	}
	in := input(t)
	in.Locale, in.Signer = "de", signer
	out, _, err := Build(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, intact := Verify(out); !intact {
		t.Fatal("not intact")
	}
	all := strings.Join(texts(t, out), "\n")
	for _, want := range []string{"Signaturprotokoll", "Ereignisprotokoll", "02.11.2026"} {
		if !strings.Contains(all, want) {
			t.Errorf("lacks %q", want)
		}
	}
	if _, err := LoadSigner(p12, "wrong", "", ""); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
}

func TestImportFields(t *testing.T) {
	fields, err := ImportFields(ctx, fixture(t, "form.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ImportedField{}
	for _, f := range fields {
		byName[f.Name] = f
	}
	for name, kind := range map[string]string{"full name": "text", "code": "cells", "notes": "text", "agree": "checkbox",
		"color": "radio", "country": "select", "tags": "multiple", "signature": "signature"} {
		if byName[name].Type != kind {
			t.Errorf("%s: got %q, want %q", name, byName[name].Type, kind)
		}
	}
	if _, ok := byName["locked"]; ok {
		t.Error("a read-only field was imported")
	}
	radio := byName["color"]
	if len(radio.Areas) != 2 || radio.Areas[0].Option == nil || *radio.Areas[0].Option != "red" {
		t.Errorf("radio areas: %+v", radio)
	}
	if options, _ := byName["country"].Prefs["options"].([]any); len(options) != 2 || options[1] != "Germany" {
		t.Errorf("select options: %+v", byName["country"].Prefs)
	}
	if byName["code"].Prefs["cells"] != float64(5) || byName["notes"].Prefs["multiline"] != true || !byName["notes"].Required {
		t.Errorf("text prefs: %+v %+v", byName["code"], byName["notes"])
	}
	a := byName["full name"].Areas[0]
	if a.X < 0.07 || a.X > 0.09 || a.Y < 0.14 || a.Y > 0.17 {
		t.Errorf("area: %+v", a)
	}
}

func TestProbeRefusalsAndRotation(t *testing.T) {
	info, err := Probe(ctx, fixture(t, "form.pdf"))
	if err != nil || !info.HasAcroForm || len(info.Pages) != 2 {
		t.Fatalf("%+v %v", info, err)
	}
	if p := info.Pages[1]; p.Rotate != 90 || p.Width != 700 || p.Height != 500 {
		t.Errorf("rotated page: %+v", p)
	}
	for name, data := range map[string][]byte{"empty": nil, "text": []byte("hello"), "garbage": []byte("%PDF-1.7\ngarbage")} {
		if _, err := Probe(ctx, data); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestImagesAndMarkdown(t *testing.T) {
	out, err := ImagePDF(ctx, pngBytes(t, 800, 400))
	if err != nil {
		t.Fatal(err)
	}
	valid(t, out)
	if info, err := Probe(ctx, out); err != nil || info.Pages[0].Width != 841.89 {
		t.Fatalf("%+v %v", info, err)
	}
	for _, name := range []string{"x.svg", "broken"} {
		if _, err := ImagePDF(ctx, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)); err == nil {
			t.Fatal(name, "accepted")
		}
	}
	doc, boxes, err := RenderMarkdown("Agreement", "# Terms\n\nSome **bold** and *slanted* text.\n\n- one\n2. two\n\n---\n"+strings.Repeat("Long paragraph. ", 800), []string{"Ann", "Ben"}, "en")
	if err != nil || len(boxes) != 2 {
		t.Fatal(err)
	}
	valid(t, doc)
	all := strings.Join(texts(t, doc), "\n")
	for _, want := range []string{"Agreement", "bold", "slanted", "Signatures", "Signature · Ben"} {
		if !strings.Contains(all, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
	appended, boxes, err := AppendSignaturePage(ctx, doc, []string{"Cleo"}, "en")
	if err != nil {
		t.Fatal(err)
	}
	valid(t, appended)
	info, _ := Probe(ctx, appended)
	if boxes[0].Signature.Page != len(info.Pages)-1 {
		t.Fatalf("page %d of %d", boxes[0].Signature.Page, len(info.Pages))
	}
}
