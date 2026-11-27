package externalstorage

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"golang.org/x/text/unicode/norm"
)

// Paths inside a mount are relative to its root, "/"-separated, without leading
// or trailing slash, "" for the root. Every path a client sends goes through
// normalizePath before anything touches a provider.

const (
	maxPathLength = 2048
	maxPathDepth  = 32
)

func invalidPath() error {
	return apperr.New(http.StatusBadRequest, "That path is not valid.", "INVALID_PATH")
}

func pathError(message string) error {
	return apperr.New(http.StatusBadRequest, message, "INVALID_PATH")
}

// normalizePath turns "a//b/" into "a/b". It refuses ".", "..", control
// characters, backslashes and anything that is not text.
func normalizePath(raw any) (string, error) {
	if raw == nil || raw == "" {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok || len(value) > maxPathLength || !utf8.ValidString(value) ||
		strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }) >= 0 {
		return "", invalidPath()
	}
	segments := strings.FieldsFunc(norm.NFC.String(value), func(r rune) bool { return r == '/' })
	if len(segments) > maxPathDepth {
		return "", invalidPath()
	}
	for _, segment := range segments {
		if segment == "." || segment == ".." || len(segment) > 255 {
			return "", invalidPath()
		}
	}
	return strings.Join(segments, "/"), nil
}

func joinPath(folder, name string) (string, error) {
	clean, err := files.ValidateName(name)
	if err != nil || folder == "" {
		return clean, err
	}
	return folder + "/" + clean, nil
}

func parentOf(path string) string {
	return path[:max(strings.LastIndexByte(path, '/'), 0)]
}

func baseName(path string) string { return path[strings.LastIndexByte(path, '/')+1:] }

// isInside reports whether path is folder or lies below it.
func isInside(folder, path string) bool {
	return path == folder || strings.HasPrefix(path, folder+"/")
}

// safeListedName accepts a name a provider listed: one segment that can't climb.
func safeListedName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00")
}

var numberSuffix = regexp.MustCompile(` \(\d+\)$`)

// numberedName turns "photo.jpg" into "photo (1).jpg".
func numberedName(name string, number int) string {
	stem, extension := name, ""
	if dot := strings.LastIndexByte(name, '.'); dot > 0 {
		stem, extension = name[:dot], name[dot:]
	}
	return fmt.Sprintf("%s (%d)%s", numberSuffix.ReplaceAllString(stem, ""), number, extension)
}
