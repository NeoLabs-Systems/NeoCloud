package signing

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The field types a document can hold, as data: how each is stored, which
// preferences it takes and how a value is checked and normalized.

const (
	storeInline = iota // the value is JSON in sign_values.value_json
	storeBlob          // an uploaded image or file
	storeStatic        // the field only shows text

	maxText    = 5000
	maxLine    = 500
	maxOptions = 50
)

type prefRule struct {
	kind     string
	min, max float64
	items    int
	values   []string
	pattern  *regexp.Regexp
}

type fieldType struct {
	storage       int
	upload        string // "image" or "file" for blob types
	maxW, maxH    int
	prefs         map[string]prefRule
	requiredPrefs []string
}

func rules(groups ...map[string]prefRule) map[string]prefRule {
	out := map[string]prefRule{}
	for _, group := range groups {
		for name, rule := range group {
			out[name] = rule
		}
	}
	return out
}

func intRule(min, max float64) prefRule  { return prefRule{kind: "int", min: min, max: max} }
func enumRule(values ...string) prefRule { return prefRule{kind: "enum", values: values} }

var fieldTypes = func() map[string]fieldType {
	style := map[string]prefRule{
		"fontSize": intRule(6, 72),
		// The bundled fonts have no italic; offering it would print something else.
		"fontType": enumRule("normal", "bold"),
		"align":    enumRule("left", "center", "right"),
		"valign":   enumRule("top", "center", "bottom"),
		"color":    {kind: "color"},
	}
	placeholder := map[string]prefRule{"placeholder": {kind: "string", max: 100}}
	options := map[string]prefRule{"options": {kind: "strings", min: 1, items: maxOptions, max: 100}}
	methods := map[string]prefRule{"methods": {kind: "strings", min: 1, items: 3, max: 10}}
	number := prefRule{kind: "number", min: -1e12, max: 1e12}
	return map[string]fieldType{
		"text": {prefs: rules(style, placeholder, map[string]prefRule{"multiline": {kind: "bool"}, "minLength": intRule(1, maxText),
			"maxLength": intRule(1, maxText), "format": enumRule("none", "email")})},
		"number": {prefs: rules(style, placeholder, map[string]prefRule{"min": number, "max": number, "decimals": intRule(0, 10),
			"format": enumRule("none", "comma", "dot", "space", "usd", "eur", "gbp")})},
		// Tokens YYYY YY MMMM MMM MM M DD D, e.g. DD.MM.YYYY.
		"date":     {prefs: rules(style, map[string]prefRule{"format": {kind: "pattern", pattern: regexp.MustCompile(`^[YMD .,/-]{1,20}$`)}, "autoToday": {kind: "bool"}})},
		"phone":    {prefs: rules(style, placeholder)},
		"checkbox": {prefs: map[string]prefRule{"color": {kind: "color"}}},
		"radio":    {prefs: rules(style, options), requiredPrefs: []string{"options"}},
		"select":   {prefs: rules(style, options), requiredPrefs: []string{"options"}},
		"multiple": {prefs: rules(style, options, map[string]prefRule{"minSelect": intRule(1, maxOptions), "maxSelect": intRule(1, maxOptions)}), requiredPrefs: []string{"options"}},
		"cells":    {prefs: rules(style, map[string]prefRule{"cells": intRule(1, 50), "charset": enumRule("any", "digits", "letters")}), requiredPrefs: []string{"cells"}},

		"signature": {storage: storeBlob, upload: "image", maxW: 1200, maxH: 600, prefs: methods},
		"initials":  {storage: storeBlob, upload: "image", maxW: 600, maxH: 300, prefs: methods},
		"stamp":     {storage: storeBlob, upload: "image", maxW: 1200, maxH: 1200},
		"image":     {storage: storeBlob, upload: "image", maxW: 1600, maxH: 1600},
		"file":      {storage: storeBlob, upload: "file"},
		"heading":   {storage: storeStatic, prefs: style},
	}
}()

var signingMethods = []string{"draw", "type", "upload"}

func prefInt(prefs map[string]any, name string) (int, bool) {
	n, ok := prefs[name].(float64)
	return int(n), ok
}

