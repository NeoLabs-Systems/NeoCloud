package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"html/template"
	"net/http"
	"strings"
)

// The pages a mail link opens: confirm your email and choose a new password.
// Plain HTML, never cached, and their policy allows exactly one inline script,
// by hash. A GET never spends the link.

type mailPageText struct {
	Eyebrow, OpenApp, ConfirmTitle, ConfirmIntro, ConfirmButton, ConfirmedTitle, ConfirmedIntro, ChangedIntro, ConfirmFailedTitle, ConfirmFailedIntro, ConfirmMissingTitle, ConfirmMissingIntro, EmailTakenIntro, ResetTitle, ResetIntro, Password, Repeat, Submit, Mismatch, HintMin, Show, Hide, ResetDoneTitle, ResetDoneIntro, ResetFailedTitle, ResetFailedIntro, TooManyIntro, ResetMissingIntro string
}

var mailPageTexts = map[string]mailPageText{
	"en": {
		Eyebrow:             "Account",
		OpenApp:             "Open {brand}",
		ConfirmTitle:        "Confirm your email",
		ConfirmIntro:        "Press the button to confirm this email address for your {brand} account.",
		ConfirmButton:       "Confirm email",
		ConfirmedTitle:      "Email confirmed",
		ConfirmedIntro:      "Your {brand} email is confirmed. You can return to the app and sign in.",
		ChangedIntro:        "Your new {brand} email is confirmed and now belongs to your account.",
		ConfirmFailedTitle:  "This link does not work",
		ConfirmFailedIntro:  "The confirmation link is invalid, already used or expired. Request a new one in the app: sign in, or use \"Resend confirmation\".",
		ConfirmMissingTitle: "Link incomplete",
		ConfirmMissingIntro: "Open the full link from your {brand} email.",
		EmailTakenIntro:     "This email address is already used by another account, so it could not be changed.",
		ResetTitle:          "Choose a new password",
		ResetIntro:          "Enter a new password for your {brand} account. You will be signed out everywhere else.",
		Password:            "New password",
		Repeat:              "Repeat new password",
		Submit:              "Change password",
		Mismatch:            "The two passwords do not match.",
		HintMin:             "At least 8 characters. Do not use your username or email.",
		Show:                "Show",
		Hide:                "Hide",
		ResetDoneTitle:      "Password changed",
		ResetDoneIntro:      "Your {brand} password has been changed and all other sessions were signed out. You can sign in with the new password.",
		ResetFailedTitle:    "This link does not work",
		ResetFailedIntro:    "The reset link is invalid, already used or expired. Request a new one with \"Forgot password?\" on the sign-in page.",
		TooManyIntro:        "Too many tries. Please wait a few minutes and request a new link.",
		ResetMissingIntro:   "Open the full link from your {brand} email.",
	},
	"de": {
		Eyebrow:             "Konto",
		OpenApp:             "{brand} öffnen",
		ConfirmTitle:        "E-Mail bestätigen",
		ConfirmIntro:        "Tippe auf die Schaltfläche, um diese E-Mail-Adresse für dein {brand}-Konto zu bestätigen.",
		ConfirmButton:       "E-Mail bestätigen",
		ConfirmedTitle:      "E-Mail bestätigt",
		ConfirmedIntro:      "Deine {brand}-E-Mail ist bestätigt. Du kannst zur App zurückkehren und dich anmelden.",
		ChangedIntro:        "Deine neue {brand}-E-Mail ist bestätigt und gehört jetzt zu deinem Konto.",
		ConfirmFailedTitle:  "Dieser Link funktioniert nicht",
		ConfirmFailedIntro:  "Der Bestätigungslink ist ungültig, schon benutzt oder abgelaufen. Fordere in der App einen neuen an: melde dich an oder nutze „Bestätigung erneut senden“.",
		ConfirmMissingTitle: "Link unvollständig",
		ConfirmMissingIntro: "Öffne den vollständigen Link aus deiner {brand}-Mail.",
		EmailTakenIntro:     "Diese E-Mail-Adresse gehört schon zu einem anderen Konto und konnte deshalb nicht übernommen werden.",
		ResetTitle:          "Neues Passwort festlegen",
		ResetIntro:          "Gib ein neues Passwort für dein {brand}-Konto ein. Überall sonst wirst du abgemeldet.",
		Password:            "Neues Passwort",
		Repeat:              "Neues Passwort wiederholen",
		Submit:              "Passwort ändern",
		Mismatch:            "Die beiden Passwörter stimmen nicht überein.",
		HintMin:             "Mindestens 8 Zeichen. Verwende nicht deinen Benutzernamen oder deine E-Mail.",
		Show:                "Zeigen",
		Hide:                "Verbergen",
		ResetDoneTitle:      "Passwort geändert",
		ResetDoneIntro:      "Dein {brand}-Passwort wurde geändert, alle anderen Sitzungen sind abgemeldet. Du kannst dich jetzt mit dem neuen Passwort anmelden.",
		ResetFailedTitle:    "Dieser Link funktioniert nicht",
		ResetFailedIntro:    "Der Link zum Zurücksetzen ist ungültig, schon benutzt oder abgelaufen. Fordere auf der Anmeldeseite mit „Passwort vergessen?“ einen neuen an.",
		TooManyIntro:        "Zu viele Versuche. Warte ein paar Minuten und fordere einen neuen Link an.",
		ResetMissingIntro:   "Öffne den vollständigen Link aus deiner {brand}-Mail.",
	},
}

