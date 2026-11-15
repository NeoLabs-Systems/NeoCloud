package handoff

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

const (
	downloadAttempts = 6
	partSuffix       = ".ncpart"
)

// contentSource opens a file of an accepted offer from an offset on.
type contentSource func(ctx context.Context, offerID string, fileID, offset int64) (*http.Response, error)

// fetchFile downloads one file into final (a path Reserve made, empty until
// now). The bytes collect in final+".ncpart" and move onto final only when
// exactly size bytes arrived, so a broken download never looks finished. An
// interrupted transfer continues from the part with a Range request; a
// server that ignores the range makes it start over.
func fetchFile(ctx context.Context, open contentSource, offerID string, file api.HandoffFile, final string, progress func(done int64), wait func(time.Duration)) error {
	part := final + partSuffix
	var lastErr error
	for attempt := 0; attempt < downloadAttempts; attempt++ {
		if attempt > 0 {
			wait(time.Duration(1<<attempt) * time.Second)
		}
		if ctx.Err() != nil {
			os.Remove(part)
			return ctx.Err()
		}
		err := fetchOnce(ctx, open, offerID, file, part, progress)
		if err == nil {
			return commit(part, final, file.Size)
		}
		lastErr = err
		var apiErr *api.Error
		if errors.As(err, &apiErr) {
			os.Remove(part)
			return err // the server said no (gone, expired): trying again won't help
		}
	}
	os.Remove(part)
	return lastErr
}

func fetchOnce(ctx context.Context, open contentSource, offerID string, file api.HandoffFile, part string, progress func(int64)) error {
	var offset int64
	if info, err := os.Stat(part); err == nil {
		offset = info.Size()
	}
	if offset > file.Size {
		offset = 0
	}
	if offset == file.Size && file.Size > 0 {
		return nil
	}
	res, err := open(ctx, offerID, file.ID, offset)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	flags := os.O_WRONLY | os.O_CREATE
	if offset > 0 && res.StatusCode == http.StatusPartialContent &&
		strings.HasPrefix(res.Header.Get("Content-Range"), "bytes "+strconv.FormatInt(offset, 10)+"-") {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		offset = 0
	}
	out, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	buf := make([]byte, 256<<10)
	for {
		n, readErr := res.Body.Read(buf)
		if n > 0 {
			// Never accept more than the declared size.
			if offset+int64(n) > file.Size {
				return fmt.Errorf("%s is larger than announced", file.Name)
			}
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
			offset += int64(n)
			progress(offset)
		}
		if readErr == io.EOF {
			return out.Sync()
		}
		if readErr != nil {
			return readErr
		}
	}
}

// commit moves a finished part onto final after checking its size.
func commit(part, final string, want int64) error {
	info, err := os.Stat(part)
	if want == 0 && errors.Is(err, os.ErrNotExist) {
		return nil // an empty file: the reserved file is already it
	}
	if err != nil {
		return err
	}
	if info.Size() != want {
		os.Remove(part)
		return fmt.Errorf("received %d of %d bytes", info.Size(), want)
	}
	return os.Rename(part, final)
}
