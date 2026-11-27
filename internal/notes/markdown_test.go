package notes

import (
	"reflect"
	"testing"
)

func TestNoteWithoutFrontmatterIsPlain(t *testing.T) {
	doc := parseNote("Hello\nworld")
	want := properties{Tags: []string{}, Color: "default"}
	if !reflect.DeepEqual(doc.properties, want) || doc.body != "Hello\nworld" || serialize(doc) != "Hello\nworld" {
		t.Fatalf("%#v", doc)
	}
}

func TestFrontmatterKeepsUnknownKeys(t *testing.T) {
	doc := parseNote("---\ntitle: \"From Obsidian\"\ntags:\n  - work\n  - \"Küche\"\npinned: true\ncolor: teal\naliases: [one, two]\ncustom:\n  - a\n---\nBody")
	want := properties{Tags: []string{"work", "Küche"}, Pinned: true, Color: "teal"}
	if !reflect.DeepEqual(doc.properties, want) || doc.body != "Body" {
		t.Fatalf("%#v", doc)
	}
	doc.Pinned, doc.Archived = false, true
	written := serialize(doc)
	expected := "---\ntags:\n  - work\n  - \"Küche\"\narchived: true\ncolor: teal\ntitle: \"From Obsidian\"\naliases: [one, two]\ncustom:\n  - a\n---\nBody"
	if written != expected {
		t.Fatalf("written %q", written)
	}
	if again := parseNote(written).properties; !reflect.DeepEqual(again, properties{Tags: []string{"work", "Küche"}, Archived: true, Color: "teal"}) {
		t.Fatalf("%#v", again)
	}
}

func TestInlineTagsAndUnknownColorsAreCleaned(t *testing.T) {
	doc := parseNote("---\ntags: [a, \"#b\", A]\ncolor: neon\n---\n")
	if !reflect.DeepEqual(doc.Tags, []string{"a", "b"}) || doc.Color != "default" {
		t.Fatalf("%#v", doc)
	}
}

func TestSnippetDropsMarksAndCountsChecklist(t *testing.T) {
	snippet, progress := summarize("# Trip\n\n- [x] **Book** flight\n- [ ] Pack [list](https://x)\n---\n> note")
	if snippet != "Trip · Book flight · Pack list · note" || progress != (checklist{Done: 1, Total: 2}) {
		t.Fatalf("%q %#v", snippet, progress)
	}
}
