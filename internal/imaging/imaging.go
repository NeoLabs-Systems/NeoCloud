// Package imaging decodes the picture formats NeoCloud previews and reads
// their size, orientation and camera metadata. Everything works in memory.
package imaging

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"

	"github.com/gen2brain/gav1d/avif"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/vpx/webp"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// MaxPixels bounds what is decoded, whatever a file's header claims.
const MaxPixels = 100_000_000

var (
	ErrUnsupported = errors.New("imaging: unsupported picture format")
	ErrTooLarge    = errors.New("imaging: picture exceeds the pixel limit")
)

type format int

const (
	formatUnknown format = iota
	formatJPEG
	formatPNG
	formatGIF
	formatWebP
	formatTIFF
	formatBMP
	formatHEIC
	formatAVIF
	formatSVG
)

// Supports reports whether pictures of this media type can be decoded here.
// SVG depends on rsvg-convert being installed.
func Supports(mediaType string) bool {
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(mediaType, ";", 2)[0])) {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/tiff", "image/tif",
		"image/bmp", "image/heic", "image/heif", "image/avif", "image/svg+xml":
		return true
	default:
		return false
	}
}

func sniff(data []byte) format {
	switch {
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return formatJPEG
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return formatPNG
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return formatGIF
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return formatWebP
	case bytes.HasPrefix(data, []byte("II*\x00")), bytes.HasPrefix(data, []byte("MM\x00*")):
		return formatTIFF
	case bytes.HasPrefix(data, []byte("BM")):
		return formatBMP
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		return sniffHEIF(data)
	case looksLikeSVG(data):
		return formatSVG
	}
	return formatUnknown
}

func sniffHEIF(data []byte) format {
	size := int(binary.BigEndian.Uint32(data[:4]))
	if size < 12 || size > len(data) {
		size = min(len(data), 64)
	}
	brands := []string{string(data[8:12])}
	for at := 16; at+4 <= size; at += 4 {
		brands = append(brands, string(data[at:at+4]))
	}
	found := formatUnknown
	for _, brand := range brands {
		switch brand {
		case "avif", "avis":
			return formatAVIF
		case "heic", "heix", "heim", "heis", "hevc", "hevx", "mif1", "msf1":
			found = formatHEIC
		}
	}
	return found
}

// config reads the stored size of a picture from its header.
func config(kind format, data []byte) (image.Config, error) {
	reader := bytes.NewReader(data)
	switch kind {
	case formatJPEG:
		return jpeg.DecodeConfig(reader)
	case formatPNG:
		return png.DecodeConfig(reader)
	case formatGIF:
		return gif.DecodeConfig(reader)
	case formatWebP:
		return webp.DecodeConfig(reader)
	case formatTIFF:
		return tiff.DecodeConfig(reader)
	case formatBMP:
		return bmp.DecodeConfig(reader)
	case formatHEIC:
		return heic.DecodeConfig(reader)
	case formatAVIF:
		return avif.DecodeConfig(reader)
	}
	return image.Config{}, ErrUnsupported
}

func checkSize(width, height int) error {
	if width < 1 || height < 1 {
		return ErrUnsupported
	}
	if int64(width) > MaxPixels/int64(height) {
		return ErrTooLarge
	}
	return nil
}

// Picture is a decoded image together with the turn it has to be shown with.
type Picture struct {
	pixels      image.Image
	orientation int
}

// Decode reads one picture (the first frame of an animation).
func Decode(ctx context.Context, data []byte) (picture *Picture, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The decoders parse untrusted input: a bug in one is this file's error.
	defer func() {
		if recovered := recover(); recovered != nil {
			picture, err = nil, fmt.Errorf("imaging: decoder failed: %v", recovered)
		}
	}()
	kind := sniff(data)
	if kind == formatSVG {
		pixels, err := rasterizeSVG(ctx, data)
		if err != nil {
			return nil, err
		}
		return &Picture{pixels: pixels, orientation: 1}, nil
	}
	header, err := config(kind, data)
	if err != nil {
		return nil, err
	}
	if err := checkSize(header.Width, header.Height); err != nil {
		return nil, err
	}
	reader := bytes.NewReader(data)
	var pixels image.Image
	switch kind {
	case formatJPEG:
		pixels, err = jpeg.Decode(reader)
	case formatPNG:
		pixels, err = png.Decode(reader)
	case formatGIF:
		pixels, err = gif.Decode(reader)
		if err == nil {
			// A first frame may cover only part of the screen.
			canvas := image.NewRGBA(image.Rect(0, 0, header.Width, header.Height))
			draw.Draw(canvas, pixels.Bounds(), pixels, pixels.Bounds().Min, draw.Src)
			pixels = canvas
		}
	case formatWebP:
		pixels, err = webp.Decode(reader, webp.Options{FrameSizeLimit: MaxPixels, Threads: 1})
	case formatTIFF:
		pixels, err = tiff.Decode(reader)
	case formatBMP:
		pixels, err = bmp.Decode(reader)
	case formatHEIC:
		pixels, err = heic.Decode(reader, heic.Options{AutoRotate: true, FrameSizeLimit: MaxPixels, Threads: 1})
	case formatAVIF:
		pixels, err = avif.Decode(reader, avif.Options{AutoRotate: true, FrameSizeLimit: MaxPixels})
	}
	if err != nil {
		return nil, err
	}
	bounds := pixels.Bounds()
	if err := checkSize(bounds.Dx(), bounds.Dy()); err != nil {
		return nil, err
	}
	orientation := 1
	// HEIF decoders apply the container's own rotation; EXIF repeats it.
	if kind != formatHEIC && kind != formatAVIF {
		orientation = parseEXIF(rawEXIF(kind, data)).orientation
	}
	return &Picture{pixels: pixels, orientation: orientation}, nil
}