// Strength meter, show/hide and the mismatch hint. Indicative only: the
// server's password policy decides.
const resetScript = `
(function () {
  var form = document.getElementById('reset');
  if (!form) return;
  var pw = form.elements.password, again = form.elements.confirmPassword;
  var bar = document.getElementById('meter'), mismatch = document.getElementById('mismatch');
  function score(v) {
    var s = 0;
    if (v.length >= 8) s++;
    if (v.length >= 12) s++;
    var kinds = [/[a-z]/, /[A-Z]/, /[0-9]/, /[^A-Za-z0-9]/].filter(function (r) { return r.test(v); }).length;
    if (kinds >= 3) s++;
    if (kinds === 4 || v.length >= 16) s++;
    return s;
  }
  pw.addEventListener('input', function () { bar.setAttribute('data-score', String(pw.value ? score(pw.value) : 0)); });
  function check() { mismatch.hidden = !again.value || again.value === pw.value; }
  again.addEventListener('input', check);
  pw.addEventListener('input', check);
  form.addEventListener('submit', function (e) { if (again.value !== pw.value) { e.preventDefault(); mismatch.hidden = false; again.focus(); } });
  var toggle = document.getElementById('toggle');
  toggle.hidden = false;
  toggle.addEventListener('click', function () {
    var show = pw.type === 'password';
    pw.type = again.type = show ? 'text' : 'password';
    toggle.textContent = toggle.getAttribute(show ? 'data-hide' : 'data-show');
  });
})();`

