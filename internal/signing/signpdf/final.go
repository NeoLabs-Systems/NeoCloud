package signpdf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Area is a box on a page: page from 0, the rest fractions of the displayed
// page with the origin top-left. Radio and multiple areas may name an option.
type Area struct {
	Page   int     `json:"page"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
	Option *string `json:"option,omitempty"`
}

// Prefs are the display preferences of a field.
type Prefs struct {
	FontSize   float64  `json:"fontSize"`
	FontType   string   `json:"fontType"`
	Color      string   `json:"color"`
	Background string   `json:"background"`
	Align      string   `json:"align"`
	Valign     string   `json:"valign"`
	Format     string   `json:"format"`
	Multiline  bool     `json:"multiline"`
	Cells      int      `json:"cells"`
	Options    []string `json:"options"`
}

// Value is what a signer (or the sender's default) put into a field.
type Value struct {
	Data     any    // the JSON value of a typed field
	Image    []byte // signature, initials, stamp and image fields
	Method   string // draw, type or upload
	Typed    string // the text of a typed signature
	File     []byte // file fields
	FileName string
	Mime     string
}

type Field struct {
	ID, PartyID, Type, Name string
	Default                 any
	Prefs                   Prefs
	Areas                   []Area
	Value                   *Value
}

type Party struct {
	ID, Name, Email, Color, SignerName, IP, UserAgent, Timezone string
	TZOffsetMinutes                                             *int
	OpenedAt, ConsentedAt, CompletedAt, DeclinedAt              string
	DeclineReason                                               string
}

type Event struct{ Type, At, PartyID, IP, Hash string }

type Document struct {
	ID, Title, SigningOrder, CreatedAt, SentAt string
	FinalizedAt                                time.Time
	OriginalSHA256                             string
	ConsentVersion                             int
}

// FinalInput is everything the final file is made of; equal inputs give equal
// bytes.
type FinalInput struct {
	PDF           []byte
	Parties       []Party // in signing order
	Fields        []Field // in field order
	Events        []Event
	Document      Document
	Locale        string
	Signer        *Signer // seals the result when set
	NoCertificate bool    // leave out the audit pages
}

type attachment struct {
	fieldName, filename, mime, sha256 string
	data                              []byte
}

func producerInfo() types.Dict {
	return types.Dict{"Producer": textString("NeoCloud"), "Creator": textString("NeoCloud")}
}

func pdfDate(at time.Time) types.Object {
	return types.StringLiteral("D:" + at.UTC().Format("20060102150405") + "Z")
}

func language(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "de") {
		return "de"
	}
	return "en"
}

// Build draws every value onto the frozen original, flattens its own form,
// strips active content, appends the certificate pages and applies the seal.
func Build(ctx context.Context, in FinalInput) ([]byte, string, error) {
	sum := sha256.Sum256(in.PDF)
	originalHash := hex.EncodeToString(sum[:])
	if in.Document.OriginalSHA256 != "" && !strings.EqualFold(in.Document.OriginalSHA256, originalHash) {
		return nil, "", refuse(CodeHash, nil)
	}
	doc, err := load(ctx, in.PDF, 0)
	if err != nil {
		return nil, "", err
	}
	originalPages := len(doc.pages)
	lang := language(in.Locale)
	faces := newFonts(doc)
	doc.stripActiveContent()
	doc.flattenForm(faces)

	r := &renderer{ctx: ctx, doc: doc, fonts: faces, locale: lang, canvases: map[int]*canvas{}, images: map[[32]byte]embedded{}}
	var files []attachment
	used := map[string]bool{}
	for _, field := range in.Fields {
		value := field.Value
		if value == nil && field.Default != nil {
			value = &Value{Data: field.Default}
		}
		fileName := ""
		if field.Type == "file" && value != nil && len(value.File) > 0 {
			fileName = uniqueName(value.FileName, used)
			digest := sha256.Sum256(value.File)
			files = append(files, attachment{field.Name, fileName, value.Mime, hex.EncodeToString(digest[:]), value.File})
		}
		if err := r.field(field, value, fileName); err != nil {
			return nil, "", err
		}
	}
	for index, p := range doc.pages {
		if c := r.canvases[index]; c != nil {
			doc.apply(p, c, true)
		}
	}
	doc.attach(files, in.Document.FinalizedAt)
	if !in.NoCertificate {
		appendCertificate(doc, faces, lang, certificateData{
			document: in.Document, parties: in.Parties, events: in.Events, originalHash: originalHash,
			originalPages: originalPages, files: files, methods: signingMethods(in.Fields),
		})
	}
	if err := faces.finish(doc); err != nil {
		return nil, "", err
	}

	info := types.Dict{}
	if source := doc.ctx.XRefTable.Info; source != nil {
		info = copyDict(doc.dict(*source))
	}
	for key, value := range producerInfo() {
		info[key] = value
	}
	if in.Document.Title != "" {
		info["Title"] = textString(in.Document.Title)
	}
	info["ModDate"] = pdfDate(in.Document.FinalizedAt)
	doc.root["Lang"] = types.StringLiteral(lang)
	delete(doc.root, "Version")

	var out []byte
	if in.Signer != nil {
		out, err = doc.serializeSealed(info, in.Signer, in.Document.FinalizedAt)
	} else {
		out, _, err = doc.serialize(info, nil)
	}
	if err != nil {
		return nil, "", err
	}
	final := sha256.Sum256(out)
	return out, hex.EncodeToString(final[:]), nil
}

func uniqueName(name string, used map[string]bool) string {
	base := []rune(strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name))
	if len(base) > 120 {
		base = base[:120]
	}
	clean := string(base)
	if clean == "" {
		clean = "attachment"
	}
	candidate := clean
	for n := 2; used[candidate]; n++ {
		candidate = clean + " (" + strconv.Itoa(n) + ")"
	}
	used[candidate] = true
	return candidate
}

// attach embeds the signers' files, keeping the ones the source already had.
func (d *document) attach(files []attachment, at time.Time) {
	if len(files) == 0 {
		return
	}
	names := copyDict(d.dict(d.root["Names"]))
	entries := map[string]types.Object{}
	var collect func(node types.Dict, depth int)
	collect = func(node types.Dict, depth int) {
		if node == nil || depth > 32 {
			return
		}
		pairs := d.array(node["Names"])
		for i := 0; i+1 < len(pairs); i += 2 {
			entries[d.text(pairs[i])] = pairs[i+1]
		}
		for _, kid := range d.array(node["Kids"]) {
			collect(d.dict(kid), depth+1)
		}
	}
	collect(d.dict(names["EmbeddedFiles"]), 0)
	for _, file := range files {
		mime := file.mime
		if mime == "" {
			mime = "application/octet-stream"
		}
		stream := d.stream(types.Dict{"Type": types.Name("EmbeddedFile"), "Subtype": types.Name(mime),
			"Params": types.Dict{"Size": types.Integer(len(file.data)), "CreationDate": pdfDate(at), "ModDate": pdfDate(at)}}, file.data)
		description := file.fieldName
		if description == "" {
			description = file.filename
		}
		key := file.filename
		for n := 2; entries[key] != nil; n++ {
			key = fmt.Sprintf("%s (%d)", file.filename, n)
		}
		entries[key] = d.add(types.Dict{"Type": types.Name("Filespec"), "F": textString(file.filename), "UF": textString(file.filename),
			"Desc": textString(description), "EF": types.Dict{"F": stream, "UF": stream}})
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	pairs := types.Array{}
	for _, key := range keys {
		pairs = append(pairs, textString(key), entries[key])
	}
	names["EmbeddedFiles"] = types.Dict{"Names": pairs}
	d.root["Names"] = names
}

// signingMethods lists which ways of signing (draw, type, upload) each party used.
func signingMethods(fields []Field) map[string][]string {
	methods := map[string][]string{}
	for _, field := range fields {
		if field.Type != "signature" && field.Type != "initials" || field.Value == nil || field.Value.Method == "" {
			continue
		}
		if !slices.Contains(methods[field.PartyID], field.Value.Method) {
			methods[field.PartyID] = append(methods[field.PartyID], field.Value.Method)
		}
	}
	return methods
}

// scalar renders a JSON value as the text it stands for.
func scalar(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = scalar(item)
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(v)
	}
}
