package signing

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// The app's field registry is checked against this file
// (flutter_app/test/signing_server_schema_test.dart). Regenerate it after
// changing fieldTypes:
//
//	UPDATE_SIGNING_SCHEMA=1 go test -run TestFieldSchemaFixture ./internal/signing
const schemaFixture = "../../flutter_app/test/fixtures/signing_server_schema.json"

type schemaPref struct {
	Kind      string   `json:"kind"`
	Min       *float64 `json:"min,omitempty"`
	Max       *float64 `json:"max,omitempty"`
	MaxItems  *int     `json:"maxItems,omitempty"`
	MaxLength *float64 `json:"maxLength,omitempty"`
	Values    []string `json:"values,omitempty"`
	Regex     string   `json:"regex,omitempty"`
}

type schemaType struct {
	Storage       string                `json:"storage"`
	RequiredPrefs []string              `json:"requiredPrefs"`
	Prefs         map[string]schemaPref `json:"prefs"`
}

func describeRule(rule prefRule) schemaPref {
	out := schemaPref{Kind: rule.kind, Values: rule.values}
	switch rule.kind {
	case "int":
		out.Min, out.Max = &rule.min, &rule.max
	case "string":
		out.Max = &rule.max
	case "strings":
		out.Min, out.MaxItems, out.MaxLength = &rule.min, &rule.items, &rule.max
	case "pattern":
		out.Regex = rule.pattern.String()
	}
	return out
}

func fieldSchema(t *testing.T) []byte {
	t.Helper()
	types := map[string]schemaType{}
	for name, kind := range fieldTypes {
		entry := schemaType{Storage: []string{"inline", "blob", "static"}[kind.storage], RequiredPrefs: kind.requiredPrefs, Prefs: map[string]schemaPref{}}
		if entry.RequiredPrefs == nil {
			entry.RequiredPrefs = []string{}
		}
		for pref, rule := range kind.prefs {
			entry.Prefs[pref] = describeRule(rule)
		}
		types[name] = entry
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{"types": types}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestFieldSchemaFixture(t *testing.T) {
	want := fieldSchema(t)
	if os.Getenv("UPDATE_SIGNING_SCHEMA") != "" {
		if err := os.WriteFile(schemaFixture, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(schemaFixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
		t.Fatal("the app's signing schema differs from the server's; run UPDATE_SIGNING_SCHEMA=1 go test -run TestFieldSchemaFixture ./internal/signing")
	}
}