const mailPageStyles = `
:root{color-scheme:light dark;--bg:#f4f1e8;--card:#fdfcf8;--ink:#1c2117;--ink-2:#49503f;--ink-3:#7e8470;--line:rgba(28,33,23,.10);--line-2:rgba(28,33,23,.16);--gold:#b07d2b;--gold-ink:#8a5f1c;--gold-muted:rgba(176,125,43,.14);--btn:#1c2117;--btn-ink:#fdfcf8;--btn-hover:#2a3226;--danger:#ae473c;--ok:#527c4f;--shadow:0 18px 48px -24px rgba(28,33,23,.28),0 8px 20px -12px rgba(28,33,23,.12);--glow:rgba(176,125,43,.18)}
@media (prefers-color-scheme:dark){:root{--bg:#0e1511;--card:#171f1a;--ink:#ecefe5;--ink-2:#aeb7a6;--ink-3:#7e8877;--line:rgba(224,240,224,.08);--line-2:rgba(224,240,224,.14);--gold:#e1b052;--gold-ink:#eac272;--gold-muted:rgba(225,176,82,.14);--btn:#e1b052;--btn-ink:#0e1511;--btn-hover:#eac272;--danger:#de8a78;--ok:#74c07c;--shadow:0 24px 60px -28px rgba(0,0,0,.65),0 10px 24px -16px rgba(0,0,0,.35);--glow:rgba(225,176,82,.16)}}
*,*::before,*::after{box-sizing:border-box}
html{-webkit-font-smoothing:antialiased;text-rendering:optimizeLegibility}
body{margin:0;min-height:100vh;background:var(--bg);color:var(--ink);font:440 16px/1.5 Inter,ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;background-image:radial-gradient(ellipse 60% 40% at 50% 0,var(--glow),transparent 70%);background-repeat:no-repeat}
main{min-height:100vh;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:32px 16px}
.brand{display:flex;align-items:center;gap:10px;width:100%;max-width:460px;padding:0 6px 18px;font-weight:700;font-size:17px;letter-spacing:-.03em}
.brand img{width:28px;height:28px;border-radius:8px}
.card{width:100%;max-width:460px;background:var(--card);border:1px solid var(--line);border-radius:26px;padding:34px 32px 32px;box-shadow:var(--shadow)}
.eyebrow{display:flex;align-items:center;gap:9px;font:600 11px/1 "IBM Plex Mono",ui-monospace,SFMono-Regular,Menlo,monospace;letter-spacing:.16em;text-transform:uppercase;color:var(--gold-ink)}
.dot{width:6px;height:6px;border-radius:99px;background:var(--gold);box-shadow:0 0 0 4px var(--gold-muted)}
.bad .dot{background:var(--danger);box-shadow:0 0 0 4px color-mix(in srgb,var(--danger) 16%,transparent)}
.bad .eyebrow{color:var(--danger)}
h1{margin:18px 0 0;font:560 30px/1.1 Fraunces,"Iowan Old Style",Georgia,serif;letter-spacing:-.015em}
p{margin:14px 0 0;color:var(--ink-2);font-size:15.5px;line-height:1.65}
form{margin:26px 0 0}
label{display:block;margin:18px 0 7px;font-size:13px;font-weight:600;color:var(--ink)}
label:first-of-type{margin-top:0}
.field{position:relative}
input[type=password],input[type=text]{width:100%;padding:13px 14px;border:1px solid var(--line-2);border-radius:14px;background:transparent;color:var(--ink);font:inherit;font-size:15px}
input:focus{outline:none;border-color:var(--gold);box-shadow:0 0 0 4px var(--gold-muted)}
.meter{display:flex;gap:5px;margin-top:10px}
.meter i{flex:1;height:4px;border-radius:99px;background:var(--line-2)}
.meter[data-score="1"] i:nth-child(-n+1),.meter[data-score="2"] i:nth-child(-n+2),.meter[data-score="3"] i:nth-child(-n+3),.meter[data-score="4"] i:nth-child(-n+4){background:var(--gold)}
.hint{margin:8px 0 0;font-size:12.5px;color:var(--ink-3);line-height:1.55}
.error{margin:18px 0 0;padding:11px 14px;border:1px solid color-mix(in srgb,var(--danger) 40%,transparent);border-radius:14px;color:var(--danger);font-size:13.5px;line-height:1.5}
.error[hidden]{display:none}
.btn{display:inline-flex;align-items:center;justify-content:center;gap:8px;margin-top:26px;padding:13px 24px;border:0;border-radius:999px;background:var(--btn);color:var(--btn-ink);font:600 14.5px Inter,system-ui,sans-serif;letter-spacing:-.01em;text-decoration:none;cursor:pointer;transition:background .2s}
.btn:hover{background:var(--btn-hover)}
.btn:focus-visible{outline:2px solid var(--gold);outline-offset:3px}
form .btn{width:100%}
.link{margin-top:14px;background:none;border:0;padding:0;color:var(--gold-ink);font:inherit;font-size:13px;font-weight:600;cursor:pointer}
.link[hidden]{display:none}
@media (max-width:420px){.card{padding:28px 22px 26px;border-radius:22px}h1{font-size:26px}}
`

var mailPagePolicy = func() string {
	digest := sha256.Sum256([]byte(resetScript))
	return "default-src 'none'; style-src 'self' 'unsafe-inline'; script-src 'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) +
		"'; font-src 'self'; img-src 'self' data:; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
}()

type mailPage struct {
	Lang, Brand, Title, Heading, Intro, Error string
	Token, ConfirmButton, OpenApp             string
	Bad, Confirm, Reset                       bool
	T                                         mailPageText
	Styles                                    template.CSS
	Script                                    template.JS
}

