package application

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type liveSocket struct {
	t    *testing.T
	conn *websocket.Conn
}

func dialLive(t *testing.T, server *httptest.Server, header http.Header) *liveSocket {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/socket.io/?EIO=4&transport=websocket", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	s := &liveSocket{t, conn}
	s.await("0{")
	s.write("40")
	return s
}

func (s *liveSocket) write(frame string) {
	s.t.Helper()
	if err := s.conn.Write(context.Background(), websocket.MessageText, []byte(frame)); err != nil {
		s.t.Fatal(err)
	}
}

// await reads frames until one starts with prefix and returns it.
func (s *liveSocket) await(prefix string) string {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, data, err := s.conn.Read(ctx)
		if err != nil {
			s.t.Fatalf("waiting for %q: %v", prefix, err)
		}
		if strings.HasPrefix(string(data), prefix) {
			return string(data)
		}
	}
}

func TestLiveSocketDeliversAccountEvents(t *testing.T) {
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()

	register := func(name string) (http.Header, int64) {
		request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(`{"username":"`+name+`","email":"`+name+`@example.test","password":"G7!mQv#2xL9rT4pZ"}`))
		request.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK || len(response.Cookies()) != 1 {
			t.Fatalf("register status=%d", response.StatusCode)
		}
		var id int64
		if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username=?`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		cookie := response.Cookies()[0]
		return http.Header{"Cookie": {cookie.Name + "=" + cookie.Value}}, id
	}
	header, userID := register("live-owner")
	otherHeader, otherID := register("live-other")

	dialLive(t, server, nil).await("44")
	if _, response, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http")+"/socket.io/?EIO=4&transport=websocket",
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://elsewhere.example"}, "Cookie": header["Cookie"]}}); err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("a foreign origin must be refused: %v", err)
	}

	socket := dialLive(t, server, header)
	socket.await("40")
	other := dialLive(t, server, otherHeader)
	other.await("40")

	app.aiHandler.AnnounceSuggestions(context.Background(), userID)
	if frame := socket.await(`42["ai:suggestions"`); !strings.Contains(frame, `"total":0`) {
		t.Fatalf("ai frame %s", frame)
	}
	app.imports.Announce(userID, "import-job", true)
	if frame := socket.await(`42["imports:changed"`); !strings.Contains(frame, `"id":"import-job"`) {
		t.Fatalf("import frame %s", frame)
	}

	// Journal rows written by any process name the folders that changed; a
	// member of a shared folder hears only about what the share covers.
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('live-root',?,NULL,'folder','','r');
		INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('live-shared',?,'live-root','folder','Shared','s'),('live-private',?,'live-root','folder','Private','p')`, userID, userID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(?,'live-shared',?,'read','live-other')`, userID, otherID); err != nil {
		t.Fatal(err)
	}
	other.await(`42["shares:changed"`)
	socket.await(`42["shares:changed"`)
	// The owner's journal is followed once the share is known.
	time.Sleep(1500 * time.Millisecond)
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO changes(user_id,node_id,op,parent_ids_json) VALUES(?,'gone','delete','["live-shared","live-private"]')`, userID); err != nil {
		t.Fatal(err)
	}
	if frame := socket.await(`42["files:changed"`); !strings.Contains(frame, "live-shared") || !strings.Contains(frame, "live-private") {
		t.Fatalf("owner files frame %s", frame)
	}
	if frame := other.await(`42["files:changed"`); !strings.Contains(frame, "live-shared") || strings.Contains(frame, "live-private") {
		t.Fatalf("member files frame %s", frame)
	}

	// Signing out ends the session's live connection.
	logout, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/logout", nil)
	logout.Header.Set("Cookie", header.Get("Cookie"))
	response, err := server.Client().Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	socket.await("41")
}

func TestRealtimeSigningDevicesAndAccountErasure(t *testing.T) {
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()

	doJSON := func(method, path string, body any, headers http.Header) (int, map[string]any, http.Header) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			encoded, _ := json.Marshal(body)
			reader = bytes.NewReader(encoded)
		}
		req, _ := http.NewRequest(method, server.URL+path, reader)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			req.Header[k] = v
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out, resp.Header
	}
	register := func(name string) (http.Header, int64) {
		status, _, respHeaders := doJSON(http.MethodPost, "/api/auth/register", map[string]string{
			"username": name, "email": name + "@example.test", "password": "G7!mQv#2xL9rT4pZ",
		}, nil)
		if status != http.StatusOK {
			t.Fatalf("register %s = %d", name, status)
		}
		var id int64
		if err := app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username=?`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return http.Header{"Cookie": respHeaders["Set-Cookie"]}, id
	}

	ownerHeader, ownerID := register("rt-owner")
	partnerHeader, partnerID := register("rt-partner")

	ownerSocket := dialLive(t, server, ownerHeader)
	ownerSocket.await("40")
	partnerSocket := dialLive(t, server, partnerHeader)
	partnerSocket.await("40")

	// 1. Device pairing (devices:paired) and remote wipe (devices:wiped)
	status, pairOut, _ := doJSON(http.MethodPost, "/api/account/devices/pairing", map[string]string{"name": "My Phone", "platform": "android"}, ownerHeader)
	if status != http.StatusOK {
		t.Fatalf("create pairing = %d %v", status, pairOut)
	}
	status, redeemed, _ := doJSON(http.MethodPost, "/api/devices/pair", map[string]any{"code": pairOut["code"]}, nil)
	if status != http.StatusOK {
		t.Fatalf("redeem pairing = %d %v", status, redeemed)
	}
	if frame := ownerSocket.await(`42["devices:paired"`); !strings.Contains(frame, "My Phone") {
		t.Fatalf("devices:paired frame = %s", frame)
	}
	deviceToken := redeemed["token"].(string)
	deviceID := strconv.FormatInt(int64(redeemed["device"].(map[string]any)["id"].(float64)), 10)

	status, _, _ = doJSON(http.MethodPost, "/api/account/devices/"+deviceID+"/wipe", map[string]bool{"eraseFiles": true}, ownerHeader)
	if status != http.StatusOK {
		t.Fatalf("wipe device = %d", status)
	}
	status, _, _ = doJSON(http.MethodGet, "/api/auth/me", nil, http.Header{"Authorization": {"Bearer " + deviceToken}})
	if status != http.StatusUnauthorized {
		t.Fatalf("wiped token = %d", status)
	}
	if frame := ownerSocket.await(`42["devices:wiped"`); !strings.Contains(frame, deviceID) {
		t.Fatalf("devices:wiped frame = %s", frame)
	}

	// 2. Signing events (signing:updated, signing:inbox, signing:saved)
	app.realtime.Publish(ownerID, "signing:updated", map[string]any{"documentId": "doc-1", "status": "sent"})
	if frame := ownerSocket.await(`42["signing:updated"`); !strings.Contains(frame, `"doc-1"`) {
		t.Fatalf("signing:updated frame = %s", frame)
	}
	app.realtime.Publish(ownerID, "signing:inbox", map[string]any{"documentId": "doc-1", "status": "sent", "counts": map[string]int{"todo": 1}})
	if frame := ownerSocket.await(`42["signing:inbox"`); !strings.Contains(frame, `"todo":1`) {
		t.Fatalf("signing:inbox frame = %s", frame)
	}
	app.realtime.Publish(ownerID, "signing:saved", map[string]any{"documentId": "doc-1", "nodeId": "node-1"})
	if frame := ownerSocket.await(`42["signing:saved"`); !strings.Contains(frame, `"node-1"`) {
		t.Fatalf("signing:saved frame = %s", frame)
	}

	// 3. Account erasure notifies share partners with both shares:changed and files:changed
	status, folderOut, _ := doJSON(http.MethodPost, "/api/folders", map[string]string{"name": "PartnerFolder", "parentId": "root"}, partnerHeader)
	if status != http.StatusCreated {
		t.Fatalf("create folder = %d %v", status, folderOut)
	}
	sharedFolderID := folderOut["id"].(string)
	status, _, _ = doJSON(http.MethodPost, "/api/nodes/"+sharedFolderID+"/members", map[string]string{"recipient": "rt-owner", "permission": "read"}, partnerHeader)
	if status != http.StatusCreated {
		t.Fatalf("share member = %d", status)
	}
	partnerSocket.await(`42["shares:changed"`)
	ownerSocket.await(`42["shares:changed"`)

	status, _, _ = doJSON(http.MethodPost, "/api/account/delete", map[string]string{
		"currentPassword": "G7!mQv#2xL9rT4pZ",
		"confirmUsername": "rt-partner",
	}, partnerHeader)
	if status != http.StatusOK {
		t.Fatalf("delete partner = %d", status)
	}
	ownerSocket.await(`42["shares:changed"`)
	if frame := ownerSocket.await(`42["files:changed"`); !strings.Contains(frame, sharedFolderID) {
		t.Fatalf("erasure files:changed frame = %s (partner %d -> %d)", frame, partnerID, ownerID)
	}
}
