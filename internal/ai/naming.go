package ai

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// File names for auto-title, all local and deterministic: which names are
// meaningless and so worth a suggestion, which date a name gets, and the name
// itself, rendered from the person's template out of the extracted fields. The
// model never writes a file name.

const (
	defaultTemplate = "{date} {issuer} – {subject}"
	maxNameRunes    = 120
	maxNameBytes    = 255
)

var placeholders = []string{"date", "year", "month", "issuer", "subject", "type", "amount", "currency", "original"}

func patterns(list ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(list))
	for index, pattern := range list {
		out[index] = regexp.MustCompile(`(?i)` + pattern)
	}
	return out
}

var cameraPatterns = patterns(
	`^(img|pxl|dsc|dscn|dscf|dji|gopr|gp|mvimg|vid|mov|sam|p)[_-]?\d{3,}`,
	`^\d{8}[_-]\d{6}`,
	`^whatsapp (image|video) \d{4}-\d{2}-\d{2}`,
	`^signal-\d{4}-\d{2}-\d{2}`,
	`^photo[_ -]?\d+`,
)

var screenshotPatterns = patterns(`^screenshot`, `^screen ?shot`, `^bildschirmfoto`, `^bildschirmaufnahme`, `^capture d.[ée]cran`, `^screen ?recording`)

var scanPatterns = patterns(`^scan(ned)?([ _-]?\d.*)?$`, `^scn[_-]?\d+`, `^doc[_-]?\d+`, `^image[_-]?\d+$`, `^\d{4}[_-]?\d{2}[_-]?\d{2}[_-]?scan`)

// Names that say nothing, alone or numbered: "download (3)", "Unbenannt-1".
var genericPattern = regexp.MustCompile(`(?i)^(download|downloads|document|dokument|doc|file|datei|untitled|untitled document|unbenannt|unnamed|new document|neues dokument|image|bild|foto|photo|attachment|anhang|invoice|rechnung|receipt|beleg|scan|page|seite|export|print|output|temp|tmp|test|neu|new|copy|kopie)( ?\(\d+\)|[ _-]?\d+)?( ?\(\d+\))?$`)

var randomPatterns = patterns(
	`^[0-9a-f]{8}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{12}$`,
	`^[0-9a-f]{12,}$`,
	// Timestamps: 20260312, 20260312_142233, 2026-03-12 14.22.33, 1710252153123.
	`^\d{8}([ _T-]?\d{4,6})?$`,
	`^\d{4}-\d{2}-\d{2}([ _T]\d{2}[.:-]?\d{2}([.:-]?\d{2})?)?$`,
	`^\d{9,13}$`,
	// Only digits and separators.
	`^[^a-zA-Z]+$`,
)

var randomAlphabet = regexp.MustCompile(`^[A-Za-z0-9]{16,}$`)

type nameClass struct{ meaningless, camera, screenshot, scan bool }

func matchesAny(list []*regexp.Regexp, value string) bool {
	return slices.ContainsFunc(list, func(pattern *regexp.Regexp) bool { return pattern.MatchString(value) })
}

// splitExtension splits "report.final.pdf" into "report.final" and ".pdf";
// dotfiles keep their name.
func splitExtension(name string) (string, string) {
	index := strings.LastIndexByte(name, '.')
	if index <= 0 {
		return name, ""
	}
	return name[:index], name[index:]
}

// A long run of letters and digits mixed, with no separator (CDN and app ids).
func looksRandom(stem string) bool {
	if !randomAlphabet.MatchString(stem) {
		return false
	}
	digits := 0
	for _, char := range stem {
		if unicode.IsDigit(char) {
			digits++
		}
	}
	return digits >= 3 && len(stem)-digits >= 3
}

