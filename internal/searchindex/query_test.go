package searchindex

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestQueriesBecomeQuotedPrefixTerms(t *testing.T) {
	terms := ParseQuery(`Crème "Quick  Brown" e-mail`)
	want := []Term{{[]string{"creme"}, true}, {[]string{"quick", "brown"}, false}, {[]string{"e", "mail"}, true}}
	if !reflect.DeepEqual(terms, want) {
		t.Fatalf("terms = %#v", terms)
	}
	for query, expression := range map[string]string{
		`Crème "Quick  Brown" e-mail`: `"creme"* AND "quick" AND "brown" AND "e" AND "mail"*`,
		`a OR b NOT c`:                `"a"* AND "or"* AND "b"* AND "not"* AND "c"*`,
		`NEAR(x y) col:z ^w *`:        `"near" AND "x"* AND "y"* AND "col" AND "z"* AND "w"*`,
		`" * ( ) : ^ -`:               ``,
		`"unclosed phrase`:            `"unclosed" AND "phrase"`,
	} {
		if got := MatchExpression(ParseQuery(query)); got != expression {
			t.Errorf("%s = %s, want %s", query, got, expression)
		}
	}
	if got := len(ParseQuery("a b c d e f g h i j")); got != 8 {
		t.Errorf("terms = %d", got)
	}
	if got := len(ParseQuery(`"a b c d e f g h i j k l m n o p q r s"`)[0].Tokens); got != 16 {
		t.Errorf("tokens = %d", got)
	}
}

func TestSnippetsKeepTheSpacesAroundMatches(t *testing.T) {
	text := strings.Repeat("x ", 200) + "Ein Bäcker backt Brötchen. " + strings.Repeat("y ", 200)
	snippet := MakeSnippet(text, ParseQuery("backer brot"))
	if !strings.HasPrefix(snippet, "… ") || !strings.HasSuffix(snippet, " …") || !strings.Contains(snippet, "Ein \u0002Bäcker\u0003 backt \u0002Brötchen\u0003.") {
		t.Fatalf("snippet = %q", snippet)
	}
	for _, c := range []struct{ text, query, want string }{
		{"a \u0002fake\u0003 b", "b", "a fake \u0002b\u0003"},
		{"short text", "zzz", "short text"},
		{"the quick\n\n  brown fox", `"quick brown"`, "the \u0002quick brown\u0003 fox"},
	} {
		if got := MakeSnippet(c.text, ParseQuery(c.query)); got != c.want {
			t.Errorf("snippet of %q = %q, want %q", c.text, got, c.want)
		}
	}
	// The window is counted in characters, not bytes.
	if got := []rune(MakeSnippet(strings.Repeat("ö", 500), nil)); len(got) != 222 {
		t.Errorf("length = %d", len(got))
	}
}

func TestPhrasesAreCheckedAgainstTheText(t *testing.T) {
	for _, c := range []struct {
		text, query string
		want        bool
	}{
		{"the quick brown fox", `"quick brown"`, true},
		{"brown, quick fox", `"quick brown"`, false},
		{"send an E-Mail now", "e-ma", true},
		{"send an e mailing", `"e mail"`, false},
	} {
		if got := matchesAll(c.text, ParseQuery(c.query)); got != c.want {
			t.Errorf("%q in %q = %t", c.query, c.text, got)
		}
	}
}

func TestPlainTextIsDecodedWhateverItsCharset(t *testing.T) {
	for _, c := range []struct {
		name, kind string
		data       []byte
		want       string
	}{
		{"bom.txt", "text", []byte("\xef\xbb\xbfbom utf-8 ä"), "bom utf-8 ä"},
		{"plain.txt", "text", []byte("plain utf-8 ß"), "plain utf-8 ß"},
		{"latin.txt", "text", []byte{'c', 'a', 'f', 0xe9, ' ', 0x80}, "café €"},
		{"be.txt", "text", []byte{0xfe, 0xff, 0, 'b', 0, 'e'}, "be"},
		{"binary.txt", "text", []byte{0x89, 'P', 'N', 'G', 0, 0, 1, 2, 0xff, 0}, ""},
		{"koi.html", "html", []byte("<meta charset=\"koi8-r\"><p>\xf0\xd2\xc9\xd7\xc5\xd4</p>"), "Привет"},
	} {
		got, err := ExtractText(context.Background(), Node{Name: c.name, Size: int64(len(c.data))}, c.data)
		if err != nil || strings.TrimSpace(got) != c.want {
			t.Errorf("%s = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}

func TestCapTextPreservesUTF8(t *testing.T) {
	text := strings.Repeat("a", MaxTextBytes-1) + "é"
	got := capText(text)
	if len(got) != MaxTextBytes-1 || !strings.HasSuffix(got, "a") {
		t.Fatalf("cap did not stop before partial rune: len=%d suffix=%q", len(got), got[len(got)-1:])
	}
}
