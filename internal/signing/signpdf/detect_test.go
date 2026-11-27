package signpdf

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Fixtures are made here: a typed German form in a standard font (one byte
// per code, WinAnsi, no widths in the file), the same layout drawn with the
// bundled font (two bytes per code and a ToUnicode map) on pages with and
// without /Rotate, NeoCloud's own signature page and the AcroForm fixture.

// typedForm is a German form written with Helvetica and underscores.
func typedForm() []byte {
	content := strings.Join([]string{
		"BT /F1 14 Tf 72 760 Td (Mietvertrag) Tj ET",
		"72 756 m 160 756 l S", // the heading is underlined: no field there
		"BT /F1 10 Tf 72 700 Td (Name: ______________________________) Tj ET",
		"BT /F1 10 Tf 72 660 Td (Ort, Datum: ______________________________) Tj ET",
		"BT /F1 10 Tf 72 600 Td (Dieser Vertrag wird mit der Unterschrift beider Seiten wirksam und gilt ab heute.) Tj ET",
		"0.8 w 72 540 m 272 540 l S",
		"BT /F1 8 Tf 72 529 Td (Unterschrift des Mieters) Tj ET",
		"330 540 m 480 540 l S",
		"BT /F1 8 Tf 330 529 Td (Datum) Tj ET",
		"72 480 11 11 re S",
		"BT /F1 10 Tf 90 481 Td (Ich stimme den Bedingungen zu) Tj ET",
		"BT /F1 10 Tf 72 440 Td (Initialen:) Tj ET",
		"BT /F1 10 Tf 72 400 Td (Telefon \\(privat\\): ____________________) Tj ET",
		"BT /F1 10 Tf 72 360 Td (\xfcber uns) Tj ET",
	}, "\n")
	return assemble(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	)
}

// drawnForm draws a form with the bundled font on a page of the given media
// size and /Rotate. Coordinates are those of the displayed page (origin
// bottom-left), so it reads upright whatever the rotation.
func drawnForm(t *testing.T, rotate int, width, height float64) []byte {
	t.Helper()
	doc := emptyDocument()
	p := doc.newPage(width, height)
	p.rotate, p.dict["Rotate"] = rotate, types.Integer(rotate)
	faces := newFonts(doc)
	c := newCanvas()
	ink := colour{0, 0, 0}
	_, h := p.size()
	top := h - 60
	c.text(faces.bold, 50, top, 14, ink, "Vereinbarung", 0)
	c.text(faces.regular, 50, top-50, 10, ink, "Datum:", 0)
	c.line(90, top-52, 230, top-52, 0.6, ink)
	c.line(50, top-120, 250, top-120, 0.8, ink)
	c.text(faces.regular, 50, top-131, 8, ink, "Unterschrift", 0)
	c.line(300, top-120, 480, top-120, 0.8, ink)
	c.text(faces.regular, 300, top-131, 8, ink, "Name in Druckbuchstaben", 0)
	c.strokeRect(rect{50, top - 180, 10, 10}, ink, 0.8)
	c.text(faces.regular, 66, top-179, 10, ink, "Newsletter abonnieren", 0)
	doc.apply(p, c, true)
	if err := faces.finish(doc); err != nil {
		t.Fatal(err)
	}
	out, _, err := doc.serialize(producerInfo(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func detect(t *testing.T, data []byte) []ImportedField {
	t.Helper()
	fields, err := DetectFields(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	return fields
}

func describe(fields []ImportedField) string {
	var out []string
	for _, f := range fields {
		a := f.Areas[0]
		out = append(out, fmt.Sprintf("%s/%s %q %.2f p%d [%.3f %.3f %.3f %.3f]", f.Type, f.Role, f.Name, f.Confidence, a.Page, a.X, a.Y, a.W, a.H))
	}
	return strings.Join(out, "\n")
}

// expect is a field the detector must find: its role, name and box in points
// of the displayed page (x, y from the top-left, width, height).
type expect struct {
	role, name string
	x, y, w, h float64
}

// match pins precision and recall: every expected field is found once, with
// its type and a box that covers most of the expected one, and nothing else.
func match(t *testing.T, fields []ImportedField, page int, width, height float64, want []expect) {
	t.Helper()
	used := map[int]bool{}
	for _, w := range want {
		found := -1
		for i, f := range fields {
			if used[i] || f.Role != w.role || f.Areas[0].Page != page {
				continue
			}
			a := f.Areas[0]
			got := box{a.X * width, a.Y * height, a.W * width, a.H * height}
			exp := box{w.x, w.y, w.w, w.h}
			if got.overlap(exp) >= 0.7*math.Min(got.area(), exp.area()) && math.Abs(got.x-exp.x) < 6 && math.Abs(got.bottom()-exp.bottom()) < 4 {
				found = i
				break
			}
		}
		if found < 0 {
			t.Errorf("not found: %+v\nfound:\n%s", w, describe(fields))
			continue
		}
		used[found] = true
		if f := fields[found]; f.Name != w.name || f.Type != fieldType(w.role) || f.Source != "layout" {
			t.Errorf("%s: got %s %q from %s", w.role, f.Type, f.Name, f.Source)
		}
	}
	for i, f := range fields {
		if !used[i] && f.Areas[0].Page == page && f.Source == "layout" {
			t.Errorf("unexpected: %s", describe([]ImportedField{f}))
		}
	}
}

func TestDetectTypedGermanForm(t *testing.T) {
	fields := detect(t, typedForm())
	// Helvetica underscores are 5.56 pt; the labels end where they start.
	nameX := 72 + 5.56*0 + textWidth("Name: ")
	placeX := 72 + textWidth("Ort, Datum: ")
	lineEnd := func(x float64, n int) float64 { return x + 5.56*float64(n) }
	placeLine := lineEnd(placeX, 30) - placeX
	placeW := (placeLine - 6) * 1 / 1.9
	match(t, fields, 0, 595, 842, []expect{
		{"name", "Name", nameX, 842 - 701 - 16, lineEnd(nameX, 30) - nameX, 16},
		{"place", "Ort", placeX, 842 - 661 - 16, placeW, 16},
		{"date", "Datum", placeX + placeW + 6, 842 - 661 - 16, placeLine - placeW - 6, 16},
		{"signature", "Unterschrift des Mieters", 72, 842 - 540 - 32, 200, 32},
		{"date", "Datum", 330, 842 - 540 - 14, 140, 14},
		{"checkbox", "Ich stimme den Bedingungen zu", 72, 842 - 491, 11, 11},
		{"initials", "Initialen", 72 + textWidth("Initialen:") + 6, 842 - 440 + 2.5 - 24, 80, 24},
		{"text", "Telefon (privat)", 72 + textWidth("Telefon (privat): "), 842 - 401 - 16, 5.56 * 20, 16},
	})
	for _, f := range fields {
		if f.Confidence < 0.5 || f.Confidence > 1 {
			t.Errorf("confidence %v: %s", f.Confidence, describe([]ImportedField{f}))
		}
	}
}

// textWidth measures Helvetica at 10 pt.
func textWidth(s string) float64 {
	w := 0.0
	for _, r := range s {
		w += standardWidth("Helvetica", int(r))
	}
	return w / 100
}

func TestDetectFollowsRotatedPages(t *testing.T) {
	for _, c := range []struct {
		rotate        int
		width, height float64
	}{{0, 595, 842}, {90, 842, 595}, {180, 595, 842}, {270, 842, 595}} {
		t.Run(fmt.Sprint(c.rotate), func(t *testing.T) {
			data := drawnForm(t, c.rotate, c.width, c.height)
			info, err := Probe(ctx, data)
			if err != nil || info.Pages[0].Width != 595 || info.Pages[0].Rotate != c.rotate {
				t.Fatalf("%+v %v", info, err)
			}
			top := 842 - 60.0
			match(t, detect(t, data), 0, 595, 842, []expect{
				{"date", "Datum", 90, 842 - (top - 52) - 16, 140, 16},
				{"signature", "Unterschrift", 50, 842 - (top - 120) - 32, 200, 32},
				{"name", "Name in Druckbuchstaben", 300, 842 - (top - 120) - 16, 180, 16},
				{"checkbox", "Newsletter abonnieren", 50, 842 - (top - 170), 10, 10},
			})
		})
	}
}

func TestDetectSidewaysTextUntilRotated(t *testing.T) {
	conf := model.NewDefaultConfiguration()
	conf.Offline = true
	var sideways bytes.Buffer
	if err := api.Rotate(ctx, bytes.NewReader(typedForm()), &sideways, 90, nil, conf); err != nil {
		t.Fatal(err)
	}
	for _, f := range detect(t, sideways.Bytes()) {
		if f.Role != "checkbox" {
			t.Errorf("sideways text gave a field: %s", describe([]ImportedField{f}))
		}
	}
	upright, info, err := Rotate(ctx, sideways.Bytes(), map[int]int{0: -1})
	if err != nil || info.Pages[0].Rotate != 0 {
		t.Fatalf("%+v %v", info, err)
	}
	if got, want := describe(detect(t, upright)), describe(detect(t, typedForm())); got != want {
		t.Errorf("after turning back:\n%s\nwant:\n%s", got, want)
	}
}

func TestDetectSignaturePage(t *testing.T) {
	for _, locale := range []string{"en", "de"} {
		base, _, err := RenderMarkdown("Agreement", "Some terms.", nil, locale)
		if err != nil && base == nil {
			t.Fatal(err)
		}
		doc, boxes, err := AppendSignaturePage(ctx, base, []string{"Ann", "Ben"}, locale)
		if err != nil {
			t.Fatal(err)
		}
		fields := detect(t, doc)
		page := boxes[0].Signature.Page
		var want []expect
		labels := signLabels[locale]
		for i, b := range boxes {
			pt := func(a Area) (float64, float64, float64, float64) {
				return a.X * a4.short, a.Y * a4.long, a.W * a4.short, a.H * a4.long
			}
			x, y, w, h := pt(b.Signature)
			want = append(want, expect{"signature", labels[1] + " · " + []string{"Ann", "Ben"}[i], x, y + h + 4 - 32, w, 32})
			x, y, w, h = pt(b.Name)
			want = append(want, expect{"name", labels[2], x, y + h + 2 - 16, w, 16})
			x, y, w, h = pt(b.Date)
			want = append(want, expect{"date", labels[3], x, y + h + 2 - 16, w, 16})
		}
		match(t, fields, page, a4.short, a4.long, want)
	}
}

func TestDetectKeepsFormFieldsAndReadsTheirLabels(t *testing.T) {
	fields := detect(t, fixture(t, "form.pdf"))
	byName := map[string]ImportedField{}
	for _, f := range fields {
		if f.Source == "form" {
			byName[f.Name] = f
		}
	}
	for name, role := range map[string]string{"full name": "name", "agree": "checkbox", "color": "radio", "signature": "signature", "notes": "text"} {
		if f, ok := byName[name]; !ok || f.Role != role || f.Confidence != 1 {
			t.Errorf("%s: %+v", name, f)
		}
	}
	for i, f := range fields {
		for j, g := range fields {
			if i < j && AreasOverlap(f.Areas, g.Areas) {
				t.Errorf("overlap: %s", describe([]ImportedField{f, g}))
			}
		}
	}
}

func TestLabelRoles(t *testing.T) {
	for label, want := range map[string]string{
		"Signature:":                     "signature",
		"Unterschrift des Auftraggebers": "signature",
		"Kundenunterschrift":             "signature",
		"(Unterschrift)":                 "signature",
		"Datum der Unterschrift":         "date",
		"Date of signature":              "date",
		"Unterschriftsdatum":             "date",
		"Ort, Datum":                     "place date",
		"Ort/Datum/Unterschrift":         "place date signature",
		"Datum, Unterschrift":            "date signature",
		"Vor- und Nachname":              "name",
		"Name, Vorname":                  "name",
		"Printed name":                   "name",
		"Initials":                       "initials",
		"Namenskürzel":                   "initials",
		"Geburtsdatum":                   "text",
		"Company name":                   "text",
		"Firmenname":                     "",
		"Updated":                        "",
		"Datenschutz":                    "",
		"By signing this agreement you confirm that you have read all of the terms": "",
	} {
		var got []string
		for _, r := range labelRoles(label) {
			got = append(got, r.role)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%q: got %v, want %q", label, got, want)
		}
	}
}

func TestEncodingTables(t *testing.T) {
	if macRoman[0x8A] != 'ä' || macRoman[0xA7] != 'ß' || macRoman[0xFF] != 'ˇ' || winAnsi[0xFC] != 'ü' || winAnsi[0x80] != '€' {
		t.Error("encoding tables are off")
	}
	if got := string(utf16Runes(pdfString{0x00, 0x55, 0x00, 0xfc})); got != "Uü" {
		t.Error(got)
	}
	cmap := parseToUnicode([]byte("1 begincodespacerange <0000> <FFFF> endcodespacerange 2 beginbfchar <0003> <0020> <0011> <00DF> endbfchar 1 beginbfrange <0020> <0022> <0041> endbfrange 1 beginbfrange <0030> <0031> [<0061> <0062>] endbfrange"))
	if string(cmap[3]) != " " || string(cmap[0x11]) != "ß" || string(cmap[0x22]) != "C" || string(cmap[0x31]) != "b" {
		t.Errorf("%v", cmap)
	}
}

func TestDetectHostileContent(t *testing.T) {
	// Deeply nested forms that draw themselves, and a stream of operators.
	loop := "q /X1 Do Q " + strings.Repeat("1 0 0 1 0 0 cm ", 1000)
	data := assemble(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /XObject << /X1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(loop), loop),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /XObject << /X1 5 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(loop), loop),
	)
	if fields := detect(t, data); len(fields) != 0 {
		t.Errorf("%s", describe(fields))
	}
	if _, err := DetectFields(ctx, []byte("%PDF-1.7 garbage")); err == nil {
		t.Error("garbage accepted")
	}
}

func TestRotateTurnsPagesAndAreas(t *testing.T) {
	out, info, err := Rotate(ctx, fixture(t, "form.pdf"), map[int]int{0: 1, 1: 1})
	if err != nil {
		t.Fatal(err)
	}
	valid(t, out)
	probe, _ := Probe(ctx, out)
	if fmt.Sprint(probe.Pages) != fmt.Sprint(info.Pages) || info.Pages[0].Rotate != 90 || info.Pages[1].Rotate != 180 || info.Pages[1].Width != 500 {
		t.Fatalf("%+v", info.Pages)
	}
	if !info.HasAcroForm {
		t.Error("the form was lost")
	}
	if _, _, err := Rotate(ctx, out, map[int]int{5: 1}); err == nil {
		t.Error("a missing page was rotated")
	}
	a := Area{Page: 0, X: 0.1, Y: 0.2, W: 0.3, H: 0.05}
	once := RotateArea(a, 1, 600, 800)
	// The centre (0.25, 0.225) goes to (0.775, 0.25); 180×40 pt stays 180×40 pt.
	if once.W != 0.225 || once.H != 0.06667 || math.Abs(once.X+once.W/2-0.775) > 1e-4 || math.Abs(once.Y+once.H/2-0.25) > 1e-4 {
		t.Errorf("one turn: %+v", once)
	}
	near := func(x, y Area) bool {
		return math.Abs(x.X-y.X) < 1e-4 && math.Abs(x.Y-y.Y) < 1e-4 && math.Abs(x.W-y.W) < 1e-4 && math.Abs(x.H-y.H) < 1e-4
	}
	if back := RotateArea(once, -1, 800, 600); !near(back, a) {
		t.Errorf("back: %+v", back)
	}
	if full := RotateArea(RotateArea(a, 2, 600, 800), 2, 600, 800); !near(full, a) {
		t.Errorf("two half turns: %+v", full)
	}
	edge := RotateArea(Area{X: 0.9, Y: 0, W: 0.1, H: 0.5}, 1, 600, 800)
	if edge.X < 0 || edge.Y < 0 || edge.X+edge.W > 1+1e-9 || edge.Y+edge.H > 1+1e-9 {
		t.Errorf("left the page: %+v", edge)
	}
}
