package signpdf

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"

	"github.com/NeoLabs-Systems/neocloud/internal/imaging"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"golang.org/x/image/draw"
)

const (
	maxImageInput = 40 << 20
	maxEmbedEdge  = 2400
)

var errImage = errors.New("unsupported or damaged image")

// rasterPicture reports whether the bytes start like a bitmap format. Vector
// pictures (SVG) are never accepted here.
func rasterPicture(data []byte) bool {
	for _, magic := range []string{"\xff\xd8\xff", "\x89PNG\r\n\x1a\n", "GIF87a", "GIF89a", "II*\x00", "MM\x00*", "BM"} {
		if bytes.HasPrefix(data, []byte(magic)) {
			return true
		}
	}
	return len(data) >= 12 && (string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" || string(data[4:8]) == "ftyp")
}

// picture decodes a bitmap upright, scaled down to lie within maxW×maxH.
func picture(ctx context.Context, data []byte, maxW, maxH int) (image.Image, error) {
	if len(data) == 0 || len(data) > maxImageInput || !rasterPicture(data) {
		return nil, errImage
	}
	decoded, err := imaging.Decode(ctx, data)
	if err != nil {
		return nil, err
	}
	var img image.Image = decoded.Fit(max(maxW, maxH))
	b := img.Bounds()
	if scale := min(float64(maxW)/float64(b.Dx()), float64(maxH)/float64(b.Dy())); scale < 1 {
		dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(b.Dx())*scale+0.5)), max(1, int(float64(b.Dy())*scale+0.5))))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}
	return img, nil
}

type embedded struct {
	name          string
	ref           types.IndirectRef
	width, height float64
}

// embedImage adds a picture as an image XObject. A plain upright JPEG of
// moderate size passes through without a second round of compression.
func (d *document) embedImage(ctx context.Context, name string, data []byte) (embedded, error) {
	img, err := picture(ctx, data, maxEmbedEdge, maxEmbedEdge)
	if err != nil {
		return embedded{}, refuse(CodeImage, err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dict := types.Dict{"Type": types.Name("XObject"), "Subtype": types.Name("Image"), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceRGB")}
	dimensions := func(width, height int) {
		dict["Width"], dict["Height"] = types.Integer(width), types.Integer(height)
	}
	if config, err := jpeg.DecodeConfig(bytes.NewReader(data)); err == nil {
		raw := data
		plain := (config.ColorModel == color.YCbCrModel || config.ColorModel == color.GrayModel) &&
			max(config.Width, config.Height) <= maxEmbedEdge && imaging.ReadExif(data).Orientation <= 1
		if plain {
			w, h = config.Width, config.Height
			if config.ColorModel == color.GrayModel {
				dict["ColorSpace"] = types.Name("DeviceGray")
			}
		} else {
			var out bytes.Buffer
			if err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 90}); err != nil {
				return embedded{}, refuse(CodeImage, err)
			}
			raw = out.Bytes()
		}
		dimensions(w, h)
		dict["Filter"] = types.Name("DCTDecode")
		return embedded{name, d.add(types.StreamDict{Dict: dict, Raw: raw}), float64(w), float64(h)}, nil
	}
	pixels := make([]byte, 0, w*h*3)
	alpha := make([]byte, 0, w*h)
	opaque := true
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			pixels = append(pixels, c.R, c.G, c.B)
			alpha = append(alpha, c.A)
			opaque = opaque && c.A == 0xff
		}
	}
	dimensions(w, h)
	if !opaque {
		dict["SMask"] = d.stream(types.Dict{"Type": types.Name("XObject"), "Subtype": types.Name("Image"), "Width": types.Integer(w), "Height": types.Integer(h),
			"ColorSpace": types.Name("DeviceGray"), "BitsPerComponent": types.Integer(8)}, alpha)
	}
	return embedded{name, d.stream(dict, pixels), float64(w), float64(h)}, nil
}
