package backups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenTargetSpecRequiresPrivateCredentialsFile(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "target.json")
	content := `{"type":"s3","config":{"bucket":"backup","accessKeyId":"private","secretAccessKey":"private"}}`
	if err := os.WriteFile(spec, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenTargetSpec(spec); err == nil || !strings.Contains(err.Error(), "permissions must be private") {
		t.Fatalf("permissive target spec error=%v", err)
	}
	if err := os.Chmod(spec, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenTargetSpec(spec); err != nil {
		t.Fatalf("private target spec error=%v", err)
	}
}
