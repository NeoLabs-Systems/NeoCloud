package imaging

// Metadata is what a picture says about itself without being decoded. Dates
// are EXIF text ("YYYY:MM:DD HH:MM:SS"); absent values are empty.
type Metadata struct {
	// Width and Height are the size as shown, zero when unknown.
	Width, Height    int
	Make, Model      string
	DateTimeOriginal string
	DateTime         string
}

// Inspect reads a picture's header and EXIF block. data may be only the head
// of a file; what lies beyond it is left empty.
func Inspect(data []byte) (found Metadata) {
	defer func() { _ = recover() }()
	kind := sniff(data)
	tags := parseEXIF(rawEXIF(kind, data))
	found = Metadata{Make: tags.make, Model: tags.model, DateTimeOriginal: tags.original, DateTime: tags.dateTime}
	if header, err := config(kind, data); err == nil {
		found.Width, found.Height = header.Width, header.Height
	}
	switch kind {
	case formatHEIC, formatAVIF:
		if width, height := heifSize(data); width > 0 && height > 0 {
			found.Width, found.Height = width, height
		}
	default:
		if turned(tags.orientation) {
			found.Width, found.Height = found.Height, found.Width
		}
	}
	return found
}
