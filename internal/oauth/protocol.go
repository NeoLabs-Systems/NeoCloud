package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

const (
	oauthCodePrefix    = "ncc_"
	oauthAccessPrefix  = "nca_"
	oauthRefreshPrefix = "ncr_"
	oauthCodeTTL       = 10 * time.Minute
	oauthAccessTTL     = time.Hour
	oauthRefreshTTL    = 90 * 24 * time.Hour
)

// ErrInvalidAccessToken indicates that a resource API bearer token is absent,
// malformed, expired, revoked, or no longer authorized by its client/account.
var ErrInvalidAccessToken = errors.New("invalid access token")

type AuthorizationRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	State               string
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod string
}

type Authorization struct {
	ClientID      string
	RedirectURI   string
	State         string
	CodeChallenge string
	Scopes        []string
}

type AuthorizationGrant struct {
	Files   string
	Signing bool
}

// AccessPrincipal describes a verified OAuth access token for resource APIs.
// The raw bearer token is never retained in this value.
type AccessPrincipal struct {
	UserID   int64
	ClientID string
	Scopes   []string
}

// SignIn checks the credentials typed on the connection's sign-in page, under
// the same rules as the app's own sign-in.
type SignIn interface {
	VerifyPassword(ctx context.Context, identifier, password string) (userID int64, secondFactor bool, err error)
	VerifySignInCode(ctx context.Context, userID int64, challenge, code string) (exhausted bool, err error)
}

type ProtocolOptions struct {
	DB               *sql.DB
	ResolvePrincipal ResolvePrincipal
	SignIn           SignIn
	Now              func() time.Time
}

type Protocol struct {
	db            *sql.DB
	resolve       ResolvePrincipal
	now           func() time.Time
	limiter       *ratelimit.Limiter
	signInLimiter *ratelimit.Limiter
	signIn        SignIn
}

type oauthProtocolError struct {
	status int
	code   string
	msg    string
}

func (e oauthProtocolError) Error() string { return e.msg }

func protocolError(status int, code, message string) error {
	return oauthProtocolError{status: status, code: code, msg: message}
}

func NewProtocol(options ProtocolOptions) (*Protocol, error) {
	if options.DB == nil || options.ResolvePrincipal == nil {
		return nil, errors.New("oauth protocol: database and principal resolver are required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Protocol{
		db: options.DB, resolve: options.ResolvePrincipal, now: options.Now,
		limiter:       ratelimit.New(ratelimit.Config{Window: 15 * time.Minute, Max: 300, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}}),
		signInLimiter: ratelimit.New(ratelimit.Config{Window: 15 * time.Minute, Max: 30, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}}),
		signIn:        options.SignIn,
	}, nil
}

// Routes registers the public token, revocation, and userinfo protocol routes.
// Authorization codes are issued by IssueAuthorizationCode after the app has
// shown and validated its user consent UI.
func (p *Protocol) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /oauth/authorize", p.authorizationConsent)
	mux.HandleFunc("POST /oauth/authorize", p.authorizationDecision)
	mux.HandleFunc("GET /oauth/sign-in", p.oauthSignInPage)
	mux.Handle("POST /oauth/sign-in", p.signInLimiter.Middleware(http.HandlerFunc(p.oauthSignIn)))
	mux.Handle("POST /oauth/token", p.limiter.Middleware(http.HandlerFunc(p.token)))
	mux.Handle("POST /oauth/revoke", p.limiter.Middleware(http.HandlerFunc(p.revoke)))
	mux.HandleFunc("GET /oauth/userinfo", p.userinfo)
}

