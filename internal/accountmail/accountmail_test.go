package accountmail

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/servicemail"
)

type captureMailer struct {
	to  string
	m   servicemail.Message
	err error
}

func (m *captureMailer) Send(_ context.Context, to string, message servicemail.Message) error {
	m.to, m.m = to, message
	return m.err
}

func TestSendTestEmailNormalizesRecipientAndLocalizesMessage(t *testing.T) {
	mailer := &captureMailer{}
	service := WithSender(servicemail.Config{
		Enabled: true, Host: "smtp.example.test", Port: 587, Pass: "smtp-secret", BrandName: "NeoCloud",
	}, mailer)
	recipient, err := service.SendTestEmail(context.Background(), "  NEO@Example.test ", "de-DE,de;q=0.9")
	if err != nil {
		t.Fatal(err)
	}
	if recipient != "neo@example.test" || mailer.to != recipient || mailer.m.Subject != "NeoCloud: Test-E-Mail" ||
		mailer.m.Title != "E-Mail funktioniert" || len(mailer.m.Details) != 2 || mailer.m.Details[1].Value != "smtp.example.test:587" {
		t.Fatalf("recipient=%q mailer=%+v", recipient, mailer)
	}
}

func TestSendTestEmailRedactsTransportSecrets(t *testing.T) {
	mailer := &captureMailer{err: errors.New("AUTH failed password=smtp-secret token=private https://user:pass@example.test")}
	service := WithSender(servicemail.Config{Enabled: true, Host: "smtp.example.test", Port: 587, Pass: "smtp-secret"}, mailer)
	_, err := service.SendTestEmail(context.Background(), "admin@example.test", "en")
	if err == nil || strings.Contains(err.Error(), "smtp-secret") || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "user:pass") || !strings.Contains(err.Error(), "[hidden]") {
		t.Fatalf("transport error was not safely redacted: %v", err)
	}
}

func TestSignupMailUsesSharedSenderContractAndLocalizedCopy(t *testing.T) {
	config := servicemail.Config{
		Enabled: true, BrandName: "NeoCloud", PublicURL: "https://cloud.example.test/base",
		TokenTTLHours: 24, RequireSignupConfirmation: true,
	}
	mailer := &captureMailer{}
	service := WithSender(config, mailer)
	if err := service.SendSignupConfirmation(context.Background(), "neo", "neo@example.test", "secret-token", "de-DE,de;q=0.9"); err != nil {
		t.Fatal(err)
	}
	if mailer.to != "neo@example.test" || mailer.m.BrandName != "NeoCloud" ||
		mailer.m.Subject != "Bestätige deine NeoCloud-E-Mail" ||
		mailer.m.ActionURL != "https://cloud.example.test/api/auth/email/confirm?token=secret-token" ||
		len(mailer.m.Details) != 2 || mailer.m.Details[1].Value != "24 Stunden" {
		t.Fatalf("unexpected localized signup mail: recipient=%q message=%+v", mailer.to, mailer.m)
	}
	if got := service.PendingSignupMessage("de-DE"); got != "Prüfe dein Postfach, um dein NeoCloud-Konto zu bestätigen." {
		t.Fatalf("German pending message = %q", got)
	}
}

func TestEmailStatusReflectsSharedMailConfig(t *testing.T) {
	service := New(servicemail.Config{Enabled: true, RequireSignupConfirmation: true, RequireChangeConfirmation: true})
	status := service.Status()
	if !status.Enabled || !status.Configured || !status.SignupConfirmationRequired || !status.EmailChangeConfirmationRequired {
		t.Fatalf("mail status = %+v", status)
	}
}

