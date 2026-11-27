package signpdf

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

type certificateData struct {
	document      Document
	parties       []Party
	events        []Event
	originalHash  string
	originalPages int
	files         []attachment
	methods       map[string][]string
}

func parseTime(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func formatUTC(value string) string {
	t, ok := parseTime(value)
	if !ok {
		return ""
	}
	return t.Format("2006-01-02 15:04:05") + " UTC"
}

func formatSize(size int) string {
	switch {
	case size < 1024:
		return strconv.Itoa(size) + " B"
	case size < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(size)/1024/1024)
	}
}

func timeZone(party Party) string {
	offset := ""
	if party.TZOffsetMinutes != nil {
		minutes, sign := *party.TZOffsetMinutes, "+"
		if minutes < 0 {
			minutes, sign = -minutes, "-"
		}
		offset = fmt.Sprintf("UTC%s%02d:%02d", sign, minutes/60, minutes%60)
	}
	if party.Timezone != "" && offset != "" {
		return party.Timezone + " (" + offset + ")"
	}
	return party.Timezone + offset
}

func (s certificateStrings) party(f *flow, index int, party Party, methods []string, consentVersion int) {
	f.ensure(120)
	f.y -= 4
	x := certMargin
	if dot, ok := parseColour(party.Color); ok {
		f.current().circle(52, f.y-6.5, 3.2, 0, dot, "f")
		x = 62
	}
	f.paragraph(fmt.Sprintf("%d. %s", index+1, party.Name), paragraphStyle{size: 10, bold: true, x: x})
	f.keyValue(s.email, party.Email)
	f.keyValue(s.signerName, party.SignerName)
	status := s.statusOther
	if party.CompletedAt != "" {
		status = s.statusCompleted
	} else if party.DeclinedAt != "" {
		status = s.statusDeclined
	}
	f.keyValue(s.status, status)
	f.keyValue(s.ip, party.IP)
	f.keyValue(s.userAgent, party.UserAgent)
	f.keyValue(s.timezone, timeZone(party))
	f.keyValue(s.opened, formatUTC(party.OpenedAt))
	consent := formatUTC(party.ConsentedAt)
	if consent != "" && consentVersion > 0 {
		consent += " (" + strings.Replace(s.consentVersion, "{version}", strconv.Itoa(consentVersion), 1) + ")"
	}
	f.keyValue(s.consent, consent)
	f.keyValue(s.signedAt, formatUTC(party.CompletedAt))
	if party.CompletedAt != "" {
		labels := make([]string, len(methods))
		for i, method := range methods {
			label, ok := map[string]string{"draw": s.methodDraw, "type": s.methodType, "upload": s.methodUpload}[method]
			if !ok {
				label = s.methodOther
			}
			labels[i] = label
		}
		f.keyValue(s.method, strings.Join(labels, ", "))
	}
	f.keyValue(s.declinedAt, formatUTC(party.DeclinedAt))
	f.keyValue(s.declineReason, party.DeclineReason)
	f.y -= 4
}

// appendCertificate adds the audit pages to the document.
func appendCertificate(doc *document, faces fonts, lang string, data certificateData) {
	s := certificateText[lang]
	f := newFlow(doc, faces)
	meta := data.document
	f.paragraph(s.title, paragraphStyle{size: 20, bold: true})
	f.paragraph(s.subtitle, paragraphStyle{size: 9.5, color: &certMuted})
	f.y -= 4

	f.heading(s.sectionDocument)
	f.keyValue(s.docTitle, meta.Title)
	f.keyValue(s.docID, meta.ID)
	f.keyValue(s.docPages, strconv.Itoa(data.originalPages))
	switch meta.SigningOrder {
	case "":
	case "random":
		f.keyValue(s.docOrder, s.orderRandom)
	default:
		f.keyValue(s.docOrder, s.orderPreserved)
	}
	f.keyValue(s.docCreated, formatUTC(meta.CreatedAt))
	f.keyValue(s.docSent, formatUTC(meta.SentAt))
	f.keyValue(s.docCompleted, formatUTC(meta.FinalizedAt.UTC().Format(time.RFC3339Nano)))
	f.keyValue(s.originalHash, data.originalHash)

	f.heading(s.sectionParties)
	for i, party := range data.parties {
		s.party(f, i, party, data.methods[party.ID], meta.ConsentVersion)
	}

	if len(data.files) > 0 {
		f.heading(s.sectionAttachments)
		for _, file := range data.files {
			name := file.fieldName
			if name == "" {
				name = "-"
			}
			f.keyValue(name, fmt.Sprintf("%s (%s)\nSHA-256 %s", file.filename, formatSize(len(file.data)), file.sha256))
		}
	}

	f.heading(s.sectionEvents)
	events := slices.Clone(data.events)
	slices.SortStableFunc(events, func(a, b Event) int {
		at, _ := parseTime(a.At)
		bt, _ := parseTime(b.At)
		return at.Compare(bt)
	})
	if len(events) == 0 {
		f.paragraph(s.noEvents, paragraphStyle{color: &certMuted})
	} else {
		names := map[string]string{}
		for _, party := range data.parties {
			names[party.ID] = party.Name
		}
		dash := func(value string) string {
			if value == "" {
				return "-"
			}
			return value
		}
		rows := make([][]string, len(events))
		for i, event := range events {
			label := s.events[event.Type]
			if label == "" {
				label = event.Type
			}
			who := s.sender
			if event.PartyID != "" {
				who = dash(names[event.PartyID])
			}
			rows[i] = []string{strconv.Itoa(i + 1), formatUTC(event.At), label, who, dash(event.IP), dash(event.Hash[:min(8, len(event.Hash))])}
		}
		f.table([]string{s.colNo, s.colTime, s.colEvent, s.colParty, s.colIP, s.colHash},
			[]float64{24, 112, 128, 100, 85, certContent - 449}, rows)
		if head := events[len(events)-1].Hash; head != "" {
			f.y -= 8
			f.ensure(40)
			f.paragraph(s.chainHead+" - "+strings.Replace(s.chainCount, "{count}", strconv.Itoa(len(events)), 1), paragraphStyle{size: 8.5, color: &certMuted})
			f.paragraph(head, paragraphStyle{size: 8.5})
		}
	}

	f.heading(s.sectionVerify)
	for _, note := range s.verify {
		f.paragraph(note, paragraphStyle{size: 8.5, color: &certMuted})
		f.y -= 3
	}
	f.finish(func(n, total int) (string, string) {
		return strings.Replace(s.footer, "{id}", meta.ID, 1),
			strings.NewReplacer("{n}", strconv.Itoa(n), "{total}", strconv.Itoa(total)).Replace(s.page)
	})
}
