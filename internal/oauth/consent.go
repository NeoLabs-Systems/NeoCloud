package oauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/lockout"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

// Sign-in here is only for the connection: the account is kept in the browser
// session for a few minutes and the browser is not signed in to NeoCloud. An
// existing NeoCloud browser session is used as it is.

const (
	oauthSignInTTL = 15 * time.Minute
	oauthFactorTTL = 10 * time.Minute
)

func authorizationFromValues(values url.Values) AuthorizationRequest {
	return AuthorizationRequest{
		ResponseType: values.Get("response_type"), ClientID: values.Get("client_id"),
		RedirectURI: values.Get("redirect_uri"), State: values.Get("state"),
		Scope: values.Get("scope"), CodeChallenge: values.Get("code_challenge"),
		CodeChallengeMethod: values.Get("code_challenge_method"),
	}
}

func authorizePath(values url.Values) string {
	keys := []string{"response_type", "client_id", "redirect_uri", "state", "scope", "code_challenge", "code_challenge_method"}
	query := url.Values{}
	for _, key := range keys {
		if value, ok := values[key]; ok && len(value) > 0 {
			query.Set(key, value[0])
		}
	}
	return "/oauth/authorize?" + query.Encode()
}

// safeContinue only ever continues to the authorization request.
func safeContinue(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path != "/oauth/authorize" || parsed.RawQuery == "" {
		return "/oauth/authorize"
	}
	return parsed.Path + "?" + parsed.RawQuery
}

func signInPath(continuePath string) string {
	return "/oauth/sign-in?continue=" + url.QueryEscape(continuePath)
}

