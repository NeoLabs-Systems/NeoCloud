package signing

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/servicemail"
)

// Turns an outbox row into the message that is sent, filled from the document,
// party and account as they are now. Links come from configuration, never
// from a request; a party's link is decrypted only to put it in that party's
// own mail.

const mailAttachmentMax = 10 << 20

func mailLanguage(locale string) string {
	if locale == "de" {
		return "de"
	}
	return "en"
}

// tr fills the {name} placeholders of one string.
func tr(lang, key string, params ...string) string {
	value, ok := mailStrings[lang][key]
	if !ok {
		value = mailStrings["en"][key]
	}
	pairs := make([]string, 0, len(params))
	for i := 0; i+1 < len(params); i += 2 {
		pairs = append(pairs, "{"+params[i]+"}", params[i+1])
	}
	return strings.NewReplacer(pairs...).Replace(value)
}

// short holds a person's words to one line of a bounded length.
func short(value string) string {
	line := singleLine(value)
	if runes := []rune(line); len(runes) > 100 {
		return string(runes[:99]) + "…"
	}
	return line
}

// formatWhen: mail has no viewer time zone, so the time says which one it is.
func formatWhen(lang, value string) string {
	at, ok := parseStamp(value)
	if !ok {
		return ""
	}
	at = at.UTC()
	clock := at.Format("15:04")
	if lang == "de" {
		months := [...]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
		return strconv.Itoa(at.Day()) + ". " + months[at.Month()-time.January] + " " + strconv.Itoa(at.Year()) + " um " + clock + " UTC"
	}
	return strconv.Itoa(at.Day()) + " " + at.Month().String() + " " + strconv.Itoa(at.Year()) + " at " + clock + " UTC"
}

type mailContext struct {
	d       document
	lang    string
	creator string
	config  servicemail.Config
}

func (c mailContext) message(subject, title, intro, url, button string) *servicemail.Message {
	footer := tr(c.lang, "footer.service", "brand", c.config.BrandName)
	if c.config.SupportURL != "" {
		footer = tr(c.lang, "footer.support", "brand", c.config.BrandName, "url", c.config.SupportURL)
	}
	return &servicemail.Message{
		Subject: singleLine(subject), Title: title, Intro: intro, ActionURL: url, ActionLabel: button,
		Lang: c.lang, Eyebrow: tr(c.lang, "eyebrow"), Fallback: tr(c.lang, "fallback"), Footer: footer, BrandName: c.config.BrandName,
		Details: []servicemail.Detail{{Label: tr(c.lang, "label.document"), Value: singleLine(c.d.Title)}},
		// Tells auto-responders and out-of-office rules to stay quiet.
		Headers: map[string]string{"Auto-Submitted": "auto-generated", "X-Auto-Response-Suppress": "All", "Precedence": "bulk"},
	}
}

func (c mailContext) names(name string) []string {
	creator := short(c.creator)
	if creator == "" {
		creator = tr(c.lang, "creatorFallback")
	}
	return []string{"creator", creator, "title", short(c.d.Title), "name", short(name)}
}

// forParty says who asked, that the mail can be ignored and where to report it.
func (c mailContext) forParty(m *servicemail.Message) *servicemail.Message {
	contact := c.config.SupportURL
	if contact == "" {
		address := c.config.ReplyTo
		if address == "" {
			address = c.config.From
		}
		if parsed, err := mail.ParseAddress(address); err == nil {
			address = parsed.Address
		}
		if address != "" {
			contact = "mailto:" + strings.TrimSpace(address)
		}
	}
	report := ""
	if contact != "" {
		report = tr(c.lang, "footer.report", "contact", contact)
	}
	creator := truncate(singleLine(c.creator), 100)
	if creator == "" {
		creator = tr(c.lang, "creatorFallback")
	}
	m.Footer = tr(c.lang, "footer.requested", "creator", creator, "brand", c.config.BrandName, "report", report)
	return m
}

// partyPageURL is the page of a party's own link, or "" when its stored copy
// is missing or no longer the live one.
func (h *Handler) partyPageURL(p party, base string) string {
	if p.TokenEnc.String == "" || !p.TokenHash.Valid {
		return ""
	}
	token, err := h.keys.DecryptValue(p.TokenEnc.String)
	if err != nil || hashToken(token) != p.TokenHash.String {
		return ""
	}
	return base + signPagePath + token
}

func (h *Handler) mailBase() (string, error) {
	base := strings.TrimRight(strings.TrimSpace(h.mailConfig().PublicURL), "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("signing: the public URL for mail is invalid")
	}
	return base, nil
}

