package imports

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/NeoLabs-Systems/neolabs-go/apperr"
)

// Where an import reads from: validation of the connection fields and the
// WebDAV folder they lead to.

const (
	maxFieldLength = 1024
	nextcloud      = "nextcloud"
	plainWebDAV    = "webdav"
)

type connection struct {
	source, url, username, password, sourcePath string
}

func invalid(message, code string) error {
	return apperr.New(http.StatusBadRequest, message, code)
}

func requiredText(value any, label, code string) (string, error) {
	typed, _ := value.(string)
	if strings.TrimSpace(typed) == "" || utf8.RuneCountInString(typed) > maxFieldLength {
		return "", invalid(label+" is required.", code)
	}
	return typed, nil
}

// normalizeSourcePath turns "/Photos//2026/" into "/Photos/2026" and nothing
// into "/".
func normalizeSourcePath(raw any) (string, error) {
	value, ok := raw.(string)
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' })
	if raw != nil && !ok || len(value) > maxPathLength || slices.Contains(parts, ".") || slices.Contains(parts, "..") {
		return "", invalid("sourcePath must be a folder path.", "INVALID_PATH")
	}
	return "/" + strings.Join(parts, "/"), nil
}

var phpPath = regexp.MustCompile(`/(?:index|remote)\.php(?:/|$)`)

func normalizeURL(raw any, source string) (string, error) {
	value, err := requiredText(raw, "The server address", "INVALID_URL")
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" {
		return "", invalid("Enter the server address, like https://cloud.example.com.", "INVALID_URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", invalid("The server address must start with https:// or http://.", "INVALID_URL")
	}
	if parsed.User != nil {
		return "", invalid("Put the user name and password in their own fields.", "INVALID_URL")
	}
	if parsed.Hostname() == "" {
		return "", invalid("Enter the server address, like https://cloud.example.com.", "INVALID_URL")
	}
	path := parsed.EscapedPath()
	if source == nextcloud {
		// People paste what their browser shows: …/index.php/apps/files or a DAV link.
		if cut := phpPath.FindStringIndex(path); cut != nil {
			path = path[:cut[0]]
		}
		path = strings.TrimRight(path, "/")
	} else if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return parsed.Scheme + "://" + parsed.Host + path, nil
}

// parseConnection validates the connection part of a request body.
func parseConnection(body map[string]any) (connection, error) {
	c := connection{source: nextcloud}
	var err error
	if given := body["source"]; given != nil && given != "" {
		c.source, _ = given.(string)
	}
	if c.source != nextcloud && c.source != plainWebDAV {
		return c, invalid("source must be nextcloud or webdav.", "INVALID_SOURCE")
	}
	if c.url, err = normalizeURL(body["url"], c.source); err != nil {
		return c, err
	}
	if c.username, err = requiredText(body["username"], "The user name", "CREDENTIALS_REQUIRED"); err != nil {
		return c, err
	}
	c.username = strings.TrimSpace(c.username)
	if c.password, err = requiredText(body["password"], "The app password", "CREDENTIALS_REQUIRED"); err != nil {
		return c, err
	}
	c.sourcePath, err = normalizeSourcePath(body["sourcePath"])
	return c, err
}

// roots are the account's WebDAV folder and the one tried when it is missing.
// The legacy /remote.php/webdav/ (always the signed-in account) covers
// Nextcloud logins that are not the user id.
func (c connection) roots() (string, string) {
	if c.source != nextcloud {
		return c.url, ""
	}
	return c.url + "/remote.php/dav/files/" + url.PathEscape(c.username) + "/", c.url + "/remote.php/webdav/"
}
