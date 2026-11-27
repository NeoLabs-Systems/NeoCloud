package activity_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/activity"
	"github.com/NeoLabs-Systems/neocloud/internal/apptest"
)

func TestRecordFoldsBurstsAndSkipsVaults(t *testing.T) {
	s := apptest.Start(t, nil)
	owner := s.Register(t, "owner")
	folder := owner.Folder(t, "root", "Team")
	if _, err := s.DB.Exec(`DELETE FROM activity`); err != nil {
		t.Fatal(err)
	}
	node := func(id, name string, vault any) string {
		if _, err := s.DB.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag,vault_id) VALUES(?,?,?,'file',?,?,?)`, id, owner.ID, folder, name, id, vault); err != nil {
			t.Fatal(err)
		}
		return id
	}
	record := func(kind, id string, detail map[string]any) {
		if err := activity.Record(context.Background(), s.DB, activity.Event{OwnerID: owner.ID, Kind: kind, NodeID: id, Detail: detail}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a", "b", "c", "d"} {
		record("upload", node(name, name+".txt", nil), nil)
	}
	record("rename", "a", map[string]any{"from": "old.txt"})
	record("rename", "b", map[string]any{"from": "older.txt"})
	record("upload", node("hidden", "SGlkZGVu", "vault-1"), nil)
	items := owner.Call(t, 200, "GET", "/api/activity?scope=mine", nil)["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items %v", items)
	}
	upload := items[2].(map[string]any)
	if upload["count"] != 4.0 || fmt.Sprint(upload["names"]) != "[a.txt b.txt c.txt]" || upload["folder"] != "Team" {
		t.Fatalf("burst %v", upload)
	}
	if rename := items[0].(map[string]any); rename["count"] != 1.0 || rename["from"] != "older.txt" {
		t.Fatalf("rename %v", rename)
	}
	// Outside the window a new entry starts.
	if _, err := s.DB.Exec(`UPDATE activity SET at=datetime('now','-6 minutes') WHERE kind='upload'`); err != nil {
		t.Fatal(err)
	}
	record("upload", node("e", "e.txt", nil), nil)
	if got := owner.Call(t, 200, "GET", "/api/activity?scope=mine", nil)["items"].([]any); len(got) != 4 || got[0].(map[string]any)["count"] != 1.0 {
		t.Fatalf("after the window %v", got)
	}
	if err := activity.Record(context.Background(), s.DB, activity.Event{OwnerID: owner.ID, Kind: "nonsense", NodeID: "a"}); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestFeedVisibility(t *testing.T) {
	s := apptest.Start(t, nil)
	owner, writer, reader, outsider := s.Register(t, "owner"), s.Register(t, "writer"), s.Register(t, "reader"), s.Register(t, "outsider")
	team := owner.Folder(t, "root", "Team")
	owner.Expect(t, 201, "POST", "/api/nodes/"+team+"/members", map[string]any{"recipient": "writer", "permission": "write"})
	grant := owner.Call(t, 201, "POST", "/api/nodes/"+team+"/members", map[string]any{"recipient": "reader"})
	feed := func(c *apptest.Client, query string) []map[string]any {
		t.Helper()
		var out []map[string]any
		for _, item := range c.Call(t, 200, "GET", "/api/activity"+query, nil)["items"].([]any) {
			out = append(out, item.(map[string]any))
		}
		return out
	}
	kinds := func(items []map[string]any) string {
		var out []string
		for _, item := range items {
			out = append(out, item["kind"].(string))
		}
		return strings.Join(out, ",")
	}
	find := func(items []map[string]any, kind string) map[string]any {
		for _, item := range items {
			if item["kind"] == kind {
				return item
			}
		}
		t.Fatalf("no %s in %v", kind, items)
		return nil
	}
	writer.Upload(t, team, "a.txt", "a")
	entry := find(feed(owner, "?scope=mine"), "upload")
	if entry["actor"].(map[string]any)["name"] != "writer" || entry["folder"] != "Team" {
		t.Fatalf("member write %v", entry)
	}
	for _, item := range feed(writer, "?scope=mine") {
		if item["kind"] == "upload" {
			t.Fatal("a member write landed in the member's tree")
		}
	}
	if find(feed(writer, "?scope=shared"), "upload")["actor"].(map[string]any)["self"] != true {
		t.Fatal("writer is not self")
	}
	if folder, present := find(feed(reader, "?nodeId="+team), "share")["folder"]; present {
		t.Fatalf("names what sits above the share: %v", folder)
	}
	if _, present := find(feed(owner, "?nodeId="+team), "folder")["folder"]; present {
		t.Fatal("the top of an account has a name")
	}

	file := owner.Upload(t, team, "Contract.pdf", "v1")
	owner.Expect(t, 200, "PATCH", "/api/nodes/"+file, map[string]any{"name": "Contract final.pdf"})
	inner := owner.Folder(t, team, "Inner")
	owner.Expect(t, 200, "PATCH", "/api/nodes/"+file, map[string]any{"parentId": inner})
	owner.Do(t, "PUT", "/api/upload?conflict=replace&name=Contract%20final.pdf&parentId="+inner, []byte("v2"))
	owner.Expect(t, 200, "DELETE", "/api/nodes/"+file, nil)
	owner.Expect(t, 200, "POST", "/api/trash/"+file+"/restore", nil)
	history := feed(owner, "?nodeId="+file)
	if got := kinds(history); got != "restore,trash,version,move,rename,upload" {
		t.Fatalf("history %s", got)
	}
	if find(history, "rename")["from"] != "Contract.pdf" {
		t.Fatal("rename without old name")
	}

	secret := owner.Folder(t, "root", "Private")
	owner.Upload(t, secret, "diary.txt", "x")
	for _, item := range feed(reader, "?scope=shared") {
		if name := item["node"].(map[string]any)["name"]; name == "diary.txt" || name == "Private" {
			t.Fatalf("private entry visible: %v", item)
		}
	}
	if items := feed(outsider, ""); len(items) != 0 {
		t.Fatalf("outsider sees %v", items)
	}
	reader.Expect(t, 404, "GET", "/api/activity?nodeId="+secret, nil)
	outsider.Expect(t, 404, "GET", "/api/activity?nodeId="+team, nil)
	outsider.Expect(t, 404, "GET", "/api/activity?nodeId="+file, nil)
	s.Visitor(t).Expect(t, 401, "GET", "/api/activity", nil)
	if got := owner.Device(t, "linux").Call(t, 403, "GET", "/api/activity", nil)["code"]; got != "PHONE_REQUIRED" {
		t.Fatal(got)
	}
	owner.Device(t, "android").Expect(t, 200, "GET", "/api/activity", nil)

	shares := 0
	for _, item := range feed(reader, "?nodeId="+team) {
		if item["kind"] == "share" {
			shares++
			if item["member"].(map[string]any)["self"] != true {
				t.Fatalf("another member's share: %v", item)
			}
		}
	}
	if shares != 1 {
		t.Fatalf("reader sees %d share entries", shares)
	}

	wander := owner.Upload(t, team, "wander.txt", "w")
	owner.Expect(t, 200, "PATCH", "/api/nodes/"+wander, map[string]any{"name": "wander2.txt"})
	owner.Expect(t, 200, "PATCH", "/api/nodes/"+wander, map[string]any{"parentId": "root"})
	for _, item := range feed(reader, "?scope=shared") {
		if item["node"].(map[string]any)["id"] == wander {
			t.Fatalf("moved out but visible: %v", item)
		}
	}
	owner.Expect(t, 200, "DELETE", fmt.Sprintf("/api/nodes/%s/members/%d", team, int(grant["id"].(float64))), nil)
	if items := feed(reader, ""); len(items) != 0 {
		t.Fatalf("ended share still visible: %v", items)
	}

	seen := map[float64]bool{}
	query := "?scope=mine&limit=4"
	for {
		page := owner.Call(t, 200, "GET", "/api/activity"+query, nil)
		for _, item := range page["items"].([]any) {
			id := item.(map[string]any)["id"].(float64)
			if seen[id] {
				t.Fatalf("entry %v repeats", id)
			}
			seen[id] = true
		}
		if page["nextCursor"] == nil {
			break
		}
		query = fmt.Sprintf("?scope=mine&limit=4&cursor=%d", int(page["nextCursor"].(float64)))
	}
	var total int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM activity WHERE owner_id=?`, owner.ID).Scan(&total); err != nil || total != len(seen) {
		t.Fatalf("paged %d of %d %v", len(seen), total, err)
	}
	owner.Expect(t, 400, "GET", "/api/activity?scope=nope", nil)
}
