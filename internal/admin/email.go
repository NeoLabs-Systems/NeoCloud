package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/runtimecfg"
)

// The mail settings are read from the environment on every request, with the
// install's env file taking precedence, and written back to that file. The SMTP
// password is only ever written.

const (
	emailPortKey     = "NEOCLOUD_EMAIL_SMTP_PORT"
	emailTTLKey      = "NEOCLOUD_EMAIL_TOKEN_TTL_HOURS"
	emailPasswordKey = "NEOCLOUD_EMAIL_SMTP_PASS"
	emailValueLimit  = 300
)

var (
	emailHostPattern    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$|^\[[0-9A-Fa-f:.]+\]$`)
	emailMailboxAddress = regexp.MustCompile(`^[^\s@<>,;"()]+@[^\s@<>,;"()]+\.[^\s@<>,;"()]+$`)
	emailMailboxAngle   = regexp.MustCompile(`<([^<>]+)>\s*$`)
)

type emailStringField struct{ name, key, label string }

var emailStringFields = []emailStringField{
	{"from", "NEOCLOUD_EMAIL_FROM", "Sender address"},
	{"smtpHost", "NEOCLOUD_EMAIL_SMTP_HOST", "SMTP host"},
	{"smtpUser", "NEOCLOUD_EMAIL_SMTP_USER", "SMTP username"},
	{"replyTo", "NEOCLOUD_EMAIL_REPLY_TO", "Reply-to address"},
	{"brandName", "NEOCLOUD_EMAIL_BRAND_NAME", "Brand name"},
	{"supportUrl", "NEOCLOUD_EMAIL_SUPPORT_URL", "Support URL"},
	{"publicUrl", "NEOCLOUD_EMAIL_PUBLIC_URL", "Public URL"},
}

// The two TLS defaults depend on the port; see readEmailSettings.
var emailBoolFields = []struct{ name, key string }{
	{"smtpSecure", "NEOCLOUD_EMAIL_SMTP_SECURE"},
	{"smtpRequireTls", "NEOCLOUD_EMAIL_SMTP_REQUIRE_TLS"},
	{"smtpRejectUnauthorized", "NEOCLOUD_EMAIL_SMTP_REJECT_UNAUTHORIZED"},
	{"requireSignupConfirmation", "NEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION"},
	{"requireEmailChangeConfirmation", "NEOCLOUD_EMAIL_REQUIRE_EMAIL_CHANGE_CONFIRMATION"},
	{"notifyNewLogin", "NEOCLOUD_EMAIL_NOTIFY_NEW_LOGIN"},
	{"notifyAccountChanges", "NEOCLOUD_EMAIL_NOTIFY_ACCOUNT_CHANGES"},
	{"signingMails", "NEOCLOUD_EMAIL_SIGNING_MAILS"},
}

type emailSettingsView struct {
	Configured bool           `json:"configured"`
	Missing    []string       `json:"missing"`
	Settings   map[string]any `json:"settings"`
}

func (h *Handler) readEmailSettings() (emailSettingsView, error) {
	file, err := config.ReadEnv(h.envFile)
	if err != nil {
		return emailSettingsView{}, err
	}
	get := func(key string) string {
		if value, ok := file[key]; ok {
			return strings.TrimSpace(value)
		}
		return strings.TrimSpace(os.Getenv(key))
	}
	positive := func(key string, fallback int) int {
		if value, err := strconv.Atoi(get(key)); err == nil && value > 0 {
			return value
		}
		return fallback
	}
	port := positive(emailPortKey, 587)
	settings := map[string]any{
		"smtpPort": port, "tokenTtlHours": positive(emailTTLKey, 24),
		"smtpPasswordConfigured": get(emailPasswordKey) != "",
	}
	for _, field := range emailStringFields {
		settings[field.name] = get(field.key)
	}
	secure := runtimecfg.Bool(get("NEOCLOUD_EMAIL_SMTP_SECURE"), port == 465)
	for _, field := range emailBoolFields {
		fallback := true
		switch field.name {
		case "smtpSecure":
			fallback = secure
		case "smtpRequireTls":
			fallback = !secure
		}
		settings[field.name] = runtimecfg.Bool(get(field.key), fallback)
	}
	missing := []string{}
	if settings["from"] == "" {
		missing = append(missing, "Sender address")
	}
	if settings["smtpHost"] == "" {
		missing = append(missing, "SMTP host")
	}
	return emailSettingsView{Configured: len(missing) == 0, Missing: missing, Settings: settings}, nil
}

func (h *Handler) getEmailConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	view, err := h.readEmailSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read email settings.", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

// updateEmailConfig writes only what the body names, and nothing unless all of
// it validates.
func (h *Handler) updateEmailConfig(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.authorize(w, r)
	if !ok {
		return
	}
	body, ok := decodeObject(w, r)
	if !ok {
		return
	}
	values, err := emailUpdates(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_VALUE")
		return
	}
	for _, value := range values {
		if _, err := encodeEnvValue(value); err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "INVALID_VALUE")
			return
		}
	}
	h.configMu.Lock()
	defer h.configMu.Unlock()
	if err := h.persistEmail(values); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update email settings.", "INTERNAL_ERROR")
		return
	}
	view, err := h.readEmailSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read email settings.", "INTERNAL_ERROR")
		return
	}
	if h.onEmailChanged != nil {
		h.onEmailChanged()
	}
	detail, _ := json.Marshal(map[string]bool{"configured": view.Configured})
	if _, err := h.db.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,detail_json) VALUES('email.config',?,?)`, actor, string(detail)); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not record the email settings change.", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

// persistEmail writes text values quoted, so the env file reads back exactly
// what was typed.
func (h *Handler) persistEmail(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	encoded := make(map[string]string, len(values))
	for key, value := range values {
		quoted, err := encodeEnvValue(value)
		if err != nil {
			return err
		}
		encoded[key] = quoted
	}
	if err := config.UpdateEnv(h.envFile, encoded); err != nil {
		return err
	}
	for key, value := range values {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

var plainEnvValue = regexp.MustCompile(`^(true|false|[0-9]+)$`)

// encodeEnvValue picks a quote the value does not contain: the env file loader
// does not unescape quotes or backslashes.
func encodeEnvValue(value string) (string, error) {
	switch {
	case plainEnvValue.MatchString(value):
		return value, nil
	case !strings.ContainsAny(value, `"\`):
		return `"` + value + `"`, nil
	case !strings.Contains(value, "'"):
		return "'" + value + "'", nil
	}
	return "", errors.New("The value may not contain both single and double quotes.")
}