func classifyName(name string) nameClass {
	stem, _ := splitExtension(strings.TrimSpace(name))
	stem = strings.TrimSpace(stem)
	class := nameClass{
		camera:     matchesAny(cameraPatterns, stem),
		screenshot: matchesAny(screenshotPatterns, stem),
		scan:       matchesAny(scanPatterns, stem),
	}
	class.meaningless = class.camera || class.screenshot || class.scan || genericPattern.MatchString(stem) ||
		stem == "" || matchesAny(randomPatterns, stem) || looksRandom(stem)
	return class
}

var (
	isoSpelling    = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})`)
	dottedSpelling = regexp.MustCompile(`^(\d{1,2})\.(\d{1,2})\.(\d{4})`)
	separatedDate  = regexp.MustCompile(`(?:^|\D)((?:19|20)\d{2})[-_.](\d{2})[-_.](\d{2})(?:\D|$)`)
	compactDate    = regexp.MustCompile(`(?:^|\D)((?:19|20)\d{2})(\d{2})(\d{2})(?:\D|$)`)
)

func isoDate(year, month, day string) string {
	value := fmt.Sprintf("%s-%02s-%02s", year, month, day)
	if _, err := time.Parse(time.DateOnly, value); err != nil {
		return ""
	}
	return value
}

// parseDate reads a YYYY-MM-DD date out of any of the usual spellings, else "".
func parseDate(value string) string {
	value = strings.TrimSpace(value)
	if match := isoSpelling.FindStringSubmatch(value); match != nil {
		return isoDate(match[1], match[2], match[3])
	}
	if match := dottedSpelling.FindStringSubmatch(value); match != nil {
		return isoDate(match[3], match[2], match[1])
	}
	return ""
}

func dateFromName(name string) string {
	for _, pattern := range []*regexp.Regexp{separatedDate, compactDate} {
		if match := pattern.FindStringSubmatch(name); match != nil {
			return isoDate(match[1], match[2], match[3])
		}
	}
	return ""
}

// chooseDate is the date a name uses and where it came from: content, EXIF, the
// old file name, then the modification time. It must be from 1900 on and not
// after the file arrived (a day's grace for time zones).
func chooseDate(content, exif, name, modifiedAt, uploadedAt string) (string, string) {
	if uploadedAt == "" {
		uploadedAt = modifiedAt
	}
	arrived, err := time.Parse(time.DateTime, uploadedAt)
	if err != nil {
		if arrived, err = time.Parse(time.RFC3339, uploadedAt); err != nil {
			arrived = time.Now().UTC()
		}
	}
	limit := arrived.Add(24 * time.Hour).Format(time.DateOnly)
	if len(modifiedAt) > 10 {
		modifiedAt = modifiedAt[:10]
	}
	for _, candidate := range [][2]string{{"content", parseDate(content)}, {"exif", parseDate(exif)}, {"filename", dateFromName(name)}, {"modified", parseDate(modifiedAt)}} {
		if date := candidate[1]; date != "" && date >= "1900-01-01" && date <= limit {
			return date, candidate[0]
		}
	}
	return "", ""
}

var windowsReserved = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com\d|lpt\d)$`)