func (p *Protocol) browserUserID(r *http.Request) (int64, bool) {
	if principal, ok := p.resolve(r); ok && principal.UserID > 0 && !principal.Device {
		return principal.UserID, true
	}
	s := sessionx.FromContext(r.Context())
	if s == nil {
		return 0, false
	}
	id, ok := s.Int64("oauthUserId")
	started, startedOK := s.Int64("oauthSignedInAt")
	if !ok || !startedOK || p.now().Sub(time.UnixMilli(started)) > oauthSignInTTL {
		return 0, false
	}
	var active int
	if err := p.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND disabled_at IS NULL)`, id).Scan(&active); err != nil || active != 1 {
		return 0, false
	}
	return id, true
}

func renderError(w http.ResponseWriter, r *http.Request, status int) {
	data := newPage(r)
	data.Title = data.S.ErrorTitle
	writePage(w, status, "error", data, "")
}

func renderSignIn(w http.ResponseWriter, r *http.Request, status int, continuePath, account, errorCode string) {
	data := newPage(r)
	data.Title, data.Continue, data.Account = data.S.SignInTitle, continuePath, account
	if errorCode != "" {
		data.Error = data.S.errorText(errorCode)
	}
	writePage(w, status, "signIn", data, "")
}

func renderCode(w http.ResponseWriter, r *http.Request, status int, continuePath, errorCode string) {
	data := newPage(r)
	data.Title, data.Continue = data.S.CodeTitle, continuePath
	if errorCode != "" {
		data.Error = data.S.errorText(errorCode)
	}
	writePage(w, status, "code", data, "")
}

func (p *Protocol) authorizationConsent(w http.ResponseWriter, r *http.Request) {
	authorization, err := p.ValidateAuthorizationRequest(r.Context(), authorizationFromValues(r.URL.Query()))
	if err != nil {
		renderError(w, r, http.StatusBadRequest)
		return
	}
	userID, ok := p.browserUserID(r)
	if !ok {
		http.Redirect(w, r, signInPath(authorizePath(r.URL.Query())), http.StatusFound)
		return
	}
	var username string
	if err := p.db.QueryRowContext(r.Context(), `SELECT username FROM users WHERE id=? AND disabled_at IS NULL`, userID).Scan(&username); err != nil {
		renderError(w, r, http.StatusInternalServerError)
		return
	}
	token := make([]byte, 24)
	s := sessionx.FromContext(r.Context())
	if _, err := rand.Read(token); err != nil || s == nil {
		renderError(w, r, http.StatusInternalServerError)
		return
	}
	csrf := base64.RawURLEncoding.EncodeToString(token)
	s.Set("oauthConsent", csrf)
	s.Set("oauthConsentRequest", authorizePath(r.URL.Query()))
	redirect, err := url.Parse(authorization.RedirectURI)
	if err != nil {
		renderError(w, r, http.StatusBadRequest)
		return
	}
	data := newPage(r)
	data.Title, data.ConsentID = data.S.ConsentTitle, csrf
	data.Intro = strings.ReplaceAll(data.S.ConsentIntro, "{host}", redirect.Host)
	data.SignedInAs = strings.ReplaceAll(data.S.SignedInAs, "{user}", username)
	data.OffersFiles = slices.Contains(authorization.Scopes, "files:read") || slices.Contains(authorization.Scopes, "files:write")
	data.OffersWrite = slices.Contains(authorization.Scopes, "files:write")
	data.OffersSigning = slices.Contains(authorization.Scopes, "signing:write")
	data.SeesSigning = slices.Contains(authorization.Scopes, "signing:read")
	writePage(w, http.StatusOK, "consent", data, authorization.RedirectURI)
}

func (p *Protocol) authorizationDecision(w http.ResponseWriter, r *http.Request) {
	s := sessionx.FromContext(r.Context())
	if err := r.ParseForm(); err != nil || s == nil {
		renderError(w, r, http.StatusBadRequest)
		return
	}
	expected := s.String("oauthConsent")
	storedRequest := safeContinue(s.String("oauthConsentRequest"))
	s.Delete("oauthConsent")
	s.Delete("oauthConsentRequest")
	provided := r.PostForm.Get("consent")
	if expected == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		renderError(w, r, http.StatusBadRequest)
		return
	}
	values, err := url.ParseQuery(strings.TrimPrefix(storedRequest, "/oauth/authorize?"))
	if err != nil {
		renderError(w, r, http.StatusBadRequest)
		return
	}
	authorization, err := p.ValidateAuthorizationRequest(r.Context(), authorizationFromValues(values))
	if err != nil {
		renderError(w, r, http.StatusBadRequest)
		return
	}
	userID, ok := p.browserUserID(r)
	if !ok {
		http.Redirect(w, r, signInPath(storedRequest), http.StatusFound)
		return
	}
	// The connection's own sign-in is used once; a browser session stays.
	s.Delete("oauthUserId")
	s.Delete("oauthSignedInAt")
	files := "read"
	if r.PostForm.Get("files") == "write" {
		files = "write"
	}
	scopes := GrantedOAuthScopes(authorization.Scopes, AuthorizationGrant{Files: files, Signing: r.PostForm.Get("signing") == "on"})
	if r.PostForm.Get("decision") != "approve" || len(scopes) == 0 {
		redirectOAuth(w, r, authorization.RedirectURI, map[string]string{"error": "access_denied", "state": authorization.State})
		return
	}
	code, err := p.issueAuthorizationCode(r.Context(), userID, authorization, scopes)
	if err != nil {
		renderError(w, r, http.StatusInternalServerError)
		return
	}
	redirectOAuth(w, r, authorization.RedirectURI, map[string]string{"code": code, "state": authorization.State})
}

type pendingCode struct {
	userID    int64
	challenge string
}

func (p *Protocol) pendingTwoFactor(s *sessionx.Session) (pendingCode, bool) {
	id, ok := s.Int64("oauthPendingUserId")
	started, startedOK := s.Int64("oauthPendingStartedAt")
	challenge := s.String("oauthPendingChallenge")
	if !ok || !startedOK || id < 1 || challenge == "" || p.now().Sub(time.UnixMilli(started)) > oauthFactorTTL {
		return pendingCode{}, false
	}
	return pendingCode{userID: id, challenge: challenge}, true
}

func clearPendingOAuth(s *sessionx.Session) {
	s.Delete("oauthPendingUserId")
	s.Delete("oauthPendingStartedAt")
	s.Delete("oauthPendingChallenge")
}

func (p *Protocol) oauthSignInPage(w http.ResponseWriter, r *http.Request) {
	continuePath := safeContinue(r.URL.Query().Get("continue"))
	s := sessionx.FromContext(r.Context())
	if s == nil {
		renderError(w, r, http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("restart") != "" {
		clearPendingOAuth(s)
	}
	if _, ok := p.browserUserID(r); ok {
		http.Redirect(w, r, continuePath, http.StatusFound)
		return
	}
	if _, pending := p.pendingTwoFactor(s); pending {
		renderCode(w, r, http.StatusOK, continuePath, "")
		return
	}
	renderSignIn(w, r, http.StatusOK, continuePath, "", "")
}

// signInFailure is the status and page code of a refused sign-in.
func signInFailure(w http.ResponseWriter, err error) (int, string) {
	var locked *lockout.LockedError
	if errors.As(err, &locked) {
		w.Header().Set("Retry-After", strconv.Itoa(locked.RetryAfter))
	}
	var coded interface {
		Status() int
		Code() string
	}
	if errors.As(err, &coded) {
		return coded.Status(), coded.Code()
	}
	return http.StatusInternalServerError, "generic"
}

func (p *Protocol) oauthSignIn(w http.ResponseWriter, r *http.Request) {
	s := sessionx.FromContext(r.Context())
	if err := r.ParseForm(); err != nil || s == nil {
		renderError(w, r, http.StatusBadRequest)
		return
	}
	continuePath := safeContinue(r.PostForm.Get("continue"))
	if r.PostForm.Has("code") {
		pending, ok := p.pendingTwoFactor(s)
		if !ok {
			clearPendingOAuth(s)
			renderSignIn(w, r, http.StatusUnauthorized, continuePath, "", "TWO_FACTOR_EXPIRED")
			return
		}
		exhausted, err := p.signIn.VerifySignInCode(r.Context(), pending.userID, pending.challenge, r.PostForm.Get("code"))
		if exhausted {
			clearPendingOAuth(s)
		}
		if err != nil {
			status, code := signInFailure(w, err)
			if exhausted {
				renderSignIn(w, r, status, continuePath, "", "TWO_FACTOR_ATTEMPTS_EXCEEDED")
				return
			}
			renderCode(w, r, status, continuePath, code)
			return
		}
		p.completeOAuthSignIn(s, pending.userID)
		http.Redirect(w, r, continuePath, http.StatusFound)
		return
	}
	account := strings.TrimSpace(r.PostForm.Get("account"))
	userID, secondFactor, err := p.signIn.VerifyPassword(r.Context(), account, r.PostForm.Get("password"))
	if err != nil {
		status, code := signInFailure(w, err)
		renderSignIn(w, r, status, continuePath, account, code)
		return
	}
	if secondFactor {
		challenge := make([]byte, 24)
		if _, err := rand.Read(challenge); err != nil {
			renderError(w, r, http.StatusInternalServerError)
			return
		}
		s.Regenerate()
		s.Set("oauthPendingUserId", userID)
		s.Set("oauthPendingStartedAt", p.now().UnixMilli())
		s.Set("oauthPendingChallenge", base64.RawURLEncoding.EncodeToString(challenge))
		renderCode(w, r, http.StatusOK, continuePath, "")
		return
	}
	p.completeOAuthSignIn(s, userID)
	http.Redirect(w, r, continuePath, http.StatusFound)
}

func (p *Protocol) completeOAuthSignIn(s *sessionx.Session, userID int64) {
	s.Regenerate()
	s.Set("oauthUserId", userID)
	s.Set("oauthSignedInAt", p.now().UnixMilli())
}

func redirectOAuth(w http.ResponseWriter, r *http.Request, redirect string, values map[string]string) {
	target, err := url.Parse(redirect)
	if err != nil {
		renderError(w, r, http.StatusInternalServerError)
		return
	}
	query := target.Query()
	for key, value := range values {
		query.Set(key, value)
	}
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}
