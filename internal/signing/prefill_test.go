package signing

import "testing"

func TestPrefillNameMatching(t *testing.T) {
	d := details{DisplayName: "Anna Maria Berger", Email: "a@example.com", Phone: "123456",
		Presets: []preset{{Label: "Address", Value: "Main St 1"}, {Label: "Mobile number", Value: "999"}}}
	for name, want := range map[string]string{
		"E-Mail-Adresse": "a@example.com", "Vollständiger Name": "Anna Maria Berger", "Vorname": "Anna Maria", "Last name": "Berger",
		"Anschrift": "Main St 1", "Straße": "Main St 1", "Mobile number": "999", "Telefon": "123456", "Name": "Anna Maria Berger",
		"Name of the bank": "", "Mailbox": "", "": "", "Hotel": "",
	} {
		if got := matchName(name, d); got != want {
			t.Errorf("%q = %q, want %q", name, got, want)
		}
	}
	if normalizeName("  Straße / Nr.  ") != "strasse nr" {
		t.Error(normalizeName("  Straße / Nr.  "))
	}
}
