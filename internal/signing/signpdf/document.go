// Package signpdf is the PDF engine of the signing feature: pure functions
// over bytes, no database and no HTTP.
package signpdf

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	// MaxPDFBytes caps the PDFs accepted for probing and signing.
	MaxPDFBytes = 50 << 20
	MaxPages    = 300
	// maxDecodedStream bounds what one object or xref stream may inflate to
	// while a file loads, so a small file cannot exhaust memory.
	maxDecodedStream = 64 << 20
)

// Error is a refusal of the engine; Code is part of the API contract.
type Error struct {
	Code string
	err  error
}

func (e *Error) Error() string {
	if e.err != nil {
		return "signpdf: " + e.Code + ": " + e.err.Error()
	}
	return "signpdf: " + e.Code
}

func (e *Error) Unwrap() error { return e.err }

const (
	CodeInvalid      = "PDF_INVALID"
	CodeEncrypted    = "PDF_ENCRYPTED"
	CodeTooLarge     = "PDF_TOO_LARGE"
	CodeTooManyPages = "PDF_TOO_MANY_PAGES"
	CodeImage        = "IMAGE_INVALID"
	CodeSigning      = "SIGNING_FAILED"
	CodeHash         = "HASH_MISMATCH"
)

func refuse(code string, err error) error { return &Error{Code: code, err: err} }

//lint:ignore SA1019 the replacement needs a configuration at every call
func init() { api.DisableConfigDir() }

// document is a loaded PDF whose object graph is edited in memory and written
// out again by serialize.
type document struct {
	ctx   *model.Context
	root  types.Dict
	pages []*page
	next  int
}

type page struct {
	ref    types.IndirectRef
	dict   types.Dict
	box    rect // visible box in user space
	rotate int
}

// load parses a PDF. maxBytes <= 0 means unlimited (for files NeoCloud made).
func load(ctx context.Context, data []byte, maxBytes int) (doc *document, err error) {
	if len(data) == 0 {
		return nil, refuse(CodeInvalid, nil)
	}
	if maxBytes > 0 && len(data) > maxBytes {
		return nil, refuse(CodeTooLarge, nil)
	}
	if !bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-")) {
		return nil, refuse(CodeInvalid, nil)
	}
	// The parser works on hostile input: a panic in it is a broken file.
	defer func() {
		if r := recover(); r != nil {
			doc, err = nil, refuse(CodeInvalid, fmt.Errorf("parser panic: %v", r))
		}
	}()
	conf := model.NewDefaultConfiguration()
	conf.Offline = true
	conf.Limits = model.DefaultResourceLimits()
	conf.Limits.MaxDecodeBytes = maxDecodedStream
	conf.Limits.MaxStreamBytes = int64(len(data))
	conf.Limits.MaxObjectCount = 1_000_000
	conf.Limits.MaxXRefEntries = 1_000_000
	looksEncrypted := bytes.Contains(data[max(0, len(data)-4096):], []byte("/Encrypt"))
	pctx, err := api.ReadContext(ctx, bytes.NewReader(data), conf)
	if err != nil {
		if errors.Is(err, pdfcpu.ErrEncrypted) || errors.Is(err, pdfcpu.ErrWrongPassword) || looksEncrypted {
			return nil, refuse(CodeEncrypted, err)
		}
		return nil, refuse(CodeInvalid, err)
	}
	if pctx.XRefTable.Encrypt != nil || pctx.XRefTable.E != nil {
		return nil, refuse(CodeEncrypted, nil)
	}
	if pctx.XRefTable.Root == nil {
		return nil, refuse(CodeInvalid, nil)
	}
	doc = &document{ctx: pctx}
	doc.root = doc.dict(*pctx.XRefTable.Root)
	if doc.root == nil {
		return nil, refuse(CodeInvalid, nil)
	}
	for number := range pctx.XRefTable.Table {
		doc.next = max(doc.next, number+1)
	}
	if err = doc.readPages(doc.root["Pages"], inherited{}, map[int]bool{}, 0); err != nil {
		return nil, err
	}
	if len(doc.pages) == 0 {
		return nil, refuse(CodeInvalid, nil)
	}
	return doc, nil
}

type inherited struct {
	resources, media, crop, rotate types.Object
}

