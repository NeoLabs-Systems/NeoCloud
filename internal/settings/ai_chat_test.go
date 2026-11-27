package settings

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/neorouter"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/llm"
)

const (
	serverKey = "sk-server-secret-key-1234"
	memberKey = "sk-member-own-key-9876"
	memberID  = 1
	otherID   = 2
	imageURL  = "data:image/png;base64,QUJD"
)

type providerCall struct {
	path, key string
	body      map[string]any
}

// fakeProvider is an OpenAI-compatible endpoint, the public reference catalog,
// and an Anthropic and Ollama server, recording what it was sent.
type fakeProvider struct {
	server   *httptest.Server
	calls    []providerCall
	failNext int
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	f := &fakeProvider{}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeProvider) serve(w http.ResponseWriter, r *http.Request) {
	reply := func(status int, body string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch r.URL.Path {
	case "/reference/models":
		reply(200, `{"data":[{"id":"vendor/fake-chat","pricing":{"prompt":"0.0000001"},"architecture":{"input_modalities":["text"]}},
			{"id":"vendor/fake-vision","pricing":{"prompt":"0.000001"},"architecture":{"input_modalities":["text","image"]}},
			{"id":"vendor/fake-big","pricing":{"prompt":"0.00001"},"architecture":{"input_modalities":["text"]}}]}`)
		return
	case "/api/tags":
		reply(200, `{"models":[{"name":"llava","capabilities":["completion","vision"]},{"name":"nomic","capabilities":["embedding"]}]}`)
		return
	case "/api/chat":
		f.calls = append(f.calls, providerCall{path: r.URL.Path, body: body})
		reply(200, `{"model":"llava","message":{"content":"a cat"},"done":true,"prompt_eval_count":3,"eval_count":2}`)
		return
	case "/api/embed":
		f.calls = append(f.calls, providerCall{path: r.URL.Path, body: body})
		reply(200, `{"model":"nomic","embeddings":[[0.5,0.5]],"prompt_eval_count":2}`)
		return
	case "/v1/messages":
		f.calls = append(f.calls, providerCall{path: r.URL.Path, key: r.Header.Get("x-api-key"), body: body})
		reply(200, `{"model":"claude-x","content":[{"type":"text","text":"a dog"}],"usage":{"input_tokens":4,"output_tokens":2}}`)
		return
	}
	if r.Header.Get("x-api-key") == "sk-ant" && r.URL.Path == "/v1/models" {
		reply(200, `{"data":[{"id":"claude-x","display_name":"Claude X","created_at":"2026-01-01T00:00:00Z"}]}`)
		return
	}
	if key != serverKey && key != memberKey {
		reply(401, `{"error":{"message":"Invalid API key"}}`)
		return
	}
	switch r.URL.Path {
	case "/v1/models":
		reply(200, `{"data":[{"id":"fake-chat","created":1700000000},{"id":"fake-vision","created":1700000000},{"id":"fake-big","created":1700000000},{"id":"fake-embed","created":1700000000},{"id":"dall-e-3"}]}`)
	case "/v1/chat/completions":
		if f.failNext > 0 {
			f.failNext--
			reply(503, `{"error":{"message":"overloaded"}}`)
			return
		}
		f.calls = append(f.calls, providerCall{path: r.URL.Path, key: key, body: body})
		content := `Hello from ` + body["model"].(string)
		if body["response_format"] != nil {
			content = "```json\n{\"title\":\"Invoice March\"}\n```"
		}
		answer, _ := json.Marshal(map[string]any{"model": body["model"], "choices": []any{map[string]any{"message": map[string]any{"content": content}}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
		reply(200, string(answer))
	case "/v1/embeddings":
		f.calls = append(f.calls, providerCall{path: r.URL.Path, key: key, body: body})
		var rows []string
		for i := range body["input"].([]any) {
			rows = append(rows, `{"index":`+strconv.Itoa(i)+`,"embedding":[0.25,0.75]}`)
		}
		reply(200, `{"data":[`+strings.Join(rows, ",")+`],"usage":{"prompt_tokens":4}}`)
	default:
		reply(404, `{}`)
	}
}

func (f *fakeProvider) last(t *testing.T) providerCall {
	t.Helper()
	if len(f.calls) == 0 {
		t.Fatal("the provider was not called")
	}
	return f.calls[len(f.calls)-1]
}

type aiFixture struct {
	t        *testing.T
	h        *Handler
	db       *dbx.DB
	mux      *http.ServeMux
	provider *fakeProvider
	enabled  bool
	budget   int64
	server   neorouter.Connection
	own      neorouter.Connection
}

func newAIFixture(t *testing.T) *aiFixture {
	t.Helper()
	base, db := newSettingsTest(t)
	keys, err := cryptox.NewKeyring(strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	f := &aiFixture{t: t, db: db, provider: newFakeProvider(t), enabled: true}
	f.h, err = New(Options{DB: db.W, Keys: keys, ResolvePrincipal: base.resolve,
		ProviderKeys:     map[string]string{"openai-compatible": serverKey},
		ProviderBaseURLs: map[string]string{"openai-compatible": f.provider.server.URL + "/v1"},
		ReferenceURL:     f.provider.server.URL + "/reference/models",
		AIPolicy:         func() (bool, int64) { return f.enabled, f.budget },
		NeoRouter: func(_ context.Context, userID int64) (neorouter.Connection, error) {
			if userID == memberID && f.own.Connected() {
				return f.own, nil
			}
			return f.server, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.mux = http.NewServeMux()
	f.h.Routes(f.mux)
	f.set(memberID, "ai_opt_in", true)
	return f
}

func (f *aiFixture) set(userID int64, key string, value any) {
	f.t.Helper()
	raw, _ := json.Marshal(value)
	if _, err := f.db.W.Exec(`INSERT INTO user_settings(user_id,key,value) VALUES(?,?,?) ON CONFLICT(user_id,key) DO UPDATE SET value=excluded.value`, userID, key, string(raw)); err != nil {
		f.t.Fatal(err)
	}
}

func (f *aiFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.W.Exec(query, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *aiFixture) call(userID int64, method, path, body string, status int) map[string]any {
	f.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), testPrincipalKey{}, Principal{UserID: userID}))
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != status {
		f.t.Fatalf("%s %s status=%d want %d body=%s", method, path, w.Code, status, w.Body)
	}
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	return result
}

func (f *aiFixture) models(userID int64) map[string]map[string]any {
	f.t.Helper()
	byID := map[string]map[string]any{}
	for _, entry := range f.call(userID, "GET", "/api/settings/meta/models", "", 200)["models"].([]any) {
		model := entry.(map[string]any)
		byID[model["id"].(string)] = model
	}
	return byID
}

func (f *aiFixture) roles(userID int64) map[string]string {
	f.t.Helper()
	settings, err := f.h.readAISettings(context.Background(), userID)
	if err != nil {
		f.t.Fatal(err)
	}
	roles, err := f.h.ResolveAIRoles(context.Background(), userID, settings)
	if err != nil {
		f.t.Fatal(err)
	}
	chosen := map[string]string{}
	for role, status := range roles {
		chosen[role] = *status.Model
		if status.RequestedUnavailable {
			chosen[role] += " (fallback)"
		}
	}
	return chosen
}

func user(text string) []llm.Message { return []llm.Message{{Role: "user", Content: text}} }

func TestCatalogVisibilityGrantsAndProviderMetadata(t *testing.T) {
	f := newAIFixture(t)
	f.exec(`UPDATE users SET is_admin=0`)
	models := f.models(memberID)
	chat := models["openai-compatible::fake-chat"]
	if len(models) != 5 || chat["kind"] != "chat" || chat["modelId"] != "fake-chat" || chat["supportsVision"] != false || chat["priceTier"] != "cheap" || chat["purpose"] != "fast" {
		t.Fatalf("models=%v", models)
	}
	if models["openai-compatible::fake-vision"]["supportsVision"] != true || models["openai-compatible::fake-big"]["priceTier"] != "expensive" || models["openai-compatible::fake-embed"]["kind"] != "embedding" {
		t.Fatalf("models=%v", models)
	}
	states := f.call(memberID, "GET", "/api/settings/meta/ai-providers", "", 200)["providers"].([]any)
	byID := map[string]map[string]any{}
	for _, entry := range states {
		byID[entry.(map[string]any)["id"].(string)] = entry.(map[string]any)
	}
	compatible := byID["openai-compatible"]
	if len(states) != 7 || compatible["available"] != true || compatible["status"] != "healthy" || compatible["modelCount"] != 5.0 || compatible["baseUrl"] != "" {
		t.Fatalf("providers=%v", states)
	}
	if byID["ollama"]["status"] != "needs_setup" || byID["neorouter"]["authentication"] != "oauth" {
		t.Fatalf("providers=%v", states)
	}
	f.exec(`UPDATE users SET is_admin=1 WHERE id=?`, otherID)
	for _, entry := range f.call(otherID, "GET", "/api/settings/meta/ai-providers", "", 200)["providers"].([]any) {
		if state := entry.(map[string]any); state["id"] == "openai-compatible" && state["baseUrl"] != f.provider.server.URL+"/v1" {
			t.Fatalf("an admin does not see the operator address: %v", state)
		}
	}

	f.h.SetDisabledModelIDs([]string{"openai-compatible::fake-big"})
	f.exec(`UPDATE users SET allowed_models='["openai-compatible::fake-big"]' WHERE id=?`, memberID)
	if big := f.models(memberID)["openai-compatible::fake-big"]; big["available"] != true || big["adminGranted"] != true {
		t.Fatalf("granted model=%v", big)
	}
	if big := f.models(otherID)["openai-compatible::fake-big"]; big["available"] != false || big["adminGranted"] != false {
		t.Fatalf("ungranted model=%v", big)
	}
	admin, err := f.h.AdminModelCatalog(context.Background())
	if err != nil || len(admin) != 5 {
		t.Fatalf("admin catalog=%v error=%v", admin, err)
	}

	f.set(memberID, "ai_model_smart", "openai-compatible::fake-big")
	if got := f.roles(memberID); got["smart"] != "openai-compatible::fake-big" || got["text"] != "openai-compatible::fake-chat" || got["vision"] != "openai-compatible::fake-vision" || got["embedding"] != "openai-compatible::fake-embed" || got["transcription"] != "" {
		t.Fatalf("roles=%v", got)
	}
	f.exec(`UPDATE users SET allowed_models=NULL WHERE id=?`, memberID)
	if got := f.roles(memberID); got["smart"] != "openai-compatible::fake-vision (fallback)" {
		t.Fatalf("roles=%v", got)
	}
}

func TestModelCallsAreRetriedMeteredAndHoldNoContent(t *testing.T) {
	f := newAIFixture(t)
	ctx := context.Background()
	const secret = "secret prompt words about the tax return"
	text, model, err := f.h.ChatForUserRole(ctx, memberID, "test_chat", "text", user(secret))
	if err != nil || text != "Hello from fake-chat" || model != "openai-compatible::fake-chat" || f.provider.last(t).key != serverKey {
		t.Fatalf("text=%q model=%q error=%v", text, model, err)
	}
	data, _, err := f.h.ChatJSONForUserRole(ctx, memberID, "test_json", "text", user(secret))
	if err != nil || data["title"] != "Invoice March" {
		t.Fatalf("data=%v error=%v", data, err)
	}
	sent := f.provider.last(t).body
	if sent["response_format"].(map[string]any)["type"] != "json_object" || sent["messages"].([]any)[0].(map[string]any)["content"] != aiJSONInstruction {
		t.Fatalf("request=%v", sent)
	}
	picture := []llm.Message{{Role: "user", Content: []any{map[string]any{"type": "text", "text": secret}, map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}}}}}
	if _, model, err = f.h.ChatForUserRole(ctx, memberID, "test_vision", "vision", picture); err != nil || model != "openai-compatible::fake-vision" {
		t.Fatalf("model=%q error=%v", model, err)
	}
	vectors, model, err := f.h.EmbedForUser(ctx, memberID, "test_embed", []string{secret, "second"})
	if err != nil || len(vectors) != 2 || model != "openai-compatible::fake-embed" {
		t.Fatalf("vectors=%v model=%q error=%v", vectors, model, err)
	}
	f.provider.failNext = 1
	if _, _, err = f.h.ChatForUserRole(ctx, memberID, "test_retry", "text", user("hi")); err != nil {
		t.Fatalf("a transient failure was not retried: %v", err)
	}
	var rows int
	var total int
	var recorded string
	if err := f.db.W.QueryRow(`SELECT COUNT(*),SUM(total_tokens),group_concat(feature||'|'||provider||'|'||model||'|'||ok) FROM ai_usage WHERE user_id=?`, memberID).Scan(&rows, &total, &recorded); err != nil {
		t.Fatal(err)
	}
	if rows != 5 || total != 64 || strings.Contains(recorded, "secret") {
		t.Fatalf("rows=%d total=%d recorded=%s", rows, total, recorded)
	}

	// A key the provider refuses is a provider error, recorded by its status.
	f.h.SetServerProviderValue("NEOCLOUD_OPENAI_COMPATIBLE_API_KEY", "sk-revoked")
	f.provider.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"fake-chat"}]}`)
			return
		}
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":{"message":"Invalid key Bearer sk-revoked"}}`)
	})
	_, _, err = f.h.ChatForUserRole(ctx, memberID, "test_fail", "text", user("hi"))
	if AIChatErrorCode(err) != "AI_PROVIDER_ERROR" || AIErrorStatus(err) != 502 || !strings.HasPrefix(AIErrorMessage(err), "The AI provider could not answer: ") || strings.Contains(AIErrorMessage(err), "sk-revoked") {
		t.Fatalf("error=%v message=%q", err, AIErrorMessage(err))
	}
	var code string
	if err := f.db.W.QueryRow(`SELECT error_code FROM ai_usage WHERE feature='test_fail' AND ok=0`).Scan(&code); err != nil || code != "HTTP_401" {
		t.Fatalf("code=%q error=%v", code, err)
	}
}

