package files

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/documentconvert"
)

func TestCapabilitiesRequiresAuthAndReportsImplementedFeatures(t *testing.T) {
	h := New(Options{})
	mux := http.NewServeMux()
	h.Routes(mux)

	unauthenticated := httptest.NewRecorder()
	mux.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}

	var got map[string]bool
	payload, err := json.Marshal(capabilityPayload())
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	want := map[string]bool{"imageThumbnails": true, "officePreview": documentconvert.Available(), "fullTextSearch": false}
	if len(got) != len(want) {
		t.Fatalf("capabilities = %#v, want exactly %#v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %t, want %t", key, got[key], value)
		}
	}
}

func TestStoragePolicyUpdatesAtomically(t *testing.T) {
	h := New(Options{DefaultQuotaBytes: 10, MaxUploadBytes: 20, TrashRetentionDays: 30, VersionRetention: 4})
	if got := h.storagePolicy(); got != (StoragePolicy{10, 20, 30, 4}) {
		t.Fatalf("initial policy = %+v", got)
	}
	h.SetStoragePolicy(StoragePolicy{DefaultQuotaBytes: -1, MaxUploadBytes: 40, TrashRetentionDays: 0, VersionRetention: 8})
	if got := h.storagePolicy(); got != (StoragePolicy{-1, 40, 0, 8}) {
		t.Fatalf("updated policy = %+v", got)
	}
}
