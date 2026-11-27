package backups

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func startBackupSFTPFixture(t *testing.T, root string, allowedKeys ...ssh.PublicKey) (string, int, string) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{PasswordCallback: func(connection ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if connection.User() == "backup" && string(password) == "secret" {
			return nil, nil
		}
		return nil, fmt.Errorf("invalid credentials")
	}}
	serverConfig.PublicKeyCallback = func(connection ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if connection.User() == "backup" {
			for _, allowed := range allowedKeys {
				if key.Type() == allowed.Type() && string(key.Marshal()) == string(allowed.Marshal()) {
					return nil, nil
				}
			}
		}
		return nil, fmt.Errorf("invalid public key")
	}
	serverConfig.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(raw net.Conn) {
				defer raw.Close()
				_, channels, requests, serverErr := ssh.NewServerConn(raw, serverConfig)
				if serverErr != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					if channel.ChannelType() != "session" {
						_ = channel.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					ch, channelRequests, channelErr := channel.Accept()
					if channelErr != nil {
						continue
					}
					go func() {
						for request := range channelRequests {
							if request.Type == "subsystem" {
								_ = request.Reply(true, nil)
								continue
							}
							_ = request.Reply(false, nil)
						}
					}()
					sftpServer, serverErr := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(root))
					if serverErr == nil {
						_ = sftpServer.Serve()
						_ = sftpServer.Close()
					}
					_ = ch.Close()
				}
			}(conn)
		}
	}()
	address := listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", address.Port, ssh.FingerprintSHA256(signer.PublicKey())
}

func TestSFTPTargetRequiresPinnedHostKeyAndSupportsAdminProbe(t *testing.T) {
	root := t.TempDir()
	remoteRoot := filepath.Join(root, "remote")
	if err := os.MkdirAll(remoteRoot, 0700); err != nil {
		t.Fatal(err)
	}
	host, port, expectedFingerprint := startBackupSFTPFixture(t, root)
	config := SFTPConfig{Host: host, Port: port, Username: "backup", Password: "secret", Path: remoteRoot}
	if _, err := NewSFTPTarget(config); err == nil {
		t.Fatal("normal constructor accepted an unpinned host key")
	}
	probe, err := NewSFTPTargetForProbe(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = probe.Open(ctx); err != nil {
		t.Fatal(err)
	}
	gotFingerprint := probe.HostKeyFingerprint()
	if gotFingerprint != expectedFingerprint {
		t.Fatalf("probe fingerprint=%q want %q", gotFingerprint, expectedFingerprint)
	}
	_ = probe.Close()
	config.HostKey = gotFingerprint
	wrongPin := config
	wrongPin.HostKey = "SHA256:not-the-server-key"
	wrongTarget, err := NewSFTPTarget(wrongPin)
	if err != nil {
		t.Fatal(err)
	}
	if err = wrongTarget.Open(ctx); err == nil {
		_ = wrongTarget.Close()
		t.Fatal("wrong host-key pin was accepted")
	}
	target, err := NewSFTPTarget(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = target.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer target.Close()

	localSource := filepath.Join(t.TempDir(), "manifest.json")
	if err = os.WriteFile(localSource, []byte(`{"format":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = target.Put(ctx, "snapshots/current/manifest.json", localSource); err != nil {
		t.Fatal(err)
	}
	exists, err := target.Exists(ctx, "snapshots/current/manifest.json")
	if err != nil || !exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	listed, err := target.List(ctx, "snapshots")
	if err != nil || len(listed) != 1 || listed[0] != "snapshots/current/manifest.json" {
		t.Fatalf("listed=%v err=%v", listed, err)
	}
	localDestination := filepath.Join(t.TempDir(), "download", "manifest.json")
	if err = target.Get(ctx, "snapshots/current/manifest.json", localDestination); err != nil {
		t.Fatal(err)
	}
	if data, readErr := os.ReadFile(localDestination); readErr != nil || string(data) != `{"format":2}` {
		t.Fatalf("download=%q err=%v", data, readErr)
	}
	if err = target.DeletePrefix(ctx, "snapshots/current"); err != nil {
		t.Fatal(err)
	}
	listed, err = target.List(ctx, "snapshots")
	if err != nil || len(listed) != 0 {
		t.Fatalf("after prefix delete=%v err=%v", listed, err)
	}
	if _, err = target.List(ctx, "../outside"); err == nil {
		t.Fatal("unsafe list prefix was accepted")
	}
}

func TestSFTPTargetParsesPasswordAndEncryptedPrivateKeys(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	privateText := string(pem.EncodeToMemory(privatePEM))
	if _, err = parseSFTPPrivateKey(privateText, ""); err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	if _, err = NewSFTPTarget(SFTPConfig{Host: "backup.example", Username: "backup", PrivateKey: privateText, Path: "/archive", HostKey: "SHA256:fixture"}); err != nil {
		t.Fatalf("validate private-key target: %v", err)
	}
	if _, err = NewSFTPTarget(SFTPConfig{Host: "backup.example", Username: "backup", Password: "pw", Path: "/archive", HostKey: "SHA256:fixture"}); err != nil {
		t.Fatalf("validate password target: %v", err)
	}
	if _, err = NewSFTPTarget(SFTPConfig{Host: "backup.example", Username: "backup", Password: "pw", Path: "/archive", HostKey: "invalid"}); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("invalid fingerprint error=%v", err)
	}
}

func TestSFTPTargetAuthenticatesWithPrivateKey(t *testing.T) {
	root := t.TempDir()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	privateText := string(pem.EncodeToMemory(privatePEM))
	host, port, fingerprint := startBackupSFTPFixture(t, root, signer.PublicKey())
	target, err := NewSFTPTarget(SFTPConfig{Host: host, Port: port, Username: "backup", PrivateKey: privateText, Path: filepath.Join(root, "archive"), HostKey: fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = target.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	source := filepath.Join(root, "manifest.json")
	if err = os.WriteFile(source, []byte(`{"format":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = target.Put(ctx, "snapshot/manifest.json", source); err != nil {
		t.Fatal(err)
	}
}

func TestSFTPTargetRunsEncryptedSnapshotLifecycle(t *testing.T) {
	fixtureRoot := t.TempDir()
	host, port, fingerprint := startBackupSFTPFixture(t, fixtureRoot)
	remotePath := filepath.Join(fixtureRoot, "neocloud-backups")
	db, keys, runner, _ := schedulerFixture(t)
	config := map[string]any{
		"host": host, "port": port, "username": "backup", "password": "secret",
		"path": remotePath, "hostKey": fingerprint,
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := keys.EncryptValue(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO backup_targets(id,name,type,config_enc,retention) VALUES(1,'sftp','sftp',?,2)`, sealed); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runner.Start(ctx)
	defer runner.Close()
	runID, err := runner.RunTarget(1, "sftp", config, 2)
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, db.W, runID)
	target, err := NewSFTPTarget(SFTPConfig{Host: host, Port: port, Username: "backup", Password: "secret", Path: remotePath, HostKey: fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := ListTargetSnapshots(ctx, target)
	if err != nil || len(listed) != 1 {
		t.Fatalf("SFTP snapshots=%v err=%v", listed, err)
	}
	verified := runner.VerifyTargetSnapshot(ctx, target)
	if !verified.OK {
		t.Fatalf("SFTP snapshot verification=%+v", verified)
	}
}
