package documentconvert

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func setBinaryForTest(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "soffice")
	if runtime.GOOS == "windows" {
		t.Skip("test stand-in uses a POSIX executable")
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	Configure(Settings{Binary: path, SandboxOff: true})
	t.Cleanup(func() { Configure(Settings{}) })
	return path
}

func TestConvertToPDFUsesPrivateHardenedProfileAndFixedInputName(t *testing.T) {
	setBinaryForTest(t, `
test -n "$HOME"
test "$HOME" = "$TMPDIR"
test -f "$HOME/profile/user/registrymodifications.xcu"
test -f "$HOME/document.docx"
printf '%%PDF-1.7\nstand-in' > "$HOME/document.pdf"
`)
	got, err := ConvertToPDF(context.Background(), "../../private.docx", strings.NewReader("docx"))
	if err != nil || !bytes.HasPrefix(got, []byte("%PDF-")) {
		t.Fatalf("ConvertToPDF err=%v output=%q", err, got)
	}
}

func TestProfileTurnsOffLinksRemoteContentAndMacros(t *testing.T) {
	for _, setting := range []struct{ item, name, value string }{
		{"/org.openoffice.Office.Common/Security/Scripting", "MacroSecurityLevel", "3"},
		{"/org.openoffice.Office.Common/Security/Scripting", "DisableMacrosExecution", "true"},
		{"/org.openoffice.Office.Common/Security/Scripting", "BlockUntrustedRefererLinks", "true"},
		{"/org.openoffice.Office.Writer/Content/Update", "Link", "0"},
		{"/org.openoffice.Office.Calc/Content/Update", "Link", "1"},
	} {
		want := `<item oor:path="` + setting.item + `"><prop oor:name="` + setting.name + `" oor:op="fuse"><value>` + setting.value + `</value>`
		if !strings.Contains(string(profileSettings), want) {
			t.Errorf("missing %s %s=%s", setting.item, setting.name, setting.value)
		}
	}
}

func TestConverterGetsAMinimalEnvironment(t *testing.T) {
	setBinaryForTest(t, `
test -z "$NEOCLOUD_MASTER_KEY$SESSION_SECRET" || exit 9
printf '%%PDF-1.7\nstand-in' > "$HOME/document.pdf"
`)
	t.Setenv("NEOCLOUD_MASTER_KEY", strings.Repeat("a", 64))
	t.Setenv("SESSION_SECRET", "secret")
	if _, err := ConvertToPDF(context.Background(), "a.docx", strings.NewReader("docx")); err != nil {
		t.Fatalf("the converter saw server secrets or failed: %v", err)
	}
}

func TestTimeoutKillsTheWholeProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	setBinaryForTest(t, `sleep 30 & echo $! > `+pidFile+`; wait`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Cancel once the grandchild exists, however long the stand-in takes to start.
	go func() {
		for ctx.Err() == nil {
			if raw, err := os.ReadFile(pidFile); err == nil && strings.HasSuffix(string(raw), "\n") {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	if _, err := ConvertToPDF(ctx, "a.docx", strings.NewReader("docx")); err == nil {
		t.Fatal("a converter that never finishes succeeded")
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	// Orphans are reaped by init shortly after the kill.
	deadline := time.Now().Add(3 * time.Second)
	for {
		process, err := os.FindProcess(pid)
		if err != nil || process.Signal(syscall.Signal(0)) != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the background child survived the timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDisabledConversionIsUnavailable(t *testing.T) {
	setBinaryForTest(t, "exit 0")
	Configure(Settings{Disabled: true})
	if Available() {
		t.Fatal("disabled conversion is reported available")
	}
	if _, err := ConvertToPDF(context.Background(), "a.docx", strings.NewReader("docx")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestBubblewrapMakesHostReadOnlyAndOnlyWorkWritable(t *testing.T) {
	args := bubblewrapArgs("/usr/bin/soffice", "/tmp/neocloud-office-test")
	want := []string{"--ro-bind", "/", "/", "--dev", "/dev", "--bind", "/tmp/neocloud-office-test", "/tmp/neocloud-office-test", "--unshare-net", "--die-with-parent", "--", "/usr/bin/soffice"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("bubblewrap args = %#v, want %#v", args, want)
	}
}

func TestConvertToPDFRejectsUnsupportedAndOversizedInput(t *testing.T) {
	setBinaryForTest(t, "exit 99")
	if _, err := ConvertToPDF(context.Background(), "file.exe", strings.NewReader("data")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported extension err=%v", err)
	}
	if _, err := ConvertToPDF(context.Background(), "file.doc", strings.NewReader(strings.Repeat("x", int(MaxSourceBytes+1)))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized source err=%v", err)
	}
}

func TestConvertToPDFStopsOnContextCancellation(t *testing.T) {
	setBinaryForTest(t, "sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := ConvertToPDF(ctx, "file.docx", strings.NewReader("docx"))
	if err == nil || time.Since(started) > 5*time.Second {
		t.Fatalf("cancellation err=%v elapsed=%s", err, time.Since(started))
	}
}

func TestLibreOfficeConvertsMinimalDOCXWhenInstalled(t *testing.T) {
	if !Available() {
		t.Skip("LibreOffice is not installed")
	}
	var input bytes.Buffer
	zw := zip.NewWriter(&input)
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>NeoCloud conversion smoke test</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`,
	}
	for name, value := range files {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	pdf, err := ConvertToPDF(ctx, "sample.docx", bytes.NewReader(input.Bytes()))
	if err != nil {
		t.Fatalf("real LibreOffice conversion failed: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("output = %.20q", pdf)
	}
}
