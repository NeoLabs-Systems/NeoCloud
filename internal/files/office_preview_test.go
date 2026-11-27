package files

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neocloud/internal/documentconvert"
	"github.com/google/uuid"
)

func minimalDOCX(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for name, value := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>NeoCloud conversion smoke test</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`,
	} {
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
	return out.Bytes()
}

func TestOfficePreviewConvertsAndCachesEncryptedPDF(t *testing.T) {
	if !documentconvert.Available() {
		t.Skip("LibreOffice is not installed")
	}
	f := newZIPFixture(t)
	f.addRoot(t, "root", 1)
	blobID := uuid.NewString()
	f.addFile(t, blobID, "root", 1, "contract.docx", minimalDOCX(t))
	if _, err := f.db.W.Exec(`UPDATE nodes SET mime='application/vnd.openxmlformats-officedocument.wordprocessingml.document' WHERE id=?`, blobID); err != nil {
		t.Fatal(err)
	}
	f.handler.previewDir = filepath.Join(t.TempDir(), "previews")
	mux := http.NewServeMux()
	f.handler.Routes(mux)
	request := httptest.NewRequest(http.MethodGet, "/api/files/"+blobID+"/preview", nil)
	request = request.WithContext(requestauth.WithIdentity(request.Context(), requestauth.RequestIdentity{UserID: 1}))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(response.Body.Bytes(), []byte("%PDF-")) {
		var body any
		_ = json.Unmarshal(response.Body.Bytes(), &body)
		t.Fatalf("preview response=%d headers=%v body=%#v", response.Code, response.Header(), body)
	}
	if got, want := response.Header().Get("ETag"), `"`+blobID+`-pdf"`; got == want || got == `"-pdf"` || got == "" {
		// ETags are based on the source node's random revision token, not node ID.
		t.Fatalf("unexpected preview ETag %q", got)
	}
	cachePath := previewPath(f.handler.previewDir, 1, blobID, officePreviewVariant)
	cache, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(cache, []byte("%PDF-")) || bytes.Contains(cache, response.Body.Bytes()) {
		t.Fatal("PDF preview cache contains plaintext")
	}
	request = httptest.NewRequest(http.MethodGet, "/api/files/"+blobID+"/preview", nil)
	request.Header.Set("If-None-Match", response.Header().Get("ETag"))
	request = request.WithContext(requestauth.WithIdentity(request.Context(), requestauth.RequestIdentity{UserID: 1}))
	response2 := httptest.NewRecorder()
	mux.ServeHTTP(response2, request)
	if response2.Code != http.StatusNotModified {
		t.Fatalf("conditional preview status=%d", response2.Code)
	}
	request = httptest.NewRequest(http.MethodHead, "/api/files/"+blobID+"/preview", nil)
	request = request.WithContext(requestauth.WithIdentity(request.Context(), requestauth.RequestIdentity{UserID: 1}))
	response3 := httptest.NewRecorder()
	mux.ServeHTTP(response3, request)
	if response3.Code != http.StatusOK || response3.Body.Len() != 0 {
		t.Fatalf("HEAD preview status=%d body=%d", response3.Code, response3.Body.Len())
	}
}

// A stand-in converter exercises the whole route without LibreOffice: shared
// access, one conversion per content, an encrypted cache and its headers.
func TestOfficePreviewRouteWithStandInConverter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	runs := filepath.Join(t.TempDir(), "runs")
	binary := filepath.Join(t.TempDir(), "soffice")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho run >> "+runs+"\nprintf '%%PDF-1.7\\nstand-in' > \"$HOME/document.pdf\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	documentconvert.Configure(documentconvert.Settings{Binary: binary, SandboxOff: true})
	t.Cleanup(func() { documentconvert.Configure(documentconvert.Settings{}) })
	f := newZIPFixture(t)
	f.addRoot(t, "root", 1)
	f.handler.previewDir = t.TempDir()
	id := previewFile(t, f, "contract.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("docx"))
	url := "/api/files/" + id + "/preview"
	first := getAs(f, 1, url)
	if first.Code != http.StatusOK || first.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(first.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("preview = %d %s", first.Code, first.Body)
	}
	if again := getAs(f, 1, url); again.Code != http.StatusOK || !bytes.Equal(again.Body.Bytes(), first.Body.Bytes()) {
		t.Fatalf("cached preview = %d", again.Code)
	}
	if conditional := getAs(f, 1, url, "If-None-Match", first.Header().Get("ETag")); conditional.Code != http.StatusNotModified {
		t.Fatalf("conditional = %d", conditional.Code)
	}
	if log, err := os.ReadFile(runs); err != nil || string(log) != "run\n" {
		t.Fatalf("conversions = %q, %v", log, err)
	}
	cache, err := os.ReadFile(previewPath(f.handler.previewDir, 1, id, officePreviewVariant))
	if err != nil || !bytes.HasPrefix(cache, []byte("NCB1")) {
		t.Fatalf("cache is not encrypted: %v", err)
	}
	if stranger := getAs(f, 2, url); stranger.Code != http.StatusNotFound {
		t.Fatalf("stranger = %d", stranger.Code)
	}
	documentconvert.Configure(documentconvert.Settings{Disabled: true})
	other := previewFile(t, f, "other.docx", "application/octet-stream", []byte("docx"))
	if missing := getAs(f, 1, "/api/files/"+other+"/preview"); missing.Code != http.StatusUnsupportedMediaType || !strings.Contains(missing.Body.String(), "Office previews need LibreOffice on the server.") {
		t.Fatalf("without LibreOffice = %d %s", missing.Code, missing.Body)
	}
}
