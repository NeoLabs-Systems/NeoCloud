// Package accountmail adapts NeoCloud's account-mail wording to the shared
// SMTP sender and HTML/text renderer.
package accountmail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/servicemail"
)

type Service struct {
	state *serviceState
}

type serviceState struct {
	mu                sync.RWMutex
	config            servicemail.Config
	sender            servicemail.Mailer
	usesDefaultSender bool
	signingMailsOff   bool
}

type Status struct {
	Enabled                         bool `json:"enabled"`
	Configured                      bool `json:"configured"`
	SignupConfirmationRequired      bool `json:"signupConfirmationRequired"`
	EmailChangeConfirmationRequired bool `json:"emailChangeConfirmationRequired"`
}

var mailSecretPattern = regexp.MustCompile(`(?i)(password|passwd|pass|secret|token|authorization)=([^&\s]+)`)
var mailURLCredentialsPattern = regexp.MustCompile(`(://)[^/@\s]+@`)

func New(config servicemail.Config) Service {
	return Service{state: &serviceState{config: config, sender: servicemail.Sender{Cfg: config}, usesDefaultSender: true}}
}

func WithSender(config servicemail.Config, sender servicemail.Mailer) Service {
	return Service{state: &serviceState{config: config, sender: sender}}
}

// SetConfig replaces the live mail configuration. Services created with New
// also replace their SMTP sender; custom senders supplied with WithSender stay
// intact. Copies of Service share this update safely.
func (s Service) SetConfig(config servicemail.Config) {
	if s.state == nil {
		return
	}
	s.state.mu.Lock()
	s.state.config = config
	if s.state.usesDefaultSender {
		s.state.sender = servicemail.Sender{Cfg: config}
	}
	s.state.mu.Unlock()
}

func (s Service) current() (servicemail.Config, servicemail.Mailer) {
	if s.state == nil {
		return servicemail.Config{}, nil
	}
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()
	return s.state.config, s.state.sender
}

func (s Service) Status() Status {
	config, _ := s.current()
	return Status{
		Enabled: config.Enabled, Configured: config.Enabled,
		SignupConfirmationRequired:      config.RequireSignupConfirmation,
		EmailChangeConfirmationRequired: config.RequireChangeConfirmation,
	}
}

// SetSigningMails is the admin's switch for mail to people who sign.
func (s Service) SetSigningMails(on bool) {
	if s.state == nil {
		return
	}
	s.state.mu.Lock()
	s.state.signingMailsOff = !on
	s.state.mu.Unlock()
}

// SigningMailEnabled reports that mail is configured and signing mail is on.
func (s Service) SigningMailEnabled() bool {
	if s.state == nil {
		return false
	}
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()
	return s.state.config.Enabled && !s.state.signingMailsOff
}

// Config is the live service mail setup.
func (s Service) Config() servicemail.Config {
	config, _ := s.current()
	return config
}

// PublicURL returns the configured public origin without exposing SMTP secrets.
func (s Service) PublicURL() string {
	config, _ := s.current()
	return strings.TrimRight(config.PublicURL, "/")
}

// BrandName returns the current shared mail brand.
func (s Service) BrandName() string { return s.brand() }

// SendMessage sends a precomposed message through the shared configured mailer.
func (s Service) SendMessage(ctx context.Context, recipient string, message servicemail.Message) error {
	config, sender := s.current()
	if !config.Enabled || sender == nil {
		return errors.New("accountmail: mail delivery is disabled")
	}
	if strings.TrimSpace(recipient) == "" {
		return errors.New("accountmail: recipient is empty")
	}
	if message.BrandName == "" {
		message.BrandName = s.brand()
	}
	return sender.Send(ctx, recipient, message)
}

func (s Service) SignupConfirmationRequired() bool {
	config, _ := s.current()
	return config.RequireSignupConfirmation
}

func (s Service) PendingSignupMessage(language string) string {
	if isGerman(language) {
		return fmt.Sprintf("Prüfe dein Postfach, um dein %s-Konto zu bestätigen.", s.brand())
	}
	return fmt.Sprintf("Check your email to confirm your %s account.", s.brand())
}

func (s Service) SendSignupConfirmation(ctx context.Context, username, email, token, language string) error {
	return s.sendTokenMail(ctx, username, email, token, language, "confirm")
}

