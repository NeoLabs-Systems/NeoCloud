package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
)

type obj = map[string]any

const aiTestKey = "sk-organize-key-5555"

// aiProvider is a fake OpenAI-compatible provider and model catalog.
type aiProvider struct {
	mu       sync.Mutex
	server   *httptest.Server
	chats    []obj
	embeds   []string
	failNext int
	reply    func(kind, prompt, last string) string
	vector   func(string) []float64
}

func (p *aiProvider) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	write := func(status int, value any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(value)
	}
	var request obj
	_ = json.NewDecoder(r.Body).Decode(&request)
	if r.URL.Path == "/reference/models" {
		model := func(id, price string, modes ...string) obj {
			return obj{"id": id, "pricing": obj{"prompt": price}, "architecture": obj{"input_modalities": modes}}
		}
		write(200, obj{"data": []obj{model("vendor/fake-chat", "0.0000001", "text"), model("vendor/fake-vision", "0.000001", "text", "image"), model("vendor/fake-big", "0.00001", "text")}})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+aiTestKey {
		write(401, obj{"error": obj{"message": "Invalid API key"}})
		return
	}
	switch r.URL.Path {
	case "/v1/models":
		var data []obj
		for _, id := range []string{"fake-chat", "fake-vision", "fake-big", "fake-embed"} {
			data = append(data, obj{"id": id, "created": 1700000000})
		}
		write(200, obj{"data": data})
	case "/v1/chat/completions":
		if p.failNext > 0 {
			p.failNext--
			write(503, obj{"error": obj{"message": "overloaded"}})
			return
		}
		p.chats = append(p.chats, request)
		prompt, last := promptOf(request)
		write(200, obj{"model": request["model"], "choices": []obj{{"message": obj{"role": "assistant", "content": p.reply(kindOfPrompt(prompt), prompt, last)}}},
			"usage": obj{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
	case "/v1/embeddings":
		inputs, _ := request["input"].([]any)
		var data []obj
		for index, input := range inputs {
			text, _ := input.(string)
			p.embeds = append(p.embeds, text)
			vector := []float64{0.1, 0.2, float64(index)}
			if p.vector != nil {
				vector = p.vector(text)
			}
			data = append(data, obj{"index": index, "embedding": vector})
		}
		write(200, obj{"data": data, "usage": obj{"prompt_tokens": 4, "total_tokens": 4}})
	default:
		write(404, obj{})
	}
}

func promptOf(request obj) (all, last string) {
	messages, _ := request["messages"].([]any)
	for _, raw := range messages {
		message, _ := raw.(obj)
		text, _ := message["content"].(string)
		if parts, ok := message["content"].([]any); ok {
			for _, part := range parts {
				value, _ := part.(obj)["text"].(string)
				text += value + " "
			}
		}
		all, last = all+text+"\n", text
	}
	return all, last
}

func kindOfPrompt(prompt string) string {
	switch {
	case strings.Contains(prompt, "report facts about it"):
		return "extract"
	case strings.Contains(prompt, "You decide where one file belongs"):
		return "sort"
	case strings.Contains(prompt, "Summarize this file"):
		return "summary"
	}
	return "other"
}

func (p *aiProvider) calls(kind string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	total := 0
	for _, chat := range p.chats {
		if prompt, _ := promptOf(chat); kindOfPrompt(prompt) == kind || kind == "" {
			total++
		}
	}
	return total
}

type aiAccount struct {
	t      *testing.T
	base   string
	client *http.Client
	id     int64
	name   string
}

func (a *aiAccount) send(method, path string, body io.Reader, headers map[string]string, status int) obj {
	a.t.Helper()
	request, err := http.NewRequest(method, a.base+path, body)
	if err != nil {
		a.t.Fatal(err)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := a.client.Do(request)
	if err != nil {
		a.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != status {
		a.t.Fatalf("%s %s = %d, want %d: %s", method, path, response.StatusCode, status, raw)
	}
	var result obj
	_ = json.Unmarshal(raw, &result)
	return result
}

func (a *aiAccount) call(method, path string, body any, status int) obj {
	a.t.Helper()
	var reader io.Reader
	headers := map[string]string{}
	if body != nil {
		raw, _ := json.Marshal(body)
		reader, headers["Content-Type"] = bytes.NewReader(raw), "application/json"
	}
	return a.send(method, path, reader, headers, status)
}

func (a *aiAccount) get(path string) obj { a.t.Helper(); return a.call("GET", path, nil, 200) }

func (a *aiAccount) upload(name string, content []byte, parent, conflict string) obj {
	a.t.Helper()
	query := url.Values{"name": {name}, "parentId": {parent}}
	if conflict != "" {
		query.Set("conflict", conflict)
	}
	return a.send("PUT", "/api/upload?"+query.Encode(), bytes.NewReader(content), map[string]string{"Content-Type": "application/octet-stream"}, 201)
}

func (a *aiAccount) file(name, content string) obj {
	a.t.Helper()
	return a.upload(name, []byte(content), "root", "")
}

func (a *aiAccount) folder(parent, name string) string {
	a.t.Helper()
	return str(a.call("POST", "/api/folders", obj{"parentId": parent, "name": name}, 201), "id")
}

func (a *aiAccount) node(id string) obj { a.t.Helper(); return a.get("/api/nodes/" + id)["node"].(obj) }

func (a *aiAccount) suggestions(query string) []obj {
	a.t.Helper()
	return objects(a.get("/api/ai/suggestions" + query)["items"])
}

func str(value obj, path ...string) string {
	for index, key := range path {
		if index == len(path)-1 {
			result, _ := value[key].(string)
			return result
		}
		value, _ = value[key].(obj)
	}
	return ""
}

func objects(value any) []obj {
	list, _ := value.([]any)
	out := make([]obj, len(list))
	for index, item := range list {
		out[index], _ = item.(obj)
	}
	return out
}

type aiWorld struct {
	t             *testing.T
	app           *Application
	provider      *aiProvider
	server        *httptest.Server
	admin, member *aiAccount
	extraction    func(prompt string) obj
	sortChoice    func(text string) obj
}

var telekom = obj{
	"date": "2026-03-12", "issuer": "Telekom", "subject": "Mobilfunk März", "type": "invoice", "amount": 39.99, "currency": "EUR",
	"tags": []string{"telefon", "rechnung"}, "language": "de", "summary": "Mobile phone bill for March.", "caption": nil,
	"confidence": obj{"date": 0.95, "issuer": 0.95, "subject": 0.9, "type": 0.95, "amount": 0.9, "tags": 0.9}, "overall": 0.93,
}

const telekomText = "Telekom Deutschland GmbH\nRechnung vom 12.03.2026\nMobilfunk März\nBetrag 39,99 EUR"

func with(base obj, changes obj) obj {
	out := obj{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range changes {
		out[key] = value
	}
	return out
}

func newAIWorld(t *testing.T) *aiWorld {
	t.Helper()
	w := &aiWorld{t: t, provider: &aiProvider{}}
	w.extraction = func(string) obj { return telekom }
	w.sortChoice = func(string) obj { return obj{"choice": 0, "confidence": 0.3, "reason": "none fits"} }
	w.provider.reply = func(kind, prompt, last string) string {
		var answer any
		switch kind {
		case "extract":
			answer = w.extraction(prompt)
		case "sort":
			answer = w.sortChoice(last)
		case "summary":
			return "Summary " + strconv.Itoa(len(w.provider.chats))
		default:
			return "hello"
		}
		raw, _ := json.Marshal(answer)
		return string(raw)
	}
	w.provider.server = httptest.NewServer(http.HandlerFunc(w.provider.serve))
	t.Cleanup(w.provider.server.Close)
	home := t.TempDir()
	environment := map[string]string{"NEOCLOUD_HOME": home, "NEOCLOUD_AI_REFERENCE_URL": w.provider.server.URL + "/reference/models"}
	app, err := Open(context.Background(), func(key string) string { return environment[key] }, home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	w.app, w.server = app, httptest.NewServer(app.Handler())
	t.Cleanup(w.server.Close)
	w.admin = w.register("orgadmin")
	w.member = w.register("orgmember")
	w.admin.call("PUT", "/api/admin/providers", obj{"key": "NEOCLOUD_OPENAI_COMPATIBLE_API_KEY", "value": aiTestKey}, 200)
	w.admin.call("PUT", "/api/admin/providers", obj{"key": "NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL", "value": w.provider.server.URL + "/v1"}, 200)
	// The worker starts following the journal from here.
	w.run()
	return w
}

func (w *aiWorld) register(name string) *aiAccount {
	w.t.Helper()
	jar, _ := cookiejar.New(nil)
	account := &aiAccount{t: w.t, base: w.server.URL, client: &http.Client{Jar: jar}, name: name}
	account.call("POST", "/api/auth/register", obj{"username": name, "email": name + "@example.test", "password": "G7!mQv#2xL9rT4pZ"}, 200)
	if err := w.app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username=?`, name).Scan(&account.id); err != nil {
		w.t.Fatal(err)
	}
	account.get("/api/sync/tree")
	return account
}

// enable turns AI on here and opts the member in with the given switches.
func (w *aiWorld) enable(switches obj) {
	w.t.Helper()
	w.admin.call("PUT", "/api/admin/ai", obj{"enabled": true}, 200)
	w.member.call("PUT", "/api/settings", obj{"ai_opt_in": true, "ai_features": switches}, 200)
}

func (w *aiWorld) features(switches obj) {
	w.t.Helper()
	w.member.call("PUT", "/api/settings", obj{"ai_features": switches}, 200)
}

func (w *aiWorld) run() {
	w.t.Helper()
	for {
		done, err := w.app.aiHandler.RunPass(context.Background())
		if err != nil {
			w.t.Fatal(err)
		}
		if done == 0 {
			return
		}
	}
}

func (w *aiWorld) request(body obj) obj {
	w.t.Helper()
	result := w.member.call("POST", "/api/ai/jobs", body, 202)
	w.run()
	return result
}

func (w *aiWorld) count(query string, args ...any) int {
	w.t.Helper()
	var total int
	if err := w.app.Runtime.DB.W.QueryRow(query, args...).Scan(&total); err != nil {
		w.t.Fatal(err)
	}
	return total
}

func (w *aiWorld) job(nodeID string) (status, code string, attempts int) {
	w.t.Helper()
	if err := w.app.Runtime.DB.W.QueryRow(`SELECT status,COALESCE(error_code,''),attempts FROM ai_jobs WHERE node_id=? ORDER BY id DESC LIMIT 1`, nodeID).Scan(&status, &code, &attempts); err != nil {
		w.t.Fatal(err)
	}
	return status, code, attempts
}

func pictureBytes(t *testing.T, encode func(io.Writer, image.Image) error, fill color.Color) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 8, 8))
	draw.Draw(picture, picture.Bounds(), image.NewUniform(fill), image.Point{}, draw.Src)
	var out bytes.Buffer
	if err := encode(&out, picture); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func equalJSON(t *testing.T, got any, want string) {
	t.Helper()
	raw, _ := json.Marshal(got)
	var expected any
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	normalized, _ := json.Marshal(expected)
	var actual any
	_ = json.Unmarshal(raw, &actual)
	again, _ := json.Marshal(actual)
	if string(again) != string(normalized) {
		t.Fatalf("got %s, want %s", again, normalized)
	}
}

func TestAIRenameReviewFlow(t *testing.T) {
	w := newAIWorld(t)
	m := w.member

	// With AI off nothing is queued, even with "on upload" on.
	m.call("PUT", "/api/settings", obj{"ai_opt_in": true, "ai_features": obj{"onUpload": true}}, 200)
	m.file("scan0001.txt", telekomText)
	w.run()
	if w.count(`SELECT COUNT(*) FROM ai_jobs`) != 0 || w.provider.calls("") != 0 {
		t.Fatal("AI off must queue and call nothing")
	}
	if code := str(m.call("POST", "/api/ai/jobs", obj{"folderId": "root"}, 403), "code"); code != "AI_DISABLED" {
		t.Fatal(code)
	}
	w.admin.call("PUT", "/api/admin/ai", obj{"enabled": true}, 200)
	m.call("PUT", "/api/settings", obj{"ai_opt_in": false}, 200)
	if code := str(m.call("POST", "/api/ai/jobs", obj{"folderId": "root"}, 403), "code"); code != "AI_NOT_OPTED_IN" {
		t.Fatal(code)
	}
	m.call("PUT", "/api/settings", obj{"ai_opt_in": true}, 200)

	// An upload with a meaningless name becomes a rename suggestion.
	w.features(obj{"onUpload": true, "tags": false, "folders": false})
	scan := m.file("scan0002.txt", telekomText)
	good := m.file("Holiday plan.txt", "Packing list for the holiday")
	w.run()
	for _, file := range []obj{scan, good} {
		if status, code, _ := w.job(str(file, "id")); status != "done" {
			t.Fatal("job", status, code)
		}
	}
	if w.provider.calls("extract") != 1 {
		t.Fatal("extractions:", w.provider.calls("extract"))
	}
	items := m.suggestions("")
	if len(items) != 1 {
		t.Fatal(items)
	}
	s := items[0]
	if str(s, "kind") != "rename" || str(s, "from") != "scan0002.txt" || str(s, "to") != "2026-03-12 Telekom – Mobilfunk März.txt" ||
		str(s, "tier") != "high" || str(s, "model") != "openai-compatible::fake-chat" || str(s, "node", "id") != str(scan, "id") ||
		str(s, "reason") != "invoice from Telekom, date from content" {
		t.Fatal(s)
	}
	node := m.node(str(scan, "id"))
	if str(node, "aiMeta", "issuer") != "Telekom" || str(node, "aiMeta", "dateSource") != "content" || len(node["aiTags"].([]any)) != 0 {
		t.Fatal(node)
	}
	if w.count(`SELECT COUNT(*) FROM ai_usage WHERE user_id=? AND feature='title'`, m.id) != 1 {
		t.Fatal("usage")
	}
	counts := m.get("/api/ai/suggestions/counts")
	if counts["high"] != float64(1) {
		t.Fatal(counts)
	}

	// Accept, undo, and the conflicts.
	accept := "/api/ai/suggestions/" + str(s, "id")
	accepted := m.call("POST", accept+"/accept", obj{}, 200)["suggestion"].(obj)
	if str(accepted, "status") != "accepted" || str(accepted, "node", "name") != "2026-03-12 Telekom – Mobilfunk März.txt" {
		t.Fatal(accepted)
	}
	m.call("POST", accept+"/accept", obj{}, 409)
	undone := m.call("POST", accept+"/undo", nil, 200)["suggestion"].(obj)
	if str(undone, "status") != "undone" || str(undone, "node", "name") != "scan0002.txt" {
		t.Fatal(undone)
	}
	m.call("POST", accept+"/undo", nil, 409)

	nodeID := str(s, "nodeId")
	again := func() string {
		w.request(obj{"nodeIds": []string{nodeID}, "kinds": []string{"names"}})
		items := m.suggestions("")
		if len(items) != 1 {
			t.Fatal(items)
		}
		return str(items[0], "id")
	}
	second := again()
	edited := m.call("POST", "/api/ai/suggestions/"+second+"/accept", obj{"value": "Telekom März.txt"}, 200)["suggestion"].(obj)
	if str(edited, "to") != "Telekom März.txt" || str(edited, "node", "name") != "Telekom März.txt" {
		t.Fatal(edited)
	}
	m.call("PATCH", "/api/nodes/"+nodeID, obj{"name": "Mine.txt"}, 200)
	if code := str(m.call("POST", "/api/ai/suggestions/"+second+"/undo", nil, 409), "code"); code != "AI_UNDO_CONFLICT" {
		t.Fatal(code)
	}
	third := again()
	m.upload("Mine.txt", []byte(telekomText+"\nnew page"), "root", "replace")
	if code := str(m.call("POST", "/api/ai/suggestions/"+third+"/accept", obj{}, 409), "code"); code != "AI_SUGGESTION_STALE" {
		t.Fatal(code)
	}
	if stale := m.suggestions("?status=stale"); len(stale) == 0 || str(stale[0], "id") != third {
		t.Fatal(stale)
	}
	fourth := again()
	if status := str(m.call("POST", "/api/ai/suggestions/"+fourth+"/reject", nil, 200), "suggestion", "status"); status != "rejected" {
		t.Fatal(status)
	}
	if len(m.suggestions("")) != 0 || str(m.node(nodeID), "name") != "Mine.txt" {
		t.Fatal("rejected suggestion must change nothing")
	}

	// Low confidence is stored but hidden; accept-all-high takes only high ones.
	low := m.file("download (1).txt", "Some letter text")
	high := m.file("download (2).txt", telekomText)
	w.extraction = func(prompt string) obj {
		if strings.Contains(prompt, "Some letter text") {
			return with(telekom, obj{"issuer": "Somebody", "subject": "Letter", "overall": 0.3})
		}
		return telekom
	}
	w.features(obj{"tags": false, "folders": false})
	w.request(obj{"nodeIds": []any{low["id"], high["id"]}, "kinds": []string{"names"}})
	listed := m.get("/api/ai/suggestions")
	if visible := objects(listed["items"]); len(visible) != 1 || str(visible[0], "nodeId") != str(high, "id") || listed["counts"].(obj)["hiddenLow"] != float64(1) {
		t.Fatal(listed)
	}
	if all := m.get("/api/ai/suggestions?tier=all"); all["total"] != float64(2) {
		t.Fatal(all)
	}
	equalJSON(t, m.call("POST", "/api/ai/suggestions/accept-high", obj{}, 200), `{"accepted":1,"failed":[]}`)
	if str(m.node(str(high, "id")), "name") != "2026-03-12 Telekom – Mobilfunk März.txt" || str(m.node(str(low, "id")), "name") != "download (1).txt" {
		t.Fatal("accept-high")
	}
	w.extraction = func(string) obj { return telekom }

	// Auto-apply applies high suggestions at once, still undoable.
	w.features(obj{"folders": false, "tags": false, "autoApplyHighConfidence": true})
	auto := m.file("document (7).txt", strings.Replace(telekomText, "März", "April", 1))
	w.extraction = func(string) obj { return with(telekom, obj{"subject": "Mobilfunk April"}) }
	w.request(obj{"nodeIds": []any{auto["id"]}})
	applied := m.suggestions("?status=applied")
	if len(applied) != 1 || str(applied[0], "nodeId") != str(auto, "id") || str(m.node(str(auto, "id")), "name") != "2026-03-12 Telekom – Mobilfunk April.txt" {
		t.Fatal(applied)
	}
	m.call("POST", "/api/ai/suggestions/"+str(applied[0], "id")+"/undo", nil, 200)
	if str(m.node(str(auto, "id")), "name") != "document (7).txt" {
		t.Fatal("undo of an auto-applied rename")
	}

	// A provider failure retries later instead of failing the job.
	w.features(obj{"folders": false, "tags": false})
	failing := m.file("scan0100.txt", "Another bill text")
	w.provider.failNext = 10
	w.request(obj{"nodeIds": []any{failing["id"]}})
	if status, code, attempts := w.job(str(failing, "id")); status != "pending" || code != "AI_PROVIDER_ERROR" || attempts != 1 {
		t.Fatal(status, code, attempts)
	}
	if w.count(`SELECT COUNT(*) FROM ai_jobs WHERE node_id=? AND run_after>datetime('now')`, failing["id"]) != 1 {
		t.Fatal("no backoff")
	}
	w.provider.failNext = 0
}

func TestAISummariesBudgetAndPhotos(t *testing.T) {
	w := newAIWorld(t)
	m := w.member
	w.enable(obj{})

	// Summaries are cached per content, encrypted and labelled with the model.
	file := m.file("contract.txt", "Mietvertrag zwischen A und B, Beginn 1.1.2026.")
	summary := "/api/ai/files/" + str(file, "id") + "/summary"
	first := m.call("POST", summary, obj{}, 200)
	if first["cached"] != false || !strings.HasPrefix(str(first, "summary"), "Summary") || str(first, "model") != "openai-compatible::fake-big" || first["etag"] != file["etag"] {
		t.Fatal(first)
	}
	chats := w.provider.calls("")
	if second := m.call("POST", summary, obj{}, 200); second["cached"] != true || second["summary"] != first["summary"] || w.provider.calls("") != chats {
		t.Fatal(second)
	}
	changed := m.upload("contract.txt", []byte("Mietvertrag, geändert."), "root", "replace")
	if third := m.call("POST", summary, obj{}, 200); third["cached"] != false || third["etag"] != changed["etag"] {
		t.Fatal(third)
	}
	if m.call("POST", summary, obj{"refresh": true}, 200)["cached"] != false {
		t.Fatal("refresh")
	}
	if w.count(`SELECT COUNT(*) FROM ai_summaries WHERE instr(summary_enc,CAST('Summary' AS BLOB))>0`) != 0 {
		t.Fatal("summary stored as text")
	}
	if w.count(`SELECT COUNT(*) FROM ai_usage WHERE user_id=? AND feature='summary'`, m.id) != 3 {
		t.Fatal("usage")
	}
	w.features(obj{"summaries": false})
	if code := str(m.call("POST", summary, obj{}, 403), "code"); code != "AI_FEATURE_OFF" {
		t.Fatal(code)
	}

	// A used-up budget pauses the queue until there is budget again.
	w.features(obj{"tags": false, "folders": false})
	budget := "/api/admin/users/" + strconv.FormatInt(m.id, 10) + "/ai-budget"
	w.admin.call("PUT", budget, obj{"monthlyTokens": 1}, 200)
	scan := m.file("scan0099.txt", telekomText)
	chats = w.provider.calls("")
	queued := w.request(obj{"nodeIds": []any{scan["id"]}})
	if queued["queued"] != float64(1) || w.provider.calls("") != chats {
		t.Fatal(queued)
	}
	status := m.get("/api/ai/jobs")
	batches := objects(status["batches"])
	if status["paused"] != "AI_BUDGET_EXCEEDED" || status["pending"] != float64(1) || str(batches[0], "id") != str(queued, "batchId") || batches[0]["pending"] != float64(1) {
		t.Fatal(status)
	}
	w.admin.call("PUT", budget, obj{"monthlyTokens": nil}, 200)
	w.run()
	status = m.get("/api/ai/jobs?batchId=" + str(queued, "batchId"))
	batches = objects(status["batches"])
	if status["paused"] != nil || batches[0]["total"] != float64(1) || batches[0]["done"] != float64(1) {
		t.Fatal(status)
	}
	m.call("GET", "/api/ai/jobs?batchId=nope", nil, 404)

	// Images go to the vision model; accepted tags are searchable.
	w.features(obj{"folders": false})
	photo := m.upload("IMG_2041.png", pictureBytes(t, png.Encode, color.RGBA{0x33, 0x66, 0x99, 255}), "root", "")
	w.extraction = func(string) obj {
		return with(telekom, obj{"type": "photo", "subject": "Blue square", "tags": []string{"blau"}})
	}
	w.request(obj{"nodeIds": []any{photo["id"]}, "kinds": []string{"names", "tags"}})
	var vision obj
	for _, chat := range w.provider.chats {
		if chat["model"] == "fake-vision" {
			vision = chat
		}
	}
	if vision == nil || !strings.Contains(jsonOf(vision), "data:image/jpeg;base64,") {
		t.Fatal("the image did not reach the vision model")
	}
	forPhoto := m.suggestions("?nodeId=" + str(photo, "id"))
	kinds := []string{}
	var tags, rename obj
	for _, item := range forPhoto {
		kinds = append(kinds, str(item, "kind"))
		if str(item, "kind") == "tags" {
			tags = item
		} else {
			rename = item
		}
	}
	slices.Sort(kinds)
	if !slices.Equal(kinds, []string{"rename", "tags"}) {
		t.Fatal(forPhoto)
	}
	equalJSON(t, tags["from"], `[]`)
	equalJSON(t, tags["to"], `["blau"]`)
	m.call("POST", "/api/ai/suggestions/"+str(tags, "id")+"/accept", obj{"value": []string{"blau", "Quadrat!"}}, 200)
	equalJSON(t, m.node(str(photo, "id"))["aiTags"], `["blau","quadrat"]`)
	found := objects(m.get("/api/search?q=quadrat")["items"])
	if len(found) != 1 || found[0]["id"] != photo["id"] {
		t.Fatal("search by tag:", found)
	}
	m.call("POST", "/api/ai/suggestions/"+str(rename, "id")+"/reject", nil, 200)
}

func jsonOf(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func (w *aiWorld) vault(parent string) string {
	w.t.Helper()
	return str(w.member.call("POST", "/api/vaults", obj{"parentId": parent, "name": "Tresor",
		"salt": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16)), "iterations": 600000,
		"wrappedKey": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 60))}, 201), "folderId")
}

func TestAISortingRulesProposalsAndNewFolders(t *testing.T) {
	w := newAIWorld(t)
	m := w.member
	w.enable(obj{"names": false, "tags": false})
	finance := m.folder("root", "Finance")
	target := m.folder(finance, "Telekom")
	m.folder("root", "Photos")
	excluded := m.folder("root", "Private")
	m.call("PUT", "/api/ai/folders/"+excluded+"/excluded", obj{"excluded": true}, 200)
	shared := m.folder("root", "Team")
	m.call("POST", "/api/nodes/"+shared+"/members", obj{"recipient": "orgadmin", "permission": "write"}, 201)
	vault := w.vault("root")
	bill := m.file("bill.txt", telekomText)
	folders := func(ids ...any) {
		w.request(obj{"nodeIds": ids, "kinds": []string{"folders"}})
	}
	one := func() obj {
		items := m.suggestions("?kind=move")
		if len(items) != 1 {
			t.Fatal(items)
		}
		return items[0]
	}
	reject := func(item obj) { m.call("POST", "/api/ai/suggestions/"+str(item, "id")+"/reject", nil, 200) }

	// A rule needs no model.
	rule := m.call("POST", "/api/ai/rules", obj{"name": "Telekom bills", "match": obj{"issuer": "telekom", "type": "invoice"}, "targetFolderId": target}, 201)["rule"].(obj)
	if str(rule, "targetPath") != "/Finance/Telekom" {
		t.Fatal(rule)
	}
	ruleID := strconv.Itoa(int(rule["id"].(float64)))
	folders(bill["id"])
	move := one()
	if str(move, "source") != "rule" || str(move, "reason") != "Rule: Telekom bills" || str(move, "tier") != "high" || len(w.provider.embeds) != 0 || w.provider.calls("sort") != 0 {
		t.Fatal(move, w.provider.embeds)
	}
	equalJSON(t, move["to"], `{"id":"`+target+`","path":"/Finance/Telekom"}`)
	equalJSON(t, move["from"], `{"id":"`+str(bill, "parentId")+`","path":"/"}`)
	reject(move)
	m.call("DELETE", "/api/ai/rules/"+ruleID, nil, 200)

	// A clear embedding match decides without the model.
	clear := func(text string) []float64 {
		switch {
		case strings.HasPrefix(text, "Folder /Finance/Telekom"):
			return []float64{1, 0, 0}
		case strings.HasPrefix(text, "Folder "):
			return []float64{0, 1, 0}
		}
		return []float64{1, 0.05, 0}
	}
	w.provider.vector = clear
	folders(bill["id"])
	move = one()
	if str(move, "source") != "ai" || str(move, "to", "id") != target || str(move, "model") != "openai-compatible::fake-embed" || w.provider.calls("sort") != 0 {
		t.Fatal(move)
	}
	for _, text := range w.provider.embeds {
		first, _, _ := strings.Cut(text, "\n")
		if strings.HasPrefix(first, "Folder ") && (strings.Contains(first, "Private") || strings.Contains(first, "Tresor") || strings.Contains(first, "Team")) {
			t.Fatal("profiled a folder AI may not see:", first)
		}
	}
	reject(move)

	// Otherwise the model picks among the allowed folders.
	w.provider.vector = func(string) []float64 { return []float64{0.7, 0.7, 0} }
	offered := ""
	pick := func(suffix string, confidence float64) func(string) obj {
		return func(text string) obj {
			offered = text
			for _, line := range strings.Split(text, "\n") {
				if strings.HasSuffix(line, suffix) {
					number, _, _ := strings.Cut(line, ".")
					choice, _ := strconv.Atoi(number)
					return obj{"choice": choice, "reason": "Phone bills live here", "confidence": confidence}
				}
			}
			return obj{"choice": 0, "confidence": confidence}
		}
	}
	w.sortChoice = pick("/Finance/Telekom", 0.7)
	folders(bill["id"])
	move = one()
	if w.provider.calls("sort") != 1 || str(move, "to", "id") != target || str(move, "tier") != "medium" || str(move, "reason") != "Phone bills live here" {
		t.Fatal(move)
	}
	if strings.Contains(offered, "Private") || strings.Contains(offered, "Tresor") || strings.Contains(offered, "Team") || strings.Contains(offered, "/Photos") || !strings.Contains(offered, ". /Finance\n") {
		t.Fatal(offered)
	}
	accept := "/api/ai/suggestions/" + str(move, "id") + "/accept"
	for value, code := range map[string]string{vault: "AI_TARGET_NOT_ALLOWED", excluded: "AI_TARGET_NOT_ALLOWED", shared: "SHARE_BOUNDARY"} {
		if got := str(m.call("POST", accept, obj{"value": value}, 409), "code"); got != code {
			t.Fatal(got, code)
		}
	}
	done := m.call("POST", accept, obj{}, 200)["suggestion"].(obj)
	if str(done, "node", "parentId") != target {
		t.Fatal(done)
	}
	back := m.call("POST", "/api/ai/suggestions/"+str(move, "id")+"/undo", nil, 200)["suggestion"].(obj)
	if back["node"].(obj)["parentId"] != bill["parentId"] {
		t.Fatal(back)
	}
	m.call("POST", "/api/ai/rules", obj{"name": "x", "match": obj{"type": "invoice"}, "targetFolderId": vault}, 409)
	m.call("POST", "/api/ai/rules", obj{"name": "x", "match": obj{}, "targetFolderId": target}, 400)

	// Three accepted moves of the same kind make a rule proposal.
	w.provider.vector = clear
	for index := range 3 {
		file := m.file("bill-"+strconv.Itoa(index)+".txt", telekomText+"\n"+strconv.Itoa(index))
		folders(file["id"])
		item := one()
		if item["nodeId"] != file["id"] {
			t.Fatal(item)
		}
		m.call("POST", "/api/ai/suggestions/"+str(item, "id")+"/accept", obj{}, 200)
		if got := len(objects(m.get("/api/ai/rules/proposals")["proposals"])); got != index/2 {
			t.Fatal("proposals", got, index)
		}
	}
	proposal := objects(m.get("/api/ai/rules/proposals")["proposals"])[0]
	if str(proposal, "type") != "invoice" || str(proposal, "issuer") != "Telekom" || str(proposal, "targetFolderId") != target || proposal["accepts"] != float64(3) {
		t.Fatal(proposal)
	}
	created := m.call("POST", "/api/ai/rules/proposals/"+strconv.Itoa(int(proposal["id"].(float64))), obj{}, 201)["rule"].(obj)
	equalJSON(t, created["match"], `{"type":"invoice","issuer":"Telekom","extension":null,"namePattern":null}`)
	updated := m.call("PATCH", "/api/ai/rules/"+strconv.Itoa(int(created["id"].(float64))), obj{"enabled": false}, 200)["rule"].(obj)
	if updated["enabled"] != false || len(objects(m.get("/api/ai/rules")["rules"])) != 1 || len(objects(m.get("/api/ai/rules/proposals")["proposals"])) != 0 {
		t.Fatal(updated)
	}
}

func TestAINewFoldersPhotosCleanupAndSkippedTypes(t *testing.T) {
	w := newAIWorld(t)
	m := w.member
	w.enable(obj{"names": false, "tags": false})
	// Folder profiles and file texts all look alike; bare folder names each get
	// their own direction.
	dimensions := map[string]int{}
	w.provider.vector = func(text string) []float64 {
		values := make([]float64, 64)
		if strings.HasPrefix(text, "Folder ") || strings.Contains(text, "\n") {
			values[0], values[1] = 1, 1
			return values
		}
		if _, ok := dimensions[text]; !ok {
			dimensions[text] = 2 + len(dimensions)
		}
		values[dimensions[text]] = 1
		return values
	}
	policy := obj{"date": "2026-02-01", "issuer": "Allianz", "subject": "Hausrat", "type": "contract", "tags": []string{}, "language": "de",
		"summary": "Insurance policy.", "confidence": obj{"date": 0.9, "issuer": 0.9, "subject": 0.9, "type": 0.9, "amount": 0.9, "tags": 0.9}, "overall": 0.9}
	w.extraction = func(string) obj { return policy }
	propose := func(name string, confidence float64) {
		w.sortChoice = func(string) obj {
			return obj{"choice": 0, "newFolder": name, "parent": 0, "confidence": confidence, "reason": "new"}
		}
	}
	three := func(prefix string) []any {
		return []any{m.file(prefix+"1.txt", "Allianz "+prefix+" 1")["id"], m.file(prefix+"2.txt", "Allianz "+prefix+" 2")["id"], m.file(prefix+"3.txt", "Allianz "+prefix+" 3")["id"]}
	}
	request := func(ids []any) { w.request(obj{"nodeIds": ids, "kinds": []string{"folders"}}) }
	moves := func(query string) []obj { return m.suggestions("?kind=move&tier=all" + query) }
	rejectAll := func() {
		for _, item := range moves("") {
			m.call("POST", "/api/ai/suggestions/"+str(item, "id")+"/reject", nil, 200)
		}
	}
	named := func(name string) int {
		return w.count(`SELECT COUNT(*) FROM nodes WHERE user_id=? AND type='folder' AND name=? AND trashed_at IS NULL`, m.id, name)
	}

	// New folders are off by default.
	fotos := m.folder("root", "Fotos")
	arbeit := m.folder("root", "Arbeit")
	propose("Insurances", 0.9)
	request(three("p"))
	if len(moves("")) != 0 {
		t.Fatal(moves(""))
	}

	// A new folder needs three files of one job and is created only on accept.
	w.features(obj{"names": false, "tags": false, "newFolders": true})
	files := three("q")
	request([]any{m.file("q4.txt", "Allianz Police D")["id"]})
	if len(moves("")) != 0 {
		t.Fatal("one file alone must be held")
	}
	request(files)
	listed := moves("")
	if len(listed) != 3 {
		t.Fatal(listed)
	}
	for _, item := range listed {
		to := item["to"].(obj)
		if to["name"] != "Versicherungen" || to["id"] != nil || str(item, "tier") != "medium" {
			t.Fatal(item)
		}
	}
	equalJSON(t, m.call("POST", "/api/ai/suggestions/accept-high", obj{}, 200), `{"accepted":0,"failed":[]}`)
	if named("Versicherungen") != 0 {
		t.Fatal("suggesting must create nothing")
	}
	first, second, third := "/api/ai/suggestions/"+str(listed[0], "id"), "/api/ai/suggestions/"+str(listed[1], "id"), "/api/ai/suggestions/"+str(listed[2], "id")
	accepted := m.call("POST", first+"/accept", obj{}, 200)["suggestion"].(obj)
	var createdID string
	if err := w.app.Runtime.DB.W.QueryRow(`SELECT id FROM nodes WHERE name='Versicherungen' AND created_by_ai=1 AND trashed_at IS NULL`).Scan(&createdID); err != nil {
		t.Fatal(err)
	}
	if str(accepted, "node", "parentId") != createdID {
		t.Fatal(accepted)
	}
	m.call("POST", second+"/accept", obj{}, 200)
	if named("Versicherungen") != 1 {
		t.Fatal("the folder must be reused")
	}
	m.call("POST", second+"/undo", nil, 200)
	if named("Versicherungen") != 1 {
		t.Fatal("still holds a file")
	}
	m.call("POST", first+"/undo", nil, 200)
	if named("Versicherungen") != 0 {
		t.Fatal("the empty AI folder must go")
	}
	w.features(obj{"names": false, "tags": false})
	if code := str(m.call("POST", third+"/accept", obj{}, 409), "code"); code != "AI_NEW_FOLDERS_OFF" {
		t.Fatal(code)
	}
	if !slices.ContainsFunc(moves("&status=stale"), func(item obj) bool { return item["id"] == listed[2]["id"] }) || named("Versicherungen") != 0 {
		t.Fatal("must be stale, nothing created")
	}

	// A failing move leaves no new folder behind.
	w.features(obj{"names": false, "tags": false, "newFolders": true})
	files = three("r")
	request(files)
	item := moves("")[0]
	blocker := m.file("Versicherungen", "x")
	m.call("POST", "/api/ai/suggestions/"+str(item, "id")+"/accept", obj{}, 409)
	if named("Versicherungen") != 0 || str(m.node(str(item, "nodeId")), "parentId") != str(blocker, "parentId") {
		t.Fatal("nothing may move or be made")
	}
	rejectAll()

	// Auto-apply never creates a folder.
	w.features(obj{"names": false, "tags": false, "newFolders": true, "autoApplyHighConfidence": true})
	propose("Policen", 0.99)
	request(three("s"))
	if len(moves("&status=applied")) != 0 || len(moves("")) != 3 || named("Policen") != 0 {
		t.Fatal(moves(""))
	}
	rejectAll()
	w.features(obj{"names": false, "tags": false, "newFolders": true})

	// An existing folder in another language is used instead of a new one.
	bills := m.folder("root", "Rechnungen")
	w.extraction = func(string) obj {
		return with(policy, obj{"type": "invoice", "issuer": "Telekom", "subject": "Mobilfunk"})
	}
	propose("Invoices", 0.8)
	request([]any{m.file("t1.txt", "Telekom Rechnung")["id"]})
	equalJSON(t, moves("")[0]["to"], `{"id":"`+bills+`","path":"/Rechnungen"}`)
	if named("Invoices") != 0 {
		t.Fatal("no duplicate folder")
	}
	rejectAll()

	// Low confidence and "keep it here" give no suggestion.
	doubtful := m.file("u1.txt", "Telekom Rechnung 2")["id"]
	w.sortChoice = func(string) obj { return obj{"choice": 1, "confidence": 0.4} }
	request([]any{doubtful})
	w.sortChoice = func(string) obj { return obj{"choice": 0, "newFolder": nil, "confidence": 0.9} }
	request([]any{doubtful})
	if len(moves("")) != 0 {
		t.Fatal(moves(""))
	}

	// Photos go only into the existing photo tree, by year.
	year := m.folder(fotos, "2026")
	meadow := pictureBytes(t, png.Encode, color.RGBA{0x88, 0xaa, 0x22, 255})
	loose := m.upload("IMG_0001.png", meadow, "root", "")["id"]
	kept := m.upload("IMG_0002.png", meadow, arbeit, "")["id"]
	w.extraction = func(string) obj {
		return with(policy, obj{"type": "photo", "issuer": nil, "subject": "Wiese", "date": "2026-05-01", "caption": "A meadow"})
	}
	propose("Wiese im Mai", 0.99)
	before := w.provider.calls("sort")
	request([]any{loose, kept})
	listed = moves("")
	if len(listed) != 1 || listed[0]["nodeId"] != loose || str(listed[0], "to", "id") != year || w.provider.calls("sort") != before {
		t.Fatal(listed)
	}
	rejectAll()

	// A file in a folder the person made is not moved out of it.
	project := m.folder("root", "Projekt X")
	inside := m.upload("v1.txt", []byte("Telekom Rechnung 3"), project, "")["id"]
	w.extraction = func(string) obj { return with(policy, obj{"type": "invoice", "issuer": "Telekom"}) }
	w.features(obj{"names": false, "tags": false})
	before = w.provider.calls("sort")
	request([]any{inside})
	if len(moves("")) != 0 || w.provider.calls("sort") != before {
		t.Fatal(moves(""))
	}

	// Empty AI folders and recent empty top-level folders can be removed.
	w.features(obj{"names": false, "tags": false, "newFolders": true})
	w.extraction = func(string) obj { return policy }
	propose("Verträge", 0.8)
	files = three("w")
	request(files)
	item = moves("")[0]
	m.call("POST", "/api/ai/suggestions/"+str(item, "id")+"/accept", obj{}, 200)
	var aiFolder string
	if err := w.app.Runtime.DB.W.QueryRow(`SELECT id FROM nodes WHERE name='Verträge' AND trashed_at IS NULL`).Scan(&aiFolder); err != nil {
		t.Fatal(err)
	}
	m.call("PATCH", "/api/nodes/"+str(item, "nodeId"), obj{"parentId": "root"}, 200)
	manual := m.folder("root", "Leer")
	cleanup := m.get("/api/ai/cleanup/folders")
	created := objects(cleanup["aiCreated"])
	if len(created) != 1 || created[0]["id"] != aiFolder || created[0]["path"] != "/Verträge" {
		t.Fatal(cleanup)
	}
	recent := []string{}
	for _, entry := range objects(cleanup["recent"]) {
		recent = append(recent, str(entry, "id"))
	}
	if !slices.Contains(recent, manual) || slices.Contains(recent, aiFolder) || slices.Contains(recent, fotos) {
		t.Fatal(recent)
	}
	result := m.call("POST", "/api/ai/cleanup/folders", obj{"ids": []string{aiFolder, manual, fotos}}, 200)
	equalJSON(t, result, `{"removed":2,"failed":[{"id":"`+fotos+`","code":"NOT_EMPTY_OR_NOT_ALLOWED"}]}`)
	m.call("POST", "/api/ai/cleanup/folders", obj{"ids": []string{}}, 400)
	rejectAll()

	// File types left out of AI are never queued, read or summarized.
	m.call("PUT", "/api/settings", obj{"ai_skip_types": obj{"categories": []string{"images"}, "extensions": []string{"md"}}}, 200)
	box := m.folder("root", "Kiste")
	note := m.upload("note.md", []byte("# Notes"), box, "")["id"]
	text := m.upload("scan0001.txt", []byte("Allianz Brief"), box, "")["id"]
	picture := m.upload("IMG_0009.png", meadow, box, "")["id"]
	m.call("POST", "/api/ai/jobs", obj{"folderId": box, "kinds": []string{"folders"}}, 202)
	pending := func(id any) int {
		return w.count(`SELECT COUNT(*) FROM ai_jobs WHERE node_id=? AND status='pending'`, id)
	}
	if pending(text) != 1 || pending(note) != 0 || pending(picture) != 0 {
		t.Fatal("skipped types were queued")
	}
	w.run()
	if code := str(m.call("POST", "/api/ai/files/"+note.(string)+"/summary", obj{}, 422), "code"); code != "AI_TYPE_SKIPPED" {
		t.Fatal(code)
	}
	request([]any{picture})
	if status, code, _ := w.job(picture.(string)); status != "skipped" || code != "AI_TYPE_SKIPPED" {
		t.Fatal(status, code)
	}
}

func TestAIGuardsAcrossAccountsVaultsAndTokens(t *testing.T) {
	w := newAIWorld(t)
	owner, other := w.admin, w.member
	w.admin.call("PUT", "/api/admin/ai", obj{"enabled": true}, 200)
	for _, account := range []*aiAccount{owner, other} {
		account.call("PUT", "/api/settings", obj{"ai_opt_in": true}, 200)
	}
	file := owner.file("scan0001.txt", "hello")
	box := owner.folder("root", "Private")
	hidden := owner.upload("scan0002.txt", []byte("secret"), box, "")
	owner.call("PUT", "/api/ai/folders/"+box+"/excluded", obj{"excluded": true}, 200)
	saved := w.member
	w.member = owner
	vaultFolder := w.vault("root")
	w.member = saved
	sealed := owner.upload("b3JwaGFu", []byte("NCV1 x"), vaultFolder, "")
	shared := owner.folder("root", "Shared")
	teamFile := owner.upload("scan0003.txt", []byte("team"), shared, "")
	owner.call("POST", "/api/nodes/"+shared+"/members", obj{"recipient": "orgmember", "permission": "write"}, 201)

	for node, code := range map[string]string{str(sealed, "id"): "AI_VAULT", str(hidden, "id"): "AI_EXCLUDED"} {
		if got := str(owner.call("POST", "/api/ai/jobs", obj{"nodeIds": []string{node}}, 403), "code"); got != code {
			t.Fatal(got, code)
		}
		owner.call("POST", "/api/ai/files/"+node+"/summary", obj{}, 403)
	}
	queued := owner.call("POST", "/api/ai/jobs", obj{"folderId": "root", "kinds": []string{"names"}}, 202)
	if int(queued["queued"].(float64)) != w.count(`SELECT COUNT(*) FROM ai_jobs WHERE user_id=?`, owner.id) ||
		w.count(`SELECT COUNT(*) FROM ai_jobs WHERE node_id IN (?,?)`, hidden["id"], sealed["id"]) != 0 ||
		w.count(`SELECT COUNT(*) FROM ai_jobs WHERE node_id=?`, file["id"]) != 1 {
		t.Fatal(queued)
	}
	if code := str(owner.call("POST", "/api/ai/jobs", obj{"folderId": box}, 403), "code"); code != "AI_EXCLUDED" {
		t.Fatal(code)
	}
	if _, err := w.app.Runtime.DB.W.Exec(`DELETE FROM ai_jobs`); err != nil {
		t.Fatal(err)
	}
	// Excluded after it was queued: skipped, never sent.
	late := owner.file("scan0004.txt", "later secret")
	owner.call("POST", "/api/ai/jobs", obj{"nodeIds": []any{late["id"]}, "kinds": []string{"names"}}, 202)
	closed := owner.folder("root", "Box")
	owner.call("PATCH", "/api/nodes/"+str(late, "id"), obj{"parentId": closed}, 200)
	owner.call("PUT", "/api/ai/folders/"+closed+"/excluded", obj{"excluded": true}, 200)
	w.run()
	if status, code, _ := w.job(str(late, "id")); status != "skipped" || code != "AI_EXCLUDED" {
		t.Fatal(status, code)
	}
	if w.provider.calls("") != 0 {
		t.Fatal("a guarded file reached the provider")
	}

	// Another account sees none of it.
	owner.call("POST", "/api/ai/jobs", obj{"nodeIds": []any{file["id"]}, "kinds": []string{"names"}}, 202)
	w.run()
	suggestion := str(owner.suggestions("")[0], "id")
	if other.get("/api/ai/suggestions")["total"] != float64(0) {
		t.Fatal("leaked suggestions")
	}
	other.call("POST", "/api/ai/suggestions/"+suggestion+"/accept", obj{}, 404)
	other.call("POST", "/api/ai/suggestions/"+suggestion+"/reject", nil, 404)
	other.call("POST", "/api/ai/suggestions/"+suggestion+"/undo", nil, 404)
	other.call("PUT", "/api/ai/folders/"+box+"/excluded", obj{"excluded": false}, 404)
	for _, node := range []obj{file, teamFile} {
		other.call("POST", "/api/ai/jobs", obj{"nodeIds": []any{node["id"]}}, 404)
		other.call("POST", "/api/ai/jobs", obj{"folderId": node["parentId"]}, 404)
		other.call("POST", "/api/ai/files/"+str(node, "id")+"/summary", obj{}, 404)
	}
	other.call("POST", "/api/ai/rules", obj{"name": "x", "match": obj{"type": "invoice"}, "targetFolderId": file["parentId"]}, 404)
	rule := owner.call("POST", "/api/ai/rules", obj{"name": "x", "match": obj{"type": "invoice"}, "targetFolderId": "root"}, 201)["rule"].(obj)
	id := strconv.Itoa(int(rule["id"].(float64)))
	other.call("PATCH", "/api/ai/rules/"+id, obj{"enabled": false}, 404)
	other.call("DELETE", "/api/ai/rules/"+id, nil, 404)
	other.call("POST", "/api/ai/duplicates/dismiss", obj{"nodeIds": []any{file["id"], hidden["id"]}}, 404)
	if len(objects(other.get("/api/ai/rules")["rules"])) != 0 || w.count(`SELECT COUNT(*) FROM ai_suggestions WHERE id=? AND status='pending'`, suggestion) != 1 {
		t.Fatal("changed by another account")
	}

	// The switch takes a folder of one's own, a boolean, and not the root.
	if code := str(owner.call("PUT", "/api/ai/folders/"+str(file, "id")+"/excluded", obj{"excluded": true}, 400), "code"); code != "NOT_A_FOLDER" {
		t.Fatal(code)
	}
	if code := str(owner.call("PUT", "/api/ai/folders/root/excluded", obj{"excluded": true}, 400), "code"); code != "ROOT_FOLDER" {
		t.Fatal(code)
	}
	owner.call("PUT", "/api/ai/folders/"+box+"/excluded", obj{"excluded": "yes"}, 400)

	// Desktop tokens don't reach the AI routes; the phone app does.
	for platform, status := range map[string]int{"lite": 403, "android": 200} {
		token := "nc_ai_test_" + platform
		if _, err := w.app.Runtime.DB.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(?,?,?,?)`, owner.id, platform, platform, passhash.DigestToken(token)); err != nil {
			t.Fatal(err)
		}
		device := &aiAccount{t: t, base: w.server.URL, client: &http.Client{}}
		for _, path := range []string{"/api/ai/status", "/api/ai/suggestions", "/api/ai/jobs", "/api/ai/rules", "/api/ai/duplicates"} {
			device.send("GET", path, nil, map[string]string{"Authorization": "Bearer " + token}, status)
		}
	}
	anonymous := &aiAccount{t: t, base: w.server.URL, client: &http.Client{}}
	anonymous.call("GET", "/api/ai/suggestions", nil, 401)
}

func TestAIDuplicates(t *testing.T) {
	w := newAIWorld(t)
	m := w.member
	pattern := func(seed, quality int) []byte {
		picture := image.NewGray(image.Rect(0, 0, 90, 80))
		state := seed
		var cells [72]uint8
		for index := range cells {
			state = (state*1103515245 + 12345) % 2147483648
			cells[index] = uint8(state % 256)
		}
		for y := range 80 {
			for x := range 90 {
				picture.SetGray(x, y, color.Gray{cells[y/10*9+x/10]})
			}
		}
		var out bytes.Buffer
		if err := jpeg.Encode(&out, picture, &jpeg.Options{Quality: quality}); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	m.upload("a.jpg", pattern(7, 95), "root", "")
	m.upload("b.jpg", pattern(7, 60), "root", "")
	m.upload("other.jpg", pattern(99, 90), "root", "")
	m.file("copy1.txt", "same text")
	m.file("copy2.txt", "same text")
	before := m.get("/api/ai/duplicates")
	if before["pending"] != float64(3) || before["hashed"] != float64(0) || before["total"] != float64(3) || len(objects(before["groups"])) != 1 {
		t.Fatal(before)
	}
	hashed := 0
	for {
		done, err := w.app.photos.HashPass(context.Background(), 20)
		if err != nil {
			t.Fatal(err)
		}
		if done == 0 {
			break
		}
		hashed += done
	}
	if hashed != 3 {
		t.Fatal("hashed", hashed)
	}
	after := m.get("/api/ai/duplicates")
	var similar []string
	for _, group := range objects(after["groups"]) {
		if group["kind"] == "similar" {
			for _, item := range objects(group["items"]) {
				similar = append(similar, str(item, "node", "name"))
			}
		}
	}
	slices.Sort(similar)
	if after["pending"] != float64(0) || !slices.Equal(similar, []string{"a.jpg", "b.jpg"}) {
		t.Fatal(after)
	}
	var ids []any
	for _, item := range objects(objects(after["groups"])[0]["items"]) {
		ids = append(ids, item["node"].(obj)["id"])
	}
	m.call("POST", "/api/ai/duplicates/dismiss", obj{"nodeIds": ids[:1]}, 400)
	m.call("POST", "/api/ai/duplicates/dismiss", obj{"nodeIds": ids}, 200)
	if len(objects(m.get("/api/ai/duplicates")["groups"])) != 1 {
		t.Fatal("dismissed group still listed")
	}
}

func TestAIJobsTriggeredByAllUploadPaths(t *testing.T) {
	w := newAIWorld(t)
	w.admin.call("PUT", "/api/admin/ai", obj{"enabled": true}, 200)
	m := w.member
	m.call("PUT", "/api/settings", obj{"ai_opt_in": true}, 200)
	w.features(obj{"onUpload": true, "tags": false, "folders": false})

	var createdIDs []string

	// 1. Resumable upload
	resumableBody := []byte(telekomText)
	session := m.call("POST", "/api/uploads", obj{"parentId": "root", "name": "scan0010.txt", "size": len(resumableBody)}, 201)
	uploadID := str(session, "id")
	m.send("PUT", "/api/uploads/"+uploadID+"/parts/0", bytes.NewReader(resumableBody), map[string]string{"Content-Type": "application/octet-stream"}, 200)
	completed := m.call("POST", "/api/uploads/"+uploadID+"/complete", obj{}, 201)
	createdIDs = append(createdIDs, str(completed, "id"))

	// 2. Copy
	copied := m.call("POST", "/api/nodes/"+str(completed, "id")+"/copy", obj{"targetParentId": "root", "name": "scan0011.txt"}, 201)
	createdIDs = append(createdIDs, str(copied, "id"))

	// 3. Agent API upload
	agentToken := "nca_" + strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(agentToken))
	if _, err := w.app.Runtime.DB.W.Exec(`INSERT OR IGNORE INTO oauth_clients(id,kind,name,redirect_uri) VALUES('agent-test','public','Agent Test','http://127.0.0.1/cb')`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.app.Runtime.DB.W.Exec(`INSERT INTO oauth_access_tokens(client_id,user_id,token_hash,scopes_json,expires_at) VALUES('agent-test',?,?,?,datetime('now','+1 hour'))`,
		m.id, hex.EncodeToString(sum[:]), `["files:read","files:write"]`); err != nil {
		t.Fatal(err)
	}
	anon := &aiAccount{t: t, base: w.server.URL, client: &http.Client{}}
	agentBody, _ := json.Marshal(obj{"path": "/scan0012.txt", "content": telekomText})
	agentCreated := anon.send("POST", "/api/v1/files", bytes.NewReader(agentBody), map[string]string{
		"Authorization": "Bearer " + agentToken,
		"Content-Type":  "application/json",
	}, 201)
	createdIDs = append(createdIDs, str(agentCreated, "id"))

	// 4. WebDAV upload
	davResp := m.call("POST", "/api/account/devices", obj{"name": "ai-dav", "platform": "webdav"}, 201)
	davPass := str(davResp, "token")
	basic := base64.StdEncoding.EncodeToString([]byte(m.name + ":" + davPass))
	anon.send("PUT", "/remote.php/webdav/scan0013.txt", strings.NewReader(telekomText), map[string]string{
		"Authorization": "Basic " + basic,
	}, 201)
	var davID string
	if err := w.app.Runtime.DB.W.QueryRow(`SELECT id FROM nodes WHERE user_id=? AND name='scan0013.txt' AND trashed_at IS NULL`, m.id).Scan(&davID); err != nil {
		t.Fatal(err)
	}
	createdIDs = append(createdIDs, davID)

	// 5. Share upload (admin uploads into a folder owned by member)
	shared := m.folder("root", "SharedUpload")
	m.call("POST", "/api/nodes/"+shared+"/members", obj{"recipient": w.admin.name, "permission": "write"}, 201)
	shareUploaded := w.admin.upload("scan0014.txt", []byte(telekomText), shared, "")
	createdIDs = append(createdIDs, str(shareUploaded, "id"))

	// 6. Import upload (admin imports from a local WebDAV mock)
	w.admin.call("PUT", "/api/settings", obj{"ai_opt_in": true, "ai_features": obj{"onUpload": true, "tags": false, "folders": false}}, 200)
	davSource := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			rw.Header().Set("Content-Type", "application/xml; charset=utf-8")
			rw.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(rw, `<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response><d:href>/dav/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
  <d:response><d:href>/dav/scan0015.txt</d:href><d:propstat><d:prop><d:resourcetype/><d:getcontentlength>`+strconv.Itoa(len(telekomText))+`</d:getcontentlength><d:getcontenttype>text/plain</d:getcontenttype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`)
		case "GET":
			rw.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(rw, telekomText)
		default:
			rw.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer davSource.Close()
	imp := w.admin.call("POST", "/api/imports", obj{
		"source":   "webdav",
		"url":      davSource.URL + "/dav/",
		"username": "u",
		"password": "p",
	}, 201)
	impID := str(imp, "id")
	var importNodeID string
	impCtx, impCancel := context.WithCancel(context.Background())
	defer impCancel()
	go w.app.imports.StartWorker(impCtx)
	var impStatus string
	for range 100 {
		job := w.admin.get("/api/imports/" + impID)
		impStatus = str(job, "status")
		if impStatus == "done" || impStatus == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	impCancel()
	if impStatus != "done" {
		t.Fatalf("import job %q status=%q resp=%v", impID, impStatus, imp)
	}
	if err := w.app.Runtime.DB.W.QueryRow(`SELECT id FROM nodes WHERE user_id=? AND name='scan0015.txt' AND trashed_at IS NULL`, w.admin.id).Scan(&importNodeID); err != nil {
		t.Fatal(err)
	}
	createdIDs = append(createdIDs, importNodeID)

	w.run()
	for _, id := range createdIDs {
		if status, code, _ := w.job(id); status != "done" {
			t.Fatalf("node %s ai_job status=%q code=%q, want done", id, status, code)
		}
	}
}