// AuthenticateAccessToken validates an OAuth bearer token against its digest,
// expiry, client, and account state. Resource handlers should check Scopes for
// each operation before returning or mutating account data.
func (p *Protocol) AuthenticateAccessToken(ctx context.Context, token string) (AccessPrincipal, error) {
	if len(token) != len(oauthAccessPrefix)+43 || !strings.HasPrefix(token, oauthAccessPrefix) || !pkceChallengePattern.MatchString(strings.TrimPrefix(token, oauthAccessPrefix)) {
		return AccessPrincipal{}, ErrInvalidAccessToken
	}
	var principal AccessPrincipal
	var scopesJSON string
	var tokenID int64
	var lastUsed sql.NullString
	err := p.db.QueryRowContext(ctx, `SELECT t.id,t.user_id,t.client_id,t.scopes_json,t.last_used_at FROM oauth_access_tokens t JOIN oauth_clients c ON c.id=t.client_id JOIN users u ON u.id=t.user_id WHERE t.token_hash=? AND t.revoked_at IS NULL AND datetime(t.expires_at)>datetime(?) AND c.revoked_at IS NULL AND u.disabled_at IS NULL`, oauthDigest(token), p.now().UTC().Format(time.RFC3339Nano)).Scan(&tokenID, &principal.UserID, &principal.ClientID, &scopesJSON, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return AccessPrincipal{}, ErrInvalidAccessToken
	}
	if err != nil {
		return AccessPrincipal{}, err
	}
	principal.Scopes = parseScopes(scopesJSON)
	if !lastUsed.Valid || parseOAuthTime(lastUsed.String).Before(p.now().Add(-time.Minute)) {
		_, _ = p.db.ExecContext(ctx, `UPDATE oauth_access_tokens SET last_used_at=? WHERE id=?`, p.now().UTC().Format(time.RFC3339Nano), tokenID)
	}
	return principal, nil
}

// ValidateAuthorizationRequest checks the immutable OAuth redirect, state,
// scope, and PKCE values before a consent screen is displayed.
func (p *Protocol) ValidateAuthorizationRequest(ctx context.Context, request AuthorizationRequest) (Authorization, error) {
	if request.ResponseType != "code" {
		return Authorization{}, protocolError(http.StatusBadRequest, "INVALID_REQUEST", "response_type must be code.")
	}
	var redirect string
	err := p.db.QueryRowContext(ctx, `SELECT redirect_uri FROM oauth_clients WHERE id=? AND revoked_at IS NULL`, request.ClientID).Scan(&redirect)
	if errors.Is(err, sql.ErrNoRows) {
		return Authorization{}, protocolError(http.StatusUnauthorized, "INVALID_CLIENT", "Unknown client.")
	}
	if err != nil {
		return Authorization{}, err
	}
	if request.RedirectURI != redirect {
		return Authorization{}, protocolError(http.StatusBadRequest, "INVALID_REQUEST", "redirect_uri does not match this client.")
	}
	if len(request.State) < 8 || len(request.State) > 512 || !statePattern.MatchString(request.State) {
		return Authorization{}, protocolError(http.StatusBadRequest, "INVALID_REQUEST", "state is missing or malformed.")
	}
	if len(request.CodeChallenge) != 43 || !pkceChallengePattern.MatchString(request.CodeChallenge) || request.CodeChallengeMethod != "S256" {
		return Authorization{}, protocolError(http.StatusBadRequest, "INVALID_REQUEST", "PKCE with code_challenge_method=S256 is required.")
	}
	scopes, err := requestedOAuthScopes(request.Scope)
	if err != nil {
		return Authorization{}, err
	}
	return Authorization{ClientID: request.ClientID, RedirectURI: redirect, State: request.State, CodeChallenge: request.CodeChallenge, Scopes: scopes}, nil
}

// GrantedOAuthScopes applies the NeoCloud consent choices to requested scopes.
func GrantedOAuthScopes(requested []string, grant AuthorizationGrant) []string {
	requestedSet := make(map[string]bool, len(requested))
	for _, scope := range requested {
		requestedSet[scope] = true
	}
	granted := make(map[string]bool)
	if requestedSet["files:read"] || requestedSet["files:write"] {
		granted["files:read"] = true
	}
	if grant.Files == "write" && requestedSet["files:write"] {
		granted["files:write"] = true
	}
	if requestedSet["signing:read"] {
		granted["signing:read"] = true
	}
	if grant.Signing && requestedSet["signing:write"] {
		granted["signing:read"] = true
		granted["signing:write"] = true
	}
	out := make([]string, 0, len(granted))
	for _, scope := range []string{"files:read", "files:write", "signing:read", "signing:write"} {
		if granted[scope] {
			out = append(out, scope)
		}
	}
	return out
}

