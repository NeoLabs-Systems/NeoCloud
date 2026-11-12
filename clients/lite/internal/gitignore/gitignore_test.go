package gitignore

import "testing"

// Cases from git's t/t3070-wildmatch.sh, the "pathmatch" column (wildmatch
// with WM_PATHNAME), which is what ignore files use.
func TestWildmatch(t *testing.T) {
	cases := []struct {
		pattern, text string
		want          bool
	}{
		{"foo", "foo", true},
		{"foo", "bar", false},
		{"", "", true},
		{"???", "foo", true},
		{"??", "foo", false},
		{"*", "foo", true},
		{"f*", "foo", true},
		{"*f", "foo", false},
		{"*foo*", "foo", true},
		{"*ob*a*r*", "foobar", true},
		{"*ab", "aaaaaaabababab", true},
		{`foo\*`, "foo*", true},
		{`foo\*bar`, "foobar", false},
		{`f\\oo`, `f\oo`, true},
		{"*[al]?", "ball", true},
		{"[ten]", "ten", false},
		{"**[!te]", "ten", true},
		{"**[!ten]", "ten", false},
		{"t[a-g]n", "ten", true},
		{"t[!a-g]n", "ten", false},
		{"t[!a-g]n", "ton", true},
		{"t[^a-g]n", "ton", true},
		{"a[]]b", "a]b", true},
		{"a[]-]b", "a-b", true},
		{"a[]-]b", "a]b", true},
		{"a[]-]b", "aab", false},
		{"a[]a-]b", "aab", true},
		{"]", "]", true},
		// Slashes: only "**" crosses them.
		{"foo*bar", "foo/baz/bar", false},
		{"foo**bar", "foo/baz/bar", false},
		{"foo**bar", "foobazbar", true},
		{"foo/**/bar", "foo/baz/bar", true},
		{"foo/**/**/bar", "foo/baz/bar", true},
		{"foo/**/bar", "foo/b/a/z/bar", true},
		{"foo/**/**/bar", "foo/b/a/z/bar", true},
		{"foo/**/bar", "foo/bar", true},
		{"foo/**/**/bar", "foo/bar", true},
		{"foo?bar", "foo/bar", false},
		{"foo[/]bar", "foo/bar", false},
		{"foo[^a-z]bar", "foo/bar", false},
		{"f[^eiu][^eiu][^eiu][^eiu][^eiu]r", "foo/bar", false},
		{"f[^eiu][^eiu][^eiu][^eiu][^eiu]r", "foo-bar", true},
		{"**/foo", "foo", true},
		{"**/foo", "XXX/foo", true},
		{"**/foo", "bar/baz/foo", true},
		{"*/foo", "bar/baz/foo", false},
		{"**/bar*", "foo/bar/baz", false},
		{"**/bar/*", "deep/foo/bar/baz", true},
		{"**/bar/*", "deep/foo/bar/baz/", false},
		{"**/bar/**", "deep/foo/bar/baz/", true},
		{"**/bar/*", "deep/foo/bar", false},
		{"**/bar/**", "deep/foo/bar/", true},
		{"**/bar**", "foo/bar/baz", false},
		{"*/bar/**", "foo/bar/baz/x", true},
		{"*/bar/**", "deep/foo/bar/baz/x", false},
		{"**/bar/*/*", "deep/foo/bar/baz/x", true},
		{"a/**", "a/b/c", true},
		{"a/**", "a", false},
		{"**", "a/b/c", true},
		// Character classes.
		{"[[:alpha:]][[:digit:]][[:upper:]]", "a1B", true},
		{"[[:digit:][:upper:][:space:]]", "a", false},
		{"[[:digit:][:upper:][:space:]]", "A", true},
		{"[[:digit:][:upper:][:space:]]", "1", true},
		{"[[:digit:][:upper:][:spaci:]]", "1", false}, // unknown class: never matches
		{"[[:digit:][:punct:][:space:]]", ".", true},
		{"[[:xdigit:]]", "f", true},
		{"[a-c[:digit:]x-z]", "5", true},
		{"[a-c[:digit:]x-z]", "y", true},
		{"[a-c[:digit:]x-z]", "q", false},
		{"[\\-^]", "]", false},
		{"[\\-^]", "[", false},
		{"[\\-_]", "-", true},
		{"[\\]]", "]", true},
		{"[\\]]", "\\]", false},
		{"[!\\]]", "a", true},
		{"[A-\\\\]", "G", true},
		{"[[]", "[", true},
		{"[", "[", false}, // unterminated
		{"[!", "[!", false},
		{"[a", "a", false},
		{"ab\\", "ab\\", false}, // a trailing backslash never matches
		// Recursion limits from the git tests.
		{"-*-*-*-*-*-*-12-*-*-*-m-*-*-*", "-adobe-courier-bold-o-normal--12-120-75-75-m-70-iso8859-1", true},
		{"-*-*-*-*-*-*-12-*-*-*-m-*-*-*", "-adobe-courier-bold-o-normal--12-120-75-75-X-70-iso8859-1", false},
		{"XXX/*/*/*/*/*/*/12/*/*/*/m/*/*/*", "XXX/adobe/courier/bold/o/normal//12/120/75/75/m/70/iso8859/1", true},
		{"**/*a*b*g*n*t", "abcd/abcdefg/abcdefghijk/abcdefghijklmnop.txt", true},
		{"**/*a*b*g*n*t", "abcd/abcdefg/abcdefghijk/abcdefghijklmnop.txtz", false},
		{"*/*/*", "foo/bba/arr", true},
		{"*/*/*", "foo/bb/aa/rr", false},
		// Characters beyond ASCII count as one.
		{"?ber", "über", true},
		{"[ä-ü]", "ö", true},
	}
	for _, c := range cases {
		got := dowild([]rune(c.pattern), 0, []rune(c.text), 0) == wmMatch
		if got != c.want {
			t.Errorf("wildmatch(%q, %q) = %v, want %v", c.pattern, c.text, got, c.want)
		}
	}
}

