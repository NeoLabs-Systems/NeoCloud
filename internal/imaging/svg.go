package imaging

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	maxSVGEdge      = 2048
	svgTimeout      = 30 * time.Second
	maxSVGPNGOutput = 32 << 20
)

func looksLikeSVG(data []byte) bool {
	head := bytes.TrimLeft(bytes.TrimPrefix(data[:min(len(data), 4096)], []byte("\xef\xbb\xbf")), " \t\r\n")
	return bytes.HasPrefix(head, []byte("<")) && bytes.Contains(head, []byte("<svg"))
}

// rasterizeSVG draws an SVG with rsvg-convert (librsvg). The document comes in
// on stdin, so it has no base URI and librsvg loads none of the files or
// addresses it may reference.
func rasterizeSVG(ctx context.Context, data []byte) (image.Image, error) {
	binary, err := exec.LookPath("rsvg-convert")
	if err != nil {
		return nil, ErrUnsupported
	}
	width, height := svgSize(data)
	ctx, cancel := context.WithTimeout(ctx, svgTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "--format=png", "--keep-aspect-ratio",
		"--width="+strconv.Itoa(width), "--height="+strconv.Itoa(height))
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "HOME=" + os.TempDir()}
	command.Stdin = bytes.NewReader(data)
	out := &boundedBuffer{limit: maxSVGPNGOutput}
	command.Stdout = out
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil {
		return nil, err
	}
	header, err := png.DecodeConfig(bytes.NewReader(out.Bytes()))
	if err != nil {
		return nil, err
	}
	if err := checkSize(header.Width, header.Height); err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(out.Bytes()))
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("imaging: rendering is too large")
	}
	return b.Buffer.Write(p)
}

// ReadFrom would bypass the limit through the embedded buffer.
func (b *boundedBuffer) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{b}, r)
}

// svgSize is the box an SVG is drawn into: its own size in pixels, bounded so
// that a document cannot ask for an enormous canvas.
func svgSize(data []byte) (int, int) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	var width, height float64
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		element, ok := token.(xml.StartElement)
		if !ok || element.Name.Local != "svg" {
			continue
		}
		var viewBox []string
		for _, attribute := range element.Attr {
			switch attribute.Name.Local {
			case "width":
				width = svgLength(attribute.Value)
			case "height":
				height = svgLength(attribute.Value)
			case "viewBox":
				viewBox = strings.FieldsFunc(attribute.Value, func(r rune) bool { return r == ' ' || r == ',' })
			}
		}
		if (width <= 0 || height <= 0) && len(viewBox) == 4 {
			width, height = svgLength(viewBox[2]), svgLength(viewBox[3])
		}
		break
	}
	if !(width > 0) || !(height > 0) || math.IsInf(width, 0) || math.IsInf(height, 0) {
		return maxSVGEdge, maxSVGEdge
	}
	ratio := math.Min(1, math.Min(maxSVGEdge/width, maxSVGEdge/height))
	return max(1, int(math.Round(width*ratio))), max(1, int(math.Round(height*ratio)))
}

// svgLength converts an absolute SVG length to pixels at 96 per inch; zero for
// relative units.
func svgLength(value string) float64 {
	value = strings.TrimSpace(value)
	scale := 1.0
	for unit, factor := range map[string]float64{"px": 1, "pt": 96.0 / 72, "pc": 16, "mm": 96 / 25.4, "cm": 96 / 2.54, "in": 96} {
		if trimmed, found := strings.CutSuffix(value, unit); found {
			value, scale = strings.TrimSpace(trimmed), factor
			break
		}
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return number * scale
}