// IssueAuthorizationCode stores a digest-only, short-lived, one-use code.
// The caller must invoke this only after validating the consent session and
// user decision associated with authorization.
func (p *Protocol) IssueAuthorizationCode(r *http.Request, authorization Authorization, scopes []string) (string, error) {
	principal, ok := p.resolve(r)
	if !ok || principal.UserID < 1 {
		return "", protocolError(http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated.")
	}
	if principal.Device {
		return "", protocolError(http.StatusForbidden, "PHONE_REQUIRED", "Use a browser for OAuth consent.")
	}
	return p.issueAuthorizationCode(r.Context(), principal.UserID, authorization, scopes)
}

func (p *Protocol) issueAuthorizationCode(ctx context.Context, userID int64, authorization Authorization, scopes []string) (string, error) {
	if len(scopes) == 0 {
		return "", protocolError(http.StatusBadRequest, "INVALID_REQUEST", "Nothing was granted.")
	}
	allowed := make(map[string]bool, len(authorization.Scopes))
	for _, scope := range authorization.Scopes {
		allowed[scope] = true
	}
	seen := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		_, supported := validScopes[scope]
		if !supported || !allowed[scope] || seen[scope] {
			return "", protocolError(http.StatusBadRequest, "INVALID_REQUEST", "Granted scopes must be requested and supported.")
		}
		seen[scope] = true
	}
	var active int
	if err := p.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND disabled_at IS NULL)`, userID).Scan(&active); err != nil {
		return "", err
	}
	if active == 0 {
		return "", protocolError(http.StatusBadRequest, "INVALID_GRANT", "This account cannot be used.")
	}
	var clientActive int
	var redirect string
	if err := p.db.QueryRowContext(ctx, `SELECT redirect_uri,revoked_at IS NULL FROM oauth_clients WHERE id=?`, authorization.ClientID).Scan(&redirect, &clientActive); errors.Is(err, sql.ErrNoRows) || err == nil && clientActive == 0 {
		return "", protocolError(http.StatusUnauthorized, "INVALID_CLIENT", "Unknown client.")
	} else if err != nil {
		return "", err
	}
	if redirect != authorization.RedirectURI || len(authorization.State) < 8 || len(authorization.State) > 512 || !statePattern.MatchString(authorization.State) || len(authorization.CodeChallenge) != 43 || !pkceChallengePattern.MatchString(authorization.CodeChallenge) {
		return "", protocolError(http.StatusBadRequest, "INVALID_REQUEST", "Authorization request is invalid.")
	}
	code, err := randomOAuthToken(oauthCodePrefix)
	if err != nil {
		return "", err
	}
	expires := p.now().UTC().Add(oauthCodeTTL).Format(time.RFC3339Nano)
	if _, err = p.db.ExecContext(ctx, `INSERT INTO oauth_codes(client_id,user_id,code_hash,redirect_uri,scopes_json,code_challenge,expires_at) VALUES(?,?,?,?,?,?,?)`, authorization.ClientID, userID, oauthDigest(code), authorization.RedirectURI, mustJSON(scopes), authorization.CodeChallenge, expires); err != nil {
		return "", err
	}
	return code, nil
}

func requestedOAuthScopes(value string) ([]string, error) {
	set := map[string]bool{}
	for _, scope := range strings.Fields(value) {
		if _, ok := validScopes[scope]; !ok {
			return nil, protocolError(http.StatusBadRequest, "INVALID_REQUEST", "Unknown scope.")
		}
		set[scope] = true
	}
	if len(set) == 0 {
		for scope := range validScopes {
			set[scope] = true
		}
	}
	out := make([]string, 0, len(set))
	for scope := range set {
		out = append(out, scope)
	}
	sort.Slice(out, func(i, j int) bool { return scopeOrder(out[i]) < scopeOrder(out[j]) })
	return out, nil
}

func scopeOrder(scope string) int {
	for i, value := range []string{"files:read", "files:write", "signing:read", "signing:write"} {
		if value == scope {
			return i
		}
	}
	return len(validScopes)
}

func (p *Protocol) token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	form, err := oauthForm(r)
	if err != nil {
		writeOAuthError(w, protocolError(http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body."))
		return
	}
	clientID := form.Get("client_id")
	var result issuedOAuthTokens
	switch form.Get("grant_type") {
	case "authorization_code":
		result, err = p.exchangeAuthorizationCode(r.Context(), clientID, form.Get("code"), form.Get("redirect_uri"), form.Get("code_verifier"))
	case "refresh_token":
		result, err = p.rotateRefreshToken(r.Context(), clientID, form.Get("refresh_token"))
	default:
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
		return
	}
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"access_token": result.accessToken, "token_type": "Bearer", "expires_in": int64(oauthAccessTTL / time.Second),
		"refresh_token": result.refreshToken, "scope": strings.Join(result.scopes, " "),
	})
}

type issuedOAuthTokens struct {
	accessToken  string
	refreshToken string
	scopes       []string
}

func (p *Protocol) exchangeAuthorizationCode(ctx context.Context, clientID, rawCode, redirectURI, verifier string) (issuedOAuthTokens, error) {
	code := strings.TrimSpace(rawCode)
	return p.withTokenTx(ctx, func(tx *sql.Tx) (issuedOAuthTokens, error) {
		client, err := p.activeClient(ctx, tx, clientID)
		if err != nil {
			return issuedOAuthTokens{}, err
		}
		var row struct {
			id, redirectURI, scopesJSON, challenge string
			userID                                 int64
		}
		err = tx.QueryRowContext(ctx, `SELECT id, user_id, redirect_uri, scopes_json, code_challenge FROM oauth_codes WHERE code_hash=? AND client_id=? AND consumed_at IS NULL AND datetime(expires_at)>datetime(?)`, oauthDigest(code), client, p.now().UTC().Format(time.RFC3339Nano)).Scan(&row.id, &row.userID, &row.redirectURI, &row.scopesJSON, &row.challenge)
		if errors.Is(err, sql.ErrNoRows) || err == nil && (row.redirectURI != redirectURI || !verifyOAuthPKCE(verifier, row.challenge)) {
			return issuedOAuthTokens{}, protocolError(http.StatusBadRequest, "INVALID_GRANT", "The authorization code is invalid or expired.")
		}
		if err != nil {
			return issuedOAuthTokens{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE oauth_codes SET consumed_at=? WHERE id=? AND consumed_at IS NULL`, p.now().UTC().Format(time.RFC3339Nano), row.id); err != nil {
			return issuedOAuthTokens{}, err
		}
		if err = p.assertUserActive(ctx, tx, row.userID); err != nil {
			return issuedOAuthTokens{}, err
		}
		scopes := parseScopes(row.scopesJSON)
		return p.issueTokens(ctx, tx, client, row.userID, scopes)
	})
}

