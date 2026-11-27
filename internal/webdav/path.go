// Package webdav is the protocol side of NeoCloud's WebDAV endpoint: URLs,
// sign-in, XML and locks. The file operations live in the files package.
package webdav

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var (
	errInvalidEncoding = errors.New("The path is not valid percent-encoded UTF-8.")
	errInvalidPath     = errors.New("The path contains an invalid segment.")
)

type Target struct {
	Base     string
	Username string // empty for /remote.php/webdav
	Segments []string
}

var mounts = []string{"/dav/files/", "/remote.php/dav/files/", "/remote.php/webdav"}

func decodeSegments(path string) ([]string, error) {
	parts := strings.Split(path, "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		value, err := url.PathUnescape(part)
		if err != nil || !utf8.ValidString(value) {
			return nil, errInvalidEncoding
		}
		value = norm.NFC.String(value)
		if value == "." || value == ".." || strings.ContainsAny(value, "/\x00") {
			return nil, errInvalidPath
		}
		segments = append(segments, value)
	}
	return segments, nil
}

// ParsePath recognizes the primary and Nextcloud-compatible WebDAV mounts. It
// returns nil for paths outside them. Base keeps the client's spelling, which
// hrefs echo back.
func ParsePath(rawPath string) (*Target, error) {
	for _, prefix := range mounts[:2] {
		rest, found := strings.CutPrefix(rawPath, prefix)
		if !found {
			continue
		}
		user, tail, _ := strings.Cut(rest, "/")
		if user == "" {
			return nil, nil
		}
		userParts, err := decodeSegments(user)
		if err != nil || len(userParts) != 1 {
			return nil, errInvalidPath
		}
		segments, err := decodeSegments(tail)
		if err != nil {
			return nil, err
		}
		return &Target{Base: prefix + user, Username: userParts[0], Segments: segments}, nil
	}
	if rawPath == mounts[2] || strings.HasPrefix(rawPath, mounts[2]+"/") {
		segments, err := decodeSegments(strings.TrimPrefix(rawPath, mounts[2]))
		if err != nil {
			return nil, err
		}
		return &Target{Base: mounts[2], Segments: segments}, nil
	}
	return nil, nil
}

// ParseDestination reads a Destination header: an absolute URL or path.
func ParseDestination(header string) (*Target, error) {
	if strings.TrimSpace(header) == "" {
		return nil, fmt.Errorf("destination header is required")
	}
	u, err := url.Parse(header)
	if err != nil || u.IsAbs() && u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid destination")
	}
	raw := u.RawPath
	if raw == "" {
		raw = u.EscapedPath()
	}
	target, err := ParsePath(raw)
	if err != nil || target == nil {
		return nil, fmt.Errorf("destination is outside WebDAV")
	}
	return target, nil
}

// encodeSegment escapes like JavaScript's encodeURIComponent, which every
// WebDAV client reads the same way.
func encodeSegment(segment string) string {
	var out strings.Builder
	for _, b := range []byte(segment) {
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.IndexByte("-_.!~*'()", b) >= 0 {
			out.WriteByte(b)
		} else {
			fmt.Fprintf(&out, "%%%02X", b)
		}
	}
	return out.String()
}

func Href(base string, segments []string, folder bool) string {
	encoded := make([]string, len(segments))
	for i, segment := range segments {
		encoded[i] = encodeSegment(segment)
	}
	path := strings.Join(encoded, "/")
	if path == "" {
		return base + "/"
	}
	if folder {
		path += "/"
	}
	return base + "/" + path
}
