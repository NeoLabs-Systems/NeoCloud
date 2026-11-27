package imaging

import (
	"encoding/hex"
	"image"
	"image/color"
	"image/draw"
	"math"
)

// DHash is the 64-bit perceptual hash of a picture, as 16 hex characters: white
// behind transparency, grayscale from linear light, reduced to 9×8 with
// Lanczos-3, each bit set when a pixel is brighter than its right neighbour.
// The phone app computes the same hash (flutter_app camera_dhash.dart).
func DHash(source image.Image) string {
	const columns, rows = 9, 8
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	line := make([]float64, width)
	narrow := make([]float64, columns*height)
	across, down := lanczosKernel(width, columns), lanczosKernel(height, rows)
	// A row at a time as RGBA: image/draw has fast paths where At would allocate
	// for every pixel.
	band := image.NewRGBA(image.Rect(0, 0, width, 1))
	for y := range height {
		draw.Draw(band, band.Bounds(), source, image.Pt(bounds.Min.X, bounds.Min.Y+y), draw.Src)
		for x := range width {
			line[x] = gray(band.RGBAAt(x, 0))
		}
		across.apply(narrow[y*columns:(y+1)*columns], width, func(index int) float64 { return line[index] })
	}
	var small [columns * rows]float64
	column := make([]float64, rows)
	for x := range columns {
		down.apply(column, height, func(index int) float64 { return narrow[index*columns+x] })
		for y := range rows {
			small[y*columns+x] = column[y]
		}
	}
	var hash [rows]byte
	for y := range rows {
		for x := range columns - 1 {
			if math.Round(small[y*columns+x]) > math.Round(small[y*columns+x+1]) {
				hash[y] |= 1 << (7 - x)
			}
		}
	}
	return hex.EncodeToString(hash[:])
}

// linear holds the sRGB transfer curve for the 256 levels of a channel.
var linear = func() (table [256]float64) {
	for index := range table {
		value := float64(index) / 255
		if value <= 0.04045 {
			table[index] = value / 12.92
		} else {
			table[index] = math.Pow((value+0.055)/1.055, 2.4)
		}
	}
	return table
}()

func gray(pixel color.RGBA) float64 {
	// Premultiplied channels plus the uncovered share of white.
	white := 255 - pixel.A
	luminance := 0.2126*linear[pixel.R+white] + 0.7152*linear[pixel.G+white] + 0.0722*linear[pixel.B+white]
	if luminance <= 0.0031308 {
		return luminance * 12.92 * 255
	}
	return (1.055*math.Pow(luminance, 1/2.4) - 0.055) * 255
}

// kernel holds, for every output of a reduction, the weights of the samples it
// is made from, so that they are worked out once and not for every row.
type kernel []tap

type tap struct {
	first   int
	weights []float64
}

// lanczosKernel resamples `length` values into `size` with a Lanczos-3 window.
func lanczosKernel(length, size int) kernel {
	factor := float64(length) / float64(size)
	radius := int(math.Ceil(3 * factor))
	out := make(kernel, size)
	for index := range out {
		centre := (float64(index)+0.5)*factor - 0.5
		first := int(math.Floor(centre)) - radius
		weights := make([]float64, int(math.Ceil(centre))+radius-first+1)
		var total float64
		for k := range weights {
			weights[k] = lanczos3((float64(first+k) - centre) / factor)
			total += weights[k]
		}
		if total != 0 {
			for k := range weights {
				weights[k] /= total
			}
		}
		out[index] = tap{first: first, weights: weights}
	}
	return out
}

// apply reduces the `length` values at into target, repeating the edges.
func (k kernel) apply(target []float64, length int, at func(int) float64) {
	for index, tap := range k {
		var sum float64
		for offset, weight := range tap.weights {
			sum += weight * at(min(max(tap.first+offset, 0), length-1))
		}
		target[index] = sum
	}
}

func lanczos3(x float64) float64 {
	if x == 0 {
		return 1
	}
	if math.Abs(x) >= 3 {
		return 0
	}
	p := math.Pi * x
	return 3 * math.Sin(p) * math.Sin(p/3) / (p * p)
}
