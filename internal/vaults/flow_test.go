package vaults_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/apptest"
)

func code(t *testing.T, c *apptest.Client, status int, method, path string, body any) string {
	t.Helper()
	value, _ := c.Call(t, status, method, path, body)["code"].(string)
	return value
}

func keys(extra map[string]any) map[string]any {
	body := apptest.KeyParams()
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func TestVaultMetadata(t *testing.T) {
	s := apptest.Start(t, nil)
	alice, bob := s.Register(t, "alice"), s.Register(t, "bob")
	params := keys(map[string]any{"parentId": "root", "name": "Tresor"})
	vault := alice.Call(t, 201, "POST", "/api/vaults", params)
	folder := vault["folder"].(map[string]any)
	if vault["salt"] != params["salt"] || vault["wrappedKey"] != params["wrappedKey"] || vault["iterations"] != 600000.0 || folder["id"] != vault["folderId"] || folder["vaultId"] != vault["id"] {
		t.Fatalf("created %v", vault)
	}
	id := vault["id"].(string)
	if got := alice.Call(t, 200, "GET", "/api/vaults/"+id, nil); got["folderId"] != vault["folderId"] || got["wrappedKey"] != params["wrappedKey"] {
		t.Fatalf("fetched %v", got)
	}
	items := alice.Call(t, 200, "GET", "/api/folders/root/children", nil)["items"].([]any)
	if items[0].(map[string]any)["vaultId"] != id {
		t.Fatalf("listing %v", items)
	}
	for _, override := range []map[string]any{
		{"salt": "c2hvcnQ="}, {"wrappedKey": params["salt"]}, {"iterations": 1000}, {"iterations": "600000.5"}, {"iterations": 599999}, {"salt": "not base64!"}, {"iterations": nil},
	} {
		body := keys(map[string]any{"name": "Bad"})
		for k, v := range override {
			body[k] = v
		}
		if got := code(t, alice, 400, "POST", "/api/vaults", body); got != "VAULT_PARAMS_INVALID" {
			t.Fatalf("%v: %s", override, got)
		}
	}
	bob.Expect(t, 404, "GET", "/api/vaults/"+id, nil)
	bob.Expect(t, 404, "PUT", "/api/vaults/"+id+"/key", keys(nil))
	bob.Expect(t, 404, "POST", "/api/vaults", keys(map[string]any{"parentId": vault["folderId"], "name": "x"}))
	bob.Expect(t, 404, "POST", "/api/vaults/"+id+"/replace", map[string]any{"folderId": "root", "cursor": 0})
	anonymous := s.Visitor(t)
	anonymous.Expect(t, 401, "GET", "/api/vaults/"+id, nil)
	anonymous.Expect(t, 401, "POST", "/api/vaults", keys(map[string]any{"name": "x"}))
	alice.Device(t, "webdav").Expect(t, 401, "GET", "/api/vaults/"+id, nil)
	alice.Device(t, "linux").Expect(t, 200, "GET", "/api/vaults/"+id, nil)
	if got := code(t, alice, 409, "POST", "/api/vaults", keys(map[string]any{"parentId": vault["folderId"], "name": "Inner"})); got != "VAULT_BOUNDARY" {
		t.Fatal(got)
	}

	rewrapped := keys(nil)
	if got := alice.Call(t, 200, "PUT", "/api/vaults/"+id+"/key", rewrapped); got["wrappedKey"] != rewrapped["wrappedKey"] {
		t.Fatalf("rewrapped %v", got)
	}
	alice.Expect(t, 400, "PUT", "/api/vaults/"+id+"/key", keys(map[string]any{"iterations": 5}))
	// Vaults made before the floor keep working; re-wrapping needs it.
	if _, err := s.DB.Exec(`UPDATE vaults SET iterations=310000 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if got := alice.Call(t, 200, "GET", "/api/vaults/"+id, nil); got["iterations"] != 310000.0 {
		t.Fatalf("old %v", got)
	}
	alice.Expect(t, 400, "PUT", "/api/vaults/"+id+"/key", keys(map[string]any{"iterations": 310000}))

	// Everything created inside inherits the vault, and nothing crosses it.
	inner := alice.Call(t, 201, "POST", "/api/folders", map[string]any{"parentId": vault["folderId"], "name": "aW5uZXI"})
	if inner["vaultId"] != id {
		t.Fatalf("inner %v", inner)
	}
	cipher := alice.Upload(t, vault["folderId"].(string), "Y2lwaGVy", "NCV1 x")
	plain := alice.Upload(t, "root", "plain.txt", "plain text")
	for _, request := range []struct{ method, path, parent string }{
		{"PATCH", "/api/nodes/" + plain, vault["folderId"].(string)},
		{"PATCH", "/api/nodes/" + cipher, "root"},
		{"POST", "/api/nodes/" + plain + "/copy", vault["folderId"].(string)},
	} {
		if got := code(t, alice, 409, request.method, request.path, map[string]any{"parentId": request.parent}); got != "VAULT_BOUNDARY" {
			t.Fatalf("%v %s", request, got)
		}
	}
	// Vault contents are never logged.
	var logged int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM activity WHERE node_id IN (?,?)`, cipher, inner["id"]).Scan(&logged); err != nil || logged != 0 {
		t.Fatalf("vault contents logged: %d %v", logged, err)
	}
}

func TestConversion(t *testing.T) {
	s := apptest.Start(t, nil)
	alice, bob := s.Register(t, "alice"), s.Register(t, "bob")
	check := func(id string) map[string]any {
		return alice.Call(t, 200, "GET", "/api/vaults/conversions/"+id, nil)
	}
	problems := func(id string) string {
		var out []string
		for _, p := range check(id)["problems"].([]any) {
			out = append(out, p.(string))
		}
		return strings.Join(out, ",")
	}
	taxes := alice.Folder(t, "root", "Taxes")
	inner := alice.Folder(t, taxes, "2026")
	alice.Upload(t, taxes, "a.txt", "hello")
	alice.Upload(t, inner, "b.bin", strings.Repeat("\x00", 70000))
	got := check(taxes)
	if got["name"] != "Taxes" || got["files"] != 2.0 || got["folders"] != 1.0 || got["bytes"] != 70005.0 || got["vaultBytes"] != float64((12+16+5)+(12+32+70000)) || len(got["problems"].([]any)) != 0 {
		t.Fatalf("check %v", got)
	}
	bob.Expect(t, 404, "GET", "/api/vaults/conversions/"+taxes, nil)

	alice.Folder(t, "root", "Clash")
	alice.Expect(t, 409, "POST", "/api/vaults", keys(map[string]any{"parentId": "root", "name": "Clash"}))
	if name := alice.Vault(t, "root", "Clash", "rename")["folder"].(map[string]any)["name"]; name != "Clash (1)" {
		t.Fatalf("renamed %v", name)
	}
	alice.Expect(t, 400, "POST", "/api/vaults", keys(map[string]any{"parentId": "root", "name": "X", "conflict": "replace"}))

	team := alice.Folder(t, "root", "Team")
	below := alice.Folder(t, team, "Below")
	alice.Expect(t, 201, "POST", "/api/nodes/"+team+"/members", map[string]any{"recipient": "bob", "permission": "read"})
	if problems(team) != "SHARED" || problems(below) != "SHARED" {
		t.Fatalf("shared %s %s", problems(team), problems(below))
	}
	linked := alice.Folder(t, "root", "Linked")
	deep := alice.Upload(t, linked, "deep.txt", "x")
	alice.Expect(t, 201, "POST", "/api/shares", map[string]any{"nodeId": deep})
	if got := problems(linked); got != "PUBLIC_LINK" {
		t.Fatal(got)
	}
	holder := alice.Folder(t, "root", "Holder")
	inside := alice.Vault(t, holder, "V", "")
	if got := problems(holder); got != "CONTAINS_VAULT" {
		t.Fatal(got)
	}
	if got := problems(inside["folderId"].(string)); !strings.Contains(got, "IN_VAULT") {
		t.Fatal(got)
	}
	if got := problems("root"); !strings.Contains(got, "ROOT") {
		t.Fatal(got)
	}
	beside := alice.Vault(t, "root", "Team", "rename")
	if got := code(t, alice, 409, "POST", "/api/vaults/"+beside["id"].(string)+"/replace", map[string]any{"folderId": team, "cursor": check(team)["cursor"]}); got != "VAULT_CONVERT_REFUSED" {
		t.Fatal(got)
	}
	if got := code(t, alice, 409, "POST", "/api/vaults/"+inside["id"].(string)+"/replace", map[string]any{"folderId": holder, "cursor": check(holder)["cursor"]}); got != "VAULT_BOUNDARY" {
		t.Fatal(got)
	}

	big := alice.Folder(t, "root", "Big")
	alice.Upload(t, big, "big.bin", strings.Repeat("\x00", 4096))
	var used int
	if err := s.DB.QueryRow(`SELECT used_bytes FROM users WHERE id=?`, alice.ID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE users SET quota_bytes=? WHERE id=?`, used+100, alice.ID); err != nil {
		t.Fatal(err)
	}
	if got := check(big); problems(big) != "QUOTA" || got["freeBytes"] != 100.0 {
		t.Fatalf("quota %v", got)
	}
	if _, err := s.DB.Exec(`UPDATE users SET quota_bytes=-1 WHERE id=?`, alice.ID); err != nil {
		t.Fatal(err)
	}
	if got := check(big); got["freeBytes"] != nil || len(got["problems"].([]any)) != 0 {
		t.Fatalf("unlimited %v", got)
	}

	home := alice.Folder(t, "root", "Home")
	private := alice.Folder(t, home, "Private")
	alice.Upload(t, private, "note.txt", "secret")
	before := check(private)
	vault := alice.Vault(t, home, "Private", "rename")
	id := vault["id"].(string)
	replace := "/api/vaults/" + id + "/replace"
	bob.Expect(t, 404, "POST", replace, map[string]any{"folderId": private, "cursor": before["cursor"]})
	alice.Expect(t, 400, "POST", replace, map[string]any{"folderId": private})
	result := alice.Call(t, 200, "POST", replace, map[string]any{"folderId": private, "cursor": before["cursor"]})
	folder := result["folder"].(map[string]any)
	if result["trashedId"] != private || folder["id"] != vault["folderId"] || folder["name"] != "Private" || folder["parentId"] != home || folder["vaultId"] != id {
		t.Fatalf("replaced %v", result)
	}
	if trash := alice.Call(t, 200, "GET", "/api/trash", nil)["items"]; !strings.Contains(apptest.Names(trash, "id"), private) {
		t.Fatal("source not in trash")
	}
	if names := apptest.Names(alice.Call(t, 200, "GET", "/api/folders/"+home+"/children", nil)["items"], "name"); names != "Private" {
		t.Fatalf("children %s", names)
	}
	// The sync journal names the folders both entries happened in.
	var journalled int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM changes WHERE node_id=? AND op='trash' AND parent_ids_json=?`, private, "["+strconv.Quote(home)+"]").Scan(&journalled); err != nil || journalled != 1 {
		t.Fatalf("trash journal %d %v", journalled, err)
	}

	moving := alice.Folder(t, "root", "Moving")
	stale := check(moving)
	target := alice.Vault(t, "root", "Moving", "rename")["id"].(string)
	alice.Upload(t, moving, "late.txt", "added later")
	if got := code(t, alice, 409, "POST", "/api/vaults/"+target+"/replace", map[string]any{"folderId": moving, "cursor": stale["cursor"]}); got != "VAULT_SOURCE_CHANGED" {
		t.Fatal(got)
	}
	alice.Expect(t, 200, "POST", "/api/vaults/"+target+"/replace", map[string]any{"folderId": moving, "cursor": check(moving)["cursor"]})
}
