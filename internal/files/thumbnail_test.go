package files

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/vpx/webp"
	"github.com/google/uuid"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func TestThumbnailRendersAndCachesEncryptedSquarePNG(t *testing.T) {
	f := newZIPFixture(t)
	f.addRoot(t, "root", 1)
	img := image.NewRGBA(image.Rect(0, 0, 80, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{R: 220, G: 70, B: 40, A: 255})
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	blobID := uuid.NewString()
	f.addFile(t, blobID, "root", 1, "photo.png", source.Bytes())
	if _, err := f.db.W.Exec(`UPDATE nodes SET mime='image/png' WHERE id=?`, blobID); err != nil {
		t.Fatal(err)
	}
	f.handler.previewDir = filepath.Join(t.TempDir(), "previews")
	mux := http.NewServeMux()
	f.handler.Routes(mux)
	r := httptest.NewRequest(http.MethodGet, "/api/files/"+blobID+"/thumbnail?size=square", nil)
	r = r.WithContext(requestauth.WithIdentity(r.Context(), requestauth.RequestIdentity{UserID: 1}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("thumbnail response=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "private, max-age=86400" {
		t.Fatalf("unpinned thumbnail cache policy=%q", got)
	}
	var contentETag string
	if err := f.db.W.QueryRow(`SELECT etag FROM nodes WHERE id=?`, blobID).Scan(&contentETag); err != nil {
		t.Fatal(err)
	}
	pinned := httptest.NewRequest(http.MethodGet, "/api/files/"+blobID+"/thumbnail?size=square&v="+contentETag, nil)
	pinned = pinned.WithContext(requestauth.WithIdentity(pinned.Context(), requestauth.RequestIdentity{UserID: 1}))
	pinnedResponse := httptest.NewRecorder()
	mux.ServeHTTP(pinnedResponse, pinned)
	if pinnedResponse.Code != http.StatusOK || pinnedResponse.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Fatalf("pinned thumbnail response=%d cache=%q", pinnedResponse.Code, pinnedResponse.Header().Get("Cache-Control"))
	}
	stale := httptest.NewRequest(http.MethodGet, "/api/files/"+blobID+"/thumbnail?size=square&v=stale", nil)
	stale = stale.WithContext(requestauth.WithIdentity(stale.Context(), requestauth.RequestIdentity{UserID: 1}))
	staleResponse := httptest.NewRecorder()
	mux.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != http.StatusOK || staleResponse.Header().Get("Cache-Control") != "private, max-age=86400" {
		t.Fatalf("stale version thumbnail response=%d cache=%q", staleResponse.Code, staleResponse.Header().Get("Cache-Control"))
	}
	decoded, err := webp.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil || decoded == nil || decoded.Bounds().Dx() != squareThumbnailPixels || decoded.Bounds().Dy() != squareThumbnailPixels {
		t.Fatalf("thumbnail dimensions=%v err=%v", boundsOf(decoded), err)
	}
	cachePath := previewPath(f.handler.previewDir, 1, blobID, "thumb-square")
	cache, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cache, w.Body.Bytes()) {
		t.Fatal("thumbnail cache contains the plaintext rendition")
	}
	if _, err := f.db.W.Exec(`UPDATE nodes SET trashed_at=datetime('now') WHERE id=?`, blobID); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/files/"+blobID+"/thumbnail?size=square", nil)
	r = r.WithContext(requestauth.WithIdentity(r.Context(), requestauth.RequestIdentity{UserID: 1}))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("trashed thumbnail status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestThumbnailRendersWebPAfterBoundedDecodeConfig(t *testing.T) {
	f := newZIPFixture(t)
	f.handler.previewDir = t.TempDir()
	f.addRoot(t, "root", 1)
	source, err := os.ReadFile(filepath.Join("testdata", "thumbnail.webp"))
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil || format != "webp" || config.Width != 32 || config.Height != 16 {
		t.Fatalf("WebP config=%+v format=%q err=%v", config, format, err)
	}
	blobID := uuid.NewString()
	f.addFile(t, blobID, "root", 1, "photo.webp", source)
	if _, err = f.db.W.Exec(`UPDATE nodes SET mime='image/webp' WHERE id=?`, blobID); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	f.handler.Routes(mux)
	r := httptest.NewRequest(http.MethodGet, "/api/files/"+blobID+"/thumbnail?size=256", nil)
	r = r.WithContext(requestauth.WithIdentity(r.Context(), requestauth.RequestIdentity{UserID: 1}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("thumbnail response=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	decoded, err := webp.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil || decoded.Bounds().Dx() > 32 || decoded.Bounds().Dy() > 16 {
		t.Fatalf("thumbnail bounds=%v err=%v", boundsOf(decoded), err)
	}
}

func TestThumbnailRejectsOversizedWebPDimensionsBeforeDecode(t *testing.T) {
	source := webpExtendedConfig(20_000, 20_000)
	f := newZIPFixture(t)
	f.handler.previewDir = t.TempDir()
	f.addRoot(t, "root", 1)
	blobID := uuid.NewString()
	f.addFile(t, blobID, "root", 1, "oversized.webp", source)
	if _, err := f.db.W.Exec(`UPDATE nodes SET mime='image/webp' WHERE id=?`, blobID); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	f.handler.Routes(mux)
	r := httptest.NewRequest(http.MethodGet, "/api/files/"+blobID+"/thumbnail?size=256", nil)
	r = r.WithContext(requestauth.WithIdentity(r.Context(), requestauth.RequestIdentity{UserID: 1}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("oversized WebP status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestThumbnailRendersTIFFAfterBoundedDecodeConfig(t *testing.T) {
	f := newZIPFixture(t)
	f.handler.previewDir = t.TempDir()
	f.addRoot(t, "root", 1)
	source := encodeTIFF(t, image.NewRGBA(image.Rect(0, 0, 32, 16)))
	config, format, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil || format != "tiff" || config.Width != 32 || config.Height != 16 {
		t.Fatalf("TIFF config=%+v format=%q err=%v", config, format, err)
	}
	blobID := uuid.NewString()
	f.addFile(t, blobID, "root", 1, "photo.tiff", source)
	if _, err = f.db.W.Exec(`UPDATE nodes SET mime='image/tiff' WHERE id=?`, blobID); err != nil {
		t.Fatal(err)
	}
	w := requestThumbnail(t, f, blobID)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("thumbnail response=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	decoded, err := webp.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil || decoded.Bounds().Dx() > 32 || decoded.Bounds().Dy() > 16 {
		t.Fatalf("thumbnail bounds=%v err=%v", boundsOf(decoded), err)
	}
}

func TestThumbnailRendersBMPAfterBoundedDecodeConfig(t *testing.T) {
	f := newZIPFixture(t)
	f.handler.previewDir = t.TempDir()
	f.addRoot(t, "root", 1)
	var encoded bytes.Buffer
	if err := bmp.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 32, 16))); err != nil {
		t.Fatal(err)
	}
	source := encoded.Bytes()
	config, format, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil || format != "bmp" || config.Width != 32 || config.Height != 16 {
		t.Fatalf("BMP config=%+v format=%q err=%v", config, format, err)
	}
	blobID := uuid.NewString()
	f.addFile(t, blobID, "root", 1, "photo.bmp", source)
	if _, err = f.db.W.Exec(`UPDATE nodes SET mime='image/bmp' WHERE id=?`, blobID); err != nil {
		t.Fatal(err)
	}
	w := requestThumbnail(t, f, blobID)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("thumbnail response=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	decoded, err := webp.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil || decoded.Bounds().Dx() > 32 || decoded.Bounds().Dy() > 16 {
		t.Fatalf("thumbnail bounds=%v err=%v", boundsOf(decoded), err)
	}
}

func TestThumbnailRejectsOversizedBMPDimensionsBeforeDecode(t *testing.T) {
	var encoded bytes.Buffer
	if err := bmp.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	source := encoded.Bytes()
	if len(source) < 26 {
		t.Fatal("short BMP")
	}
	binary.LittleEndian.PutUint32(source[18:22], 20_000)
	binary.LittleEndian.PutUint32(source[22:26], 20_000)
	config, format, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil || format != "bmp" || config.Width != 20_000 || config.Height != 20_000 {
		t.Fatalf("oversized BMP config=%+v format=%q err=%v", config, format, err)
	}
	f := newZIPFixture(t)
	f.handler.previewDir = t.TempDir()
	f.addRoot(t, "root", 1)
	blobID := uuid.NewString()
	f.addFile(t, blobID, "root", 1, "oversized.bmp", source)
	if _, err = f.db.W.Exec(`UPDATE nodes SET mime='image/bmp' WHERE id=?`, blobID); err != nil {
		t.Fatal(err)
	}
	w := requestThumbnail(t, f, blobID)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("oversized BMP status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestThumbnailRendersHEICAndHEIFWithinPixelLimit(t *testing.T) {
	var encoded bytes.Buffer
	if err := heic.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 32, 16))); err != nil {
		t.Fatal(err)
	}
	source := encoded.Bytes()
	config, format, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil || format != "heic" || config.Width != 32 || config.Height != 16 {
		t.Fatalf("HEIC config=%+v format=%q err=%v", config, format, err)
	}

	for _, mime := range []string{"image/heic", "image/heif"} {
		t.Run(mime, func(t *testing.T) {
			f := newZIPFixture(t)
			f.handler.previewDir = t.TempDir()
			f.addRoot(t, "root", 1)
			blobID := uuid.NewString()
			f.addFile(t, blobID, "root", 1, "photo.heic", source)
			if _, err := f.db.W.Exec(`UPDATE nodes SET mime=? WHERE id=?`, mime, blobID); err != nil {
				t.Fatal(err)
			}
			w := requestThumbnail(t, f, blobID)
			if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/webp" {
				t.Fatalf("thumbnail response=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
			}
			decoded, err := webp.Decode(bytes.NewReader(w.Body.Bytes()))
			if err != nil || decoded.Bounds().Dx() > 32 || decoded.Bounds().Dy() > 16 {
				t.Fatalf("thumbnail bounds=%v err=%v", boundsOf(decoded), err)
			}
		})
	}
}

func TestThumbnailRejectsOversizedHEICDimensionsBeforeDecode(t *testing.T) {
	var encoded bytes.Buffer
	if err := heic.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	source := encoded.Bytes()
	ispe := bytes.Index(source, []byte("ispe"))
	if ispe < 0 || ispe+16 > len(source) {
		t.Fatal("encoded HEIC does not contain a complete ispe property")
	}
	binary.BigEndian.PutUint32(source[ispe+8:ispe+12], 20_000)
	binary.BigEndian.PutUint32(source[ispe+12:ispe+16], 20_000)
	config, err := heic.DecodeConfig(bytes.NewReader(source))
	if err != nil || config.Width != 20_000 || config.Height != 20_000 {
		t.Fatalf("oversized HEIC config=%+v err=%v", config, err)
	}
	f := newZIPFixture(t)
	f.handler.previewDir = t.TempDir()
	f.addRoot(t, "root", 1)
	blobID := uuid.NewString()
	f.addFile(t, blobID, "root", 1, "oversized.heic", source)
	if _, err := f.db.W.Exec(`UPDATE nodes SET mime='image/heic' WHERE id=?`, blobID); err != nil {
		t.Fatal(err)
	}
	w := requestThumbnail(t, f, blobID)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("oversized HEIC status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestThumbnailRejectsOversizedAndMalformedTIFF(t *testing.T) {
	oversized := encodeTIFF(t, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	setTIFFDimensions(t, oversized, 20_000, 20_000)
	config, err := tiff.DecodeConfig(bytes.NewReader(oversized))
	if err != nil || config.Width != 20_000 || config.Height != 20_000 {
		t.Fatalf("oversized TIFF config=%+v err=%v", config, err)
	}
	for _, tc := range []struct {
		name   string
		data   []byte
		status int
	}{
		{name: "pixel limit", data: oversized, status: http.StatusUnsupportedMediaType},
		{name: "malformed header", data: []byte("not a TIFF"), status: http.StatusUnsupportedMediaType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newZIPFixture(t)
			f.handler.previewDir = t.TempDir()
			f.addRoot(t, "root", 1)
			blobID := uuid.NewString()
			f.addFile(t, blobID, "root", 1, "input.tiff", tc.data)
			if _, err := f.db.W.Exec(`UPDATE nodes SET mime='image/tiff' WHERE id=?`, blobID); err != nil {
				t.Fatal(err)
			}
			w := requestThumbnail(t, f, blobID)
			if w.Code != tc.status {
				t.Fatalf("thumbnail response=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
		})
	}
}

func encodeTIFF(t *testing.T, source image.Image) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := tiff.Encode(&out, source, nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func setTIFFDimensions(t *testing.T, source []byte, width, height int) {
	t.Helper()
	if len(source) < 8 {
		t.Fatal("short TIFF")
	}
	var order binary.ByteOrder = binary.LittleEndian
	if bytes.Equal(source[:2], []byte("MM")) {
		order = binary.BigEndian
	} else if !bytes.Equal(source[:2], []byte("II")) {
		t.Fatal("unknown TIFF byte order")
	}
	ifd := int(order.Uint32(source[4:8]))
	if ifd < 8 || ifd+2 > len(source) {
		t.Fatal("invalid TIFF IFD offset")
	}
	count := int(order.Uint16(source[ifd : ifd+2]))
	for i := 0; i < count; i++ {
		entry := ifd + 2 + i*12
		if entry+12 > len(source) {
			t.Fatal("truncated TIFF IFD")
		}
		tag := order.Uint16(source[entry : entry+2])
		if tag != 256 && tag != 257 {
			continue
		}
		typ := order.Uint16(source[entry+2 : entry+4])
		value := width
		if tag == 257 {
			value = height
		}
		switch typ {
		case 3:
			order.PutUint16(source[entry+8:entry+10], uint16(value))
		case 4:
			order.PutUint32(source[entry+8:entry+12], uint32(value))
		default:
			t.Fatalf("unsupported TIFF dimension type %d", typ)
		}
	}
}

func requestThumbnail(t *testing.T, f *zipFixture, blobID string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	f.handler.Routes(mux)
	r := httptest.NewRequest(http.MethodGet, "/api/files/"+blobID+"/thumbnail?size=256", nil)
	r = r.WithContext(requestauth.WithIdentity(r.Context(), requestauth.RequestIdentity{UserID: 1}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func webpExtendedConfig(width, height int) []byte {
	const chunkSize = 8 + 10
	out := make([]byte, 12+chunkSize)
	copy(out[:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	copy(out[8:12], "WEBP")
	copy(out[12:16], "VP8X")
	binary.LittleEndian.PutUint32(out[16:20], 10)
	width--
	height--
	out[24], out[25], out[26] = byte(width), byte(width>>8), byte(width>>16)
	out[27], out[28], out[29] = byte(height), byte(height>>8), byte(height>>16)
	return out
}

func boundsOf(img image.Image) image.Rectangle {
	if img == nil {
		return image.Rectangle{}
	}
	return img.Bounds()
}
