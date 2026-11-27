package oauth

import (
	"bytes"
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

// The pages a person sees while connecting an app: sign-in, the 2FA code,
// consent and an error page. Plain HTML without script, in English or German by
// the browser's Accept-Language.

type pageStrings struct {
	Lang, Eyebrow, SignInTitle, SignInIntro, Account, Password, Continue, CodeTitle, CodeIntro, Code, Verify, Back, ConsentTitle, ConsentIntro, SignedInAs, Files, FilesRead, FilesReadHint, FilesWrite, FilesWriteHint, Signatures, SignaturesToggle, SignaturesHint, SignaturesStatus, Vaults, Allow, Deny, ErrorTitle, ErrorIntro string
	Errors                                                                                                                                                                                                                                                                                                                           map[string]string
}

var pageText = map[string]pageStrings{
	"en": {
		Lang:             "en",
		Eyebrow:          "Connect an app",
		SignInTitle:      "Sign in to connect NeoAgent",
		SignInIntro:      "Use your NeoCloud account. Your password stays with NeoCloud; NeoAgent never sees it.",
		Account:          "Username or email",
		Password:         "Password",
		Continue:         "Continue",
		CodeTitle:        "Enter your code",
		CodeIntro:        "Enter the 6-digit code from your authenticator app, or one of your recovery codes.",
		Code:             "Code",
		Verify:           "Verify",
		Back:             "Back",
		ConsentTitle:     "Connect NeoAgent?",
		ConsentIntro:     "NeoAgent at {host} would like to work with your NeoCloud. You can change or remove this at any time in Settings.",
		SignedInAs:       "Signed in as {user}",
		Files:            "Files",
		FilesRead:        "Read only",
		FilesReadHint:    "See, open and search your files.",
		FilesWrite:       "Read & write",
		FilesWriteHint:   "Also create, change, move and put files in the trash. Nothing is deleted for good.",
		Signatures:       "Signatures",
		SignaturesToggle: "Create and send signature requests",
		SignaturesHint:   "Prepare documents for signing and send signing links in your name.",
		SignaturesStatus: "NeoAgent can see how your signature requests are going.",
		Vaults:           "Files in your encrypted vaults stay out of reach.",
		Allow:            "Allow",
		Deny:             "Don’t allow",
		ErrorTitle:       "This didn’t work",
		ErrorIntro:       "The connection request is no longer valid. Start connecting again from NeoAgent.",
		Errors: map[string]string{
			"INVALID_CREDENTIALS":          "That username or password isn’t right.",
			"MISSING_FIELDS":               "Fill in both fields.",
			"LOGIN_LOCKED":                 "Too many attempts. Try again in a few minutes.",
			"ACCOUNT_DISABLED":             "This account is turned off.",
			"EMAIL_NOT_CONFIRMED":          "Confirm your email address first, then try again.",
			"INVALID_CODE":                 "That code isn’t right.",
			"TWO_FACTOR_EXPIRED":           "That took too long. Sign in again.",
			"TWO_FACTOR_ATTEMPTS_EXCEEDED": "Too many wrong codes. Sign in again.",
			"generic":                      "Something went wrong. Try again.",
		},
	},
	"de": {
		Lang:             "de",
		Eyebrow:          "App verbinden",
		SignInTitle:      "Melde dich an, um NeoAgent zu verbinden",
		SignInIntro:      "Nutze dein NeoCloud-Konto. Dein Passwort bleibt bei NeoCloud; NeoAgent sieht es nie.",
		Account:          "Benutzername oder E-Mail",
		Password:         "Passwort",
		Continue:         "Weiter",
		CodeTitle:        "Gib deinen Code ein",
		CodeIntro:        "Gib den 6-stelligen Code aus deiner Authenticator-App oder einen deiner Wiederherstellungscodes ein.",
		Code:             "Code",
		Verify:           "Bestätigen",
		Back:             "Zurück",
		ConsentTitle:     "NeoAgent verbinden?",
		ConsentIntro:     "NeoAgent unter {host} möchte mit deiner NeoCloud arbeiten. Du kannst das jederzeit in den Einstellungen ändern oder entfernen.",
		SignedInAs:       "Angemeldet als {user}",
		Files:            "Dateien",
		FilesRead:        "Nur lesen",
		FilesReadHint:    "Deine Dateien sehen, öffnen und durchsuchen.",
		FilesWrite:       "Lesen & schreiben",
		FilesWriteHint:   "Außerdem Dateien anlegen, ändern, verschieben und in den Papierkorb legen. Endgültig gelöscht wird nichts.",
		Signatures:       "Signaturen",
		SignaturesToggle: "Signaturanfragen erstellen und versenden",
		SignaturesHint:   "Dokumente zum Unterschreiben vorbereiten und Signatur-Links in deinem Namen versenden.",
		SignaturesStatus: "NeoAgent sieht, wie es um deine Signaturanfragen steht.",
		Vaults:           "Dateien in deinen verschlüsselten Tresoren bleiben unerreichbar.",
		Allow:            "Erlauben",
		Deny:             "Nicht erlauben",
		ErrorTitle:       "Das hat nicht geklappt",
		ErrorIntro:       "Die Verbindungsanfrage ist nicht mehr gültig. Starte das Verbinden in NeoAgent noch einmal.",
		Errors: map[string]string{
			"INVALID_CREDENTIALS":          "Benutzername oder Passwort stimmen nicht.",
			"MISSING_FIELDS":               "Fülle beide Felder aus.",
			"LOGIN_LOCKED":                 "Zu viele Versuche. Versuch es in ein paar Minuten noch einmal.",
			"ACCOUNT_DISABLED":             "Dieses Konto ist deaktiviert.",
			"EMAIL_NOT_CONFIRMED":          "Bestätige zuerst deine E-Mail-Adresse und versuch es dann noch einmal.",
			"INVALID_CODE":                 "Der Code stimmt nicht.",
			"TWO_FACTOR_EXPIRED":           "Das hat zu lange gedauert. Melde dich noch einmal an.",
			"TWO_FACTOR_ATTEMPTS_EXCEEDED": "Zu viele falsche Codes. Melde dich noch einmal an.",
			"generic":                      "Etwas ist schiefgelaufen. Versuch es noch einmal.",
		},
	},
}

type page struct {
	S                                          pageStrings
	Title, Error, Account, Continue, ConsentID string
	Intro, SignedInAs                          string
	OffersFiles, OffersWrite, OffersSigning    bool
	SeesSigning                                bool
	Styles                                     template.CSS
}

const pageStyles = `
:root{color-scheme:light dark;--bg:#f4f1e8;--card:#fdfcf8;--ink:#1c2117;--ink-2:#49503f;--ink-3:#7e8470;--line:rgba(28,33,23,.10);--line-2:rgba(28,33,23,.16);--gold:#b07d2b;--gold-ink:#8a5f1c;--gold-muted:rgba(176,125,43,.14);--btn:#1c2117;--btn-ink:#fdfcf8;--btn-hover:#2a3226;--danger:#ae473c;--shadow:0 18px 48px -24px rgba(28,33,23,.28),0 8px 20px -12px rgba(28,33,23,.12);--glow:rgba(176,125,43,.18)}
@media (prefers-color-scheme:dark){:root{--bg:#0e1511;--card:#171f1a;--ink:#ecefe5;--ink-2:#aeb7a6;--ink-3:#7e8877;--line:rgba(224,240,224,.08);--line-2:rgba(224,240,224,.14);--gold:#e1b052;--gold-ink:#eac272;--gold-muted:rgba(225,176,82,.14);--btn:#e1b052;--btn-ink:#0e1511;--btn-hover:#eac272;--danger:#de8a78;--shadow:0 24px 60px -28px rgba(0,0,0,.65),0 10px 24px -16px rgba(0,0,0,.35);--glow:rgba(225,176,82,.16)}}
*,*::before,*::after{box-sizing:border-box}
html{-webkit-font-smoothing:antialiased}
body{margin:0;min-height:100vh;background:var(--bg);color:var(--ink);font:440 16px/1.5 Inter,ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;background-image:radial-gradient(ellipse 60% 40% at 50% 0,var(--glow),transparent 70%);background-repeat:no-repeat}
main{min-height:100vh;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:32px 16px}
.brand{display:flex;align-items:center;gap:10px;width:100%;max-width:480px;padding:0 6px 18px;font-weight:700;font-size:17px;letter-spacing:-.03em}
.brand img{width:28px;height:28px}
.card{width:100%;max-width:480px;background:var(--card);border:1px solid var(--line);border-radius:26px;padding:34px 32px 32px;box-shadow:var(--shadow)}
.eyebrow{display:flex;align-items:center;gap:9px;font:600 11px/1 "IBM Plex Mono",ui-monospace,Menlo,monospace;letter-spacing:.16em;text-transform:uppercase;color:var(--gold-ink)}
.dot{width:6px;height:6px;border-radius:99px;background:var(--gold);box-shadow:0 0 0 4px var(--gold-muted)}
h1{margin:18px 0 0;font:560 28px/1.15 Fraunces,"Iowan Old Style",Georgia,serif;letter-spacing:-.015em}
p{margin:12px 0 0;color:var(--ink-2);font-size:15px;line-height:1.6}
.muted{color:var(--ink-3);font-size:13px}
form{margin:24px 0 0}
label.input{display:block;margin:16px 0 7px;font-size:13px;font-weight:600}
input[type=password],input[type=text]{width:100%;padding:13px 14px;border:1px solid var(--line-2);border-radius:14px;background:transparent;color:var(--ink);font:inherit;font-size:15px}
input:focus{outline:none;border-color:var(--gold);box-shadow:0 0 0 4px var(--gold-muted)}
fieldset{margin:20px 0 0;padding:0;border:0}
legend{padding:0;font-size:13px;font-weight:600}
.choice{display:flex;gap:12px;align-items:flex-start;margin-top:10px;padding:13px 14px;border:1px solid var(--line-2);border-radius:14px;cursor:pointer}
.choice input{margin:4px 0 0;accent-color:var(--gold)}
.choice b{display:block;font-size:14.5px;font-weight:600}
.choice span{display:block;color:var(--ink-3);font-size:13px;line-height:1.5;margin-top:2px}
.choice:has(input:checked){border-color:var(--gold);box-shadow:0 0 0 3px var(--gold-muted)}
.error{margin:18px 0 0;padding:11px 14px;border:1px solid color-mix(in srgb,var(--danger) 40%,transparent);border-radius:14px;color:var(--danger);font-size:13.5px}
.actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:26px}
.btn{flex:1;display:inline-flex;align-items:center;justify-content:center;padding:13px 22px;border:1px solid transparent;border-radius:999px;background:var(--btn);color:var(--btn-ink);font:600 14.5px Inter,system-ui,sans-serif;cursor:pointer;text-decoration:none}
.btn:hover{background:var(--btn-hover)}
.btn.secondary{background:transparent;color:var(--ink);border-color:var(--line-2)}
.btn:focus-visible{outline:2px solid var(--gold);outline-offset:3px}
a.back{display:inline-block;margin-top:14px;color:var(--gold-ink);font-size:13px;font-weight:600;text-decoration:none}
@media (max-width:420px){.card{padding:28px 22px 26px;border-radius:22px}h1{font-size:24px}}
`

var pages = template.Must(template.New("oauth").Parse(`{{define "head"}}<!doctype html>
<html lang="{{.S.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark">
<meta name="robots" content="noindex,nofollow">
<title>{{.Title}} · NeoCloud</title>
<link rel="icon" href="/assets/favicon.png">
<link rel="stylesheet" href="/css/fonts.css">
<style>{{.Styles}}</style>
</head>
<body>
<main>
<div class="brand"><img src="/assets/logo.svg" alt="" width="28" height="28"><span>NeoCloud</span></div>
<section class="card">
<div class="eyebrow"><span class="dot"></span>{{.S.Eyebrow}}</div>
<h1>{{.Title}}</h1>
{{end}}{{define "foot"}}</section>
</main>
</body>
</html>
{{end}}{{define "signIn"}}{{template "head" .}}<p>{{.S.SignInIntro}}</p>
{{with .Error}}<div class="error" role="alert">{{.}}</div>{{end}}
<form method="post" action="/oauth/sign-in">
<input type="hidden" name="continue" value="{{.Continue}}">
<label class="input" for="account">{{.S.Account}}</label>
<input id="account" name="account" type="text" autocomplete="username" value="{{.Account}}" required{{if not .Account}} autofocus{{end}}>
<label class="input" for="password">{{.S.Password}}</label>
<input id="password" name="password" type="password" autocomplete="current-password" required{{if .Account}} autofocus{{end}}>
<div class="actions"><button class="btn" type="submit">{{.S.Continue}}</button></div>
</form>
{{template "foot" .}}{{end}}{{define "code"}}{{template "head" .}}<p>{{.S.CodeIntro}}</p>
{{with .Error}}<div class="error" role="alert">{{.}}</div>{{end}}
<form method="post" action="/oauth/sign-in">
<input type="hidden" name="continue" value="{{.Continue}}">
<label class="input" for="code">{{.S.Code}}</label>
<input id="code" name="code" type="text" inputmode="text" autocomplete="one-time-code" maxlength="32" spellcheck="false" autocapitalize="none" required autofocus>
<div class="actions"><button class="btn" type="submit">{{.S.Verify}}</button></div>
</form>
<a class="back" href="/oauth/sign-in?restart=1&amp;continue={{.Continue}}">&larr; {{.S.Back}}</a>
{{template "foot" .}}{{end}}{{define "consent"}}{{template "head" .}}<p>{{.Intro}}</p>
<p class="muted">{{.SignedInAs}}</p>
<form method="post" action="/oauth/authorize">
<input type="hidden" name="consent" value="{{.ConsentID}}">
{{if .OffersFiles}}<fieldset><legend>{{.S.Files}}</legend>
<label class="choice"><input type="radio" name="files" value="read" checked><div><b>{{.S.FilesRead}}</b><span>{{.S.FilesReadHint}}</span></div></label>
{{if .OffersWrite}}<label class="choice"><input type="radio" name="files" value="write"><div><b>{{.S.FilesWrite}}</b><span>{{.S.FilesWriteHint}}</span></div></label>{{end}}
</fieldset>{{end}}
{{if .OffersSigning}}<fieldset><legend>{{.S.Signatures}}</legend>
<label class="choice"><input type="checkbox" name="signing" value="on"><div><b>{{.S.SignaturesToggle}}</b><span>{{.S.SignaturesHint}}</span></div></label>
</fieldset>{{end}}
{{if .SeesSigning}}<p class="muted">{{.S.SignaturesStatus}}</p>{{end}}
{{if .OffersFiles}}<p class="muted">{{.S.Vaults}}</p>{{end}}
<div class="actions">
<button class="btn" type="submit" name="decision" value="approve">{{.S.Allow}}</button>
<button class="btn secondary" type="submit" name="decision" value="deny">{{.S.Deny}}</button>
</div>
</form>
{{template "foot" .}}{{end}}{{define "error"}}{{template "head" .}}<p>{{.S.ErrorIntro}}</p>
{{template "foot" .}}{{end}}`))

func newPage(r *http.Request) page {
	lang := "en"
	if first, _, _ := strings.Cut(r.Header.Get("Accept-Language"), ","); strings.HasPrefix(strings.ToLower(strings.TrimSpace(first)), "de") {
		lang = "de"
	}
	return page{S: pageText[lang], Styles: template.CSS(pageStyles)}
}

func (s pageStrings) errorText(code string) string {
	if text, ok := s.Errors[code]; ok {
		return text
	}
	return s.Errors["generic"]
}

// writePage sends one of the pages. redirectURI widens form-action, which
// browsers apply to the whole redirect chain of the consent form.
func writePage(w http.ResponseWriter, status int, name string, data page, redirectURI string) {
	var body bytes.Buffer
	if err := pages.ExecuteTemplate(&body, name, data); err != nil {
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}
	formAction := "'self'"
	if parsed, err := url.Parse(redirectURI); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		formAction += " " + parsed.Scheme + "://" + parsed.Host
	}
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store, max-age=0")
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; form-action "+formAction+"; base-uri 'none'; frame-ancestors 'none'")
	// Not no-referrer: browsers then send "Origin: null" on the form post, which
	// the origin check refuses. same-origin keeps the URL from other sites.
	header.Set("Referrer-Policy", "same-origin")
	header.Set("X-Robots-Tag", "noindex, nofollow")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}