func TestSetConfigAppliesToServiceCopiesAndKeepsInjectedSender(t *testing.T) {
	mailer := &captureMailer{}
	service := WithSender(servicemail.Config{Enabled: false}, mailer)
	serviceCopy := service
	service.SetConfig(servicemail.Config{
		Enabled: true, BrandName: "Updated Cloud", PublicURL: "https://updated.example.test",
		TokenTTLHours: 6, RequireSignupConfirmation: true, RequireChangeConfirmation: true,
	})
	if got := serviceCopy.Status(); !got.Enabled || !got.SignupConfirmationRequired || !got.EmailChangeConfirmationRequired {
		t.Fatalf("updated service status = %+v", got)
	}
	if err := serviceCopy.SendSignupConfirmation(context.Background(), "neo", "neo@example.test", "token", "en"); err != nil {
		t.Fatal(err)
	}
	if mailer.to != "neo@example.test" || mailer.m.BrandName != "Updated Cloud" ||
		mailer.m.ActionURL != "https://updated.example.test/api/auth/email/confirm?token=token" ||
		mailer.m.Details[1].Value != "6 hours" {
		t.Fatalf("updated service mail = %+v", mailer.m)
	}
}

func TestRecoveryMailUsesConfiguredOriginAndLocalizedTemplates(t *testing.T) {
	config := servicemail.Config{
		Enabled: true, BrandName: "NeoCloud", PublicURL: "https://cloud.example.test",
		TokenTTLHours: 12, NotifyAccountChanges: true,
	}
	mailer := &captureMailer{}
	service := WithSender(config, mailer)
	if err := service.SendPasswordReset(context.Background(), "neo", "neo@example.test", "reset-token", "de-DE,de;q=0.9"); err != nil {
		t.Fatal(err)
	}
	if mailer.to != "neo@example.test" || mailer.m.Subject != "Setze dein NeoCloud-Passwort zurück" ||
		mailer.m.ActionURL != "https://cloud.example.test/api/auth/password/reset?token=reset-token" ||
		mailer.m.ActionLabel != "Passwort zurücksetzen" {
		t.Fatalf("unexpected localized reset mail: recipient=%q message=%+v", mailer.to, mailer.m)
	}
	if err := service.SendPasswordChangedNotice(context.Background(), "neo", "neo@example.test", "en"); err != nil {
		t.Fatal(err)
	}
	if mailer.to != "neo@example.test" || mailer.m.Subject != "NeoCloud password changed" || mailer.m.ActionURL != "" {
		t.Fatalf("unexpected password-change notice: recipient=%q message=%+v", mailer.to, mailer.m)
	}
}

func TestEmailChangeConfirmationUsesConfiguredOriginAndNoticesBothStates(t *testing.T) {
	config := servicemail.Config{
		Enabled: true, BrandName: "NeoCloud", PublicURL: "https://cloud.example.test",
		TokenTTLHours: 24, NotifyAccountChanges: true,
	}
	mailer := &captureMailer{}
	service := WithSender(config, mailer)
	if err := service.SendEmailConfirmation(context.Background(), "neo", "new@example.test", "safe-token", "de-DE", true); err != nil {
		t.Fatal(err)
	}
	if mailer.to != "new@example.test" || mailer.m.Subject != "Bestätige deine neue NeoCloud-E-Mail" ||
		mailer.m.ActionURL != "https://cloud.example.test/api/auth/email/confirm?token=safe-token" ||
		len(mailer.m.Details) != 3 || mailer.m.Details[1].Value != "new@example.test" {
		t.Fatalf("unexpected email-change confirmation: to=%q message=%+v", mailer.to, mailer.m)
	}
	if err := service.SendEmailChangeRequestedNotice(context.Background(), "neo", "old@example.test", "new@example.test", "en"); err != nil {
		t.Fatal(err)
	}
	if mailer.to != "old@example.test" || mailer.m.Subject != "NeoCloud email change requested" || !strings.Contains(mailer.m.Intro, "Nothing changes until the new address is confirmed") {
		t.Fatalf("unexpected request notice: to=%q message=%+v", mailer.to, mailer.m)
	}
	if err := service.SendEmailChangeNotice(context.Background(), "neo", "old@example.test", "new@example.test", "en"); err != nil {
		t.Fatal(err)
	}
	if mailer.to != "old@example.test" || mailer.m.Subject != "NeoCloud email changed" || !strings.Contains(mailer.m.Intro, "new@example.test") {
		t.Fatalf("unexpected completion notice: to=%q message=%+v", mailer.to, mailer.m)
	}
}