func (s Service) SendPasswordReset(ctx context.Context, username, email, token, language string) error {
	return s.sendTokenMail(ctx, username, email, token, language, "reset")
}

func (s Service) SendEmailConfirmation(ctx context.Context, username, email, token, language string, changing bool) error {
	config, sender := s.current()
	base, err := s.confirmationURL(token)
	if err != nil {
		return err
	}
	lang := "en"
	if isGerman(language) {
		lang = "de"
	}
	brand := s.brand()
	hours := config.TokenTTLHours
	if hours <= 0 {
		hours = 24
	}
	expires := durationText(lang, hours)
	var message servicemail.Message
	if changing {
		if lang == "de" {
			message = servicemail.Message{Subject: "Bestätige deine neue " + brand + "-E-Mail", Title: "Neue E-Mail bestätigen", Intro: "Bestätige diese Adresse, bevor " + brand + " die E-Mail-Adresse von " + username + " ändert.", ActionURL: base, ActionLabel: "Neue E-Mail bestätigen", BrandName: brand, Details: []servicemail.Detail{{Label: "Konto", Value: username}, {Label: "Neue E-Mail", Value: email}, {Label: "Gültig bis", Value: expires}}}
		} else {
			message = servicemail.Message{Subject: "Confirm your new " + brand + " email", Title: "Confirm your new email", Intro: "Confirm this address before " + brand + " changes the account email for " + username + ".", ActionURL: base, ActionLabel: "Confirm new email", BrandName: brand, Details: []servicemail.Detail{{Label: "Account", Value: username}, {Label: "New email", Value: email}, {Label: "Expires", Value: expires}}}
		}
	} else {
		message = mailText(lang, brand, username, base, expires)
	}
	return sender.Send(ctx, email, message)
}

// SendTestEmail sends a localized SMTP test and returns the normalized
// recipient. Transport errors are reduced to one line and scrubbed of the
// configured password, URL credentials, and common secret-bearing fields.
func (s Service) SendTestEmail(ctx context.Context, recipient, language string) (string, error) {
	config, sender := s.current()
	if !config.Enabled || sender == nil {
		return "", servicemail.ErrNotConfigured
	}
	address := strings.ToLower(strings.TrimSpace(recipient))
	parsed, err := mail.ParseAddress(address)
	if err != nil || parsed.Address != address || len(address) > 254 || !strings.Contains(address, "@") {
		return "", errors.New("accountmail: a valid email address is required")
	}
	brand := s.brand()
	message := servicemail.Message{BrandName: brand}
	account := "Account"
	if isGerman(language) {
		message.Subject = brand + ": Test-E-Mail"
		message.Title = "E-Mail funktioniert"
		message.Intro = "Das ist eine Testnachricht von " + brand + ". Deine SMTP-Einstellungen funktionieren."
		account = "Konto"
	} else {
		message.Subject = brand + " test email"
		message.Title = "Email works"
		message.Intro = "This is a test message from " + brand + ". Your SMTP settings are working."
	}
	message.Details = []servicemail.Detail{{Label: account, Value: address}, {Label: "SMTP", Value: fmt.Sprintf("%s:%d", config.Host, config.Port)}}
	if err = sender.Send(ctx, address, message); err != nil {
		return "", errors.New(sanitizeMailError(err, config.Pass))
	}
	return address, nil
}

func sanitizeMailError(err error, password string) string {
	text := strings.Join(strings.Fields(err.Error()), " ")
	if password != "" {
		text = strings.ReplaceAll(text, password, "[hidden]")
	}
	text = mailURLCredentialsPattern.ReplaceAllString(text, `${1}[hidden]@`)
	text = mailSecretPattern.ReplaceAllString(text, `${1}=[hidden]`)
	if len(text) > 500 {
		text = text[:500]
	}
	if text == "" {
		return "Sending failed."
	}
	return text
}

func (s Service) SendEmailChangeNotice(ctx context.Context, username, address, newEmail, language string) error {
	config, sender := s.current()
	if !config.Enabled || !config.NotifyAccountChanges || strings.TrimSpace(address) == "" {
		return nil
	}
	brand := s.brand()
	message := servicemail.Message{BrandName: brand}
	if isGerman(language) {
		message.Subject = brand + "-E-Mail geändert"
		message.Title = "Die E-Mail deines Kontos wurde geändert"
		message.Intro = "Die E-Mail-Adresse für " + username + " wurde zu " + newEmail + " geändert. Warst du das nicht, wende dich an die Person, die " + brand + " betreibt."
	} else {
		message.Subject = brand + " email changed"
		message.Title = "Your account email changed"
		message.Intro = "The email address for " + username + " was changed to " + newEmail + ". If this was not you, contact the person who runs " + brand + "."
	}
	return sender.Send(ctx, address, message)
}

