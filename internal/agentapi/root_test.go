package agentapi

import (
	"net/http"
	"testing"
)

// A connected app may be the first thing that touches a new account.
func TestFirstAgentRequestCreatesTheRootFolder(t *testing.T) {
	h, db, _ := fixture(t)
	if _, err := db.W.Exec(`DELETE FROM nodes WHERE user_id=1`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if response := serve(h, http.MethodGet, "/api/v1/files?path=/", "Bearer read-token"); response.Code != http.StatusOK {
			t.Fatalf("list on a new account = %d: %s", response.Code, response.Body.String())
		}
	}
	var roots int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM nodes WHERE user_id=1 AND parent_id IS NULL`).Scan(&roots); err != nil || roots != 1 {
		t.Fatalf("root folders = %d, %v", roots, err)
	}
}
