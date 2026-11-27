package files

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/gen2brain/gav1d/avif"
	"github.com/gen2brain/vpx/webp"
	"github.com/google/uuid"
)

func previewFile(t *testing.T, f *zipFixture, name, mime string, data []byte) string {
	t.Helper()
	id := uuid.NewString()
	f.addFile(t, id, "root", 1, name, data)
	if _, err := f.db.W.Exec(`UPDATE nodes SET mime=? WHERE id=?`, mime, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func getAs(f *zipFixture, user int64, url string, header ...string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	f.handler.Routes(mux)
	request := httptest.NewRequest(http.MethodGet, url, nil)
	if user > 0 {
		request = request.WithContext(requestauth.WithIdentity(request.Context(), requestauth.RequestIdentity{UserID: user}))
	}
	for i := 0; i+1 < len(header); i += 2 {
		request.Header.Set(header[i], header[i+1])
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestThumbnailSizesETagAndAVIF(t *testing.T) {
	f := newZIPFixture(t)
	f.addRoot(t, "root", 1)
	f.handler.previewDir = t.TempDir()
	var avifData bytes.Buffer
	if err := avif.Encode(&avifData, image.NewRGBA(image.Rect(0, 0, 40, 20))); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]image.Point{
		previewFile(t, f, "photo.png", "image/png", pngBytes(t, 1600, 900)): {X: 256, Y: 144},
		previewFile(t, f, "photo.avif", "image/avif", avifData.Bytes()):     {X: 40, Y: 20},
	} {
		response := getAs(f, 1, "/api/files/"+id+"/thumbnail?size=256")
		if response.Code != http.StatusOK {
			t.Fatalf("thumbnail = %d %s", response.Code, response.Body)
		}
		decoded, err := webp.Decode(bytes.NewReader(response.Body.Bytes()))
		if err != nil || decoded.Bounds().Size() != want {
			t.Fatalf("thumbnail size = %v, %v; want %v", boundsOf(decoded), err, want)
		}
		if again := getAs(f, 1, "/api/files/"+id+"/thumbnail?size=256", "If-None-Match", response.Header().Get("ETag")); again.Code != http.StatusNotModified {
			t.Fatalf("conditional = %d", again.Code)
		}
		if bad := getAs(f, 1, "/api/files/"+id+"/thumbnail?size=999"); bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "INVALID_SIZE") {
			t.Fatalf("bad size = %d %s", bad.Code, bad.Body)
		}
	}
	text := previewFile(t, f, "notes.txt", "text/plain", []byte("hello"))
	if response := getAs(f, 1, "/api/files/"+text+"/thumbnail"); response.Code != http.StatusUnsupportedMediaType || !strings.Contains(response.Body.String(), "No thumbnail for this file type.") {
		t.Fatalf("text thumbnail = %d %s", response.Code, response.Body)
	}
	if response := getAs(f, 1, "/api/files/"+text+"/preview"); response.Code != http.StatusUnsupportedMediaType || !strings.Contains(response.Body.String(), "No document preview") {
		t.Fatalf("text preview = %d %s", response.Code, response.Body)
	}
}

// A rendition is only for the owner and for accounts the file is shared with,
// also once it is cached.
func TestPreviewAuthorization(t *testing.T) {
	f := newZIPFixture(t)
	f.addRoot(t, "root", 1)
	f.handler.previewDir = t.TempDir()
	if _, err := f.db.W.Exec(`INSERT INTO users(id,username,password) VALUES(3,'stranger','x')`); err != nil {
		t.Fatal(err)
	}
	folder := uuid.NewString()
	if _, err := f.db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,etag) VALUES(?,1,'root','folder','Shared',0,?)`, folder, folder); err != nil {
		t.Fatal(err)
	}
	id := previewFile(t, f, "photo.png", "image/png", pngBytes(t, 8, 8))
	if _, err := f.db.W.Exec(`UPDATE nodes SET parent_id=? WHERE id=?`, folder, id); err != nil {
		t.Fatal(err)
	}
	url := "/api/files/" + id + "/thumbnail"
	if response := getAs(f, 1, url); response.Code != http.StatusOK {
		t.Fatalf("owner = %d %s", response.Code, response.Body)
	}
	for user, want := range map[int64]int{0: http.StatusUnauthorized, 2: http.StatusNotFound, 3: http.StatusNotFound} {
		if response := getAs(f, user, url); response.Code != want {
			t.Fatalf("user %d before sharing = %d %s", user, response.Code, response.Body)
		}
	}
	if _, err := f.db.W.Exec(`INSERT INTO user_shares(node_id,owner_id,grantee_id,permission,recipient) VALUES(?,1,2,'read','zip-member')`, folder); err != nil {
		t.Fatal(err)
	}
	if response := getAs(f, 2, url); response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("grantee = %d %s", response.Code, response.Body)
	}
	if response := getAs(f, 3, url); response.Code != http.StatusNotFound {
		t.Fatalf("stranger = %d %s", response.Code, response.Body)
	}
	if _, err := f.db.W.Exec(`UPDATE nodes SET trashed_at=datetime('now') WHERE id=?`, folder); err != nil {
		t.Fatal(err)
	}
	for _, user := range []int64{1, 2} {
		if response := getAs(f, user, url); response.Code != http.StatusNotFound {
			t.Fatalf("user %d after trashing = %d", user, response.Code)
		}
	}
}

func TestGridThumbnailAheadOfTimeConcurrentRendersAndPurge(t *testing.T) {
	f := newZIPFixture(t)
	f.addRoot(t, "root", 1)
	f.handler.previewDir = t.TempDir()
	id := previewFile(t, f, "photo.png", "image/png", pngBytes(t, 40, 30))
	if err := f.handler.PrepareGridThumbnail(context.Background(), 1, id); err != nil {
		t.Fatal(err)
	}
	cached := previewPath(f.handler.previewDir, 1, id, squareThumbnailVariant)
	if data, err := os.ReadFile(cached); err != nil || !bytes.HasPrefix(data, []byte("NCB1")) {
		t.Fatalf("cached grid thumbnail: %v", err)
	}
	text := previewFile(t, f, "a.txt", "text/plain", []byte("x"))
	broken := previewFile(t, f, "b.png", "image/png", []byte("not a png"))
	for _, skipped := range []string{text, broken, "missing"} {
		if err := f.handler.PrepareGridThumbnail(context.Background(), 1, skipped); err != nil {
			t.Fatalf("%s: %v", skipped, err)
		}
	}
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if response := getAs(f, 1, "/api/files/"+id+"/thumbnail?size=1024"); response.Code != http.StatusOK {
				t.Errorf("concurrent = %d %s", response.Code, response.Body)
			}
		}()
	}
	group.Wait()
	f.handler.RemoveBlobFiles(1, []string{id})
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Fatalf("preview survives its blob: %v", err)
	}
}