// cleanText makes a value safe on every OS: path separators and colons become
// dashes, the other characters Windows refuses go, whitespace collapses.
func cleanText(value string) string {
	var out strings.Builder
	for _, char := range norm.NFC.String(value) {
		switch {
		case char < 0x20 || char == 0x7f:
			out.WriteRune(' ')
		case char == '/' || char == '\\' || char == ':':
			out.WriteRune('-')
		case strings.ContainsRune(`*?"<>|`, char):
		default:
			out.WriteRune(char)
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

func trimTrailing(value string) string {
	return strings.TrimRightFunc(value, func(char rune) bool { return char == '.' || unicode.IsSpace(char) })
}

// sanitizeFileName is a name valid on Windows, macOS and Linux, at most 120
// characters, extension kept; "" when nothing is left.
func sanitizeFileName(stem, extension string) string {
	clean := trimTrailing(strings.TrimLeftFunc(cleanText(stem), func(char rune) bool { return char == '.' || unicode.IsSpace(char) }))
	if windowsReserved.MatchString(clean) {
		clean += "_"
	}
	if room := maxNameRunes - utf8.RuneCountInString(extension); utf8.RuneCountInString(clean) > room {
		clean = strings.TrimSpace(truncate(clean, max(room, 1)))
	}
	for clean != "" && len(clean)+len(extension) > maxNameBytes {
		_, size := utf8.DecodeLastRuneInString(clean)
		clean = clean[:len(clean)-size]
	}
	if clean = trimTrailing(clean); clean == "" {
		return ""
	}
	return clean + extension
}

func formatAmount(amount any) string {
	value, ok := amount.(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return ""
	}
	if value == math.Trunc(value) {
		return strconv.FormatFloat(value, 'f', 0, 64)
	}
	return strconv.FormatFloat(value, 'f', 2, 64)
}

const separators = `–—\-_,;.·|`

var (
	spaces         = regexp.MustCompile(`\s+`)
	emptyBrackets  = regexp.MustCompile(`\(\s*\)|\[\s*\]|\{\s*\}`)
	separatorRuns  = regexp.MustCompile(`\s+([` + separators + `])(?:\s*[` + separators + `])+(\s)`)
	danglingEnds   = regexp.MustCompile(`^[\s` + separators + `]+|[\s` + separators + `]+$`)
	templateFields = regexp.MustCompile(`\{([a-z]+)\}`)
)

// tidy cleans what is left of a rendered template once missing values are gone.
func tidy(text string) string {
	result := spaces.ReplaceAllString(text, " ")
	for range 3 {
		result = emptyBrackets.ReplaceAllString(result, "")
		result = separatorRuns.ReplaceAllString(result, " $1$2")
		result = danglingEnds.ReplaceAllString(result, "")
		result = spaces.ReplaceAllString(result, " ")
	}
	return strings.TrimSpace(result)
}

func usableTemplate(template string) string {
	if slices.ContainsFunc(placeholders, func(name string) bool { return strings.Contains(template, "{"+name+"}") }) {
		return template
	}
	return defaultTemplate
}

// renderName is the file name a template gives for extracted fields, keeping
// the original's extension; "" when only separators would be left.
func renderName(template string, f fields, original string) string {
	stem, extension := splitExtension(original)
	date := parseDate(f.Date)
	values := map[string]string{
		"date": date, "year": "", "month": "",
		"issuer": cleanText(f.Issuer), "subject": cleanText(f.Subject), "type": cleanText(f.Type),
		"amount": formatAmount(f.amount()), "currency": strings.ToUpper(cleanText(f.Currency)),
		"original": cleanText(stem),
	}
	if date != "" {
		values["year"], values["month"] = date[:4], date[5:7]
	}
	rendered := tidy(templateFields.ReplaceAllStringFunc(usableTemplate(template), func(token string) string {
		if value, ok := values[token[1:len(token)-1]]; ok {
			return value
		}
		return token
	}))
	if !strings.ContainsFunc(rendered, func(char rune) bool { return unicode.IsLetter(char) || unicode.IsNumber(char) }) {
		return ""
	}
	return sanitizeFileName(rendered, extension)
}

// nameConfidence is bounded by the confidence of the fields the name uses.
func nameConfidence(template string, f fields) float64 {
	confidenceOf := map[string]string{"date": "date", "year": "date", "month": "date", "issuer": "issuer", "subject": "subject", "type": "type", "amount": "amount", "currency": "amount"}
	present := map[string]bool{
		"date": f.Date != "", "year": f.Date != "", "month": f.Date != "", "issuer": f.Issuer != "",
		"subject": f.Subject != "", "type": f.Type != "", "amount": f.Amount != nil, "currency": f.Currency != "",
	}
	template = usableTemplate(template)
	result, used := f.Overall, false
	for _, name := range placeholders {
		if strings.Contains(template, "{"+name+"}") && present[name] {
			used = true
			result = min(result, f.Confidence[confidenceOf[name]])
		}
	}
	if !used {
		return 0
	}
	return result
}
