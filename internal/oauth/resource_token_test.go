package oauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAuthenticateAccessTokenReturnsPrincipalAndScopes(t *testing.T) {
	_, db, now := testHandler(t)
	if _, err := db.W.Exec(`INSERT INTO oauth_clients(id,kind,name,redirect_uri) VALUES('resource-client','companion:neoagent','NeoAgent','http://localhost:3333/callback')`); err != nil {
		t.Fatal(err)
	}
	const token = "nca_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	if len(token) != len(oauthAccessPrefix)+43 {
		t.Fatalf("bad test token length %d", len(token))
	}
	expires := now.Add(time.Hour).Format(time.RFC3339Nano)
	if _, err := db.W.Exec(`INSERT INTO oauth_access_tokens(client_id,user_id,token_hash,scopes_json,expires_at) VALUES('resource-client',1,?,?,?)`, oauthDigest(token), `["files:read","signing:read"]`, expires); err != nil {
		t.Fatal(err)
	}
	protocol, err := NewProtocol(ProtocolOptions{DB: db.W, Now: func() time.Time { return now }, ResolvePrincipal: resolveTestPrincipal})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := protocol.AuthenticateAccessToken(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.UserID != 1 || principal.ClientID != "resource-client" || len(principal.Scopes) != 2 || principal.Scopes[0] != "files:read" || principal.Scopes[1] != "signing:read" {
		t.Fatalf("principal = %#v", principal)
	}
	var hash, lastUsed string
	if err = db.W.QueryRow(`SELECT token_hash,last_used_at FROM oauth_access_tokens WHERE client_id='resource-client'`).Scan(&hash, &lastUsed); err != nil {
		t.Fatal(err)
	}
	if hash != oauthDigest(token) || hash == token || lastUsed == "" {
		t.Fatalf("token persistence hash=%q last_used=%q", hash, lastUsed)
	}
}

func TestAuthenticateAccessTokenRejectsExpiredRevokedDisabledAndMalformedTokens(t *testing.T) {
	_, db, now := testHandler(t)
	if _, err := db.W.Exec(`INSERT INTO oauth_clients(id,kind,name,redirect_uri) VALUES('resource-client','companion:neoagent','NeoAgent','http://localhost:3333/callback')`); err != nil {
		t.Fatal(err)
	}
	const token = "nca_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	expires := now.Add(time.Hour).Format(time.RFC3339Nano)
	if _, err := db.W.Exec(`INSERT INTO oauth_access_tokens(client_id,user_id,token_hash,scopes_json,expires_at) VALUES('resource-client',1,?,?,?)`, oauthDigest(token), `["files:read"]`, expires); err != nil {
		t.Fatal(err)
	}
	protocol, err := NewProtocol(ProtocolOptions{DB: db.W, Now: func() time.Time { return now }, ResolvePrincipal: resolveTestPrincipal})
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", "nca_short", "ncc_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNO"} {
		_, err = protocol.AuthenticateAccessToken(context.Background(), invalid)
		if err == nil {
			t.Fatalf("accepted malformed token %q", invalid)
		}
		if !errors.Is(err, ErrInvalidAccessToken) {
			t.Fatalf("malformed token error = %v", err)
		}
	}
	if _, err = db.W.Exec(`UPDATE oauth_access_tokens SET expires_at=?`, now.Add(-time.Second).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = protocol.AuthenticateAccessToken(context.Background(), token); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("expired token error = %v", err)
	}
	if _, err = db.W.Exec(`UPDATE oauth_access_tokens SET expires_at=?,revoked_at=?`, expires, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = protocol.AuthenticateAccessToken(context.Background(), token); err == nil {
		t.Fatal("accepted revoked token")
	}
	if _, err = db.W.Exec(`UPDATE oauth_access_tokens SET revoked_at=NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`UPDATE users SET disabled_at=? WHERE id=1`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = protocol.AuthenticateAccessToken(context.Background(), token); err == nil {
		t.Fatal("accepted token for disabled account")
	}
}
