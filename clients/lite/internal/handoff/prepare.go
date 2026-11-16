package handoff

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The server's limits for one send (docs/handoff.md). The server enforces
// them too; checking first spares the person an upload that cannot work.
const (
	MaxFiles = 50
	MaxBytes = int64(2) << 30
)

// Item is one local file to send.
type Item struct {
	Path string
	Name string
	Size int64
	// Temporary files (a drop staged by the app window) are deleted once
	// the send no longer needs them.
	Temporary bool
}

// UserError is a problem that is the person's to fix. Its text is a whole
// sentence meant to be shown as it is.
type UserError struct{ Message string }

func (e *UserError) Error() string { return e.Message }

func userErrorf(format string, args ...any) error {
	return &UserError{Message: fmt.Sprintf(format, args...)}
}

// Prepare checks the paths a person chose and returns what to send. Only
// plain, readable files can be sent: a folder (or anything else) makes the
// whole send refuse, so nothing goes out that the person did not expect.
// The names are the files' own; the server sanitises them for the receiver.
func Prepare(paths []string) ([]Item, error) {
	var items []Item
	var folders []string
	seen := map[string]bool{}
	var total int64
	for _, raw := range paths {
		path := filepath.Clean(strings.TrimSpace(raw))
		if raw == "" || path == "." || seen[path] {
			continue
		}
		seen[path] = true
		info, err := os.Stat(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			return nil, userErrorf("%s no longer exists.", filepath.Base(path))
		case err != nil:
			return nil, userErrorf("%s can't be read: %s", filepath.Base(path), reason(err))
		case info.IsDir():
			folders = append(folders, filepath.Base(path))
			continue
		case !info.Mode().IsRegular():
			return nil, userErrorf("%s is not a regular file, so it can't be sent.", filepath.Base(path))
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, userErrorf("%s can't be read: %s", filepath.Base(path), reason(err))
		}
		file.Close()
		items = append(items, Item{Path: path, Name: filepath.Base(path), Size: info.Size()})
		total += info.Size()
	}
	switch {
	case len(folders) > 0:
		return nil, userErrorf("%s %s a folder. Only files can be sent; put a folder in a zip file first.",
			strings.Join(folders[:min(len(folders), maxListedNames)], ", "), isOrAre(len(folders)))
	case len(items) == 0:
		return nil, userErrorf("Choose at least one file.")
	case len(items) > MaxFiles:
		return nil, userErrorf("You can send up to %d files at once; %d were chosen.", MaxFiles, len(items))
	case total > MaxBytes:
		return nil, userErrorf("That is %s. You can send up to %s at once.", humanSize(total), humanSize(MaxBytes))
	}
	return items, nil
}

func isOrAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

// reason is a short, plain cause for a file error.
func reason(err error) string {
	switch {
	case errors.Is(err, os.ErrPermission):
		return "you don't have permission to open it."
	case errors.Is(err, os.ErrNotExist):
		return "it no longer exists."
	}
	return "it could not be opened."
}

// Describe is "report.pdf" or "3 files".
func Describe(items []Item) string {
	if len(items) == 1 {
		return SafeName(items[0].Name)
	}
	return fmt.Sprintf("%d files", len(items))
}

// TotalSize is the bytes of all items.
func TotalSize(items []Item) (total int64) {
	for _, item := range items {
		total += item.Size
	}
	return total
}
