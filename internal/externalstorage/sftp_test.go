package externalstorage

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func newSigner(t *testing.T) (ssh.Signer, ed25519.PrivateKey) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer, private
}

// serveSFTP runs an SFTP server accepting the password "pw" or the client key.
func serveSFTP(t *testing.T, host ssh.Signer, client ssh.PublicKey) int {
	t.Helper()
	config := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) == "pw" {
				return nil, nil
			}
			return nil, fmt.Errorf("bad password")
		},
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if client != nil && string(key.Marshal()) == string(client.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("unknown key")
		},
	}
	config.AddHostKey(host)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					channel, channelRequests, err := incoming.Accept()
					if err != nil {
						return
					}
					go func() {
						for request := range channelRequests {
							_ = request.Reply(request.Type == "subsystem", nil)
						}
					}()
					go func() {
						server, err := sftp.NewServer(channel)
						if err == nil {
							_ = server.Serve()
						}
						_ = channel.Close()
					}()
				}
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port
}

func TestSFTPBrowsingLinksAndHostKey(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	write := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "a.txt"), "0123456789")
	write(filepath.Join(outside, "secret.txt"), "secret")
	_ = os.Mkdir(filepath.Join(root, "sub"), 0o700)
	for link, target := range map[string]string{"escape": outside, "secret-link": filepath.Join(outside, "secret.txt")} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	host, _ := newSigner(t)
	clientSigner, clientKey := newSigner(t)
	port := serveSFTP(t, host, clientSigner.PublicKey())
	f := newFixture(t, Options{AllowPrivate: true})
	config := map[string]any{"host": "127.0.0.1", "port": port, "username": "u", "password": "pw", "path": root}
	created := f.expect(201, f.call(owner, "POST", "/api/external-storage", map[string]any{"name": "Box", "provider": "sftp", "config": config}))
	id := created["id"].(string)
	base := "/api/external-storage/" + id
	if _, shown := created["config"].(map[string]any)["hostKey"]; shown {
		t.Fatal("host key is not a client setting")
	}

	// The fingerprint is stored the way earlier servers stored it: hex SHA-256.
	stored, err := f.handler.mount(t.Context(), owner, id)
	sum := sha256.Sum256(host.PublicKey().Marshal())
	if err != nil || stored.Config.text("hostKey") != hex.EncodeToString(sum[:]) {
		t.Fatal(stored.Config["hostKey"], err)
	}

	listing := f.expect(200, f.call(owner, "GET", base+"/list", nil))
	if got := names(listing); !slices.Equal(got, []string{"folder:sub", "file:a.txt"}) {
		t.Fatal(got)
	}
	part := f.call(owner, "GET", base+"/file?path=a.txt", nil, "Range", "bytes=2-4")
	if part.Code != 206 || part.Body.String() != "234" {
		t.Fatal(part.Code, part.Body)
	}
	if got := f.call(owner, "GET", base+"/file?path=a.txt", nil); got.Body.String() != "0123456789" {
		t.Fatal(got.Code, got.Body)
	}
	f.expect(201, f.call(owner, "PUT", base+"/file?path=sub/b.txt", []byte("bee")))
	f.expect(200, f.call(owner, "POST", base+"/move", map[string]any{"from": "sub/b.txt", "to": "sub/c.txt"}))
	f.expect(409, f.call(owner, "POST", base+"/move", map[string]any{"from": "a.txt", "to": "sub/c.txt"}))

	// Nothing leads through a link.
	f.expect(400, f.call(owner, "GET", base+"/list?path=escape/x", nil))
	f.expect(404, f.call(owner, "GET", base+"/list?path=escape", nil))
	f.expect(400, f.call(owner, "GET", base+"/file?path=escape/secret.txt", nil))
	f.expect(404, f.call(owner, "GET", base+"/file?path=secret-link", nil))
	f.expect(400, f.call(owner, "PUT", base+"/file?path=secret-link&conflict=replace", []byte("overwritten")))
	f.expect(400, f.call(owner, "PUT", base+"/file?path=escape/new.txt", []byte("x")))
	if content, _ := os.ReadFile(filepath.Join(outside, "secret.txt")); string(content) != "secret" {
		t.Fatalf("wrote through a link: %q", content)
	}
	_ = os.Symlink(outside, filepath.Join(root, "sub", "inner-link"))
	f.expect(200, f.call(owner, "DELETE", base+"/entry?path=sub", nil))
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Fatal("deleting followed a link")
	}
	if _, err := os.Stat(filepath.Join(root, "sub")); !os.IsNotExist(err) {
		t.Fatal("folder kept")
	}
	f.expect(404, f.call(owner, "DELETE", base+"/entry?path=missing", nil))

	// A private key signs in too.
	block, err := ssh.MarshalPrivateKeyWithPassphrase(clientKey, "", []byte("phrase"))
	if err != nil {
		t.Fatal(err)
	}
	keyed := map[string]any{"host": "127.0.0.1", "port": fmt.Sprint(port), "username": "u", "privateKey": string(pem.EncodeToMemory(block)), "passphrase": "phrase", "path": root}
	if got := f.expect(200, f.call(owner, "POST", "/api/external-storage/test", map[string]any{"provider": "sftp", "config": keyed})); got["ok"] != true {
		t.Fatal(got)
	}
	config["password"] = "wrong"
	if got := f.expect(200, f.call(owner, "POST", "/api/external-storage/test", map[string]any{"provider": "sftp", "config": config})); got["code"] != "EXTERNAL_AUTH_FAILED" {
		t.Fatal(got)
	}

	// The key seen first is the only one accepted afterwards.
	stored.Config["hostKey"] = strings.Repeat("0", 64)
	if _, err = f.handler.save(t.Context(), owner, stored); err != nil {
		t.Fatal(err)
	}
	if got := f.expect(502, f.call(owner, "GET", base+"/list", nil)); got["code"] != "EXTERNAL_HOST_KEY" {
		t.Fatal(got)
	}
}

func TestSFTPValidation(t *testing.T) {
	for want, config := range map[string]map[string]any{
		"password or a private key": {"host": "h", "username": "u"},
		"port":                      {"host": "h", "username": "u", "password": "p", "port": float64(70000)},
		"folder":                    {"host": "h", "username": "u", "password": "p", "path": "/a/../b"},
		"host is required":          {"username": "u", "password": "p"},
	} {
		if _, err := cleanConfig("sftp", config, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(want, err)
		}
	}
}