// emailUpdates is the env file values an update body asks for.
func emailUpdates(body map[string]any) (map[string]string, error) {
	updates := map[string]string{}
	for _, field := range emailStringFields {
		raw, exists := body[field.name]
		if !exists {
			continue
		}
		value, err := emailString(field, raw)
		if err != nil {
			return nil, err
		}
		updates[field.key] = value
	}
	for _, number := range []struct {
		name, key, label string
		max              int
	}{{"smtpPort", emailPortKey, "SMTP port", 65535}, {"tokenTtlHours", emailTTLKey, "Token lifetime", 8760}} {
		raw, exists := body[number.name]
		if !exists {
			continue
		}
		value, ok := emailInteger(raw, number.max)
		if !ok {
			return nil, fmt.Errorf("%s must be an integer from 1 to %d.", number.label, number.max)
		}
		updates[number.key] = strconv.Itoa(value)
	}
	for _, field := range emailBoolFields {
		raw, exists := body[field.name]
		if !exists {
			continue
		}
		value, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("%s must be true or false.", field.name)
		}
		updates[field.key] = strconv.FormatBool(value)
	}
	password := ""
	if raw, exists := body["smtpPassword"]; exists {
		var err error
		if password, err = emailLine(raw, "SMTP password"); err != nil {
			return nil, err
		}
	}
	if body["clearSmtpPassword"] == true {
		updates[emailPasswordKey] = ""
	} else if password != "" {
		updates[emailPasswordKey] = password
	}
	return updates, nil
}

func emailLine(raw any, label string) (string, error) {
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be text.", label)
	}
	if strings.IndexFunc(value, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return "", fmt.Errorf("%s must be a single line.", label)
	}
	value = strings.TrimSpace(value)
	if len([]rune(value)) > emailValueLimit {
		return "", fmt.Errorf("%s is too long.", label)
	}
	return value, nil
}

func emailString(field emailStringField, raw any) (string, error) {
	value, err := emailLine(raw, field.label)
	if err != nil || value == "" {
		return value, err
	}
	switch field.name {
	case "from", "replyTo":
		// "Name <a@b.example>" or "a@b.example".
		inner := value
		if match := emailMailboxAngle.FindStringSubmatch(value); match != nil {
			inner = match[1]
		}
		if !emailMailboxAddress.MatchString(strings.TrimSpace(inner)) || strings.ContainsAny(emailMailboxAngle.ReplaceAllString(value, ""), "<>") {
			return "", fmt.Errorf("%s must be an email address, optionally as \"Name <address>\".", field.label)
		}
	case "smtpHost":
		if !emailHostPattern.MatchString(value) {
			return "", fmt.Errorf("%s must be a host name or address.", field.label)
		}
	case "supportUrl", "publicUrl":
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", errors.New(field.label + " must be an HTTP or HTTPS URL.")
		}
		return strings.TrimRight(value, "/"), nil
	}
	return value, nil
}

func emailInteger(raw any, max int) (int, bool) {
	text := ""
	switch value := raw.(type) {
	case json.Number:
		text = value.String()
	case string:
		text = strings.TrimSpace(value)
	default:
		return 0, false
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || number != float64(int(number)) || number < 1 || number > float64(max) {
		return 0, false
	}
	return int(number), true
}