func TestPolicyOptInAndBudgetStopModelCalls(t *testing.T) {
	f := newAIFixture(t)
	refused := func(userID int64, code string, status int) {
		t.Helper()
		_, _, chatErr := f.h.ChatForUserRole(context.Background(), userID, "test", "text", user("hi"))
		_, _, embedErr := f.h.EmbedForUser(context.Background(), userID, "embed", []string{"hi"})
		for _, err := range []error{chatErr, embedErr} {
			if AIChatErrorCode(err) != code || AIErrorStatus(err) != status {
				t.Fatalf("error=%v status=%d, want %s %d", err, AIErrorStatus(err), code, status)
			}
		}
		if len(f.provider.calls) != 0 {
			t.Fatal("a refused call reached the provider")
		}
	}
	f.enabled = false
	refused(memberID, "AI_DISABLED", 403)
	f.enabled = true
	refused(otherID, "AI_NOT_OPTED_IN", 403)
	f.exec(`INSERT INTO ai_usage(user_id,feature,provider,model,total_tokens,latency_ms,ok) VALUES(?,'x','p','m',100,1,1)`, memberID)
	f.budget = 100
	refused(memberID, "AI_BUDGET_EXCEEDED", 429)
	f.budget = 0
	f.exec(`UPDATE users SET ai_monthly_tokens=50 WHERE id=?`, memberID)
	refused(memberID, "AI_BUDGET_EXCEEDED", 429)
	f.budget = 10
	f.exec(`UPDATE users SET ai_monthly_tokens=0 WHERE id=?`, memberID)
	if _, _, err := f.h.ChatForUserRole(context.Background(), memberID, "test", "text", user("hi")); err != nil {
		t.Fatalf("an exempt account was refused: %v", err)
	}
	f.h.SetDisabledModelIDs([]string{"fake-vision"})
	_, _, err := f.h.ChatForUserRole(context.Background(), memberID, "test", "vision", user("hi"))
	if AIChatErrorCode(err) != "AI_MODELS_UNAVAILABLE" || AIErrorStatus(err) != 503 {
		t.Fatalf("error=%v", err)
	}
}

