package handoff

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

// serve answers like the server: whole body, or from a Range offset; the
// first `cut` bodies are cut short after `after` bytes (a dropped link).
func serve(content string, cut, after int) contentSource {
	calls := 0
	return func(_ context.Context, _ string, _, offset int64) (*http.Response, error) {
		calls++
		body := content[offset:]
		status := http.StatusOK
		header := http.Header{}
		if offset > 0 {
			status = http.StatusPartialContent
			header.Set("Content-Range", "bytes "+strconv.FormatInt(offset, 10)+"-")
		}
		var reader io.Reader = strings.NewReader(body)
		if calls <= cut {
			reader = io.MultiReader(strings.NewReader(body[:min(after, len(body))]), errReader{})
		}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(reader)}, nil
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func noWait(time.Duration) {}

func setup(t *testing.T) string {
	t.Helper()
	final, err := Reserve(t.TempDir(), "got.bin")
	if err != nil {
		t.Fatal(err)
	}
	return final
}

func TestFetchResumesAfterADroppedLink(t *testing.T) {
	final := setup(t)
	content := strings.Repeat("0123456789", 100)
	file := api.HandoffFile{ID: 1, Name: "got.bin", Size: int64(len(content))}
	if err := fetchFile(context.Background(), serve(content, 2, 300), "o", file, final, func(int64) {}, noWait); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(final)
	if string(got) != content {
		t.Errorf("content differs (%d bytes)", len(got))
	}
	if _, err := os.Stat(final + partSuffix); !os.IsNotExist(err) {
		t.Error("the part file was left behind")
	}
}

func TestFetchRejectsWrongSizes(t *testing.T) {
	final := setup(t)
	file := api.HandoffFile{ID: 1, Name: "got.bin", Size: 10}
	if err := fetchFile(context.Background(), serve("short", 0, 0), "o", file, final, func(int64) {}, noWait); err == nil {
		t.Error("a short file was accepted")
	}
	if _, err := os.Stat(final + partSuffix); !os.IsNotExist(err) {
		t.Error("the part file was left behind")
	}
	file.Size = 3
	if err := fetchFile(context.Background(), serve("too long", 0, 0), "o", file, final, func(int64) {}, noWait); err == nil {
		t.Error("an oversized file was accepted")
	}
}

func TestFetchGivesUpOnServerRefusalWithoutRetrying(t *testing.T) {
	final := setup(t)
	calls := 0
	open := func(context.Context, string, int64, int64) (*http.Response, error) {
		calls++
		return nil, &api.Error{Status: 410, Code: "OFFER_EXPIRED"}
	}
	err := fetchFile(context.Background(), open, "o", api.HandoffFile{ID: 1, Size: 5}, final, func(int64) {}, noWait)
	if !api.HasCode(err, "OFFER_EXPIRED") || calls != 1 {
		t.Errorf("err %v after %d calls", err, calls)
	}
}

func TestFetchEmptyFile(t *testing.T) {
	final := setup(t)
	if err := fetchFile(context.Background(), serve("", 0, 0), "o", api.HandoffFile{ID: 1, Name: "e", Size: 0}, final, func(int64) {}, noWait); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(final); err != nil || info.Size() != 0 {
		t.Error("the empty file is missing")
	}
}

func TestFetchStopsWhenCancelled(t *testing.T) {
	final := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := fetchFile(ctx, serve("abc", 0, 0), "o", api.HandoffFile{ID: 1, Size: 3}, final, func(int64) {}, noWait)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(final), "got.bin"+partSuffix)); !os.IsNotExist(err) {
		t.Error("part left behind")
	}
}

func TestReadEventsParsesStream(t *testing.T) {
	stream := "retry: 3000\n: connected\n\nevent: handoff:offer\ndata: {\"id\":\"a\"}\n\n: ping\n\nevent: handoff:devices\ndata: {}\n\nevent: broken\n\n"
	var got []string
	if err := readEvents(strings.NewReader(stream), func(e Event) { got = append(got, e.Name+"="+string(e.Data)) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != `handoff:offer={"id":"a"}` || got[1] != "handoff:devices={}" {
		t.Errorf("got %v", got)
	}
}
