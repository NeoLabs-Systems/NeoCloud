package externalstorage

import (
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/NeoLabs-Systems/neolabs-go/apperr"
)

// A connection's settings are validated against its provider's field list and
// shown to the client without any secret.

type kind int

const (
	text kind = iota
	number
	boolean
)

type field struct {
	name     string
	kind     kind
	required bool
	secret   bool
	// internal fields are set by the server, never by the client.
	internal bool
	max      int
}

type provider struct {
	fields   []field
	host     func(settings) string
	validate func(settings) error
}

// settings is the decrypted JSON document of a connection.
type settings map[string]any

func (s settings) text(name string) string { value, _ := s[name].(string); return value }
func (s settings) flag(name string) bool   { value, _ := s[name].(bool); return value }

var bucketName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.\-_]{1,254}$`)

var providers = map[string]provider{
	"s3": {
		fields: []field{
			{name: "endpoint", max: 512},
			{name: "region", max: 64},
			{name: "bucket", required: true, max: 255},
			{name: "prefix", max: 512},
			{name: "accessKeyId", required: true, max: 256},
			{name: "secretAccessKey", required: true, secret: true, max: 512},
			{name: "forcePathStyle", kind: boolean},
		},
		host: func(s settings) string { return hostName(s.text("endpoint")) },
		validate: func(s settings) error {
			if endpoint := s.text("endpoint"); endpoint != "" {
				if err := checkHTTPURL(endpoint, "The endpoint"); err != nil {
					return err
				}
			}
			if !bucketName.MatchString(s.text("bucket")) {
				return invalidConfig("That is not a valid bucket name.")
			}
			return nil
		},
	},
	"webdav": {
		fields: []field{
			{name: "url", required: true, max: 1024},
			{name: "username", required: true, max: 256},
			{name: "password", required: true, secret: true, max: 1024},
		},
		host:     func(s settings) string { return hostName(s.text("url")) },
		validate: func(s settings) error { return checkHTTPURL(s.text("url"), "The address") },
	},
	"sftp": {
		fields: []field{
			{name: "host", required: true, max: 255},
			{name: "port", kind: number},
			{name: "username", required: true, max: 256},
			{name: "password", secret: true, max: 1024},
			{name: "privateKey", secret: true, max: 16384},
			{name: "passphrase", secret: true, max: 1024},
			{name: "path", max: 1024},
			{name: "hostKey", internal: true, max: 128},
		},
		host: func(s settings) string { return s.text("host") },
		validate: func(s settings) error {
			if s.text("password") == "" && s.text("privateKey") == "" {
				return invalidConfig("Enter a password or a private key.")
			}
			if port, ok := s["port"].(float64); ok && (port != math.Trunc(port) || port < 1 || port > 65535) {
				return invalidConfig("The port must be a number between 1 and 65535.")
			}
			folder := s.text("path")
			if strings.IndexFunc(folder, unicode.IsControl) >= 0 || slices.Contains(strings.Split(folder, "/"), "..") {
				return invalidConfig("That folder is not valid.")
			}
			return nil
		},
	},
}

func invalidConfig(message string) error {
	return apperr.New(http.StatusBadRequest, message, "EXTERNAL_CONFIG_INVALID")
}

func providerFor(name string) (provider, error) {
	found, ok := providers[name]
	if !ok {
		return provider{}, apperr.New(http.StatusBadRequest, "provider must be one of s3, webdav, sftp.", "EXTERNAL_PROVIDER_INVALID")
	}
	return found, nil
}

func hostName(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// checkHTTPURL accepts an http(s) address without credentials in it (those have
// their own fields).
func checkHTTPURL(value, label string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return invalidConfig(label + " must be a full address like https://example.com.")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return invalidConfig(label + " must start with https:// or http://.")
	}
	if parsed.User != nil {
		return invalidConfig("Put the user name and password in their own fields.")
	}
	if parsed.Hostname() == "" {
		return invalidConfig(label + " must be a full address like https://example.com.")
	}
	return nil
}

func cleanValue(f field, value any) (any, error) {
	switch f.kind {
	case boolean:
		return value == true || value == "true", nil
	case number:
		switch typed := value.(type) {
		case float64:
			return typed, nil
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
			if err == nil && !math.IsInf(parsed, 0) && !math.IsNaN(parsed) {
				return parsed, nil
			}
		}
		return nil, invalidConfig(f.name + " must be a number.")
	}
	typed, ok := value.(string)
	if !ok {
		return nil, invalidConfig(f.name + " must be text.")
	}
	if !f.secret {
		typed = strings.TrimSpace(typed)
	}
	if utf8.RuneCountInString(typed) > f.max {
		return nil, invalidConfig(f.name + " is too long.")
	}
	return typed, nil
}

// cleanConfig builds the settings to store; previous supplies the secrets (and
// the host key) left blank.
func cleanConfig(providerName string, incoming any, previous settings) (settings, error) {
	spec, err := providerFor(providerName)
	if err != nil {
		return nil, err
	}
	given, ok := incoming.(map[string]any)
	if !ok && incoming != nil {
		return nil, invalidConfig("config must be an object.")
	}
	config := settings{}
	for _, f := range spec.fields {
		var value any
		present := false
		if !f.internal {
			value, present = given[f.name]
		}
		if f.secret && (!present || value == "") {
			value = previous[f.name]
		}
		if value == nil || value == "" {
			continue
		}
		if value, err = cleanValue(f, value); err != nil {
			return nil, err
		}
		if value != "" {
			config[f.name] = value
		}
	}
	for _, f := range spec.fields {
		if f.required && config[f.name] == nil {
			return nil, invalidConfig(f.name + " is required.")
		}
	}
	// A server whose address changed has a different identity.
	if key := previous.text("hostKey"); key != "" && spec.host(config) == spec.host(previous) && config["port"] == previous["port"] {
		config["hostKey"] = key
	}
	return config, spec.validate(config)
}

// publicConfig is what the client may see: secrets show only whether they are set.
func publicConfig(providerName string, config settings) map[string]any {
	out := map[string]any{}
	for _, f := range providers[providerName].fields {
		switch {
		case f.internal:
		case f.secret:
			out[f.name+"Set"] = config.text(f.name) != ""
		default:
			if value, ok := config[f.name]; ok {
				out[f.name] = value
			}
		}
	}
	return out
}
