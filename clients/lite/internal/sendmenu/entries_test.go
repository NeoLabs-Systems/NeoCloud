package sendmenu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxEntriesAreWrittenAndRemoved(t *testing.T) {
	data := t.TempDir()
	exe := `/opt/My "Apps"/neo cloud/$x/neocloud-lite`
	if err := write(linuxFiles(data, exe)); err != nil {
		t.Fatal(err)
	}
	desktop, err := os.ReadFile(filepath.Join(data, "applications", "neocloud-send.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(desktop)
	for _, want := range []string{"Name=Send to my device", "MimeType=application/octet-stream;", `Exec="/opt/My \"Apps\"/neo cloud/\$x/neocloud-lite" send %F`} {
		if !strings.Contains(text, want) {
			t.Errorf("desktop entry lacks %q:\n%s", want, text)
		}
	}
	script := filepath.Join(data, "nautilus", "scripts", "Send to my device")
	body, _ := os.ReadFile(script)
	if !strings.Contains(string(body), `exec '/opt/My "Apps"/neo cloud/$x/neocloud-lite' send "$@"`) {
		t.Errorf("script: %s", body)
	}
	if info, _ := os.Stat(script); info == nil || info.Mode()&0o111 == 0 {
		t.Error("the script must be executable")
	}
	menu, _ := os.ReadFile(filepath.Join(data, "kio", "servicemenus", "neocloud-send.desktop"))
	if !strings.Contains(string(menu), "[Desktop Action send]") {
		t.Errorf("service menu: %s", menu)
	}
	if err := remove(linuxFiles(data, exe)); err != nil {
		t.Fatal(err)
	}
	for _, f := range linuxFiles(data, exe) {
		if _, err := os.Stat(f.path); err == nil {
			t.Errorf("%s is still there", f.path)
		}
	}
	if err := remove(linuxFiles(data, exe)); err != nil {
		t.Errorf("removing what is not there: %v", err)
	}
}

func TestQuotingKeepsPercentAndQuotesLiteral(t *testing.T) {
	if got := quoteExec(`/a%b`); got != `"/a%%b"` {
		t.Errorf("exec: %s", got)
	}
	if got := quoteShell(`it's`); got != `'it'\''s'` {
		t.Errorf("shell: %s", got)
	}
}
