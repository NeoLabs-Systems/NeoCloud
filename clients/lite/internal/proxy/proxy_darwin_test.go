package proxy

import "testing"

func TestParseScutil(t *testing.T) {
	got := parseScutil("<dictionary> {\n  HTTPEnable : 1\n  HTTPPort : 3128\n  HTTPProxy : proxy.lan\n  HTTPSEnable : 0\n}\n")
	if got.http == nil || got.http.String() != "http://proxy.lan:3128" || got.https != nil {
		t.Errorf("%+v", got)
	}
}
