package ai

import (
	"slices"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/imaging"
)

func TestCandidateHeuristic(t *testing.T) {
	meaningless := map[string]string{
		"IMG_2041.jpg": "camera", "PXL_20260312_142233512.jpg": "camera", "DSC01234.JPG": "camera", "20260312_142233.jpg": "camera",
		"WhatsApp Image 2026-03-12 at 14.22.33.jpeg": "camera", "Screenshot 2026-03-12 at 14.22.33.png": "screenshot",
		"Bildschirmfoto 2026-03-12 um 14.22.33.png": "screenshot", "Scan_20260312.pdf": "scan", "scan0001.pdf": "scan", "SCN_0042.pdf": "scan",
		"doc00123.pdf": "scan", "3f2a9c1e-1b2c-4d5e-8f90-123456789abc.pdf": "", "a8f3e9c2b1d4e5f6.pdf": "", "x7Kq9mZ2pL4vB8nR3t.pdf": "",
		"1710252153123.pdf": "", "2026-03-12 14.22.33.pdf": "", "download (3).pdf": "", "document.pdf": "", "document (2).pdf": "",
		"untitled.txt": "", "Untitled document.docx": "", "Unbenannt-1.docx": "", "invoice.pdf": "",
	}
	for name, kind := range meaningless {
		class := classifyName(name)
		if !class.meaningless || kind == "camera" && !class.camera || kind == "screenshot" && !class.screenshot || kind == "scan" && !class.scan {
			t.Errorf("%s: %+v", name, class)
		}
	}
	for _, name := range []string{"Rechnung Telekom März.pdf", "Holiday plan.docx", "project-notes-v2.md", "Bericht 2026.pdf", "2026-03-12 Telekom – Rechnung.pdf", "README"} {
		if classifyName(name).meaningless {
			t.Errorf("%s flagged as meaningless", name)
		}
	}
}

func TestNamesFromTemplate(t *testing.T) {
	amount := 39.99
	full := fields{Date: "2026-03-12", Issuer: "Telekom", Subject: "Mobilfunk März", Type: "invoice", Amount: &amount, Currency: "eur"}
	standard := "{date} {issuer} – {subject}"
	for _, c := range []struct {
		template string
		f        fields
		original string
		want     string
	}{
		{standard, full, "scan0001.pdf", "2026-03-12 Telekom – Mobilfunk März.pdf"},
		{"{date} {issuer} {type} {amount}{currency}", full, "x.pdf", "2026-03-12 Telekom invoice 39.99EUR.pdf"},
		{"{year}-{month} {subject} ({original})", full, "IMG_1.JPG", "2026-03 Mobilfunk März (IMG_1).JPG"},
		{standard, fields{Subject: "Mietvertrag"}, "doc1.pdf", "Mietvertrag.pdf"},
		{standard, fields{Date: "2026-03-12", Issuer: "Telekom"}, "doc1.pdf", "2026-03-12 Telekom.pdf"},
		{"{subject} [{amount}]", fields{Subject: "Quittung"}, "a.pdf", "Quittung.pdf"},
		{standard, fields{}, "doc1.pdf", ""},
		{"nothing here", full, "a.pdf", "2026-03-12 Telekom – Mobilfunk März.pdf"},
		{"{subject}", fields{Subject: "Notes"}, "README", "Notes"},
		{"{subject}", fields{Subject: `a/b\c: d*e?f"g<h>i|j`}, "x.txt", "a-b-c- defghij.txt"},
		{"{issuer} {subject}", fields{Issuer: "..hidden", Subject: "trail..."}, "x.txt", "hidden trail.txt"},
		{"{subject}", fields{Subject: "CON"}, "x.txt", "CON_.txt"},
		{"{subject}", fields{Subject: "line\nbreak\ttab"}, "x.txt", "line break tab.txt"},
	} {
		if got := renderName(c.template, c.f, c.original); got != c.want {
			t.Errorf("%q %q = %q, want %q", c.template, c.original, got, c.want)
		}
	}
	long := renderName("{subject}", fields{Subject: strings.Repeat("Wort ", 60)}, "x.pdf")
	if len(long) > 120 || !strings.HasSuffix(long, ".pdf") || strings.HasSuffix(long, " .pdf") {
		t.Error(long)
	}
	if sanitizeFileName("   ", ".txt") != "" {
		t.Error("empty stem")
	}
	if emoji := sanitizeFileName(strings.Repeat("🙂", 200), ".txt"); len(emoji) > 255 || !strings.HasSuffix(emoji, ".txt") {
		t.Error(emoji)
	}
}

