package externalstorage

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 is a path-style S3 holding one bucket in memory.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]string
}

func (s *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	query := r.URL.Query()
	fail := func(status int, code string) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "<Error><Code>"+code+"</Code></Error>")
	}
	if bucket != "photos" {
		fail(404, "NoSuchBucket")
		return
	}
	switch {
	case r.Method == "GET" && query.Get("list-type") == "2":
		s.list(w, query)
	case r.Method == "POST" && query.Has("delete"):
		var request struct {
			Objects []struct{ Key string } `xml:"Object"`
		}
		_ = xml.NewDecoder(r.Body).Decode(&request)
		for _, object := range request.Objects {
			delete(s.objects, object.Key)
		}
		_, _ = io.WriteString(w, "<DeleteResult/>")
	case r.Method == "PUT" && r.Header.Get("X-Amz-Copy-Source") != "":
		source, _ := url.PathUnescape(r.Header.Get("X-Amz-Copy-Source"))
		s.objects[key] = s.objects[strings.TrimPrefix(strings.TrimPrefix(source, "/"), "photos/")]
		_, _ = io.WriteString(w, "<CopyObjectResult><ETag>x</ETag></CopyObjectResult>")
	case r.Method == "PUT":
		body, _ := io.ReadAll(r.Body)
		s.objects[key] = string(body)
	default:
		content, found := s.objects[key]
		if !found {
			fail(404, "NoSuchKey")
			return
		}
		w.Header().Set("Last-Modified", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Format(http.TimeFormat))
		if from, to, ok := strings.Cut(strings.TrimPrefix(r.Header.Get("Range"), "bytes="), "-"); ok {
			start, _ := strconv.Atoi(from)
			end, _ := strconv.Atoi(to)
			content = content[start : end+1]
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(206)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		}
		if r.Method == "GET" {
			_, _ = io.WriteString(w, content)
		}
	}
}

func (s *fakeS3) list(w io.Writer, query url.Values) {
	type object struct {
		Key  string
		Size int
	}
	type prefix struct{ Prefix string }
	var result struct {
		XMLName        xml.Name `xml:"ListBucketResult"`
		IsTruncated    bool
		Contents       []object
		CommonPrefixes []prefix
	}
	keys := make([]string, 0, len(s.objects))
	for key := range s.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	seen := map[string]bool{}
	for _, key := range keys {
		rest, ok := strings.CutPrefix(key, query.Get("prefix"))
		if !ok {
			continue
		}
		if folder, _, nested := strings.Cut(rest, "/"); nested && query.Get("delimiter") == "/" {
			common := query.Get("prefix") + folder + "/"
			if !seen[common] {
				result.CommonPrefixes = append(result.CommonPrefixes, prefix{common})
			}
			seen[common] = true
			continue
		}
		result.Contents = append(result.Contents, object{key, len(s.objects[key])})
	}
	_ = xml.NewEncoder(w).Encode(result)
}

func (s *fakeS3) has(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, found := s.objects[key]
	return content, found
}

func TestS3StaysInsideItsPrefix(t *testing.T) {
	store := &fakeS3{objects: map[string]string{"files/hello.txt": "hello from s3", "files/sub/deep.txt": "deep", "other/outside.txt": "outside the prefix"}}
	server := httptest.NewServer(store)
	defer server.Close()
	f := newFixture(t, Options{AllowPrivate: true})
	config := map[string]any{"endpoint": server.URL, "region": "us-east-1", "bucket": "photos", "prefix": "files", "accessKeyId": "AKIATEST", "secretAccessKey": "super-secret-key"}
	created := f.call(owner, "POST", "/api/external-storage", map[string]any{"name": "My bucket", "provider": "s3", "config": config})
	body := f.expect(201, created)
	if strings.Contains(created.Body.String(), "super-secret-key") || body["config"].(map[string]any)["secretAccessKeySet"] != true {
		t.Fatal(created.Body)
	}
	base := "/api/external-storage/" + body["id"].(string)

	root := f.expect(200, f.call(owner, "GET", base+"/list", nil))
	if got := names(root); !slices.Equal(got, []string{"folder:sub", "file:hello.txt"}) {
		t.Fatal(got)
	}
	sub := f.expect(200, f.call(owner, "GET", base+"/list?path=sub", nil))
	if got := sub["items"].([]any)[0].(map[string]any); got["path"] != "sub/deep.txt" || got["mime"] != "text/plain" {
		t.Fatal(got)
	}
	whole := f.call(owner, "GET", base+"/file?path=hello.txt", nil)
	if whole.Body.String() != "hello from s3" || !strings.HasPrefix(whole.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal(whole.Code, whole.Body, whole.Header())
	}
	part := f.call(owner, "GET", base+"/file?path=hello.txt", nil, "Range", "bytes=6-9")
	if part.Code != 206 || part.Body.String() != "from" || part.Header().Get("Content-Range") != "bytes 6-9/13" {
		t.Fatal(part.Code, part.Body, part.Header())
	}

	f.expect(201, f.call(owner, "POST", base+"/folder", map[string]any{"path": "new"}))
	f.expect(409, f.call(owner, "POST", base+"/folder", map[string]any{"path": "new"}))
	f.expect(201, f.call(owner, "PUT", base+"/file?path=new/up.txt", []byte("uploaded")))
	if content, _ := store.has("files/new/up.txt"); content != "uploaded" {
		t.Fatalf("%q", content)
	}
	f.expect(409, f.call(owner, "PUT", base+"/file?path=new/up.txt", []byte("again")))
	if got := f.expect(201, f.call(owner, "PUT", base+"/file?path=new/up.txt&conflict=rename", []byte("again"))); got["name"] != "up (1).txt" {
		t.Fatal(got)
	}
	f.expect(200, f.call(owner, "POST", base+"/move", map[string]any{"from": "new/up.txt", "to": "new/moved.txt"}))
	f.expect(200, f.call(owner, "POST", base+"/move", map[string]any{"from": "new", "to": "renamed"}))
	if _, found := store.has("files/renamed/moved.txt"); !found {
		t.Fatal(store.objects)
	}
	f.expect(200, f.call(owner, "DELETE", base+"/entry?path=renamed", nil))
	for key := range store.objects {
		if strings.HasPrefix(key, "files/renamed") || strings.HasPrefix(key, "files/new") {
			t.Fatal("left behind", key)
		}
	}
	if _, found := store.has("other/outside.txt"); !found {
		t.Fatal("touched outside the prefix")
	}
	f.expect(404, f.call(owner, "DELETE", base+"/entry?path=nothing-here", nil))
}
