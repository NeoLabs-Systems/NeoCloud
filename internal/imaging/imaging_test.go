package imaging

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gen2brain/gav1d/avif"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/vpx/webp"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// stripes is red on the left half and blue on the right half.
func stripes(width, height int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			if x < width/2 {
				out.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				out.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	return out
}

func encode(t *testing.T, write func(*bytes.Buffer) error) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := write(&out); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// exifBlock is a big-endian TIFF structure with an orientation, a camera make
// and a capture date.
func exifBlock(orientation int) []byte {
	const date = "2019:05:04 12:34:56\x00"
	out := []byte("MM\x00*\x00\x00\x00\x08")
	entry := func(tag, kind uint16, count, value uint32) {
		out = binary.BigEndian.AppendUint16(out, tag)
		out = binary.BigEndian.AppendUint16(out, kind)
		out = binary.BigEndian.AppendUint32(out, count)
		out = binary.BigEndian.AppendUint32(out, value)
	}
	out = binary.BigEndian.AppendUint16(out, 3)
	entry(0x010f, 2, 4, binary.BigEndian.Uint32([]byte("Neo\x00")))
	entry(0x0112, 3, 1, uint32(orientation)<<16)
	entry(0x8769, 4, 1, 50)
	out = binary.BigEndian.AppendUint32(out, 0)
	out = binary.BigEndian.AppendUint16(out, 1)
	entry(0x9003, 2, uint32(len(date)), 68)
	out = binary.BigEndian.AppendUint32(out, 0)
	return append(out, date...)
}

func jpegWithEXIF(t *testing.T, source image.Image, orientation int) []byte {
	plain := encode(t, func(b *bytes.Buffer) error { return jpeg.Encode(b, source, &jpeg.Options{Quality: 95}) })
	segment := append([]byte("Exif\x00\x00"), exifBlock(orientation)...)
	head := []byte{0xff, 0xd8, 0xff, 0xe1}
	head = binary.BigEndian.AppendUint16(head, uint16(len(segment)+2))
	return append(append(head, segment...), plain[2:]...)
}

func fixtures(t *testing.T) map[string][]byte {
	source := stripes(64, 32)
	return map[string][]byte{
		"jpeg": encode(t, func(b *bytes.Buffer) error { return jpeg.Encode(b, source, nil) }),
		"png":  encode(t, func(b *bytes.Buffer) error { return png.Encode(b, source) }),
		"webp": encode(t, func(b *bytes.Buffer) error { return webp.Encode(b, source) }),
		"tiff": encode(t, func(b *bytes.Buffer) error { return tiff.Encode(b, source, nil) }),
		"bmp":  encode(t, func(b *bytes.Buffer) error { return bmp.Encode(b, source) }),
		"gif": []byte("GIF89a\x02\x00\x01\x00\x80\x00\x00\xff\x00\x00\x00\x00\xff" +
			"!\xf9\x04\x00\x00\x00\x00\x00,\x00\x00\x00\x00\x02\x00\x01\x00\x00\x02\x02D\x0a\x00;"),
		"avif": encode(t, func(b *bytes.Buffer) error { return avif.Encode(b, source, avif.EncodeOptions{Quality: 90}) }),
		"heic": encode(t, func(b *bytes.Buffer) error { return heic.Encode(b, source, heic.EncodeOptions{Quality: 90}) }),
	}
}

func TestEveryRasterFormatDecodesAndReportsItsSize(t *testing.T) {
	for name, data := range fixtures(t) {
		t.Run(name, func(t *testing.T) {
			wantWidth, wantHeight := 64, 32
			if name == "gif" {
				wantWidth, wantHeight = 2, 1
			}
			picture, err := Decode(context.Background(), data)
			if err != nil {
				t.Fatal(err)
			}
			if w, h := picture.Size(); w != wantWidth || h != wantHeight {
				t.Fatalf("size = %dx%d", w, h)
			}
			if found := Inspect(data); found.Width != wantWidth || found.Height != wantHeight {
				t.Fatalf("inspected size = %dx%d", found.Width, found.Height)
			}
			thumb := picture.Fit(16)
			r, _, b, _ := thumb.At(0, 0).RGBA()
			if r < 0xc000 || b > 0x4000 {
				t.Fatalf("left edge = %v", thumb.At(0, 0))
			}
			encoded, err := EncodeWebP(thumb, 78)
			if err != nil || sniff(encoded) != formatWebP {
				t.Fatalf("webp rendition: %v", err)
			}
		})
	}
}

func TestEXIFOrientationAndTags(t *testing.T) {
	data := jpegWithEXIF(t, stripes(64, 32), 6)
	picture, err := Decode(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := picture.Size(); w != 32 || h != 64 {
		t.Fatalf("turned size = %dx%d", w, h)
	}
	// Turned 90° clockwise, the red left half is on top.
	thumb := picture.Fit(64)
	if r, _, _, _ := thumb.At(16, 5).RGBA(); r < 0xc000 {
		t.Fatalf("top = %v", thumb.At(16, 5))
	}
	if _, _, b, _ := thumb.At(16, 58).RGBA(); b < 0xc000 {
		t.Fatalf("bottom = %v", thumb.At(16, 58))
	}
	found := Inspect(data)
	if found.Width != 32 || found.Height != 64 || found.Make != "Neo" || DateTimeFromEXIF(found.DateTimeOriginal) != "2019-05-04T12:34:56" {
		t.Fatalf("inspect = %+v", found)
	}
	if exif := ReadExif(data); exif.Orientation != 6 || exif.TakenAt != "2019-05-04" || exif.Make != "Neo" {
		t.Fatalf("exif = %+v", exif)
	}
	// The hash of a turned photo is that of the upright picture.
	upright := image.NewRGBA(image.Rect(0, 0, 32, 64))
	for y := range 64 {
		for x := range 32 {
			upright.Set(x, y, stripes(64, 32).At(y, 31-x))
		}
	}
	plain := encode(t, func(b *bytes.Buffer) error { return jpeg.Encode(b, upright, &jpeg.Options{Quality: 95}) })
	other, err := Decode(context.Background(), plain)
	if err != nil {
		t.Fatal(err)
	}
	if picture.DHash() != other.DHash() {
		t.Fatalf("turned hash %s != upright hash %s", picture.DHash(), other.DHash())
	}
}

func TestFitNeverEnlargesAndCoverIsSquare(t *testing.T) {
	picture, err := Decode(context.Background(), fixtures(t)["png"])
	if err != nil {
		t.Fatal(err)
	}
	if got := picture.Fit(1024).Bounds(); got.Dx() != 64 || got.Dy() != 32 {
		t.Fatalf("fit = %v", got)
	}
	if got := picture.Cover(320).Bounds(); got.Dx() != 320 || got.Dy() != 320 {
		t.Fatalf("cover = %v", got)
	}
}

func TestWebPRenditionKeepsTransparency(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 8 {
			source.Set(x, y, color.NRGBA{G: 200, A: 128})
		}
	}
	encoded, err := EncodeWebP(source, 78)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := webp.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if _, g, _, a := decoded.At(2, 2).RGBA(); a < 0x7000 || a > 0x9000 || g < 0x5800 || g > 0x7000 {
		t.Fatalf("half transparent green = %v", decoded.At(2, 2))
	}
	if _, _, _, a := decoded.At(14, 2).RGBA(); a != 0 {
		t.Fatalf("clear pixel alpha = %d", a)
	}
}

func TestHEIFRotationSwapsTheReportedSize(t *testing.T) {
	for _, name := range []string{"irot1.avif", "clap-irot.avif"} {
		matches, _ := filepath.Glob(filepath.Join(os.Getenv("HOME"), "go/pkg/mod/github.com/gen2brain/gav1d@*/avif/testdata", name))
		if len(matches) == 0 {
			t.Skip("decoder test files are not in the module cache")
		}
		data, err := os.ReadFile(matches[0])
		if err != nil {
			t.Fatal(err)
		}
		picture, err := Decode(context.Background(), data)
		if err != nil {
			t.Fatal(err)
		}
		w, h := picture.Size()
		if found := Inspect(data); found.Width != w || found.Height != h {
			t.Fatalf("%s: inspect %dx%d, decoded %dx%d", name, found.Width, found.Height, w, h)
		}
	}
}

func TestOversizedAndBrokenInputIsRefused(t *testing.T) {
	huge := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x01\x86\xa0\x00\x01\x86\xa0\x08\x02\x00\x00\x00\x00\x00\x00\x00")
	if _, err := Decode(context.Background(), huge); err == nil {
		t.Fatal("a 100000x100000 header was accepted")
	}
	bomb := []byte("BM\x00\x00\x00\x00\x00\x00\x00\x00\x36\x00\x00\x00\x28\x00\x00\x00\x00\x00\x01\x00\x00\x00\x01\x00\x01\x00\x18\x00" + string(make([]byte, 24)))
	if _, err := Decode(context.Background(), bomb); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized bitmap: %v", err)
	}
	for _, data := range [][]byte{nil, []byte("text"), []byte("\xff\xd8\xff"), []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), []byte("\x00\x00\x00\x18ftypavif")} {
		if _, err := Decode(context.Background(), data); err == nil {
			t.Fatalf("%q decoded", data)
		}
		Inspect(data)
		ReadExif(data)
	}
}

