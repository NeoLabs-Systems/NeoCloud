package ai

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/NeoLabs-Systems/neocloud/internal/imaging"
)

// The extraction call's answer made safe and checked: known types only, short
// clean texts, tags normalized, amounts parsed. The model's confidence is
// lowered where a value can't be confirmed, and the date is picked by
// precedence.

var docTypes = []string{
	"invoice", "receipt", "contract", "payslip", "letter", "statement", "certificate", "manual",
	"report", "notes", "presentation", "spreadsheet", "screenshot", "photo", "scan", "other",
}

var confidenceFields = []string{"date", "issuer", "subject", "type", "amount", "tags"}

const (
	maxExtractedTags  = 8
	unconfirmedFactor = 0.7
)

var dateSourceConfidence = map[string]float64{"exif": 0.9, "filename": 0.6, "modified": 0.4}

// fields is what ai_node_meta keeps of one file. Empty strings are stored and
// served as null.
type fields struct {
	Issuer, Subject, Type, Currency, Language, Summary, Caption, Date, DateSource string
	Amount                                                                        *float64
	Tags                                                                          []string
	Confidence                                                                    map[string]float64
	Overall                                                                       float64
	Camera                                                                        bool
}

func (f fields) amount() any {
	if f.Amount == nil {
		return nil
	}
	return *f.Amount
}

func (f fields) encode() map[string]any {
	return map[string]any{
		"issuer": nullableString(f.Issuer), "subject": nullableString(f.Subject), "type": nullableString(f.Type),
		"amount": f.amount(), "currency": nullableString(f.Currency), "tags": append([]string{}, f.Tags...),
		"language": nullableString(f.Language), "summary": nullableString(f.Summary), "caption": nullableString(f.Caption),
		"date": nullableString(f.Date), "dateSource": nullableString(f.DateSource),
		"confidence": f.Confidence, "overall": f.Overall, "camera": f.Camera,
	}
}

func decodeFields(raw map[string]any) fields {
	text := func(key string) string { value, _ := raw[key].(string); return value }
	f := fields{
		Issuer: text("issuer"), Subject: text("subject"), Type: text("type"), Currency: text("currency"),
		Language: text("language"), Summary: text("summary"), Caption: text("caption"), Date: text("date"),
		DateSource: text("dateSource"), Camera: raw["camera"] == true, Confidence: map[string]float64{},
	}
	if amount, ok := raw["amount"].(float64); ok {
		f.Amount = &amount
	}
	tags, _ := raw["tags"].([]any)
	for _, tag := range tags {
		if value, ok := tag.(string); ok {
			f.Tags = append(f.Tags, value)
		}
	}
	confidence, _ := raw["confidence"].(map[string]any)
	for _, key := range confidenceFields {
		f.Confidence[key] = clamp(confidence[key], 0.5)
	}
	f.Overall = clamp(raw["overall"], 0.5)
	return f
}

// number reads a JSON number, also when a model sends it as a string.
func number(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	}
	return 0, false
}

func clamp(value any, fallback float64) float64 {
	parsed, ok := number(value)
	if !ok {
		return fallback
	}
	return min(max(parsed, 0), 1)
}

func shortText(value any, limit int) string {
	text, _ := value.(string)
	return truncate(strings.Join(strings.Fields(text), " "), limit)
}

var (
	amountNoise  = regexp.MustCompile(`[^\d.,-]`)
	commaDecimal = regexp.MustCompile(`,\d{1,2}$`)
	currencyCode = regexp.MustCompile(`^[A-Za-z]{3}$`)
	languageCode = regexp.MustCompile(`^[A-Za-z]{2}$`)
)

func parseAmount(value any) *float64 {
	switch v := value.(type) {
	case float64:
		return &v
	case string:
		text := amountNoise.ReplaceAllString(v, "")
		// 1.234,56 and 1,234.56 are both 1234.56.
		if commaDecimal.MatchString(text) {
			text = strings.Replace(strings.ReplaceAll(text, ".", ""), ",", ".", 1)
		} else {
			text = strings.ReplaceAll(text, ",", "")
		}
		if parsed, err := strconv.ParseFloat(text, 64); err == nil && !math.IsInf(parsed, 0) && !math.IsNaN(parsed) {
			return &parsed
		}
	}
	return nil
}

func normalizeTag(value any) string {
	text, _ := value.(string)
	kept := strings.Map(func(char rune) rune {
		if unicode.IsLetter(char) || unicode.IsNumber(char) || strings.ContainsRune(" &+-", char) {
			return char
		}
		return -1
	}, strings.ToLower(text))
	return truncate(strings.Join(strings.Fields(kept), " "), 30)
}

func normalizeTags(value any, limit int) []string {
	list, _ := value.([]any)
	tags := []string{}
	for _, item := range list {
		if tag := normalizeTag(item); tag != "" && !slices.Contains(tags, tag) && len(tags) < limit {
			tags = append(tags, tag)
		}
	}
	return tags
}

func amountInText(amount float64, text string) bool {
	fixed := strconv.FormatFloat(amount, 'f', 2, 64)
	plain := strconv.FormatFloat(amount, 'f', -1, 64)
	for _, spelling := range []string{fixed, strings.Replace(fixed, ".", ",", 1), plain, strings.Replace(plain, ".", ",", 1)} {
		if strings.Contains(text, spelling) {
			return true
		}
	}
	return false
}

// normalizeExtraction checks the model's answer. text is what it read, nil for
// an image.
func normalizeExtraction(raw map[string]any, text *string, n node, exif imaging.Exif) fields {
	confidences, _ := raw["confidence"].(map[string]any)
	f := fields{
		Issuer:     shortText(raw["issuer"], 80),
		Subject:    shortText(raw["subject"], 100),
		Amount:     parseAmount(raw["amount"]),
		Tags:       normalizeTags(raw["tags"], maxExtractedTags),
		Summary:    shortText(raw["summary"], 400),
		Caption:    shortText(raw["caption"], 200),
		Camera:     exif.Make != "" || exif.Model != "",
		Confidence: map[string]float64{},
	}
	for _, key := range confidenceFields {
		f.Confidence[key] = clamp(confidences[key], 0.5)
	}
	if kind := strings.ToLower(shortText(raw["type"], 40)); kind != "" {
		f.Type = "other"
		if slices.Contains(docTypes, kind) {
			f.Type = kind
		}
	}
	if currency := shortText(raw["currency"], 8); currencyCode.MatchString(currency) {
		f.Currency = strings.ToUpper(currency)
	}
	if language := shortText(raw["language"], 8); languageCode.MatchString(language) {
		f.Language = strings.ToLower(language)
	}
	if text != nil {
		if f.Issuer != "" && !strings.Contains(strings.ToLower(*text), strings.ToLower(f.Issuer)) {
			f.Confidence["issuer"] *= unconfirmedFactor
		}
		if f.Amount != nil && !amountInText(*f.Amount, *text) {
			f.Confidence["amount"] *= unconfirmedFactor
		}
	}
	content, _ := raw["date"].(string)
	f.Date, f.DateSource = chooseDate(content, exif.TakenAt, n.Name, n.ModifiedAt, n.CreatedAt)
	if confidence, ok := dateSourceConfidence[f.DateSource]; ok {
		f.Confidence["date"] = confidence
	}
	mean := 0.0
	for _, key := range confidenceFields {
		mean += f.Confidence[key] / float64(len(confidenceFields))
	}
	f.Overall = clamp(raw["overall"], mean)
	return f
}