func TestDatePrecedence(t *testing.T) {
	for _, c := range []struct{ content, exif, name, date, source string }{
		{"2026-03-12", "2025-05-05", "IMG_20240101.jpg", "2026-03-12", "content"},
		{"12.03.2026", "", "", "2026-03-12", "content"},
		{"2099-01-01", "2025-05-05", "", "2025-05-05", "exif"},
		{"1850-01-01", "", "scan_2024-01-31.pdf", "2024-01-31", "filename"},
		{"2026-02-30", "", "PXL_20240229_101010.jpg", "2024-02-29", "filename"},
		{"soon", "", "download.pdf", "2026-04-01", "modified"},
	} {
		date, source := chooseDate(c.content, c.exif, c.name, "2026-04-01 10:00:00", "2026-04-02 10:00:00")
		if date != c.date || source != c.source {
			t.Errorf("%+v = %s %s", c, date, source)
		}
	}
	if date, source := chooseDate("", "", "x", "", ""); date != "" || source != "" {
		t.Error(date, source)
	}
	for name, want := range map[string]string{"IMG_20260312_101010.jpg": "2026-03-12", "Scan 2026_03_12.pdf": "2026-03-12", "IMG_2041.jpg": "", "20261340.pdf": ""} {
		if got := dateFromName(name); got != want {
			t.Errorf("%s = %s", name, got)
		}
	}
}

func TestModelAnswerIsCheckedAndCleaned(t *testing.T) {
	n := node{Name: "scan0001.pdf", ModifiedAt: "2026-04-01 10:00:00", CreatedAt: "2026-04-02 10:00:00"}
	text := "Telekom Deutschland GmbH, Rechnung vom 12.03.2026, Betrag 39,99 EUR"
	confidence := map[string]any{"date": 0.9, "issuer": 0.9, "subject": 0.9, "type": 0.9, "amount": 0.9, "tags": 0.9}
	f := normalizeExtraction(map[string]any{
		"date": "2026-03-12", "issuer": "Telekom", "subject": "  Mobilfunk\nMärz ", "type": "Invoice", "amount": "39,99", "currency": "eur",
		"tags": []any{"Telefon", "telefon", "<b>x</b>"}, "language": "DE", "confidence": confidence, "overall": 2.0,
	}, &text, n, imaging.Exif{})
	if f.Issuer != "Telekom" || f.Subject != "Mobilfunk März" || f.Type != "invoice" || f.Amount == nil || *f.Amount != 39.99 || f.Currency != "EUR" ||
		!slices.Equal(f.Tags, []string{"telefon", "bxb"}) || f.Language != "de" || f.Overall != 1 || f.Confidence["issuer"] != 0.9 || f.DateSource != "content" {
		t.Errorf("%+v", f)
	}
	doubtful := normalizeExtraction(map[string]any{"issuer": "Vodafone", "amount": 77.0, "type": "poem", "confidence": confidence}, &text, n, imaging.Exif{TakenAt: "2025-05-05"})
	if doubtful.Confidence["issuer"] >= 0.9 || doubtful.Confidence["amount"] >= 0.9 || doubtful.Type != "other" || doubtful.DateSource != "exif" || doubtful.Confidence["date"] != 0.9 {
		t.Errorf("%+v", doubtful)
	}
}

func TestFolderNamesAcrossLanguagesAndForms(t *testing.T) {
	for _, pair := range [][2]string{{"Invoices", "01_Rechnungen"}, {"Photos", "Bilder"}, {"Dokument", "Dokumente"}, {"Package", "Packages"}, {"Verträge", "contracts"}} {
		if folderKey(pair[0]) != folderKey(pair[1]) {
			t.Errorf("%q and %q: %q, %q", pair[0], pair[1], folderKey(pair[0]), folderKey(pair[1]))
		}
	}
	if folderLanguage([]string{"Fotos", "Arbeit"}) != "de" || folderLanguage([]string{"Work", "Photos"}) != "en" || folderLanguage([]string{"Xy"}) != "" {
		t.Error("folder language")
	}
	if !isInboxName("Downloads") || !isInboxName("Eingang") || !isMediaName("Camera Uploads") || !isMonthName("03 März", 3) || !isMonthName("2026-03", 3) || isMonthName("13", 3) {
		t.Error("inbox, media or month")
	}
	for raw, want := range map[string]string{"invoices": "Invoices", "Invoice 2026": "", "one two three four": "", "a/b": "Ab", "..": ""} {
		if got := cleanNewFolderName(raw); got != want {
			t.Errorf("%q = %q", raw, got)
		}
	}
}

func TestGlobIsLinear(t *testing.T) {
	if !globMatches("Rech*.p?f", "Rechnung.PDF") || globMatches("a*b", "abc") || globMatches(strings.Repeat("*a", 60)+"b", strings.Repeat("a", 5000)) {
		t.Error("glob")
	}
}