func (p *Protocol) rotateRefreshToken(ctx context.Context, clientID, rawToken string) (issuedOAuthTokens, error) {
	return p.withTokenTx(ctx, func(tx *sql.Tx) (issuedOAuthTokens, error) {
		client, err := p.activeClient(ctx, tx, clientID)
		if err != nil {
			return issuedOAuthTokens{}, err
		}
		var id string
		var userID int64
		var accessID sql.NullInt64
		var scopesJSON string
		err = tx.QueryRowContext(ctx, `SELECT id,user_id,access_token_id,scopes_json FROM oauth_refresh_tokens WHERE token_hash=? AND client_id=? AND revoked_at IS NULL AND datetime(expires_at)>datetime(?)`, oauthDigest(rawToken), client, p.now().UTC().Format(time.RFC3339Nano)).Scan(&id, &userID, &accessID, &scopesJSON)
		if errors.Is(err, sql.ErrNoRows) {
			return issuedOAuthTokens{}, protocolError(http.StatusBadRequest, "INVALID_GRANT", "The refresh token is invalid or expired.")
		}
		if err != nil {
			return issuedOAuthTokens{}, err
		}
		if err = p.assertUserActive(ctx, tx, userID); err != nil {
			return issuedOAuthTokens{}, err
		}
		now := p.now().UTC().Format(time.RFC3339Nano)
		if _, err = tx.ExecContext(ctx, `UPDATE oauth_refresh_tokens SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, now, id); err != nil {
			return issuedOAuthTokens{}, err
		}
		if accessID.Valid {
			if _, err = tx.ExecContext(ctx, `UPDATE oauth_access_tokens SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, now, accessID.Int64); err != nil {
				return issuedOAuthTokens{}, err
			}
		}
		return p.issueTokens(ctx, tx, client, userID, parseScopes(scopesJSON))
	})
}

