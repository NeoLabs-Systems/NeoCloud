package signpdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// assemble builds a small file with a classic cross-reference table.
func assemble(objects ...string) []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n")
	offsets := []int{}
	for i, object := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}

func deflate(data []byte) []byte {
	var out bytes.Buffer
	w := zlib.NewWriter(&out)
	_, _ = w.Write(data)
	_ = w.Close()
	return out.Bytes()
}

// bomb is a small file whose object stream inflates to hundreds of megabytes.
func bomb() []byte {
	head := "9 0 "
	content := deflate(append([]byte(head+"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] >>"), bytes.Repeat([]byte(" "), 400<<20)...))
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n")
	offsets := map[int]int{}
	add := func(n int, body string) {
		offsets[n] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	add(1, "<< /Type /Catalog /Pages 2 0 R >>")
	add(2, "<< /Type /Pages /Kids [9 0 R] /Count 1 >>")
	add(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] >>")
	add(4, fmt.Sprintf("<< /Type /ObjStm /N 1 /First %d /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", len(head), len(content), content))
	var table bytes.Buffer
	entry := func(kind byte, value, index int) {
		table.Write([]byte{kind, byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value), byte(index)})
	}
	xref := out.Len()
	entry(0, 0, 0)
	for n := 1; n <= 4; n++ {
		entry(1, offsets[n], 0)
	}
	for n := 5; n <= 7; n++ {
		entry(0, 0, 0)
	}
	entry(1, xref, 0)
	entry(2, 4, 0)
	fmt.Fprintf(&out, "8 0 obj\n<< /Type /XRef /Size 10 /W [1 4 1] /Root 1 0 R /Length %d >>\nstream\n%s\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", table.Len(), table.Bytes(), xref)
	return out.Bytes()
}

func TestHostileFilesAreRefused(t *testing.T) {
	minimal := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] >>"}
	if _, err := Probe(ctx, assemble(minimal...)); err != nil {
		t.Fatal("the minimal file:", err)
	}
	file := bomb()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := Probe(ctx, file)
	runtime.ReadMemStats(&after)
	if grown := (after.TotalAlloc - before.TotalAlloc) >> 20; err == nil || grown > 700 {
		t.Errorf("bomb of %d bytes: %v, allocated %d MB", len(file), err, grown)
	} else {
		t.Logf("bomb of %d bytes refused (%v), allocated %d MB", len(file), err, grown)
	}

	cycle := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [2 0 R] /Count 1 >>"}
	deep := []string{"<< /Type /Catalog /Pages 2 0 R /X " + strings.Repeat("[", 200000) + strings.Repeat("]", 200000) + " >>", minimal[1], minimal[2]}
	encrypted := bytes.Replace(assemble(minimal...), []byte("/Root 1 0 R"), []byte("/Root 1 0 R /Encrypt 9 0 R"), 1)
	good := assemble(minimal...)
	for name, data := range map[string][]byte{
		"page cycle": assemble(cycle...), "deep nesting": assemble(deep...), "no pages": assemble(cycle[0], "<< /Type /Pages /Kids [] /Count 0 >>"),
		"truncated": good[:len(good)/3], "header only": []byte("%PDF-1.7\n%%EOF"), "encrypted": encrypted,
	} {
		if _, err := Probe(ctx, data); err == nil {
			t.Errorf("%s accepted", name)
		} else if !strings.Contains(err.Error(), "PDF_") {
			t.Errorf("%s: untyped error %v", name, err)
		}
	}
}

func TestTooManyPages(t *testing.T) {
	kids := strings.Repeat("3 0 R ", MaxPages+1)
	_, err := Probe(ctx, assemble("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids ["+kids+"] /Count 301 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 1 1] >>"))
	if err == nil {
		t.Fatal("accepted")
	}
}