func (s Service) SendEmailChangedNewNotice(ctx context.Context, username, email, language string) error {
	config, sender := s.current()
	if !config.Enabled || !config.NotifyAccountChanges || strings.TrimSpace(email) == "" {
		return nil
	}
	brand := s.brand()
	message := servicemail.Message{BrandName: brand}
	if isGerman(language) {
		message.Subject = brand + "-E-Mail geändert"
		message.Title = "Diese E-Mail ist jetzt verknüpft"
		message.Intro = "Diese Adresse ist jetzt mit dem " + brand + "-Konto " + username + " verknüpft."
	} else {
		message.Subject = brand + " email changed"
		message.Title = "This email is now linked"
		message.Intro = "This address is now linked to the " + brand + " account " + username + "."
	}
	return sender.Send(ctx, email, message)
}

func (s Service) SendEmailChangeRequestedNotice(ctx context.Context, username, address, newEmail, language string) error {
	config, sender := s.current()
	if !config.Enabled || !config.NotifyAccountChanges || strings.TrimSpace(address) == "" {
		return nil
	}
	brand := s.brand()
	message := servicemail.Message{BrandName: brand}
	if isGerman(language) {
		message.Subject = brand + ": Änderung der E-Mail angefragt"
		message.Title = "Änderung der E-Mail angefragt"
		message.Intro = "Jemand möchte die E-Mail-Adresse von " + username + " zu " + newEmail + " ändern. Erst nach der Bestätigung ändert sich etwas. Warst du das nicht, ändere dein Passwort."
	} else {
		message.Subject = brand + " email change requested"
		message.Title = "Email change requested"
		message.Intro = "Someone asked to change the email for " + username + " to " + newEmail + ". Nothing changes until the new address is confirmed. If this was not you, change your password."
	}
	return sender.Send(ctx, address, message)
}

func (s Service) sendTokenMail(ctx context.Context, username, email, token, language, kind string) error {
	config, sender := s.current()
	base, err := s.confirmationURL(token)
	if err != nil {
		return err
	}
	if kind == "reset" {
		base, err = s.actionURL("/api/auth/password/reset", token)
		if err != nil {
			return err
		}
	}
	lang := "en"
	if isGerman(language) {
		lang = "de"
	}
	hours := config.TokenTTLHours
	if hours <= 0 {
		hours = 24
	}
	message := mailText(lang, s.brand(), username, base, durationText(lang, hours))
	if kind == "reset" {
		message = resetMailText(lang, s.brand(), username, base, durationText(lang, hours))
	}
	return sender.Send(ctx, email, message)
}

func (s Service) confirmationURL(token string) (string, error) {
	return s.actionURL("/api/auth/email/confirm", token)
}