func (p *Protocol) issueTokens(ctx context.Context, tx *sql.Tx, clientID string, userID int64, scopes []string) (issuedOAuthTokens, error) {
	access, err := randomOAuthToken(oauthAccessPrefix)
	if err != nil {
		return issuedOAuthTokens{}, err
	}
	refresh, err := randomOAuthToken(oauthRefreshPrefix)
	if err != nil {
		return issuedOAuthTokens{}, err
	}
	now := p.now().UTC()
	accessExpiry := now.Add(oauthAccessTTL).Format(time.RFC3339Nano)
	refreshExpiry := now.Add(oauthRefreshTTL).Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `INSERT INTO oauth_access_tokens(client_id,user_id,token_hash,scopes_json,expires_at) VALUES(?,?,?,?,?)`, clientID, userID, oauthDigest(access), mustJSON(scopes), accessExpiry)
	if err != nil {
		return issuedOAuthTokens{}, err
	}
	accessID, err := result.LastInsertId()
	if err != nil {
		return issuedOAuthTokens{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO oauth_refresh_tokens(access_token_id,client_id,user_id,token_hash,scopes_json,expires_at) VALUES(?,?,?,?,?,?)`, accessID, clientID, userID, oauthDigest(refresh), mustJSON(scopes), refreshExpiry); err != nil {
		return issuedOAuthTokens{}, err
	}
	return issuedOAuthTokens{accessToken: access, refreshToken: refresh, scopes: scopes}, nil
}

func (p *Protocol) withTokenTx(ctx context.Context, fn func(*sql.Tx) (issuedOAuthTokens, error)) (issuedOAuthTokens, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return issuedOAuthTokens{}, err
	}
	defer tx.Rollback()
	result, err := fn(tx)
	if err != nil {
		return issuedOAuthTokens{}, err
	}
	if err = tx.Commit(); err != nil {
		return issuedOAuthTokens{}, err
	}
	return result, nil
}

func (p *Protocol) activeClient(ctx context.Context, tx *sql.Tx, clientID string) (string, error) {
	var active int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM oauth_clients WHERE id=? AND revoked_at IS NULL)`, clientID).Scan(&active)
	if err != nil {
		return "", err
	}
	if active == 0 {
		return "", protocolError(http.StatusUnauthorized, "INVALID_CLIENT", "Unknown client.")
	}
	return clientID, nil
}

func (p *Protocol) assertUserActive(ctx context.Context, tx *sql.Tx, userID int64) error {
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND disabled_at IS NULL)`, userID).Scan(&active); err != nil {
		return err
	}
	if active == 0 {
		return protocolError(http.StatusBadRequest, "INVALID_GRANT", "This account cannot be used.")
	}
	return nil
}

func (p *Protocol) revoke(w http.ResponseWriter, r *http.Request) {
	form, err := oauthForm(r)
	if err != nil {
		writeOAuthError(w, protocolError(http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body."))
		return
	}
	clientID, token := form.Get("client_id"), form.Get("token")
	tx, err := p.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = p.activeClient(r.Context(), tx, clientID); err != nil {
		writeOAuthError(w, err)
		return
	}
	if token != "" {
		now := p.now().UTC().Format(time.RFC3339Nano)
		var accessID sql.NullInt64
		_ = tx.QueryRowContext(r.Context(), `SELECT access_token_id FROM oauth_refresh_tokens WHERE client_id=? AND token_hash=?`, clientID, oauthDigest(token)).Scan(&accessID)
		if _, err = tx.ExecContext(r.Context(), `UPDATE oauth_refresh_tokens SET revoked_at=? WHERE client_id=? AND token_hash=? AND revoked_at IS NULL`, now, clientID, oauthDigest(token)); err == nil {
			_, err = tx.ExecContext(r.Context(), `UPDATE oauth_access_tokens SET revoked_at=? WHERE client_id=? AND token_hash=? AND revoked_at IS NULL`, now, clientID, oauthDigest(token))
		}
		if err == nil && accessID.Valid {
			_, err = tx.ExecContext(r.Context(), `UPDATE oauth_access_tokens SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, now, accessID.Int64)
		}
		if err != nil {
			writeOAuthError(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeOAuthError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{})
}