func TestOwnKeysAreUsedForThatAccountOnly(t *testing.T) {
	f := newAIFixture(t)
	f.h.SetServerProviderValue("NEOCLOUD_OPENAI_BASE_URL", f.provider.server.URL+"/v1")
	f.call(memberID, "PUT", "/api/settings/byok/neorouter", `{"apiKey":"x"}`, 404)
	if body := f.call(memberID, "PUT", "/api/settings/byok/openai", `{}`, 400); body["code"] != "API_KEY_REQUIRED" {
		t.Fatalf("code=%v", body["code"])
	}
	if body := f.call(memberID, "POST", "/api/settings/byok/openai/test", `{"apiKey":"sk-wrong"}`, 200); body["ok"] != false || body["error"] == "" {
		t.Fatalf("test=%v", body)
	}
	f.call(memberID, "PUT", "/api/settings/byok/openai", `{"apiKey":"`+memberKey+`"}`, 200)
	if body := f.call(memberID, "POST", "/api/settings/byok/openai/test", "", 200); body["ok"] != true || body["message"] != "Connected — found 4 model(s)." {
		t.Fatalf("test=%v", body)
	}
	if mine := f.models(memberID)["openai::fake-chat"]; mine["isByok"] != true {
		t.Fatalf("model=%v", mine)
	}
	if _, ok := f.models(otherID)["openai::fake-chat"]; ok {
		t.Fatal("another account sees a model that runs on the member's key")
	}
	f.set(memberID, "ai_model_text", "openai::fake-chat")
	if _, _, err := f.h.ChatForUserRole(context.Background(), memberID, "test", "text", user("hi")); err != nil || f.provider.last(t).key != memberKey {
		t.Fatalf("key=%q error=%v", f.provider.last(t).key, err)
	}
	f.call(memberID, "DELETE", "/api/settings/byok/openai", "", 200)
	if got := f.roles(memberID)["text"]; got != "openai-compatible::fake-chat (fallback)" {
		t.Fatalf("role=%q", got)
	}
}