func TestSVGIsDrawnWithoutReachingOutside(t *testing.T) {
	if _, err := exec.LookPath("rsvg-convert"); err != nil {
		t.Skip("rsvg-convert is not installed")
	}
	picture, err := Decode(context.Background(), []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="#f00"/><image href="file:///etc/hosts" width="5" height="5"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if w, h := picture.Size(); w != 40 || h != 20 {
		t.Fatalf("size = %dx%d", w, h)
	}
}

func TestSVGBoxIsBounded(t *testing.T) {
	cases := map[string][2]int{
		`<svg width="40" height="20"/>`:                         {40, 20},
		`<svg width="100000" height="50000"/>`:                  {2048, 1024},
		`<svg viewBox="0 0 24 24"/>`:                            {24, 24},
		`<svg width="100%" height="100%"/>`:                     {2048, 2048},
		`<?xml version="1.0"?><svg width="1in" height="72pt"/>`: {96, 96},
	}
	for document, want := range cases {
		if w, h := svgSize([]byte(document)); w != want[0] || h != want[1] {
			t.Errorf("%s = %dx%d, want %v", document, w, h, want)
		}
		if sniff([]byte(document)) != formatSVG {
			t.Errorf("%s not recognised", document)
		}
	}
}

// A stand-in for rsvg-convert checks how it is run: the document on stdin, no
// file name, no server secrets, and a bounded size.
func TestSVGRendererInvocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	directory := t.TempDir()
	drawing := filepath.Join(directory, "drawing.png")
	if err := os.WriteFile(drawing, encode(t, func(b *bytes.Buffer) error { return png.Encode(b, stripes(40, 20)) }), 0o600); err != nil {
		t.Fatal(err)
	}
	arguments := filepath.Join(directory, "arguments")
	script := "#!/bin/sh\ntest -z \"$SESSION_SECRET\" || exit 9\necho \"$@\" > " + arguments + "\ncat > /dev/null\ncat " + drawing + "\n"
	if err := os.WriteFile(filepath.Join(directory, "rsvg-convert"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SESSION_SECRET", "secret")
	picture, err := Decode(context.Background(), []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="9000" height="4500"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if w, h := picture.Size(); w != 40 || h != 20 {
		t.Fatalf("size = %dx%d", w, h)
	}
	got, err := os.ReadFile(arguments)
	if err != nil || strings.TrimSpace(string(got)) != "--format=png --keep-aspect-ratio --width=2048 --height=1024" {
		t.Fatalf("arguments = %q, %v", got, err)
	}
}