func (p *Protocol) userinfo(w http.ResponseWriter, r *http.Request) {
	token := bearerOAuthToken(r.Header.Get("Authorization"))
	if token == "" {
		w.Header().Set("WWW-Authenticate", "Bearer")
		httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized", "code": "UNAUTHORIZED"})
		return
	}
	var userID int64
	var username string
	var email sql.NullString
	var scopesJSON string
	var tokenID int64
	var lastUsed sql.NullString
	err := p.db.QueryRowContext(r.Context(), `SELECT t.id,t.user_id,u.username,u.email,t.scopes_json,t.last_used_at FROM oauth_access_tokens t JOIN oauth_clients c ON c.id=t.client_id JOIN users u ON u.id=t.user_id WHERE t.token_hash=? AND t.revoked_at IS NULL AND datetime(t.expires_at)>datetime(?) AND c.revoked_at IS NULL AND u.disabled_at IS NULL`, oauthDigest(token), p.now().UTC().Format(time.RFC3339Nano)).Scan(&tokenID, &userID, &username, &email, &scopesJSON, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized", "code": "UNAUTHORIZED"})
		return
	}
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	if !lastUsed.Valid || parseOAuthTime(lastUsed.String).Before(p.now().Add(-time.Minute)) {
		_, _ = p.db.ExecContext(r.Context(), `UPDATE oauth_access_tokens SET last_used_at=? WHERE id=?`, p.now().UTC().Format(time.RFC3339Nano), tokenID)
	}
	scopes := parseScopes(scopesJSON)
	var emailValue any
	if email.Valid {
		emailValue = email.String
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sub": fmt.Sprint(userID), "preferred_username": username, "email": emailValue, "scope": strings.Join(scopes, " ")})
}

func writeOAuthError(w http.ResponseWriter, err error) {
	var protocol oauthProtocolError
	if errors.As(err, &protocol) {
		if protocol.code == "INVALID_CLIENT" {
			httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client", "error_description": protocol.msg})
			return
		}
		if protocol.code == "INVALID_GRANT" {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": protocol.msg})
			return
		}
		if protocol.code == "INVALID_REQUEST" {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": protocol.msg})
			return
		}
		if protocol.code == "UNAUTHORIZED" {
			httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized", "code": "UNAUTHORIZED"})
			return
		}
		if protocol.code == "PHONE_REQUIRED" {
			httpx.WriteJSON(w, http.StatusForbidden, map[string]string{"error": protocol.msg, "code": "PHONE_REQUIRED"})
			return
		}
	}
	httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error", "error_description": "The OAuth request could not be completed."})
}

func oauthForm(r *http.Request) (url.Values, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, errors.New("OAuth request body exceeds limit")
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType == "application/json" {
		var values map[string]any
		if err := json.Unmarshal(body, &values); err != nil {
			return nil, err
		}
		if values == nil {
			return nil, errors.New("OAuth request body must be an object")
		}
		form := make(url.Values)
		for key, value := range values {
			if value == nil {
				form.Set(key, "")
			} else {
				form.Set(key, fmt.Sprint(value))
			}
		}
		return form, nil
	}
	if mediaType != "application/x-www-form-urlencoded" && len(body) > 0 {
		return nil, errors.New("unsupported OAuth request content type")
	}
	return url.ParseQuery(string(body))
}

func bearerOAuthToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func oauthDigest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func randomOAuthToken(prefix string) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func verifyOAuthPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	digest := sha256.Sum256([]byte(verifier))
	actual := base64.RawURLEncoding.EncodeToString(digest[:])
	return len(actual) == len(challenge) && subtle.ConstantTimeCompare([]byte(actual), []byte(challenge)) == 1
}

func mustJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func parseOAuthTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

var statePattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
var pkceChallengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