func TestOwnAddressesMayNotReachTheServerNetwork(t *testing.T) {
	f := newAIFixture(t)
	for _, address := range []string{"http://127.0.0.1:9/v1", "http://169.254.169.254/latest", "http://[::1]:8080", "http://router.local/v1"} {
		if body := f.call(memberID, "PUT", "/api/settings/byok/openai-compatible", `{"apiKey":"sk-x","baseUrl":"`+address+`"}`, 400); body["code"] != "URL_NOT_ALLOWED" {
			t.Fatalf("%s code=%v", address, body["code"])
		}
	}
	if body := f.call(memberID, "PUT", "/api/settings/byok/openai-compatible", `{"apiKey":"sk-x","baseUrl":"file:///etc/passwd"}`, 400); body["code"] != "INVALID_URL" {
		t.Fatalf("code=%v", body["code"])
	}
	if body := f.call(memberID, "PUT", "/api/settings/byok/ollama", `{"baseUrl":"http://localhost:11434"}`, 400); body["code"] != "URL_NOT_ALLOWED" {
		t.Fatalf("code=%v", body["code"])
	}
	if body := f.call(memberID, "POST", "/api/settings/byok/openai-compatible/test", `{"apiKey":"sk-x","baseUrl":"http://10.0.0.1/v1"}`, 400); body["code"] != "URL_NOT_ALLOWED" {
		t.Fatalf("code=%v", body["code"])
	}
	if body := f.call(memberID, "POST", "/api/settings/byok/openai-compatible/test", `{"baseUrl":"https://8.8.8.8/v1"}`, 400); body["code"] != "API_KEY_REQUIRED" {
		t.Fatalf("the saved or server key must not be tried against a typed address: %v", body)
	}
	// The admin's own address is allowed.
	f.call(memberID, "PUT", "/api/settings/byok/openai-compatible", `{"apiKey":"`+memberKey+`","baseUrl":"`+f.provider.server.URL+`/v1"}`, 200)

	// A stored private address is ignored, so the server key never reaches it.
	f.set(otherID, providerConfigsSetting, map[string]any{"openai-compatible": map[string]string{"baseUrl": "http://127.0.0.1:1/v1"}})
	access, err := f.h.providerAccess(context.Background(), otherID)
	if err != nil {
		t.Fatal(err)
	}
	if runtime, _ := access.runtime("openai-compatible"); runtime.BaseURL != f.provider.server.URL+"/v1" || runtime.BYOK {
		t.Fatalf("runtime=%+v", runtime)
	}
}

