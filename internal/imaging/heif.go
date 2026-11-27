package imaging

import "encoding/binary"

// heifSize is the shown size of the primary item of a HEIC or AVIF file: its
// stored size (ispe) after the crop (clap) and rotation (irot) it declares.
// Zero when the file does not say.
func heifSize(data []byte) (width, height int) {
	meta := childBox(data, "meta")
	if len(meta) < 4 {
		return 0, 0
	}
	meta = meta[4:]
	pitm := childBox(meta, "pitm")
	var primary uint32
	switch {
	case len(pitm) >= 6 && pitm[0] == 0:
		primary = uint32(binary.BigEndian.Uint16(pitm[4:]))
	case len(pitm) >= 8:
		primary = binary.BigEndian.Uint32(pitm[4:])
	default:
		return width, height
	}
	iprp := childBox(meta, "iprp")
	var properties []box
	eachBox(childBox(iprp, "ipco"), func(b box) { properties = append(properties, b) })
	ipma := childBox(iprp, "ipma")
	if len(ipma) < 8 {
		return width, height
	}
	version, wide := ipma[0], ipma[3]&1 == 1
	entries := binary.BigEndian.Uint32(ipma[4:])
	at := 8
	for range entries {
		var item uint32
		if version < 1 {
			if at+2 > len(ipma) {
				return width, height
			}
			item, at = uint32(binary.BigEndian.Uint16(ipma[at:])), at+2
		} else {
			if at+4 > len(ipma) {
				return width, height
			}
			item, at = binary.BigEndian.Uint32(ipma[at:]), at+4
		}
		if at >= len(ipma) {
			return width, height
		}
		count := int(ipma[at])
		at++
		for range count {
			var index int
			if wide {
				if at+2 > len(ipma) {
					return width, height
				}
				index, at = int(binary.BigEndian.Uint16(ipma[at:])&0x7fff), at+2
			} else {
				if at >= len(ipma) {
					return width, height
				}
				index, at = int(ipma[at]&0x7f), at+1
			}
			if item != primary || index < 1 || index > len(properties) {
				continue
			}
			switch property := properties[index-1]; {
			case property.kind == "ispe" && len(property.body) >= 12:
				width, height = dimension(property.body[4:]), dimension(property.body[8:])
			case property.kind == "clap" && len(property.body) >= 16:
				if across, down := dimension(property.body[4:]), dimension(property.body[12:]); across > 0 && down > 0 {
					width, height = dimension(property.body)/across, dimension(property.body[8:])/down
				}
			case property.kind == "irot" && len(property.body) >= 1 && property.body[0]&1 == 1:
				width, height = height, width
			}
		}
	}
	return width, height
}

func dimension(data []byte) int { return int(binary.BigEndian.Uint32(data) & 0x7fffffff) }

type box struct {
	kind string
	body []byte
}

func eachBox(data []byte, visit func(box)) {
	for at, count := 0, 0; at+8 <= len(data) && count < 4096; count++ {
		size, header := uint64(binary.BigEndian.Uint32(data[at:])), uint64(8)
		switch size {
		case 1:
			if at+16 > len(data) {
				return
			}
			size, header = binary.BigEndian.Uint64(data[at+8:]), 16
		case 0:
			size = uint64(len(data) - at)
		}
		if size < header || size > uint64(len(data)-at) {
			return
		}
		visit(box{kind: string(data[at+4 : at+8]), body: data[at+int(header) : at+int(size)]})
		at += int(size)
	}
}

func childBox(data []byte, kind string) []byte {
	var found []byte
	eachBox(data, func(b box) {
		if b.kind == kind && found == nil {
			found = b.body
		}
	})
	return found
}
