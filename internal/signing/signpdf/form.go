package signpdf

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	flagReadOnly    = 1
	flagRequired    = 1 << 1
	flagMultiline   = 1 << 12
	flagRadio       = 1 << 15
	flagPushButton  = 1 << 16
	flagMultiSelect = 1 << 21
	flagComb        = 1 << 24

	annotHidden = 2
	annotNoView = 32
)

// formField is a terminal field of the AcroForm with its inherited attributes.
type formField struct {
	name         string
	kind, da     string
	flags, align int
	maxLen       int
	tooltip      string
	value, opt   types.Object
	widgets      []types.IndirectRef
}

// formFields walks the field tree of the document.
func (d *document) formFields() []formField {
	form := d.dict(d.root["AcroForm"])
	if form == nil {
		return nil
	}
	var out []formField
	seen := map[int]bool{}
	var walk func(node types.Object, parent formField, depth int)
	walk = func(node types.Object, parent formField, depth int) {
		ref, ok := node.(types.IndirectRef)
		if !ok || depth > 32 || seen[ref.ObjectNumber.Value()] || len(out) > 5000 {
			return
		}
		seen[ref.ObjectNumber.Value()] = true
		dict := d.dict(ref)
		if dict == nil {
			return
		}
		f := parent
		f.widgets = nil
		if t := d.text(dict["T"]); t != "" {
			if f.name != "" {
				f.name += "."
			}
			f.name += t
		}
		if v := d.name(dict["FT"]); v != "" {
			f.kind = v
		}
		if v, ok := dict["Ff"]; ok {
			f.flags = int(d.number(v))
		}
		if v, ok := dict["Q"]; ok {
			f.align = int(d.number(v))
		}
		if v, ok := dict["MaxLen"]; ok {
			f.maxLen = int(d.number(v))
		}
		if v := d.text(dict["TU"]); v != "" {
			f.tooltip = v
		}
		if v := d.text(dict["DA"]); v != "" {
			f.da = v
		}
		if v, ok := dict["V"]; ok {
			f.value = v
		}
		if v, ok := dict["Opt"]; ok {
			f.opt = v
		}
		var children []types.Object
		for _, kid := range d.array(dict["Kids"]) {
			child := d.dict(kid)
			if child != nil && (child["T"] != nil || child["Kids"] != nil) {
				children = append(children, kid)
			} else if kidRef, ok := kid.(types.IndirectRef); ok && child != nil {
				f.widgets = append(f.widgets, kidRef)
			}
		}
		if len(children) > 0 {
			for _, child := range children {
				walk(child, f, depth+1)
			}
			return
		}
		if len(f.widgets) == 0 && d.name(dict["Subtype"]) == "Widget" {
			f.widgets = []types.IndirectRef{ref}
		}
		if f.kind != "" {
			out = append(out, f)
		}
	}
	defaults := formField{da: d.text(form["DA"]), align: int(d.number(form["Q"]))}
	for _, node := range d.array(form["Fields"]) {
		walk(node, defaults, 0)
	}
	return out
}

// widgetPages maps annotation object numbers to the page that shows them.
func (d *document) widgetPages() map[int]int {
	pages := map[int]int{}
	for index, p := range d.pages {
		for _, annot := range d.array(p.dict["Annots"]) {
			if ref, ok := annot.(types.IndirectRef); ok {
				pages[ref.ObjectNumber.Value()] = index
			}
		}
	}
	return pages
}

func (d *document) visible(widget types.Dict) bool {
	return int(d.number(widget["F"]))&(annotHidden|annotNoView) == 0
}

// options are the display texts of a choice field's /Opt entries.
func (d *document) options(opt types.Object) []string {
	var out []string
	for _, entry := range d.array(opt) {
		if pair := d.array(entry); len(pair) == 2 {
			out = append(out, d.text(pair[1]))
		} else {
			out = append(out, d.text(entry))
		}
	}
	return out
}