var mailPageTemplate = template.Must(template.New("mail").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark">
<meta name="robots" content="noindex,nofollow">
<title>{{.Title}} · {{.Brand}}</title>
<link rel="icon" href="/assets/favicon.png">
<link rel="stylesheet" href="/css/fonts.css">
<style>{{.Styles}}</style>
</head>
<body>
<main{{if .Bad}} class="bad"{{end}}>
<div class="brand"><img src="/assets/logo.svg" alt="" width="28" height="28"><span>{{.Brand}}</span></div>
<section class="card">
<div class="eyebrow"><span class="dot"></span>{{.T.Eyebrow}}</div>
<h1>{{.Heading}}</h1>
<p>{{.Intro}}</p>
{{if .Confirm}}<form method="post" action="/api/auth/email/confirm">
<input type="hidden" name="token" value="{{.Token}}">
<button class="btn" type="submit">{{.T.ConfirmButton}} &rarr;</button>
</form>{{else if .Reset}}<form id="reset" method="post" action="/api/auth/password/reset" autocomplete="off">
<input type="hidden" name="token" value="{{.Token}}">
<label for="password">{{.T.Password}}</label>
<div class="field"><input id="password" name="password" type="password" autocomplete="new-password" minlength="8" required autofocus></div>
<div class="meter" id="meter" data-score="0"><i></i><i></i><i></i><i></i></div>
<div class="hint">{{.T.HintMin}}</div>
<label for="confirmPassword">{{.T.Repeat}}</label>
<div class="field"><input id="confirmPassword" name="confirmPassword" type="password" autocomplete="new-password" minlength="8" required></div>
<div class="error" id="mismatch" role="alert" hidden>{{.T.Mismatch}}</div>
{{with .Error}}<div class="error" role="alert">{{.}}</div>{{end}}
<button class="btn" type="submit">{{.T.Submit}} &rarr;</button>
<button class="link" id="toggle" type="button" hidden data-show="{{.T.Show}}" data-hide="{{.T.Hide}}">{{.T.Show}}</button>
</form>
<script>{{.Script}}</script>{{else}}<a class="btn" href="/app">{{.OpenApp}} &rarr;</a>{{end}}
</section>
</main>
</body>
</html>`))

func (h *Handler) newMailPage(language string) mailPage {
	if language != "de" {
		language = "en"
	}
	brand := h.emailBrand()
	return mailPage{Lang: language, Brand: brand, T: mailPageTexts[language], Styles: template.CSS(mailPageStyles), Script: template.JS(resetScript),
		OpenApp: strings.ReplaceAll(mailPageTexts[language].OpenApp, "{brand}", brand)}
}

func writeMailPage(w http.ResponseWriter, status int, page mailPage) {
	page.Intro = strings.ReplaceAll(page.Intro, "{brand}", page.Brand)
	var body bytes.Buffer
	if err := mailPageTemplate.Execute(&body, page); err != nil {
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store, max-age=0")
	// Not no-referrer: browsers then send "Origin: null" on the form post.
	header.Set("Referrer-Policy", "strict-origin")
	header.Set("X-Robots-Tag", "noindex, nofollow")
	header.Set("Content-Security-Policy", mailPagePolicy)
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

// writeConfirmationPage renders state ready, done, changed, failed, missing,
// taken or toomany.
func (h *Handler) writeConfirmationPage(w http.ResponseWriter, status int, state, language, token string) {
	page := h.newMailPage(language)
	t := page.T
	page.Title, page.Heading, page.Intro, page.Bad = t.ConfirmFailedTitle, t.ConfirmFailedTitle, t.ConfirmFailedIntro, true
	switch state {
	case "ready":
		page.Title, page.Heading, page.Intro, page.Bad = t.ConfirmTitle, t.ConfirmTitle, t.ConfirmIntro, false
		page.Confirm, page.Token = true, token
	case "done", "changed":
		page.Title, page.Heading, page.Intro, page.Bad = t.ConfirmTitle, t.ConfirmedTitle, t.ConfirmedIntro, false
		if state == "changed" {
			page.Intro = t.ChangedIntro
		}
	case "missing":
		page.Title, page.Heading, page.Intro = t.ConfirmMissingTitle, t.ConfirmMissingTitle, t.ConfirmMissingIntro
	case "taken":
		page.Intro = t.EmailTakenIntro
	case "toomany":
		page.Intro = t.TooManyIntro
	}
	writeMailPage(w, status, page)
}

// writeResetPage renders state form (with an optional error), done, failed,
// missing or toomany.
func (h *Handler) writeResetPage(w http.ResponseWriter, status int, language, state, token, message string) {
	page := h.newMailPage(language)
	t := page.T
	page.Title, page.Heading, page.Intro, page.Bad = t.ResetFailedTitle, t.ResetFailedTitle, t.ResetFailedIntro, true
	switch state {
	case "form":
		page.Title, page.Heading, page.Intro, page.Bad = t.ResetTitle, t.ResetTitle, t.ResetIntro, false
		page.Reset, page.Token, page.Error = true, token, message
	case "done":
		page.Title, page.Heading, page.Intro, page.Bad = t.ResetTitle, t.ResetDoneTitle, t.ResetDoneIntro, false
	case "missing":
		page.Intro = t.ResetMissingIntro
	case "toomany":
		page.Intro = t.TooManyIntro
	}
	writeMailPage(w, status, page)
}
