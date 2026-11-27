package imaging

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"

	"github.com/gen2brain/gav1d/avif"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/vpx/webp"
)

// Exif is what NeoCloud reads from a photo's EXIF block.
type Exif struct {
	Make, Model string
	// TakenAt is "YYYY-MM-DD", or empty.
	TakenAt     string
	Orientation int
}

// ReadExif reads the EXIF values of a JPEG, TIFF, PNG, WebP, HEIF or AVIF
// picture; anything absent or malformed reads as nothing.
func ReadExif(data []byte) (found Exif) {
	found.Orientation = 1
	defer func() { _ = recover() }()
	tags := parseEXIF(rawEXIF(sniff(data), data))
	found = Exif{Make: tags.make, Model: tags.model, Orientation: tags.orientation}
	for _, value := range []string{tags.original, tags.dateTime} {
		if taken := DateTimeFromEXIF(value); taken != "" {
			found.TakenAt = taken[:10]
			break
		}
	}
	return found
}

// rawEXIF returns the TIFF structure holding a picture's EXIF tags.
func rawEXIF(kind format, data []byte) []byte {
	var block []byte
	var err error
	switch kind {
	case formatJPEG:
		return jpegEXIF(data)
	case formatPNG:
		return pngEXIF(data)
	case formatTIFF:
		return data
	case formatWebP:
		block, err = webp.RawExif(bytes.NewReader(data))
	case formatHEIC:
		block, err = heic.RawExif(bytes.NewReader(data))
	case formatAVIF:
		block, err = avif.RawExif(bytes.NewReader(data))
	}
	if err != nil {
		return nil
	}
	return bytes.TrimPrefix(block, []byte("Exif\x00\x00"))
}

func jpegEXIF(data []byte) []byte {
	for at := 2; at+4 <= len(data); {
		if data[at] != 0xff {
			at++
			continue
		}
		marker := data[at+1]
		if marker == 0xff {
			at++
			continue
		}
		at += 2
		if marker == 0xd9 || marker == 0xda {
			break
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if at+2 > len(data) {
			break
		}
		length := int(binary.BigEndian.Uint16(data[at:]))
		if length < 2 || at+length > len(data) {
			break
		}
		segment := data[at+2 : at+length]
		if marker == 0xe1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return segment[6:]
		}
		at += length
	}
	return nil
}

func pngEXIF(data []byte) []byte {
	for at := 8; at+8 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[at:]))
		if length < 0 || length > len(data)-at-8 {
			break
		}
		if string(data[at+4:at+8]) == "eXIf" {
			return data[at+8 : at+8+length]
		}
		at += 12 + length
	}
	return nil
}

type exifTags struct {
	orientation        int
	make, model        string
	original, dateTime string
}

const (
	exifMaxEntries = 512
	exifMaxText    = 1024
)

// parseEXIF reads the few tags NeoCloud uses. Anything malformed reads as
// nothing.
func parseEXIF(data []byte) exifTags {
	tags := exifTags{orientation: 1}
	if len(data) < 8 {
		return tags
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return tags
	}
	if order.Uint16(data[2:]) != 42 {
		return tags
	}
	root := readIFD(data, order, int64(order.Uint32(data[4:])))
	tags.make, tags.model = root.text[0x010f], root.text[0x0110]
	tags.dateTime = root.text[0x0132]
	if value := root.number[0x0112]; value >= 1 && value <= 8 {
		tags.orientation = int(value)
	}
	if offset, ok := root.number[0x8769]; ok {
		tags.original = readIFD(data, order, int64(offset)).text[0x9003]
	}
	return tags
}

type ifd struct {
	text   map[uint16]string
	number map[uint16]uint32
}

func readIFD(data []byte, order binary.ByteOrder, offset int64) ifd {
	found := ifd{text: map[uint16]string{}, number: map[uint16]uint32{}}
	if offset < 8 || offset+2 > int64(len(data)) {
		return found
	}
	count := min(int(order.Uint16(data[offset:])), exifMaxEntries)
	for index := range count {
		at := offset + 2 + int64(index)*12
		if at+12 > int64(len(data)) {
			break
		}
		tag, kind, length := order.Uint16(data[at:]), order.Uint16(data[at+2:]), int64(order.Uint32(data[at+4:]))
		switch {
		case kind == 3 && length == 1:
			found.number[tag] = uint32(order.Uint16(data[at+8:]))
		case kind == 4 && length == 1:
			found.number[tag] = order.Uint32(data[at+8:])
		case kind == 2 && length > 0 && length <= exifMaxText:
			start := at + 8
			if length > 4 {
				start = int64(order.Uint32(data[at+8:]))
			}
			if start+length <= int64(len(data)) {
				found.text[tag] = strings.TrimSpace(strings.TrimRight(strings.ToValidUTF8(string(data[start:start+length]), ""), "\x00"))
			}
		}
	}
	return found
}

// DateTimeFromEXIF turns EXIF date text into "YYYY-MM-DDTHH:MM:SS" (the
// camera's wall clock, midnight when only a date is given), or "".
func DateTimeFromEXIF(value string) string {
	if len(value) < 10 || value[4] != ':' || value[7] != ':' || strings.HasPrefix(value, "0000") {
		return ""
	}
	for _, part := range []string{value[:4], value[5:7], value[8:10]} {
		if _, err := strconv.ParseUint(part, 10, 16); err != nil {
			return ""
		}
	}
	date := value[:4] + "-" + value[5:7] + "-" + value[8:10]
	if len(value) >= 19 && (value[10] == ' ' || value[10] == 'T') && value[13] == ':' && value[16] == ':' {
		clock := value[11:19]
		if _, err := strconv.ParseUint(clock[:2]+clock[3:5]+clock[6:], 10, 32); err == nil {
			return date + "T" + clock
		}
	}
	return date + "T00:00:00"
}
