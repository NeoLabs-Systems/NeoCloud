package application

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/gen2brain/vpx/webp"
)

type dataPlane struct {
	t      *testing.T
	app    *Application
	server *httptest.Server
}

func openDataPlane(t *testing.T) *dataPlane {
	t.Helper()
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	return &dataPlane{t: t, app: app, server: server}
}

// account registers a user; the first one is the administrator.
func (p *dataPlane) account(name string) *http.Client {
	p.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		p.t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	status, body := p.send(client, http.MethodPost, "/api/auth/register",
		fmt.Sprintf(`{"username":%q,"email":"%s@example.test","password":"Q8!zLp#3vT7mK5rX"}`, name, name), nil)
	if status != http.StatusOK {
		p.t.Fatalf("register %s = %d %s", name, status, body)
	}
	return client
}

func (p *dataPlane) send(client *http.Client, method, path, body string, headers map[string]string) (int, []byte) {
	p.t.Helper()
	request, err := http.NewRequest(method, p.server.URL+path, strings.NewReader(body))
	if err != nil {
		p.t.Fatal(err)
	}
	if body != "" && method != http.MethodPut {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		p.t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		p.t.Fatal(err)
	}
	return response.StatusCode, data
}

// json sends a request, expects the status and decodes the answer into out.
func (p *dataPlane) json(client *http.Client, method, path, body string, want int, out any) {
	p.t.Helper()
	status, data := p.send(client, method, path, body, nil)
	if status != want {
		p.t.Fatalf("%s %s = %d %s, want %d", method, path, status, data, want)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			p.t.Fatalf("%s %s: %v in %s", method, path, err, data)
		}
	}
}

type uploaded struct {
	ID       string `json:"id"`
	ETag     string `json:"etag"`
	ParentID string `json:"parentId"`
}

func (p *dataPlane) upload(client *http.Client, parent, name string, content []byte, headers map[string]string) uploaded {
	p.t.Helper()
	status, data := p.send(client, http.MethodPut, "/api/upload?parentId="+parent+"&name="+url.QueryEscape(name), string(content), headers)
	if status != http.StatusCreated {
		p.t.Fatalf("upload %s = %d %s", name, status, data)
	}
	var node uploaded
	if err := json.Unmarshal(data, &node); err != nil {
		p.t.Fatal(err)
	}
	return node
}