func TestAnthropicAndOllamaServeVisionAndEmbeddings(t *testing.T) {
	f := newAIFixture(t)
	ctx := context.Background()
	f.h.SetServerProviderValue("NEOCLOUD_ANTHROPIC_API_KEY", "sk-ant")
	f.h.SetServerProviderValue("NEOCLOUD_ANTHROPIC_BASE_URL", f.provider.server.URL)
	f.h.SetServerProviderValue("NEOCLOUD_OLLAMA_URL", f.provider.server.URL)
	picture := []llm.Message{{Role: "user", Content: []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}}}}}

	f.set(memberID, "ai_model_vision", "anthropic::claude-x")
	text, _, err := f.h.ChatForUserRole(ctx, memberID, "test", "vision", picture)
	if sent, _ := json.Marshal(f.provider.last(t).body); err != nil || text != "a dog" || !strings.Contains(string(sent), `"data":"QUJD"`) || f.provider.last(t).key != "sk-ant" {
		t.Fatalf("text=%q error=%v request=%s", text, err, sent)
	}
	f.set(memberID, "ai_model_vision", "ollama::llava")
	text, _, err = f.h.ChatForUserRole(ctx, memberID, "test", "vision", picture)
	if sent, _ := json.Marshal(f.provider.last(t).body); err != nil || text != "a cat" || !strings.Contains(string(sent), `"images":["QUJD"]`) {
		t.Fatalf("text=%q error=%v request=%s", text, err, sent)
	}
	f.set(memberID, "ai_model_embedding", "ollama::nomic")
	vectors, model, err := f.h.EmbedForUser(ctx, memberID, "embed", []string{"x"})
	if err != nil || len(vectors) != 1 || model != "ollama::nomic" {
		t.Fatalf("vectors=%v model=%q error=%v", vectors, model, err)
	}
	if local := f.models(memberID)["ollama::llava"]; local["inputCostPerM"] != 0.0 || local["priceTier"] != "free" || local["providerStatus"] != "healthy" {
		t.Fatalf("local model=%v", local)
	}
}