func (d *document) readPages(node types.Object, parent inherited, seen map[int]bool, depth int) error {
	ref, ok := node.(types.IndirectRef)
	if !ok || depth > 64 || seen[ref.ObjectNumber.Value()] {
		return refuse(CodeInvalid, errors.New("damaged page tree"))
	}
	seen[ref.ObjectNumber.Value()] = true
	dict := d.dict(ref)
	if dict == nil {
		return refuse(CodeInvalid, errors.New("damaged page tree"))
	}
	own := parent
	if v, ok := dict["Resources"]; ok {
		own.resources = v
	}
	if v, ok := dict["MediaBox"]; ok {
		own.media = v
	}
	if v, ok := dict["CropBox"]; ok {
		own.crop = v
	}
	if v, ok := dict["Rotate"]; ok {
		own.rotate = v
	}
	if kids, ok := d.resolve(dict["Kids"]).(types.Array); ok {
		for _, kid := range kids {
			if err := d.readPages(kid, own, seen, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if len(d.pages) >= MaxPages {
		return refuse(CodeTooManyPages, nil)
	}
	// Inherited attributes are written onto the page so that editing one page
	// never changes another.
	for key, value := range map[string]types.Object{"Resources": own.resources, "MediaBox": own.media, "CropBox": own.crop, "Rotate": own.rotate} {
		if value != nil {
			dict[key] = value
		}
	}
	media, ok := d.rectangle(own.media)
	if !ok || media.w <= 0 || media.h <= 0 {
		media = rect{0, 0, 612, 792}
	}
	box := media
	if crop, ok := d.rectangle(own.crop); ok {
		if both, ok := media.intersect(crop); ok {
			box = both
		}
	}
	d.pages = append(d.pages, &page{ref: ref, dict: dict, box: box, rotate: normalizeRotation(d.number(own.rotate))})
	d.set(ref, dict)
	return nil
}

func (d *document) resolve(obj types.Object) types.Object {
	switch v := obj.(type) {
	case types.IndirectRef:
		resolved, err := d.ctx.XRefTable.Dereference(v)
		if err != nil {
			return nil
		}
		return resolved
	default:
		return obj
	}
}

func (d *document) dict(obj types.Object) types.Dict {
	switch v := d.resolve(obj).(type) {
	case types.Dict:
		return v
	case types.StreamDict:
		return v.Dict
	default:
		return nil
	}
}

func (d *document) array(obj types.Object) types.Array {
	a, _ := d.resolve(obj).(types.Array)
	return a
}

func (d *document) number(obj types.Object) float64 {
	switch v := d.resolve(obj).(type) {
	case types.Integer:
		return float64(v.Value())
	case types.Float:
		return v.Value()
	default:
		return 0
	}
}

func (d *document) name(obj types.Object) string {
	n, _ := d.resolve(obj).(types.Name)
	return string(n)
}

// text decodes a PDF text string (literal or hex, PDFDocEncoding or UTF-16).
func (d *document) text(obj types.Object) string {
	var raw []byte
	switch v := d.resolve(obj).(type) {
	case types.StringLiteral:
		b, err := types.Unescape(v.Value())
		if err != nil {
			return ""
		}
		raw = b
	case types.HexLiteral:
		b, err := v.Bytes()
		if err != nil {
			return ""
		}
		raw = b
	case types.Name:
		return string(v)
	default:
		return ""
	}
	if len(raw) >= 2 && raw[0] == 0xfe && raw[1] == 0xff {
		units := make([]uint16, 0, len(raw)/2)
		for i := 2; i+1 < len(raw); i += 2 {
			units = append(units, uint16(raw[i])<<8|uint16(raw[i+1]))
		}
		return string(utf16.Decode(units))
	}
	if len(raw) >= 3 && raw[0] == 0xef && raw[1] == 0xbb && raw[2] == 0xbf {
		return string(raw[3:])
	}
	out := make([]rune, len(raw))
	for i, b := range raw {
		out[i] = rune(b)
	}
	return string(out)
}

func (d *document) rectangle(obj types.Object) (rect, bool) {
	a := d.array(obj)
	if len(a) != 4 {
		return rect{}, false
	}
	x1, y1, x2, y2 := d.number(a[0]), d.number(a[1]), d.number(a[2]), d.number(a[3])
	return rect{math.Min(x1, x2), math.Min(y1, y2), math.Abs(x2 - x1), math.Abs(y2 - y1)}, true
}

// set stores obj under an existing reference.
func (d *document) set(ref types.IndirectRef, obj types.Object) {
	number := ref.ObjectNumber.Value()
	if entry, ok := d.ctx.XRefTable.Table[number]; ok {
		entry.Object, entry.Free, entry.Compressed = obj, false, false
		return
	}
	generation := ref.GenerationNumber.Value()
	d.ctx.XRefTable.Table[number] = &model.XRefTableEntry{Object: obj, Generation: &generation}
}

// add stores a new object and returns its reference.
func (d *document) add(obj types.Object) types.IndirectRef {
	ref := *types.NewIndirectRef(d.next, 0)
	d.next++
	d.set(ref, obj)
	return ref
}

// stream adds a compressed stream object.
func (d *document) stream(dict types.Dict, content []byte) types.IndirectRef {
	return d.add(flateStream(dict, content))
}

func textString(s string) types.Object {
	units := utf16.Encode([]rune(s))
	raw := []byte{0xfe, 0xff}
	for _, u := range units {
		raw = append(raw, byte(u>>8), byte(u))
	}
	return types.NewHexLiteral(raw)
}

func formatReal(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	s := strconv.FormatFloat(math.Round(v*1e6)/1e6, 'f', -1, 64)
	if s == "-0" {
		return "0"
	}
	return s
}

// serialize writes the objects reachable from the catalog and the information
// dictionary as a fresh file with a classic cross-reference table. Nothing
// else of the source survives, and equal inputs give equal bytes. raw objects
// are written verbatim; their offsets are returned.
func (d *document) serialize(info types.Dict, raw map[int]string) ([]byte, map[int]int, error) {
	infoRef := d.add(info)
	rootRef := *d.ctx.XRefTable.Root
	numbers := map[int]int{}
	var order []int
	var queue []types.IndirectRef
	visit := func(ref types.IndirectRef) int {
		old := ref.ObjectNumber.Value()
		if n, ok := numbers[old]; ok {
			return n
		}
		numbers[old] = len(order) + 1
		order = append(order, old)
		queue = append(queue, ref)
		return numbers[old]
	}
	visit(rootRef)
	visit(infoRef)

	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := []int{0}
	rawOffsets := map[int]int{}
	for i := 0; i < len(queue); i++ {
		old := queue[i].ObjectNumber.Value()
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n", numbers[old])
		if body, ok := raw[old]; ok {
			rawOffsets[old] = out.Len()
			// References inside a raw body are not followed.
			out.WriteString(body)
		} else if err := d.write(&out, d.resolve(queue[i]), visit, 0); err != nil {
			return nil, nil, err
		}
		out.WriteString("\nendobj\n")
		if out.Len() > 4*MaxPDFBytes {
			return nil, nil, refuse(CodeTooLarge, nil)
		}
	}
	sum := md5.Sum(out.Bytes())
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R /ID [<%x> <%x>] >>\nstartxref\n%d\n%%%%EOF\n",
		len(offsets), numbers[rootRef.ObjectNumber.Value()], numbers[infoRef.ObjectNumber.Value()], sum, sum, xref)
	return out.Bytes(), rawOffsets, nil
}

func (d *document) write(out *bytes.Buffer, obj types.Object, visit func(types.IndirectRef) int, depth int) error {
	if depth > 256 {
		return refuse(CodeInvalid, errors.New("objects nested too deeply"))
	}
	switch v := obj.(type) {
	case nil:
		out.WriteString("null")
	case types.IndirectRef:
		entry, ok := d.ctx.XRefTable.Table[v.ObjectNumber.Value()]
		if !ok || entry.Free {
			out.WriteString("null")
			return nil
		}
		fmt.Fprintf(out, "%d 0 R", visit(v))
	case types.Dict:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteString("<<")
		for _, key := range keys {
			out.WriteString(types.Name(key).PDFString())
			out.WriteByte(' ')
			if err := d.write(out, v[key], visit, depth+1); err != nil {
				return err
			}
		}
		out.WriteString(">>")
	case types.Array:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(' ')
			}
			if err := d.write(out, item, visit, depth+1); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case types.StreamDict:
		dict := types.Dict{}
		for key, value := range v.Dict {
			dict[key] = value
		}
		dict["Length"] = types.Integer(len(v.Raw))
		if err := d.write(out, dict, visit, depth+1); err != nil {
			return err
		}
		out.WriteString("\nstream\n")
		out.Write(v.Raw)
		out.WriteString("\nendstream")
	case types.Float:
		out.WriteString(formatReal(v.Value()))
	case types.LazyObjectStreamObject:
		decoded, err := v.DecodedObject(context.Background())
		if err != nil {
			return refuse(CodeInvalid, err)
		}
		return d.write(out, decoded, visit, depth+1)
	case types.ObjectStreamDict, types.XRefStreamDict:
		out.WriteString("null")
	default:
		s := v.PDFString()
		if strings.TrimSpace(s) == "" {
			s = "null"
		}
		out.WriteString(s)
	}
	return nil
}

func newContext() *model.Context {
	return &model.Context{XRefTable: &model.XRefTable{Table: map[int]*model.XRefTableEntry{}}}
}