func prefStrings(prefs map[string]any, name string) []string {
	list, _ := prefs[name].([]any)
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func number(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func invalidField(message string) *apiError { return httpError(400, message, "INVALID_FIELD") }
func invalidValue(message string) *apiError { return httpError(400, message, "INVALID_VALUE") }

// normalizePrefs keeps only schema names, each checked.
func normalizePrefs(kind string, input any) (map[string]any, error) {
	t := fieldTypes[kind]
	out := map[string]any{}
	if input != nil {
		prefs, ok := input.(map[string]any)
		if !ok {
			return nil, invalidField("Preferences must be an object.")
		}
		names := make([]string, 0, len(prefs))
		for name := range prefs {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			rule, known := t.prefs[name]
			if !known {
				return nil, invalidField(`Unknown preference "` + name + `".`)
			}
			if prefs[name] == nil {
				continue
			}
			checked, err := checkPref(name, rule, prefs[name])
			if err != nil {
				return nil, err
			}
			out[name] = checked
		}
	}
	for _, name := range t.requiredPrefs {
		if out[name] == nil {
			return nil, invalidField(`A ` + kind + ` field needs "` + name + `".`)
		}
	}
	above := func(low, high string) bool {
		a, okA := out[low].(float64)
		b, okB := out[high].(float64)
		return okA && okB && a > b
	}
	if above("minLength", "maxLength") {
		return nil, invalidField("minLength is above maxLength.")
	}
	if above("min", "max") {
		return nil, invalidField("min is above max.")
	}
	if above("minSelect", "maxSelect") {
		return nil, invalidField("minSelect is above maxSelect.")
	}
	for _, method := range prefStrings(out, "methods") {
		if !slices.Contains(signingMethods, method) {
			return nil, invalidField("methods must be from: " + strings.Join(signingMethods, ", ") + ".")
		}
	}
	return out, nil
}

func checkPref(name string, rule prefRule, value any) (any, error) {
	switch rule.kind {
	case "bool":
		if b, ok := value.(bool); ok {
			return b, nil
		}
		return nil, invalidField(name + " must be true or false.")
	case "int":
		n, ok := value.(float64)
		if !ok || n != math.Trunc(n) || n < rule.min || n > rule.max {
			return nil, invalidField(name + " must be a whole number from " + number(rule.min) + " to " + number(rule.max) + ".")
		}
		return n, nil
	case "number":
		n, ok := value.(float64)
		if !ok || math.IsInf(n, 0) || math.IsNaN(n) || n < rule.min || n > rule.max {
			return nil, invalidField(name + " must be a number.")
		}
		return n, nil
	case "enum":
		s, ok := value.(string)
		if !ok || !slices.Contains(rule.values, s) {
			return nil, invalidField(name + " must be one of: " + strings.Join(rule.values, ", ") + ".")
		}
		return s, nil
	case "string":
		s, ok := value.(string)
		if !ok || length(s) > int(rule.max) {
			return nil, invalidField(name + " must be text of at most " + number(rule.max) + " characters.")
		}
		return s, nil
	case "pattern":
		s, ok := value.(string)
		if !ok || !rule.pattern.MatchString(s) {
			return nil, invalidField(name + " is not in a valid format.")
		}
		return s, nil
	case "color":
		s, ok := value.(string)
		if !ok || !colorPattern.MatchString(s) {
			return nil, invalidField(name + " must be a color like #1a2b3c.")
		}
		return strings.ToLower(s), nil
	default: // strings
		list, ok := value.([]any)
		if !ok || len(list) < int(rule.min) || len(list) > rule.items {
			return nil, invalidField(name + " must be a list of " + number(rule.min) + " to " + strconv.Itoa(rule.items) + " entries.")
		}
		seen := map[string]bool{}
		out := make([]any, 0, len(list))
		for _, entry := range list {
			s, _ := entry.(string)
			s = strings.TrimSpace(s)
			if s == "" || length(s) > int(rule.max) {
				return nil, invalidField(name + " entries must be 1 to " + number(rule.max) + " characters.")
			}
			if seen[s] {
				return nil, invalidField(name + " entries must be different.")
			}
			seen[s] = true
			out = append(out, s)
		}
		return out, nil
	}
}

func typedText(value any) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", invalidValue("Value must be text.")
	}
	return plainText(s), nil
}

var (
	numberPattern = regexp.MustCompile(`^-?\d{1,15}(\.\d{1,10})?$`)
	phonePattern  = regexp.MustCompile(`^\+?[0-9][0-9 ()./-]{3,24}$`)
	nonDigits     = regexp.MustCompile(`\D`)
)