func TestNeoRouterTakesTheRolesItHandles(t *testing.T) {
	var chatModel, embedModel, usedKey string
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		usedKey = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			data := map[string]string{
				"chat":          `{"id":"auto","name":"Auto (key default: Balanced)"},{"id":"auto:fast","name":"Auto (Fast)"},{"id":"auto:smart","name":"Auto (Smartest)"},{"id":"router-chat","name":"Router Chat","supports_vision":true,"pricing":{"prompt":"0.000002"},"neorouter":{"vision":true,"tools":true}}`,
				"embedding":     `{"id":"auto","name":"Auto"},{"id":"router-embed","name":"Router Embed","pricing":{"prompt":"0.0000001"}}`,
				"transcription": `{"id":"auto","name":"Auto"}`,
			}[r.URL.Query().Get("type")]
			_, _ = io.WriteString(w, `{"data":[`+data+`]}`)
		case "/v1/chat/completions":
			chatModel = body["model"].(string)
			_, _ = io.WriteString(w, `{"model":"served","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
		case "/v1/embeddings":
			embedModel = body["model"].(string)
			_, _ = io.WriteString(w, `{"model":"google-2/embed","data":[{"index":0,"embedding":[1,0]}]}`)
		}
	}))
	defer router.Close()
	f := newAIFixture(t)
	ctx := context.Background()
	f.server = neorouter.Connection{Scope: "server", APIBase: router.URL + "/v1", APIKey: "nr-server", Roles: neorouter.Roles, Trusted: true}

	got := f.roles(memberID)
	want := map[string]string{"text": "neorouter::auto:fast", "smart": "neorouter::auto:smart", "vision": "neorouter::auto:default", "embedding": "neorouter::auto:default", "transcription": "neorouter::auto:default"}
	for role, model := range want {
		if got[role] != model {
			t.Fatalf("roles=%v", got)
		}
	}
	if routed := f.models(memberID)["neorouter::router-chat"]; routed["supportsVision"] != true || routed["label"] != "Router Chat" || routed["priceTier"] != "medium" {
		t.Fatalf("model=%v", routed)
	}
	if _, model, err := f.h.ChatForUserRole(ctx, memberID, "test", "smart", user("hi")); err != nil || model != "neorouter::auto:smart" || chatModel != "auto:smart" || usedKey != "nr-server" {
		t.Fatalf("model=%q sent=%q error=%v", model, chatModel, err)
	}
	if _, _, err := f.h.EmbedForUser(ctx, memberID, "embed", []string{"x"}); err != nil || embedModel != "auto" {
		t.Fatalf("sent=%q error=%v", embedModel, err)
	}
	f.server.Roles = []string{"chat"}
	if got := f.roles(memberID)["embedding"]; got != "openai-compatible::fake-embed" {
		t.Fatalf("embedding role=%q", got)
	}

	// The person's own connection wins and uses their key.
	f.set(memberID, providerConfigsSetting, map[string]any{"neorouter": map[string]string{"baseUrl": router.URL + "/v1"}})
	sealed, err := f.h.keys.EncryptValue(`{"neorouter":"nr-own"}`)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO user_settings(user_id,key,value) VALUES(?,?,?)`, memberID, providerSecretsSetting, sealed)
	f.own = neorouter.Connection{Scope: "user", APIBase: router.URL + "/v1", APIKey: "nr-own", Roles: neorouter.Roles, Trusted: true}
	if _, _, err := f.h.ChatForUserRole(ctx, memberID, "test", "text", user("hi")); err != nil || usedKey != "nr-own" {
		t.Fatalf("key=%q error=%v", usedKey, err)
	}
	// Saving another provider's key keeps the NeoRouter connection.
	f.call(memberID, "PUT", "/api/settings/byok/openai", `{"apiKey":"sk-more"}`, 200)
	configs, secrets, err := f.h.readBYOK(ctx, memberID)
	if err != nil || secrets["neorouter"] != "nr-own" || configs["neorouter"].BaseURL != router.URL+"/v1" {
		t.Fatalf("configs=%v secrets=%v error=%v", configs, secrets, err)
	}
}