func (s Service) actionURL(path, token string) (string, error) {
	config, _ := s.current()
	base, err := url.Parse(strings.TrimRight(config.PublicURL, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("accountmail: invalid configured public URL")
	}
	base.Path = path
	query := base.Query()
	query.Set("token", token)
	base.RawQuery = query.Encode()
	return base.String(), nil
}

func (s Service) SendPasswordChangedNotice(ctx context.Context, username, email, language string) error {
	config, sender := s.current()
	if !config.Enabled || !config.NotifyAccountChanges || strings.TrimSpace(email) == "" {
		return nil
	}
	brand := s.brand()
	message := servicemail.Message{BrandName: brand}
	if isGerman(language) {
		message.Subject = brand + "-Passwort geändert"
		message.Title = "Dein Passwort wurde geändert"
		message.Intro = "Das Passwort für " + username + " wurde geändert. Warst du das nicht, setze dein Passwort sofort zurück und melde andere Geräte ab."
	} else {
		message.Subject = brand + " password changed"
		message.Title = "Your password was changed"
		message.Intro = "The password for " + username + " was changed. If this was not you, reset your password right away and sign out other devices."
	}
	return sender.Send(ctx, email, message)
}

func (s Service) SendNewSignInNotice(ctx context.Context, username, email, ip, userAgent, language string) error {
	config, sender := s.current()
	if !config.Enabled || !config.NotifyUnusualLogin || strings.TrimSpace(email) == "" {
		return nil
	}
	brand := s.brand()
	if ip == "" {
		ip = "-"
	}
	if userAgent == "" {
		userAgent = "-"
	}
	message := servicemail.Message{BrandName: brand}
	account, address, device, at := "Account", "IP address", "Device", "Time"
	if isGerman(language) {
		message.Subject = "Neue Anmeldung bei " + brand
		message.Title = "Neue Anmeldung erkannt"
		message.Intro = "Dein " + brand + "-Konto wurde von einem Gerät oder Netzwerk angemeldet, das wir länger nicht gesehen haben. Warst du das, ist nichts zu tun. Wenn nicht, ändere dein Passwort und melde andere Geräte ab."
		account, address, device, at = "Konto", "IP-Adresse", "Gerät", "Zeit"
	} else {
		message.Subject = "New " + brand + " sign-in"
		message.Title = "New sign-in detected"
		message.Intro = "Your " + brand + " account was signed in from a device or network we have not seen recently. If this was you, nothing to do. If not, change your password and sign out other devices."
	}
	message.Details = []servicemail.Detail{
		{Label: account, Value: username}, {Label: address, Value: ip}, {Label: device, Value: userAgent},
		{Label: at, Value: time.Now().UTC().Format("2006-01-02 15:04") + " UTC"},
	}
	return sender.Send(ctx, email, message)
}

func (s Service) brand() string {
	config, _ := s.current()
	if value := strings.TrimSpace(config.BrandName); value != "" {
		return value
	}
	return "NeoCloud"
}

func isGerman(language string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(language)), "de")
}

func durationText(language string, hours float64) string {
	n := strconv.FormatFloat(hours, 'f', -1, 64)
	if isGerman(language) {
		if hours == 1 {
			return n + " Stunde"
		}
		return n + " Stunden"
	}
	if hours == 1 {
		return n + " hour"
	}
	return n + " hours"
}

func mailText(language, brand, username, actionURL, expires string) servicemail.Message {
	if isGerman(language) {
		return servicemail.Message{
			Subject:   "Bestätige deine " + brand + "-E-Mail",
			Title:     "E-Mail bestätigen",
			Intro:     fmt.Sprintf("Bestätige diese E-Mail-Adresse, um das %s-Konto %s fertig anzulegen.", brand, username),
			ActionURL: actionURL, ActionLabel: "E-Mail bestätigen", BrandName: brand,
			Details: []servicemail.Detail{{Label: "Konto", Value: username}, {Label: "Gültig bis", Value: expires}},
		}
	}
	return servicemail.Message{
		Subject:   "Confirm your " + brand + " email",
		Title:     "Confirm your email",
		Intro:     fmt.Sprintf("Confirm this email address to finish creating the %s account %s.", brand, username),
		ActionURL: actionURL, ActionLabel: "Confirm email", BrandName: brand,
		Details: []servicemail.Detail{{Label: "Account", Value: username}, {Label: "Expires", Value: expires}},
	}
}

func resetMailText(language, brand, username, actionURL, expires string) servicemail.Message {
	if isGerman(language) {
		return servicemail.Message{
			Subject: "Setze dein " + brand + "-Passwort zurück", Title: "Passwort zurücksetzen",
			Intro:     "Über diesen Link legst du ein neues Passwort für " + username + " fest. Hast du das nicht angefordert, ignoriere diese Mail; dein Passwort bleibt, wie es ist.",
			ActionURL: actionURL, ActionLabel: "Passwort zurücksetzen", BrandName: brand,
			Details: []servicemail.Detail{{Label: "Konto", Value: username}, {Label: "Gültig bis", Value: expires}},
		}
	}
	return servicemail.Message{
		Subject: "Reset your " + brand + " password", Title: "Reset your password",
		Intro:     "Use this link to set a new password for " + username + ". Ignore this email if you did not ask for it; your password stays as it is.",
		ActionURL: actionURL, ActionLabel: "Reset password", BrandName: brand,
		Details: []servicemail.Detail{{Label: "Account", Value: username}, {Label: "Expires", Value: expires}},
	}
}
