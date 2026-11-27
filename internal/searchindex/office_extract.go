package searchindex

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

const (
	maxOfficeEntries    = 10_000
	maxOfficeEntryBytes = 32 << 20
	maxOfficeTotalBytes = 96 << 20
)

func extractOffice(ctx context.Context, source []byte, extension string) (string, error) {
	archive, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		return "", fmt.Errorf("searchindex: invalid office archive: %w", err)
	}
	if len(archive.File) > maxOfficeEntries {
		return "", errors.New("searchindex: office archive has too many entries")
	}
	parts := make([]*zip.File, 0, len(archive.File))
	var declared uint64
	for _, file := range archive.File {
		if !officeTextPart(extension, file.Name) {
			continue
		}
		// An oversized part is left out; the rest of the document still counts.
		if file.UncompressedSize64 > maxOfficeEntryBytes || declared+file.UncompressedSize64 > maxOfficeTotalBytes {
			continue
		}
		declared += file.UncompressedSize64
		parts = append(parts, file)
	}
	sort.Slice(parts, func(i, j int) bool { return officePartOrder(parts[i].Name) < officePartOrder(parts[j].Name) })
	textParts := make(map[string][]byte, len(parts))
	var expanded int64
	for _, part := range parts {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		reader, err := part.Open()
		if err != nil {
			return "", err
		}
		content, readErr := io.ReadAll(io.LimitReader(reader, maxOfficeEntryBytes+1))
		closeErr := reader.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		expanded += int64(len(content))
		if len(content) > maxOfficeEntryBytes || expanded > maxOfficeTotalBytes {
			return "", errors.New("searchindex: office archive expanded beyond extraction limits")
		}
		textParts[part.Name] = content
	}
	if extension == ".xlsx" {
		shared := []string{}
		if content, ok := textParts["xl/sharedStrings.xml"]; ok {
			shared, err = sharedStrings(content)
			if err != nil {
				return "", err
			}
		}
		var out strings.Builder
		for _, part := range parts {
			if strings.HasPrefix(part.Name, "xl/worksheets/sheet") {
				content, parseErr := worksheetText(textParts[part.Name], shared)
				if parseErr != nil {
					return "", parseErr
				}
				out.WriteString(content)
			}
		}
		return out.String(), nil
	}
	var out strings.Builder
	for _, part := range parts {
		content, parseErr := xmlDocumentText(textParts[part.Name])
		if parseErr != nil {
			return "", parseErr
		}
		out.WriteString(content)
		out.WriteByte('\n')
	}
	return out.String(), nil
}

func officeTextPart(extension, rawName string) bool {
	name := path.Clean(rawName)
	switch extension {
	case ".docx":
		if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") {
			return false
		}
		base := strings.TrimSuffix(strings.TrimPrefix(name, "word/"), ".xml")
		return base == "document" || base == "footnotes" || base == "endnotes" || base == "comments" || strings.HasPrefix(base, "header") || strings.HasPrefix(base, "footer")
	case ".pptx":
		return numberedXML(name, "ppt/slides/slide") || numberedXML(name, "ppt/notesSlides/notesSlide")
	case ".xlsx":
		return name == "xl/sharedStrings.xml" || numberedXML(name, "xl/worksheets/sheet")
	case ".odt", ".ods", ".odp":
		return name == "content.xml"
	default:
		return false
	}
}

func numberedXML(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".xml") {
		return false
	}
	number := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".xml")
	_, err := strconv.Atoi(number)
	return err == nil
}

func officePartOrder(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] < '0' || name[i] > '9' {
			continue
		}
		start := i
		for start > 0 && name[start-1] >= '0' && name[start-1] <= '9' {
			start--
		}
		digits := name[start : i+1]
		return name[:start] + strings.Repeat("0", max(0, 8-len(digits))) + digits + name[i+1:]
	}
	return name
}

func xmlDocumentText(source []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(source))
	var out strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return out.String(), nil
		}
		if err != nil {
			return "", err
		}
		switch value := token.(type) {
		case xml.CharData:
			out.Write(value)
		case xml.StartElement:
			switch value.Name.Local {
			case "tab", "br", "cr", "s", "line-break":
				out.WriteByte(' ')
			}
		case xml.EndElement:
			switch value.Name.Local {
			case "p", "h", "si", "table-cell":
				out.WriteByte('\n')
			}
		}
	}
}

func sharedStrings(source []byte) ([]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(source))
	values := []string{}
	depth, itemDepth := 0, -1
	var current strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return values, nil
		}
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			depth++
			if value.Name.Local == "si" && itemDepth < 0 {
				itemDepth = depth
			}
		case xml.CharData:
			if itemDepth >= 0 {
				current.Write(value)
			}
		case xml.EndElement:
			if itemDepth == depth && value.Name.Local == "si" {
				values = append(values, current.String())
				current.Reset()
				itemDepth = -1
			}
			depth--
		}
	}
}

func worksheetText(source []byte, shared []string) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(source))
	var out strings.Builder
	depth := 0
	cellDepth, valueDepth, inlineDepth := -1, -1, -1
	cellType := ""
	var value, inline strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return out.String(), nil
		}
		if err != nil {
			return "", err
		}
		switch item := token.(type) {
		case xml.StartElement:
			depth++
			switch item.Name.Local {
			case "c":
				cellDepth, cellType = depth, ""
				value.Reset()
				inline.Reset()
				for _, attr := range item.Attr {
					if attr.Name.Local == "t" {
						cellType = attr.Value
					}
				}
			case "v":
				valueDepth = depth
			case "is":
				inlineDepth = depth
			}
		case xml.CharData:
			if valueDepth >= 0 {
				value.Write(item)
			} else if inlineDepth >= 0 {
				inline.Write(item)
			}
		case xml.EndElement:
			if valueDepth == depth && item.Name.Local == "v" {
				valueDepth = -1
			}
			if inlineDepth == depth && item.Name.Local == "is" {
				inlineDepth = -1
			}
			if cellDepth == depth && item.Name.Local == "c" {
				cell := value.String()
				switch cellType {
				case "s":
					index, parseErr := strconv.Atoi(strings.TrimSpace(cell))
					if parseErr == nil && index >= 0 && index < len(shared) {
						cell = shared[index]
					} else {
						cell = ""
					}
				case "inlineStr":
					cell = inline.String()
				}
				if cell != "" {
					out.WriteString(cell)
					out.WriteByte('\n')
				}
				cellDepth = -1
			}
			depth--
		}
	}
}