func TestParseLine(t *testing.T) {
	cases := []struct {
		line     string
		ok       bool
		negate   bool
		dirOnly  bool
		anchored bool
		glob     string
	}{
		{"", false, false, false, false, ""},
		{"   ", false, false, false, false, ""},
		{"# comment", false, false, false, false, ""},
		{`\#hash`, true, false, false, false, `\#hash`},
		{"!keep.log", true, true, false, false, "keep.log"},
		{`\!bang`, true, false, false, false, `\!bang`},
		{"build/", true, false, true, false, "build"},
		{"/build", true, false, false, true, "build"},
		{"/build/", true, false, true, true, "build"},
		{"doc/frotz", true, false, false, true, "doc/frotz"},
		{"doc/frotz/", true, false, true, true, "doc/frotz"},
		{"**/foo", true, false, false, true, "**/foo"},
		{"trailing   ", true, false, false, false, "trailing"},
		{`space\ `, true, false, false, false, `space\ `},
		{`two\  `, true, false, false, false, `two\ `},
		{"!", false, false, false, false, ""},
		{"/", false, false, false, false, ""},
		{"  lead", true, false, false, false, "  lead"}, // leading spaces count
	}
	for _, c := range cases {
		p, ok := ParseLine(c.line)
		if ok != c.ok {
			t.Errorf("ParseLine(%q) ok = %v", c.line, ok)
			continue
		}
		if !ok {
			continue
		}
		if p.Negate != c.negate || p.DirOnly != c.dirOnly || p.anchored != c.anchored || string(p.glob) != c.glob {
			t.Errorf("ParseLine(%q) = %+v (glob %q)", c.line, p, string(p.glob))
		}
	}
}

func TestPatternMatch(t *testing.T) {
	cases := []struct {
		pattern string
		rel     string
		isDir   bool
		want    bool
	}{
		// No slash: the name at any depth.
		{"*.log", "app.log", false, true},
		{"*.log", "a/b/app.log", false, true},
		{"frotz", "a/frotz", true, true},
		{"frotz", "a/frotz", false, true},
		// A trailing slash: folders only.
		{"frotz/", "a/frotz", true, true},
		{"frotz/", "a/frotz", false, false},
		// A leading or middle slash anchors to the file's folder.
		{"/frotz", "frotz", false, true},
		{"/frotz", "a/frotz", false, false},
		{"doc/frotz", "doc/frotz", true, true},
		{"doc/frotz", "a/doc/frotz", true, false},
		{"/*.c", "cat-file.c", false, true},
		{"/*.c", "mozilla-sha1/sha1.c", false, false},
		{"Documentation/*.html", "Documentation/git.html", false, true},
		{"Documentation/*.html", "Documentation/ppc/ppc.html", false, false},
		// "**" forms.
		{"**/foo", "foo", false, true},
		{"**/foo", "x/y/foo", true, true},
		{"**/foo/bar", "x/foo/bar", false, true},
		{"abc/**", "abc/x/y", false, true},
		{"abc/**", "abc", true, false},
		{"a/**/b", "a/b", false, true},
		{"a/**/b", "a/x/y/b", false, true},
		{"a/**/b", "x/a/b", false, false},
		// Escapes and spaces.
		{`\#notes`, "#notes", false, true},
		{`\!important`, "!important", false, true},
		{`name\ `, "name ", false, true},
		{`name\ `, "name", false, false},
		{"name   ", "name", false, true},
		// Character classes in names.
		{"report[0-9].txt", "report7.txt", false, true},
		{"report[!0-9].txt", "report7.txt", false, false},
		{"report[!0-9].txt", "reportX.txt", false, true},
		// Case matters, like git with core.ignorecase off.
		{"*.log", "APP.LOG", false, false},
	}
	for _, c := range cases {
		p, ok := ParseLine(c.pattern)
		if !ok {
			t.Fatalf("ParseLine(%q) failed", c.pattern)
		}
		if got := p.Match(c.rel, c.isDir); got != c.want {
			t.Errorf("%q.Match(%q, dir=%v) = %v, want %v", c.pattern, c.rel, c.isDir, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	patterns := Parse([]byte("\xef\xbb\xbf# top\r\n*.log\r\n\r\n!keep.log\nbuild/  \n"))
	if len(patterns) != 3 {
		t.Fatalf("got %d patterns: %+v", len(patterns), patterns)
	}
	want := []struct {
		line int
		text string
	}{{2, "*.log"}, {4, "!keep.log"}, {5, "build/"}}
	for i, w := range want {
		if patterns[i].Line != w.line || patterns[i].Text != w.text {
			t.Errorf("pattern %d = line %d %q, want line %d %q", i, patterns[i].Line, patterns[i].Text, w.line, w.text)
		}
	}
}