func picture(t *testing.T, width, height int, exifDate string, shade uint8) []byte {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			source.Set(x, y, color.RGBA{R: uint8((x*7 + y*13) % 251), G: shade, B: uint8(x * 255 / width), A: 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, source, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	if exifDate == "" {
		return out.Bytes()
	}
	// A big-endian EXIF block with DateTimeOriginal in the Exif IFD.
	block := []byte("Exif\x00\x00MM\x00*\x00\x00\x00\x08" +
		"\x00\x01\x87\x69\x00\x04\x00\x00\x00\x01\x00\x00\x00\x1a\x00\x00\x00\x00" +
		"\x00\x01\x90\x03\x00\x02\x00\x00\x00\x14\x00\x00\x00\x2c\x00\x00\x00\x00" + exifDate + "\x00")
	head := []byte{0xff, 0xd8, 0xff, 0xe1, byte((len(block) + 2) >> 8), byte(len(block) + 2)}
	return append(append(head, block...), out.Bytes()[2:]...)
}

func box(kind string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	size := len(body) + 8
	return append([]byte{byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size), kind[0], kind[1], kind[2], kind[3]}, body...)
}

// movie has its moov box after the media data, as phones write it, and a
// track turned by 90 degrees.
func movie(created uint32, seconds, width, height int) []byte {
	put := func(b []byte, at int, v uint32) {
		b[at], b[at+1], b[at+2], b[at+3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
	}
	mvhd, tkhd := make([]byte, 100), make([]byte, 84)
	put(mvhd, 4, created)
	put(mvhd, 12, 1000)
	put(mvhd, 16, uint32(seconds*1000))
	put(tkhd, 44, 65536)
	put(tkhd, 76, uint32(width*65536))
	put(tkhd, 80, uint32(height*65536))
	return bytes.Join([][]byte{box("ftyp", []byte("isom\x00\x00\x00\x00isom")), box("mdat", make([]byte, 4096)), box("moov", box("mvhd", mvhd), box("trak", box("tkhd", tkhd)))}, nil)
}

type timelineItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	TakenAt     string  `json:"takenAt"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Duration    float64 `json:"durationSeconds"`
	DateSource  string  `json:"dateSource"`
	Provisional bool    `json:"provisional"`
}

func (p *dataPlane) timeline(client *http.Client) []timelineItem {
	p.t.Helper()
	var page struct {
		Items []timelineItem `json:"items"`
	}
	p.json(client, http.MethodGet, "/api/photos?limit=500", "", http.StatusOK, &page)
	return page.Items
}

func TestComposedPhotosThumbnailsAndSimilarUploads(t *testing.T) {
	p := openDataPlane(t)
	alice := p.account("alice")
	bob := p.account("bobby")

	beach := p.upload(alice, "root", "beach.jpg", picture(t, 900, 600, "2019:05:04 12:34:56", 90), nil)
	dated := p.upload(alice, "root", "download.jpg", picture(t, 40, 30, "", 10), map[string]string{"X-File-Created": "2023-04-05T06:07:08+02:00"})
	// 2020-07-01T08:00:00Z in QuickTime seconds.
	clip := p.upload(alice, "root", "clip.mp4", movie(1593590400+2082844800, 42, 1920, 1080), nil)
	named := p.upload(alice, "root", "IMG_20210101_101010.jpg", picture(t, 8, 8, "", 200), nil)
	p.upload(alice, "root", "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), nil)
	p.upload(alice, "root", "notes.txt", []byte("not a photo"), nil)

	if early := p.timeline(alice); len(early) != 4 || !early[0].Provisional {
		t.Fatalf("before reading = %+v", early)
	}
	if _, err := p.app.photos.RunPass(context.Background(), 20); err != nil {
		t.Fatal(err)
	}
	items := p.timeline(alice)
	var order []string
	byName := map[string]timelineItem{}
	for _, item := range items {
		order = append(order, item.Name)
		byName[item.Name] = item
	}
	if want := []string{"download.jpg", "IMG_20210101_101010.jpg", "clip.mp4", "beach.jpg"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v", order)
	}
	if got := byName["beach.jpg"]; got.TakenAt != "2019-05-04T12:34:56" || got.DateSource != "exif" || got.Width != 900 || got.Height != 600 {
		t.Fatalf("beach = %+v", got)
	}
	if got := byName["download.jpg"]; got.DateSource != "client" || got.TakenAt != "2023-04-05T06:07:08" {
		t.Fatalf("client date = %+v", got)
	}
	if got := byName["clip.mp4"]; got.TakenAt != "2020-07-01T08:00:00" || got.Duration != 42 || got.Width != 1080 || got.Height != 1920 || got.Kind != "video" {
		t.Fatalf("movie = %+v", got)
	}
	if got := byName["IMG_20210101_101010.jpg"]; got.DateSource != "filename" {
		t.Fatalf("named = %+v", got)
	}
	_, _ = dated, named

	// The grid thumbnail was made by the indexer.
	var previews int
	if err := p.app.Runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM previews v JOIN nodes n ON n.blob_id=v.blob_id WHERE n.id=? AND v.variant='thumb-square' AND v.mime='image/webp'`, beach.ID).Scan(&previews); err != nil || previews != 1 {
		t.Fatalf("prepared thumbnails = %d, %v", previews, err)
	}
	status, data := p.send(alice, http.MethodGet, "/api/files/"+beach.ID+"/thumbnail?size=square&v="+beach.ETag, "", nil)
	if status != http.StatusOK {
		t.Fatalf("thumbnail = %d %s", status, data)
	}
	if decoded, err := webp.Decode(bytes.NewReader(data)); err != nil || decoded.Bounds().Dx() != 320 || decoded.Bounds().Dy() != 320 {
		t.Fatalf("square thumbnail: %v", err)
	}
	if status, _ = p.send(bob, http.MethodGet, "/api/files/"+beach.ID+"/thumbnail", "", nil); status != http.StatusNotFound {
		t.Fatalf("another account's thumbnail = %d", status)
	}
	if len(p.timeline(bob)) != 0 {
		t.Fatal("another account sees the photos")
	}
	var capabilities map[string]bool
	p.json(alice, http.MethodGet, "/api/capabilities", "", http.StatusOK, &capabilities)
	if !capabilities["imageThumbnails"] {
		t.Fatalf("capabilities = %v", capabilities)
	}

	// Similar-upload check.
	for {
		hashed, err := p.app.photos.HashPass(context.Background(), 10)
		if err != nil {
			t.Fatal(err)
		}
		if hashed == 0 {
			break
		}
	}
	var hash string
	if err := p.app.Runtime.DB.W.QueryRow(`SELECT dhash FROM image_hashes WHERE node_id=?`, beach.ID).Scan(&hash); err != nil || len(hash) != 16 {
		t.Fatalf("hash = %q, %v", hash, err)
	}
	var similar struct {
		Matches []struct {
			Hash       string  `json:"hash"`
			NodeID     string  `json:"nodeId"`
			Similarity float64 `json:"similarity"`
		} `json:"matches"`
	}
	p.json(alice, http.MethodPost, "/api/uploads/similar", `{"hash":"`+strings.ToUpper(hash)+`"}`, http.StatusOK, &similar)
	if len(similar.Matches) != 1 || similar.Matches[0].NodeID != beach.ID || similar.Matches[0].Similarity != 100 || similar.Matches[0].Hash != hash {
		t.Fatalf("matches = %+v", similar.Matches)
	}
	p.json(bob, http.MethodPost, "/api/uploads/similar", `{"hashes":["`+hash+`"]}`, http.StatusOK, &similar)
	if len(similar.Matches) != 0 {
		t.Fatalf("another account matched: %+v", similar.Matches)
	}
	for body, code := range map[string]string{
		`{}`: "INVALID_HASH", `{"hashes":[]}`: "INVALID_HASH", `{"hash":5}`: "INVALID_HASH", `{"hash":"zz"}`: "INVALID_HASH",
		`{"hashes":[` + strings.Repeat(`"0000000000000000",`, 200) + `"0000000000000000"]}`: "TOO_MANY_HASHES",
	} {
		if status, data := p.send(alice, http.MethodPost, "/api/uploads/similar", body, nil); status != http.StatusBadRequest || !strings.Contains(string(data), code) {
			t.Fatalf("%.40s = %d %s", body, status, data)
		}
	}

	// The phone app reads the timeline; desktop tokens do not.
	var owner int64
	if err := p.app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username='alice'`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	for platform, want := range map[string]int{"android": http.StatusOK, "macos": http.StatusForbidden} {
		token := "nc_test_" + platform
		if _, err := p.app.Runtime.DB.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(?,?,?,?)`, owner, platform, platform, passhash.DigestToken(token)); err != nil {
			t.Fatal(err)
		}
		header := map[string]string{"Authorization": "Bearer " + token}
		for _, route := range [][2]string{{http.MethodGet, "/api/photos"}, {http.MethodGet, "/api/photos/summary"}, {http.MethodPost, "/api/uploads/similar"}} {
			body := ""
			if route[0] == http.MethodPost {
				body = `{"hash":"0000000000000000"}`
				header["Content-Type"] = "application/json"
			}
			if status, data := p.send(http.DefaultClient, route[0], route[1], body, header); status != want {
				t.Fatalf("%s %s with a %s token = %d %s", route[0], route[1], platform, status, data)
			}
		}
		// Thumbnails are for every device.
		if status, _ := p.send(http.DefaultClient, http.MethodGet, "/api/files/"+beach.ID+"/thumbnail", "", header); status != http.StatusOK {
			t.Fatalf("thumbnail with a %s token = %d", platform, status)
		}
	}
	for _, path := range []string{"/api/photos", "/api/photos/summary", "/api/files/" + beach.ID + "/thumbnail", "/api/capabilities"} {
		if status, _ := p.send(http.DefaultClient, http.MethodGet, path, "", nil); status != http.StatusUnauthorized {
			t.Fatalf("anonymous %s = %d", path, status)
		}
	}

	// Trashed files leave the timeline and the similar check at once.
	p.json(alice, http.MethodDelete, "/api/nodes/"+beach.ID, "", http.StatusOK, nil)
	if got := p.timeline(alice); len(got) != 3 {
		t.Fatalf("after trashing = %+v", got)
	}
	p.json(alice, http.MethodPost, "/api/uploads/similar", `{"hash":"`+hash+`"}`, http.StatusOK, &similar)
	for _, match := range similar.Matches {
		if match.NodeID == beach.ID {
			t.Fatal("a trashed photo still matches")
		}
	}
	_ = clip
}

func officeArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	for name, content := range files {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func onePagePDF(text string) []byte {
	stream := "BT /F1 18 Tf 72 720 Td (" + text + ") Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var out strings.Builder
	out.WriteString("%PDF-1.4\n")
	var offsets []int
	for index, body := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", index+1, body)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return []byte(out.String())
}

type hit struct {
	Name    string `json:"name"`
	Match   string `json:"match"`
	Snippet string `json:"snippet"`
}

func (p *dataPlane) search(client *http.Client, query, extra string) []hit {
	p.t.Helper()
	var found struct {
		Items []hit `json:"items"`
	}
	p.json(client, http.MethodGet, "/api/search?content=1&q="+url.QueryEscape(query)+extra, "", http.StatusOK, &found)
	return found.Items
}

func names(hits []hit) []string {
	out := []string{}
	for _, h := range hits {
		out = append(out, h.Name)
	}
	slices.Sort(out)
	return out
}

func (p *dataPlane) index() int {
	p.t.Helper()
	total := 0
	for {
		done, err := p.app.searchIndexer.RunPass(context.Background(), 25)
		if err != nil {
			p.t.Fatal(err)
		}
		if done == 0 {
			return total
		}
		total += done
	}
}

type indexStatus struct {
	Enabled                         bool
	Indexed, Failed, Pending, Bytes int64
	Available                       struct{ Pdftotext, Office bool }
}

func TestComposedFullTextSearch(t *testing.T) {
	p := openDataPlane(t)
	owner := p.account("ftsowner")
	other := p.account("ftsother")
	enable := func(on bool) {
		p.json(owner, http.MethodPut, "/api/admin/config", fmt.Sprintf(`{"fullTextSearch":%t}`, on), http.StatusOK, nil)
	}
	enable(true)

	p.upload(owner, "root", "notes.txt", []byte("Grandma's crème brûlée needs vanilla."), nil)
	p.upload(owner, "root", "legacy.txt", []byte("Herr M\xfcller wohnt in der Hauptstra\xdfe."), nil)
	p.upload(owner, "root", "page.html", []byte("<html><head><script>var hiddenscriptword = 1;</script></head><body><p>Visible &amp; gooseberry</p></body></html>"), nil)
	p.upload(owner, "root", "report.docx", officeArchive(t, map[string]string{"word/document.xml": `<w:document xmlns:w="x"><w:body><w:p><w:r><w:t>Annual albatross summary</w:t></w:r></w:p><w:p><w:r><w:t>par</w:t></w:r><w:r><w:t>agraph</w:t></w:r></w:p></w:body></w:document>`}), nil)
	p.upload(owner, "root", "budget.xlsx", officeArchive(t, map[string]string{
		"xl/sharedStrings.xml":     `<sst><si><t>Quokka expenses</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c t="s"><v>0</v></c><c><v>4711</v></c><c t="inlineStr"><is><t>Wombat income</t></is></c></row></sheetData></worksheet>`,
	}), nil)
	p.upload(owner, "root", "deck.pptx", officeArchive(t, map[string]string{"ppt/slides/slide1.xml": `<p:sld xmlns:p="p" xmlns:a="a"><a:p><a:r><a:t>Narwhal roadmap</a:t></a:r></a:p></p:sld>`}), nil)
	p.upload(owner, "root", "letter.odt", officeArchive(t, map[string]string{"content.xml": `<office:document-content xmlns:office="o" xmlns:text="t"><office:body><text:p>Dear axolotl committee</text:p></office:body></office:document-content>`}), nil)
	p.upload(owner, "root", "scan.pdf", onePagePDF("Quarterly lighthouse inspection"), nil)
	p.upload(owner, "root", "broken.docx", []byte("this is not a zip"), nil)
	p.upload(owner, "root", "photo.jpg", []byte("random"), nil)
	p.upload(other, "root", "private.txt", []byte("Other account mentions albatross too."), nil)
	p.index()

	for query, want := range map[string]string{
		"creme brulee": "notes.txt", "müller": "legacy.txt", "hauptstraße": "legacy.txt", "gooseberry": "page.html",
		"albatross": "report.docx", "paragraph": "report.docx", "quokka": "budget.xlsx", "wombat": "budget.xlsx",
		"4711": "budget.xlsx", "narwhal": "deck.pptx", "axolotl": "letter.odt", "lighthouse": "scan.pdf",
	} {
		got := p.search(owner, query, "")
		if len(got) != 1 || got[0].Name != want || got[0].Match != "content" {
			t.Errorf("%s = %+v, want %s", query, got, want)
		}
	}
	if got := p.search(owner, "hiddenscriptword", ""); len(got) != 0 {
		t.Fatalf("script text is searchable: %+v", got)
	}

	var status indexStatus
	p.json(owner, http.MethodGet, "/api/admin/search-index", "", http.StatusOK, &status)
	if !status.Enabled || status.Pending != 0 || status.Failed != 1 || status.Indexed != 9 || status.Bytes <= 0 || !status.Available.Office {
		t.Fatalf("status = %+v", status)
	}
	for _, route := range [][2]string{{http.MethodGet, "/api/admin/search-index"}, {http.MethodPost, "/api/admin/search-index/rebuild"}} {
		if code, _ := p.send(other, route[0], route[1], "", nil); code != http.StatusForbidden {
			t.Fatalf("member %v = %d", route, code)
		}
		if code, _ := p.send(http.DefaultClient, route[0], route[1], "", nil); code != http.StatusUnauthorized && code != http.StatusForbidden {
			t.Fatalf("anonymous %v = %d", route, code)
		}
	}

	// Ranking, phrases and snippets.
	p.upload(owner, "root", "kiwi.txt", []byte("nothing about the fruit here"), nil)
	p.upload(owner, "root", "kiwi-both.txt", []byte("one kiwi here"), nil)
	p.upload(owner, "root", "phrase-yes.txt", []byte("the quick brown fox"), nil)
	p.upload(owner, "root", "phrase-no.txt", []byte("brown is quick"), nil)
	p.index()
	got := p.search(owner, "kiwi", "")
	if len(got) != 2 || got[0].Name != "kiwi.txt" || got[0].Match != "name" || got[1].Match != "both" || got[1].Snippet != "one \u0002kiwi\u0003 here" {
		t.Fatalf("kiwi = %+v", got)
	}
	if phrase := p.search(owner, `"quick brown"`, ""); len(phrase) != 1 || phrase[0].Name != "phrase-yes.txt" || phrase[0].Snippet != "the \u0002quick brown\u0003 fox" {
		t.Fatalf("phrase = %+v", phrase)
	}
	if words := names(p.search(owner, "quick brown", "")); !slices.Equal(words, []string{"phrase-no.txt", "phrase-yes.txt"}) {
		t.Fatalf("words = %v", words)
	}

	// Accounts and scopes.
	if theirs := names(p.search(other, "albatross", "")); !slices.Equal(theirs, []string{"private.txt"}) {
		t.Fatalf("other account = %v", theirs)
	}
	var folder uploaded
	p.json(owner, http.MethodPost, "/api/folders", `{"parentId":"root","name":"Mine"}`, http.StatusCreated, &folder)
	inside := p.upload(owner, folder.ID, "inside.txt", []byte("tamarind inside"), nil)
	p.upload(owner, "root", "outside.txt", []byte("tamarind outside"), nil)
	p.index()
	if scoped := names(p.search(owner, "tamarind", "&parentId="+folder.ID)); !slices.Equal(scoped, []string{"inside.txt"}) {
		t.Fatalf("scoped = %v", scoped)
	}
	if code, _ := p.send(other, http.MethodGet, "/api/search?content=1&q=tamarind&parentId="+folder.ID, "", nil); code != http.StatusNotFound {
		t.Fatalf("another account's folder as scope = %d", code)
	}

	// The index follows the tree.
	count := func(where string, arg any) int {
		var n int
		if err := p.app.Runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM search_docs WHERE `+where, arg).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	p.json(owner, http.MethodDelete, "/api/nodes/"+folder.ID, "", http.StatusOK, nil)
	p.index()
	if len(p.search(owner, "tamarind inside", "")) != 0 || count("node_id=?", inside.ID) != 0 {
		t.Fatal("trashed folder is still indexed")
	}
	p.json(owner, http.MethodPost, "/api/trash/"+folder.ID+"/restore", "", http.StatusOK, nil)
	p.index()
	if count("node_id=?", inside.ID) != 1 {
		t.Fatal("restored file was not indexed again")
	}
	p.json(owner, http.MethodPatch, "/api/nodes/"+inside.ID, `{"name":"inside.bin"}`, http.StatusOK, nil)
	p.index()
	if count("node_id=?", inside.ID) != 0 {
		t.Fatal("renamed to an unread type but still indexed")
	}

	// Opt-out.
	p.json(other, http.MethodPut, "/api/settings", `{"fullTextSearch":false}`, http.StatusOK, nil)
	p.index()
	if len(p.search(other, "albatross", "")) != 0 {
		t.Fatal("opted-out account still gets content matches")
	}
	var otherID int64
	if err := p.app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username='ftsother'`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	if count("user_id=?", otherID) != 0 {
		t.Fatal("opted-out account is still indexed")
	}

	// Switch off, then rebuild.
	enable(false)
	p.upload(owner, "root", "paused.txt", []byte("guava while paused"), nil)
	if p.index() != 0 || len(p.search(owner, "albatross", "")) != 0 {
		t.Fatal("indexing or content search runs while switched off")
	}
	p.json(owner, http.MethodGet, "/api/admin/search-index", "", http.StatusOK, &status)
	if status.Enabled || status.Pending != 1 {
		t.Fatalf("status while off = %+v", status)
	}
	enable(true)
	if p.index() != 1 {
		t.Fatal("the paused file was not read after switching on")
	}
	p.json(owner, http.MethodGet, "/api/admin/search-index", "", http.StatusOK, &status)
	before := status
	p.json(owner, http.MethodPost, "/api/admin/search-index/rebuild", "", http.StatusAccepted, &status)
	if status.Indexed != 0 || status.Failed != 0 || status.Pending != before.Indexed+before.Failed {
		t.Fatalf("rebuild = %+v, before %+v", status, before)
	}
	p.index()
	if got := names(p.search(owner, "lighthouse", "")); !slices.Equal(got, []string{"scan.pdf"}) {
		t.Fatalf("after rebuild = %v", got)
	}
}