// onState is the appearance state name of a button widget that means "on".
func (d *document) onState(widget types.Dict) string {
	states := d.dict(d.dict(widget["AP"])["N"])
	names := make([]string, 0, len(states))
	for name := range states {
		if name != "Off" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if len(names) == 0 {
		return ""
	}
	decoded, err := types.DecodeName(names[0])
	if err != nil {
		return names[0]
	}
	return decoded
}

var (
	daSize = regexp.MustCompile(`([\d.]+)\s+Tf`)
	daRGB  = regexp.MustCompile(`([\d.]+)\s+([\d.]+)\s+([\d.]+)\s+rg`)
	daGray = regexp.MustCompile(`([\d.]+)\s+g(?:\s|$)`)
)

// prefsFromDA reads font size and colour of a default appearance string.
func prefsFromDA(da string) map[string]any {
	prefs := map[string]any{}
	if m := daSize.FindStringSubmatch(da); m != nil {
		if size, err := strconv.ParseFloat(m[1], 64); err == nil && size > 0 {
			prefs["fontSize"] = size
		}
	}
	var channels []string
	if m := daRGB.FindStringSubmatch(da); m != nil {
		channels = m[1:]
	} else if m := daGray.FindStringSubmatch(da); m != nil {
		channels = []string{m[1], m[1], m[1]}
	}
	if channels != nil {
		hex := ""
		for _, c := range channels {
			v, _ := strconv.ParseFloat(c, 64)
			hex += fmt.Sprintf("%02x", int(math.Round(math.Min(1, v)*255)))
		}
		if hex != "000000" {
			prefs["color"] = "#" + hex
		}
	}
	return prefs
}

// ImportedField is a field found in the PDF as a draft signing field: an
// AcroForm field (Source "form") or a spot the page layout marks for one
// (Source "layout"). Its preference values are what a JSON decoder would
// produce. Role says what it is for: signature, initials, date, name, place,
// text, checkbox, radio, select, multiple or cells. Confidence runs from 0 to
// 1; form fields have 1.
type ImportedField struct {
	Type, Name string
	Required   bool
	Prefs      map[string]any
	Areas      []Area
	Role       string
	Confidence float64
	Source     string
}

// ImportFields lists the form fields of a PDF in reading order. Read-only
// fields and push buttons are left alone: they are flattened into the page.
func ImportFields(ctx context.Context, data []byte) ([]ImportedField, error) {
	doc, err := load(ctx, data, MaxPDFBytes)
	if err != nil {
		return nil, err
	}
	out := doc.importForm()
	for i := range out {
		if out[i].Role == "" {
			out[i].Role = "text"
		}
	}
	sortReading(out)
	return out, nil
}

func (d *document) importForm() []ImportedField {
	pages := d.widgetPages()
	var out []ImportedField
	for _, f := range d.formFields() {
		if f.flags&flagReadOnly != 0 {
			continue
		}
		spec := ImportedField{Name: f.name, Required: f.flags&flagRequired != 0, Prefs: map[string]any{}, Confidence: 1, Source: "form"}
		var options []string
		switch f.kind {
		case "Tx":
			spec.Type = "text"
			spec.Prefs = prefsFromDA(f.da)
			if f.align == 1 {
				spec.Prefs["align"] = "center"
			} else if f.align == 2 {
				spec.Prefs["align"] = "right"
			}
			if f.flags&flagComb != 0 && f.maxLen > 0 {
				spec.Type, spec.Prefs["cells"] = "cells", float64(f.maxLen)
			} else if f.flags&flagMultiline != 0 {
				spec.Prefs["multiline"] = true
			}
		case "Btn":
			switch {
			case f.flags&flagPushButton != 0:
				continue
			case f.flags&flagRadio != 0:
				spec.Type = "radio"
				options = d.options(f.opt)
			default:
				spec.Type = "checkbox"
			}
		case "Ch":
			spec.Type = "select"
			if f.flags&flagMultiSelect != 0 {
				spec.Type = "multiple"
			}
			options = d.options(f.opt)
		case "Sig":
			spec.Type = "signature"
		default:
			continue
		}
		spec.Role = spec.Type
		if spec.Type == "text" {
			spec.Role = ""
			if roles := labelRoles(fieldNameWords(f.name + " " + f.tooltip)); len(roles) > 0 {
				spec.Role = roles[0].role
				for _, r := range roles {
					if r.role == "date" {
						spec.Role = "date"
					}
				}
			}
		}
		radioOptions := slices.Clone(options)
		for _, ref := range f.widgets {
			widget := d.dict(ref)
			index, onPage := pages[ref.ObjectNumber.Value()]
			if widget == nil || !onPage || !d.visible(widget) {
				continue
			}
			box, ok := d.rectangle(widget["Rect"])
			if !ok {
				continue
			}
			area := d.pages[index].area(box)
			if area.W < 0.001 || area.H < 0.001 {
				continue
			}
			area.Page = index
			if spec.Type == "radio" {
				// A widget's on state is the option itself, or its index into /Opt.
				on := d.onState(widget)
				option := on
				if n, err := strconv.Atoi(on); err == nil && n >= 0 && n < len(radioOptions) && !slices.Contains(radioOptions, on) {
					option = radioOptions[n]
				}
				if option != "" {
					area.Option = &option
					if !slices.Contains(options, option) {
						options = append(options, option)
					}
				}
			}
			spec.Areas = append(spec.Areas, area)
		}
		if len(spec.Areas) == 0 {
			continue
		}
		if spec.Type == "radio" || spec.Type == "select" || spec.Type == "multiple" {
			unique := []any{}
			seen := map[string]bool{}
			for _, option := range options {
				if option = strings.TrimSpace(option); option != "" && !seen[option] {
					seen[option] = true
					unique = append(unique, option)
				}
			}
			if len(unique) > 0 {
				spec.Prefs["options"] = unique
			}
		}
		out = append(out, spec)
	}
	return out
}

// retype makes a form text field the kind its role asks for: a text box a
// form calls "Signature" becomes a signature field.
func (f *ImportedField) retype() {
	if f.Type != "text" {
		return
	}
	switch f.Role {
	case "signature", "initials":
		f.Type, f.Prefs = f.Role, map[string]any{}
	case "date":
		prefs := map[string]any{}
		for _, key := range []string{"fontSize", "color", "align"} {
			if v, ok := f.Prefs[key]; ok {
				prefs[key] = v
			}
		}
		f.Type, f.Prefs = "date", prefs
	case "":
		f.Role = "text"
	}
}

func sortReading(out []ImportedField) {
	slices.SortStableFunc(out, func(a, b ImportedField) int {
		x, y := a.Areas[0], b.Areas[0]
		if x.Page != y.Page {
			return x.Page - y.Page
		}
		if math.Abs(x.Y-y.Y) > 0.004 {
			if x.Y < y.Y {
				return -1
			}
			return 1
		}
		if x.X < y.X {
			return -1
		} else if x.X > y.X {
			return 1
		}
		return 0
	})
}

// stripActiveContent removes what a sealed document must not carry from its
// source: open and additional actions and JavaScript.
func (d *document) stripActiveContent() {
	delete(d.root, "OpenAction")
	delete(d.root, "AA")
	delete(d.root, "Perms")
	if names := d.dict(d.root["Names"]); names != nil {
		names = copyDict(names)
		delete(names, "JavaScript")
		d.root["Names"] = names
	}
	for _, p := range d.pages {
		delete(p.dict, "AA")
		for _, annot := range d.array(p.dict["Annots"]) {
			dict := d.dict(annot)
			if dict == nil {
				continue
			}
			delete(dict, "AA")
			if kind := d.name(d.dict(dict["A"])["S"]); kind == "JavaScript" || kind == "Launch" || kind == "SubmitForm" || kind == "ImportData" {
				delete(dict, "A")
			}
		}
	}
}

// flattenForm bakes the source's form fields into the page content so nothing
// in the final file stays editable. Existing appearances are kept; text the
// file asks readers to generate is drawn with the bundled fonts.
func (d *document) flattenForm(faces fonts) {
	form := d.dict(d.root["AcroForm"])
	needsAppearances := form != nil && d.resolve(form["NeedAppearances"]) == types.Boolean(true)
	byWidget := map[int]formField{}
	for _, f := range d.formFields() {
		for _, ref := range f.widgets {
			byWidget[ref.ObjectNumber.Value()] = f
		}
	}
	names := 0
	for _, p := range d.pages {
		annots := d.array(p.dict["Annots"])
		if annots == nil {
			continue
		}
		kept := types.Array{}
		existing := newCanvas()
		generated := &renderer{doc: d, fonts: faces, canvases: map[int]*canvas{0: newCanvas()}}
		for _, annot := range annots {
			widget := d.dict(annot)
			if widget == nil {
				continue
			}
			if d.name(widget["Subtype"]) != "Widget" {
				kept = append(kept, annot)
				continue
			}
			box, ok := d.rectangle(widget["Rect"])
			if !ok || box.w <= 0 || box.h <= 0 || !d.visible(widget) {
				continue
			}
			field := formField{}
			if ref, ok := annot.(types.IndirectRef); ok {
				field = byWidget[ref.ObjectNumber.Value()]
			}
			appearance, state := d.appearance(widget)
			textual := field.kind == "Tx" || field.kind == "Ch"
			if textual && (needsAppearances || appearance == nil) {
				area := p.area(box)
				var lines []string
				if list := d.array(field.value); list != nil {
					for _, item := range list {
						lines = append(lines, d.text(item))
					}
				} else if v := d.text(field.value); v != "" {
					lines = []string{v}
				}
				prefs := Prefs{Multiline: field.flags&flagMultiline != 0, Align: []string{"left", "center", "right"}[max(0, min(2, field.align))]}
				if size, ok := prefsFromDA(field.da)["fontSize"].(float64); ok {
					prefs.FontSize = size
				}
				generated.textBox(generated.canvases[0], p.displayed(area), strings.Join(lines, "\n"), prefs, false, 0)
				continue
			}
			if appearance == nil {
				if field.kind == "Btn" && state != "" && state != "Off" {
					checkbox(generated.canvases[0], p.displayed(p.area(box)), true, Prefs{})
				}
				continue
			}
			ref, transform, ok := d.placeAppearance(*appearance, box)
			if !ok {
				continue
			}
			names++
			existing.xobject(fmt.Sprintf("NCW%d", names), ref, transform)
		}
		p.dict["Annots"] = kept
		d.apply(p, existing, false)
		d.apply(p, generated.canvases[0], true)
	}
	delete(d.root, "AcroForm")
}

// appearance is the normal appearance stream a widget currently shows.
func (d *document) appearance(widget types.Dict) (*types.IndirectRef, string) {
	state := d.name(widget["AS"])
	normal := d.dict(widget["AP"])["N"]
	ref, isRef := normal.(types.IndirectRef)
	if isRef {
		if _, ok := d.resolve(ref).(types.StreamDict); ok {
			return &ref, state
		}
	}
	if states := d.dict(normal); states != nil {
		chosen := state
		if chosen == "" {
			chosen = "Off"
		}
		if r, ok := states[chosen].(types.IndirectRef); ok {
			if _, ok := d.resolve(r).(types.StreamDict); ok {
				return &r, state
			}
		}
	}
	return nil, state
}

// placeAppearance returns the matrix that puts a form XObject into a widget's
// rectangle.
func (d *document) placeAppearance(ref types.IndirectRef, box rect) (types.IndirectRef, [6]float64, bool) {
	stream, ok := d.resolve(ref).(types.StreamDict)
	if !ok {
		return ref, [6]float64{}, false
	}
	bounds, ok := d.rectangle(stream.Dict["BBox"])
	if !ok {
		return ref, [6]float64{}, false
	}
	m := [6]float64{1, 0, 0, 1, 0, 0}
	if values := d.array(stream.Dict["Matrix"]); len(values) == 6 {
		for i := range m {
			m[i] = d.number(values[i])
		}
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, corner := range [4][2]float64{{bounds.x, bounds.y}, {bounds.x + bounds.w, bounds.y}, {bounds.x, bounds.y + bounds.h}, {bounds.x + bounds.w, bounds.y + bounds.h}} {
		x := m[0]*corner[0] + m[2]*corner[1] + m[4]
		y := m[1]*corner[0] + m[3]*corner[1] + m[5]
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	if maxX-minX <= 0 || maxY-minY <= 0 {
		return ref, [6]float64{}, false
	}
	stream.Dict["Type"], stream.Dict["Subtype"] = types.Name("XObject"), types.Name("Form")
	d.set(ref, stream)
	sx, sy := box.w/(maxX-minX), box.h/(maxY-minY)
	return ref, [6]float64{sx, 0, 0, sy, box.x - minX*sx, box.y - minY*sy}, true
}
