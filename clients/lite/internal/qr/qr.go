// Package qr renders the device-login URL as a QR code: half-block text for
// the terminal and an SVG for the settings page (docs/clients.md §5). It is
// all generated locally; nothing is fetched from a CDN.
package qr

import (
	"encoding/base64"
	"fmt"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// Black on bright white, so the code reads the right way round on dark and
// light terminals alike.
const (
	ansiBlackOnWhite = "\x1b[30;107m"
	ansiReset        = "\x1b[0m"
)

// Terminal returns text drawing the QR code with half-block characters, two
// module rows per line, quiet zone included.
func Terminal(text string) (string, error) {
	code, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimRight(code.ToSmallString(true), "\n"), "\n") {
		out.WriteString(ansiBlackOnWhite + line + ansiReset + "\n")
	}
	return out.String(), nil
}

// SVG returns a standalone SVG of the QR code, one unit per module.
func SVG(text string) (string, error) {
	code, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bitmap := code.Bitmap() // includes the quiet zone
	size := len(bitmap)
	var path strings.Builder
	for y, row := range bitmap {
		for x := 0; x < len(row); {
			if !row[x] {
				x++
				continue
			}
			start := x
			for x < len(row) && row[x] {
				x++
			}
			fmt.Fprintf(&path, "M%d %dh%dv1h-%dz", start, y, x-start, x-start)
		}
	}
	return fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+
			`<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="%s"/></svg>`,
		size, size, size, size, path.String(),
	), nil
}

// DataURI wraps SVG output for an <img src>.
func DataURI(svg string) string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}
