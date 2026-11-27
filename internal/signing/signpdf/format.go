package signpdf

import (
	"regexp"
	"strconv"
	"strings"
)

var monthNames = map[string][12]string{
	"en": {"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
	"de": {"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
}

var (
	isoDate     = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})`)
	dateTokens  = regexp.MustCompile(`YYYY|YY|MMMM|MMM|MM|M|DD|D`)
	plainNumber = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

// formatDate formats a calendar date with the tokens YYYY YY MMMM MMM MM M DD
// D. Unparseable input is returned unchanged.
func formatDate(value, format, locale string) string {
	m := isoDate.FindStringSubmatch(value)
	if m == nil {
		return value
	}
	month, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	name := ""
	if month >= 1 && month <= 12 {
		name = monthNames[locale][month-1]
	}
	if format == "" {
		format = "YYYY-MM-DD"
		if locale == "de" {
			format = "DD.MM.YYYY"
		}
	}
	return dateTokens.ReplaceAllStringFunc(format, func(token string) string {
		switch token {
		case "YYYY":
			return m[1]
		case "YY":
			return m[1][2:]
		case "MMMM":
			return name
		case "MMM":
			if r := []rune(name); len(r) > 3 {
				return string(r[:3])
			}
			return name
		case "MM":
			return m[2]
		case "M":
			return strconv.Itoa(month)
		case "DD":
			return m[3]
		default:
			return strconv.Itoa(day)
		}
	})
}

var numberStyles = map[string]struct{ group, decimal, prefix, suffix string }{
	"comma": {",", ".", "", ""},
	"dot":   {".", ",", "", ""},
	"space": {" ", ",", "", ""},
	"usd":   {",", ".", "$", ""},
	"eur":   {".", ",", "", " €"},
	"gbp":   {",", ".", "£", ""},
}

func formatNumber(value, format string) string {
	text := strings.TrimSpace(value)
	style, ok := numberStyles[format]
	if !ok || !plainNumber.MatchString(text) {
		return text
	}
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign, text = "-", text[1:]
	}
	integer, fraction, hasFraction := strings.Cut(text, ".")
	var out strings.Builder
	for i, r := range integer {
		if i > 0 && (len(integer)-i)%3 == 0 {
			out.WriteString(style.group)
		}
		out.WriteRune(r)
	}
	if hasFraction {
		out.WriteString(style.decimal + fraction)
	}
	return sign + style.prefix + out.String() + style.suffix
}