// normalizeValue checks a typed or picked value against the field's type; nil
// clears the field.
func normalizeValue(kind string, prefs map[string]any, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	t := fieldTypes[kind]
	if t.storage != storeInline {
		return nil, invalidValue("This field takes an upload, not a typed value.")
	}
	switch kind {
	case "text":
		s, err := typedText(value)
		if err != nil {
			return nil, err
		}
		multiline := prefs["multiline"] == true
		if !multiline {
			s = strings.ReplaceAll(s, "\n", " ")
		}
		s = strings.TrimSpace(s)
		limit := maxLine
		if multiline {
			limit = maxText
		}
		if n, ok := prefInt(prefs, "maxLength"); ok {
			limit = n
		}
		if length(s) > limit {
			return nil, invalidValue("The text is too long.")
		}
		if n, ok := prefInt(prefs, "minLength"); ok && s != "" && length(s) < n {
			return nil, invalidValue("Enter at least " + strconv.Itoa(n) + " characters.")
		}
		if s != "" && prefs["format"] == "email" && !emailPattern.MatchString(s) {
			return nil, invalidValue("Enter a valid email address.")
		}
		return s, nil
	case "number":
		var raw string
		if n, ok := value.(float64); ok {
			raw = number(n)
		} else {
			s, err := typedText(value)
			if err != nil {
				return nil, err
			}
			raw = strings.TrimSpace(s)
		}
		if raw == "" {
			return "", nil
		}
		if !numberPattern.MatchString(raw) {
			return nil, invalidValue("Enter a number.")
		}
		_, decimals, _ := strings.Cut(raw, ".")
		if n, ok := prefInt(prefs, "decimals"); ok && len(decimals) > n {
			return nil, invalidValue("At most " + strconv.Itoa(n) + " decimal places.")
		}
		n, _ := strconv.ParseFloat(raw, 64)
		if min, ok := prefs["min"].(float64); ok && n < min {
			return nil, invalidValue("The number must be at least " + number(min) + ".")
		}
		if max, ok := prefs["max"].(float64); ok && n > max {
			return nil, invalidValue("The number must be at most " + number(max) + ".")
		}
		return raw, nil
	case "date":
		s, err := typedText(value)
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		if s != "" {
			if _, err := time.Parse("2006-01-02", s); err != nil {
				return nil, invalidValue("Enter a valid date (YYYY-MM-DD).")
			}
		}
		return s, nil
	case "phone":
		s, err := typedText(value)
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return "", nil
		}
		digits := len(nonDigits.ReplaceAllString(s, ""))
		if !phonePattern.MatchString(s) || digits < 5 || digits > 15 {
			return nil, invalidValue("Enter a valid phone number.")
		}
		return s, nil
	case "checkbox":
		if b, ok := value.(bool); ok {
			return b, nil
		}
		return nil, invalidValue("A checkbox is either checked or not.")
	case "radio", "select":
		s, err := typedText(value)
		if err != nil {
			return nil, err
		}
		if s != "" && !slices.Contains(prefStrings(prefs, "options"), s) {
			return nil, invalidValue("That is not one of the options.")
		}
		return s, nil
	case "multiple":
		list, ok := value.([]any)
		if !ok {
			return nil, invalidValue("Pick from the options.")
		}
		options := prefStrings(prefs, "options")
		picked := map[string]bool{}
		for _, entry := range list {
			s, ok := entry.(string)
			if !ok || !slices.Contains(options, s) {
				return nil, invalidValue("That is not one of the options.")
			}
			picked[s] = true
		}
		// Stored in the order of the options.
		out := []any{}
		for _, option := range options {
			if picked[option] {
				out = append(out, option)
			}
		}
		max := maxOptions
		if n, ok := prefInt(prefs, "maxSelect"); ok {
			max = n
		}
		if len(out) > max {
			return nil, invalidValue("Pick at most " + strconv.Itoa(max) + ".")
		}
		if n, ok := prefInt(prefs, "minSelect"); ok && len(out) > 0 && len(out) < n {
			return nil, invalidValue("Pick at least " + strconv.Itoa(n) + ".")
		}
		return out, nil
	default: // cells
		s, err := typedText(value)
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		cells, _ := prefInt(prefs, "cells")
		if len([]rune(s)) > cells {
			return nil, invalidValue("At most " + strconv.Itoa(cells) + " characters.")
		}
		for _, r := range s {
			allowed := !unicode.IsSpace(r)
			switch prefs["charset"] {
			case "digits":
				allowed = r >= '0' && r <= '9'
			case "letters":
				allowed = unicode.IsLetter(r)
			}
			if !allowed {
				return nil, invalidValue("Those characters are not allowed here.")
			}
		}
		return s, nil
	}
}

// emptyValue reports a value that clears the field.
func emptyValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case bool:
		return !v
	case []any:
		return len(v) == 0
	default:
		return false
	}
}

// isFilled reports whether a stored value counts for a required field.
func isFilled(kind string, prefs map[string]any, value any) bool {
	switch {
	case fieldTypes[kind].storage == storeStatic:
		return true
	case fieldTypes[kind].storage == storeBlob:
		return value != nil
	case kind == "checkbox":
		// A required checkbox is an agreement: false is empty.
		return value == true
	case kind == "multiple":
		list, ok := value.([]any)
		return ok && len(list) > 0
	case kind == "cells":
		// Required means every cell is filled.
		s, ok := value.(string)
		cells, _ := prefInt(prefs, "cells")
		return ok && len([]rune(s)) == cells
	default:
		return value != nil && value != ""
	}
}

// normalizeDefault checks the sender's default like a signer's value.
func normalizeDefault(kind string, prefs map[string]any, value any) (any, error) {
	if value == nil || value == "" {
		return nil, nil
	}
	switch fieldTypes[kind].storage {
	case storeStatic:
		s, ok := value.(string)
		if !ok || length(s) > 2000 {
			return nil, invalidField("The heading text is too long.")
		}
		return s, nil
	case storeBlob:
		return nil, invalidField("This type has no default value.")
	}
	normalized, err := normalizeValue(kind, prefs, value)
	if err != nil {
		return nil, invalidField("Default value: " + err.Error())
	}
	return normalized, nil
}