func (h *Handler) requestMessage(c mailContext, kind, url string, note string) *servicemail.Message {
	prefix := map[string]string{mailInvitation: "invitation", mailReminder: "reminder", mailYourTurn: "turn", mailExpiryWarning: "expiry"}[kind]
	who := append(c.names(""), "when", formatWhen(c.lang, c.d.ExpiresAt.String))
	m := c.message(tr(c.lang, prefix+".subject", who...), tr(c.lang, prefix+".title"), tr(c.lang, prefix+".intro", who...), url, tr(c.lang, prefix+".button"))
	m.Paragraphs = []string{tr(c.lang, "noAccount"), tr(c.lang, "privacy")}
	if singleLine(note) != "" {
		m.Quote = &servicemail.Quote{Label: tr(c.lang, "whoMessage", who...), Text: strings.TrimSpace(note)}
	}
	if when := formatWhen(c.lang, c.d.ExpiresAt.String); when != "" {
		m.Details = append(m.Details, servicemail.Detail{Label: tr(c.lang, "label.expires"), Value: when})
	}
	return m
}

// completedMessage attaches the signed file when it fits.
func (h *Handler) completedMessage(ctx context.Context, c mailContext, url string, ownerView bool) (*servicemail.Message, error) {
	who := c.names("")
	button := "completed.buttonSigner"
	clean := "completed.cleanSigner"
	if ownerView {
		button, clean = "completed.buttonOwner", "completed.cleanOwner"
	}
	m := c.message(tr(c.lang, "completed.subject", who...), tr(c.lang, "completed.title"), tr(c.lang, "completed.intro", who...), url, tr(c.lang, button))
	if url == "" {
		m.ActionLabel = ""
	}
	node, live, err := liveFinalNode(ctx, h.db, c.d)
	if err != nil || !live {
		// The signed file is gone from the library: nothing to point to.
		return m, err
	}
	closing := "completed.tooLarge"
	if node.size <= h.attachmentLimit {
		content, err := h.readBlob(ctx, c.d.OwnerID, node.blobID, node.size)
		if err != nil {
			return nil, err
		}
		closing = "completed.attached"
		m.Attachments = []servicemail.Attachment{{Name: finalFileName(c.d, true), ContentType: "application/pdf", Content: content}}
	}
	m.Paragraphs = []string{tr(c.lang, closing), tr(c.lang, clean)}
	return m, nil
}

func (h *Handler) noticeMessage(c mailContext, kind, url string, payload map[string]any) *servicemail.Message {
	prefix := map[string]string{mailCreatorSigned: "creatorSigned", mailCreatorDeclined: "creatorDeclined", mailCreatorExpired: "creatorExpired"}[kind]
	name, _ := payload["name"].(string)
	who := c.names(name)
	m := c.message(tr(c.lang, prefix+".subject", who...), tr(c.lang, prefix+".title", who...), tr(c.lang, prefix+".intro", who...), url, tr(c.lang, prefix+".button"))
	if reason, _ := payload["reason"].(string); singleLine(reason) != "" {
		m.Quote = &servicemail.Quote{Label: tr(c.lang, "creatorDeclined.reasonLabel"), Text: strings.TrimSpace(reason)}
	}
	done, _ := payload["done"].(float64)
	if total, _ := payload["total"].(float64); total > 0 {
		m.Details = append(m.Details, servicemail.Detail{Label: tr(c.lang, "label.progress"),
			Value: tr(c.lang, "progress", "done", number(done), "total", number(total))})
	}
	return m
}

// compose returns nil when the mail is no longer wanted.
func (h *Handler) compose(ctx context.Context, row outboxRow) (*servicemail.Message, error) {
	d, err := documentByID(ctx, h.db, row.documentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, permanentError{"The document no longer exists."}
	}
	if err != nil {
		return nil, err
	}
	var p party
	if row.partyID.Valid {
		if p, err = partyByID(ctx, h.db, row.partyID.String); errors.Is(err, sql.ErrNoRows) {
			return nil, permanentError{"The party no longer exists."}
		} else if err != nil {
			return nil, err
		}
	}
	var creator string
	if err := h.db.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(display_name,''),username) FROM users WHERE id=?`, d.OwnerID).Scan(&creator); err != nil {
		return nil, err
	}
	base, err := h.mailBase()
	if err != nil {
		return nil, err
	}
	config := h.mailConfig()
	if config.BrandName == "" {
		config.BrandName = "NeoCloud"
	}
	c := mailContext{d: d, lang: mailLanguage(d.Locale), creator: creator, config: config}
	payload, _ := decodeJSON(row.payload, nil).(map[string]any)
	ownerPage := base + ownerPagePath + d.ID
	switch {
	case slices.Contains(requestKinds, row.kind):
		if !row.partyID.Valid || !isLive(d.Status) || p.Status == partyCompleted || p.Status == partyDeclined {
			return nil, nil
		}
		link := h.partyPageURL(p, base)
		if link == "" {
			return nil, permanentError{"The signing link is no longer available."}
		}
		note, written := payload["message"].(string)
		if !written && (row.kind == mailInvitation || row.kind == mailYourTurn) {
			note = d.Message.String
		}
		return c.forParty(h.requestMessage(c, row.kind, link, note)), nil
	case row.kind == mailCompleted:
		if !row.partyID.Valid {
			return h.completedMessage(ctx, c, ownerPage, true)
		}
		m, err := h.completedMessage(ctx, c, h.partyPageURL(p, base), false)
		if err != nil {
			return nil, err
		}
		return c.forParty(m), nil
	default:
		return h.noticeMessage(c, row.kind, ownerPage, payload), nil
	}
}
