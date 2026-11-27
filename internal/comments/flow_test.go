package comments_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/apptest"
)

func url(id string) string { return "/api/files/" + id + "/comments" }

func say(t *testing.T, c *apptest.Client, node, body string) string {
	t.Helper()
	return fmt.Sprint(int(c.Call(t, 201, "POST", url(node), map[string]any{"body": body})["id"].(float64)))
}

func TestComments(t *testing.T) {
	s := apptest.Start(t, nil)
	owner, member, stranger := s.Register(t, "owner"), s.Register(t, "member"), s.Register(t, "stranger")
	folder := owner.Folder(t, "root", "Shared")
	file := owner.Upload(t, folder, "plan.txt", "plan")
	owner.Expect(t, 201, "POST", "/api/nodes/"+folder+"/members", map[string]any{"recipient": "member"})

	first := owner.Call(t, 201, "POST", url(file), map[string]any{"body": "  First note "})
	if first["body"] != "First note" || first["mine"] != true || first["editedAt"] != nil {
		t.Fatalf("first %v", first)
	}
	say(t, member, file, "Looks good\nthanks")
	list := owner.Call(t, 200, "GET", url(file), nil)
	newest := list["comments"].([]any)[0].(map[string]any)
	if apptest.Names(list["comments"], "body") != "Looks good\nthanks,First note" || newest["mine"] != false || newest["author"] != "member" || list["nextCursor"] != nil {
		t.Fatalf("list %v", list)
	}
	var stored string
	if err := s.DB.QueryRow(`SELECT group_concat(body,'') FROM node_comments`).Scan(&stored); err != nil || strings.Contains(stored, "First note") {
		t.Fatalf("stored unsealed %v", err)
	}
	// A read-only member's comment shows as activity.
	feed := owner.Call(t, 200, "GET", "/api/activity?nodeId="+file, nil)["items"].([]any)[0].(map[string]any)
	if feed["kind"] != "comment" || feed["actor"].(map[string]any)["name"] != "member" {
		t.Fatalf("activity %v", feed)
	}

	for _, body := range []any{"", "   ", 5, nil, strings.Repeat("a", 2001), "bell\u0007"} {
		owner.Expect(t, 400, "POST", url(file), map[string]any{"body": body})
	}
	say(t, owner, file, strings.Repeat("a", 2000))
	if html := owner.Call(t, 201, "POST", url(file), map[string]any{"body": "<b>x</b>"}); html["body"] != "<b>x</b>" {
		t.Fatalf("html %v", html)
	}

	mine := say(t, member, file, "mine")
	if edited := member.Call(t, 200, "PATCH", url(file)+"/"+mine, map[string]any{"body": "mine, edited"}); edited["body"] != "mine, edited" || edited["editedAt"] == nil {
		t.Fatalf("edited %v", edited)
	}
	theirs := say(t, owner, file, "owner note")
	member.Expect(t, 403, "PATCH", url(file)+"/"+theirs, map[string]any{"body": "x"})
	member.Expect(t, 403, "DELETE", url(file)+"/"+theirs, nil)
	owner.Expect(t, 403, "PATCH", url(file)+"/"+mine, map[string]any{"body": "x"})

	// Another account's ids are missing, on every route.
	for _, node := range []string{file, folder, "no-such-node", "root"} {
		stranger.Expect(t, 404, "GET", url(node), nil)
		stranger.Expect(t, 404, "POST", url(node), map[string]any{"body": "hi"})
		stranger.Expect(t, 404, "PATCH", url(node)+"/"+mine, map[string]any{"body": "hi"})
		stranger.Expect(t, 404, "DELETE", url(node)+"/"+mine, nil)
	}
	anonymous := s.Visitor(t)
	anonymous.Expect(t, 401, "GET", url(file), nil)
	anonymous.Expect(t, 401, "POST", url(file), map[string]any{"body": "x"})
	anonymous.Expect(t, 401, "DELETE", url(file)+"/"+mine, nil)
	other := owner.Upload(t, "root", "other.txt", "o")
	owner.Expect(t, 404, "PATCH", url(other)+"/"+mine, map[string]any{"body": "x"})
	owner.Expect(t, 404, "DELETE", url(other)+"/"+mine, nil)

	owner.Expect(t, 200, "DELETE", url(file)+"/"+mine, nil)
	owner.Expect(t, 200, "DELETE", url(file)+"/"+theirs, nil)
	owner.Expect(t, 404, "DELETE", url(file)+"/"+theirs, nil)

	page := owner.Call(t, 200, "GET", url(file)+"?limit=2", nil)
	cursor := page["nextCursor"].(float64)
	next := owner.Call(t, 200, "GET", url(file)+fmt.Sprintf("?limit=100&cursor=%d", int(cursor)), nil)
	for _, item := range next["comments"].([]any) {
		if item.(map[string]any)["id"].(float64) >= cursor {
			t.Fatalf("cursor %v %v", page, next)
		}
	}

	// Share-link visitors get nothing.
	link := owner.Call(t, 201, "POST", "/api/shares", map[string]any{"nodeId": file})["token"].(string)
	if raw := anonymous.Expect(t, 200, "GET", "/api/public/shares/"+link, nil).Body; strings.Contains(string(raw), "First note") {
		t.Fatal("comments on the public page")
	}
	anonymous.Expect(t, 404, "GET", "/api/public/shares/"+link+"/comments", nil)
	anonymous.Expect(t, 404, "GET", "/api/public/shares/"+link+"/files/"+file+"/comments", nil)

	// Vault nodes and roots take none.
	vault := owner.Vault(t, "root", "Safe", "")
	cipher := owner.Upload(t, vault["folderId"].(string), "c2VjcmV0", "NCV1 x")
	owner.Expect(t, 404, "GET", url(cipher), nil)
	owner.Expect(t, 404, "POST", url(cipher), map[string]any{"body": "x"})
	owner.Expect(t, 404, "GET", url("root"), nil)
	owner.Expect(t, 404, "GET", "/api/activity?nodeId="+cipher, nil)

	// Members lose comments when access ends; trash hides the node.
	member.Expect(t, 200, "GET", url(file), nil)
	if _, err := s.DB.Exec(`UPDATE user_shares SET expires_at=datetime('now','-1 minute')`); err != nil {
		t.Fatal(err)
	}
	member.Expect(t, 404, "GET", url(file), nil)
	member.Expect(t, 404, "GET", "/api/activity?nodeId="+file, nil)
	if items := member.Call(t, 200, "GET", "/api/activity", nil)["items"].([]any); len(items) != 0 {
		t.Fatalf("expired member sees %v", items)
	}
	if _, err := s.DB.Exec(`UPDATE user_shares SET expires_at=NULL`); err != nil {
		t.Fatal(err)
	}
	// They follow the node through rename and move.
	before := len(owner.Call(t, 200, "GET", url(file), nil)["comments"].([]any))
	owner.Expect(t, 200, "PATCH", "/api/nodes/"+file, map[string]any{"name": "renamed.txt", "parentId": "root"})
	if after := len(owner.Call(t, 200, "GET", url(file), nil)["comments"].([]any)); after != before {
		t.Fatalf("lost on move: %d %d", before, after)
	}
	member.Expect(t, 404, "GET", url(file), nil)
	say(t, owner, folder, "about the folder")
	member.Expect(t, 200, "GET", url(folder), nil)

	doomed := owner.Upload(t, "root", "doomed.txt", "x")
	say(t, owner, doomed, "bye")
	owner.Expect(t, 200, "DELETE", "/api/nodes/"+doomed, nil)
	owner.Expect(t, 404, "GET", url(doomed), nil)
	owner.Expect(t, 200, "DELETE", "/api/trash/"+doomed, nil)
	count := func(query string, args ...any) (n int) {
		if err := s.DB.QueryRow(query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(`SELECT COUNT(*) FROM node_comments WHERE node_id=?`, doomed) != 0 {
		t.Fatal("comments outlive their node")
	}

	// The export carries them, and an erased account's go.
	say(t, member, folder, "from the member")
	export := string(owner.Expect(t, 200, "POST", "/api/account/export", map[string]any{"currentPassword": apptest.Password}).Body)
	for _, want := range []string{"about the folder", "from the member", `"onMyFile":true`, `"byMe":false`, `"entries":[{`} {
		if !strings.Contains(export, want) {
			t.Fatalf("export lacks %s", want)
		}
	}
	member.Expect(t, 200, "POST", "/api/account/delete", map[string]any{"currentPassword": apptest.Password, "confirmUsername": "member"})
	if count(`SELECT COUNT(*) FROM node_comments WHERE user_id=?`, member.ID) != 0 || count(`SELECT COUNT(*) FROM activity WHERE actor_id=?`, member.ID) != 0 {
		t.Fatal("erased account left comments or activity")
	}

	// Writes are limited per account.
	limited := false
	for i := 0; i < 130 && !limited; i++ {
		limited = stranger.Do(t, "DELETE", url("none")+"/1", nil).Status == 429
	}
	if !limited {
		t.Fatal("comment writes are not limited")
	}
	owner.Expect(t, 201, "POST", url(folder), map[string]any{"body": "own budget"})
}
