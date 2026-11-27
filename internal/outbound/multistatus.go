package outbound

import (
	"encoding/xml"
	"errors"
	"io"
	"net/url"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const dav = "DAV:"

var (
	ErrListingTooLarge = errors.New("the WebDAV answer is too large")
	ErrNotWebDAV       = errors.New("the answer is not a WebDAV multistatus")
)

// Prop is one property element of a multistatus answer.
type Prop struct {
	XMLName  xml.Name
	Text     string `xml:",chardata"`
	Children []Prop `xml:",any"`
}

// Response is one resource: its href as the server wrote it and the properties
// it answered successfully.
type Response struct {
	Href  string
	Props []Prop
}

// ReadMultistatus parses a 207 body, reading at most limit bytes.
func ReadMultistatus(body io.Reader, limit int64) ([]Response, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrListingTooLarge
	}
	var document struct {
		XMLName   xml.Name
		Responses []struct {
			Href  string `xml:"DAV: href"`
			Stats []struct {
				Status string `xml:"DAV: status"`
				Props  []Prop `xml:"DAV: prop"`
			} `xml:"DAV: propstat"`
		} `xml:"DAV: response"`
	}
	if err = xml.Unmarshal(data, &document); err != nil || document.XMLName != (xml.Name{Space: dav, Local: "multistatus"}) {
		return nil, ErrNotWebDAV
	}
	responses := make([]Response, 0, len(document.Responses))
	for _, item := range document.Responses {
		response := Response{Href: strings.TrimSpace(item.Href)}
		for _, stat := range item.Stats {
			// Some minimal servers send no status at all.
			if fields := strings.Fields(stat.Status); len(fields) > 1 && !strings.HasPrefix(fields[1], "2") {
				continue
			}
			for _, prop := range stat.Props {
				response.Props = append(response.Props, prop.Children...)
			}
		}
		responses = append(responses, response)
	}
	return responses, nil
}

// Text is the value of a property by local name, whatever its namespace.
func (r Response) Text(name string) string {
	for _, prop := range r.Props {
		if prop.XMLName.Local == name {
			return strings.TrimSpace(prop.Text)
		}
	}
	return ""
}

// IsCollection reports whether the resource is a folder.
func (r Response) IsCollection() bool {
	for _, prop := range r.Props {
		if prop.XMLName == (xml.Name{Space: dav, Local: "resourcetype"}) {
			for _, child := range prop.Children {
				if child.XMLName == (xml.Name{Space: dav, Local: "collection"}) {
					return true
				}
			}
		}
	}
	return false
}

// Segments splits an escaped URL path into decoded names, spelled as the server
// spells them.
func Segments(escapedPath string) []string {
	var segments []string
	for _, part := range strings.Split(escapedPath, "/") {
		if part == "" {
			continue
		}
		if decoded, err := url.PathUnescape(part); err == nil {
			part = decoded
		}
		segments = append(segments, part)
	}
	return segments
}

// Below returns the segments of path under base, ignoring Unicode composition
// differences; ok is false for a path elsewhere.
func Below(base, path []string) ([]string, bool) {
	if len(path) < len(base) {
		return nil, false
	}
	for i := range base {
		if norm.NFC.String(base[i]) != norm.NFC.String(path[i]) {
			return nil, false
		}
	}
	return path[len(base):], true
}
