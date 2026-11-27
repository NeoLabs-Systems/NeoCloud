package backups

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	pathpkg "path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	sftpReadyTimeout = 20 * time.Second
	sftpListMaxDepth = 32
	sftpListMaxFiles = 2_000_000
)

// SFTPConfig describes an SSH account and a remote backup directory. HostKey
// is the pinned SHA-256 OpenSSH fingerprint. Only the administrative probe
// constructor accepts an empty pin to capture a first-use fingerprint.
type SFTPConfig struct {
	Host                 string
	Port                 int
	Username             string
	Password             string
	PrivateKey           string
	Passphrase           string
	Path                 string
	HostKey              string
	allowUnpinnedHostKey bool
}

// SFTPTarget implements encrypted snapshot storage over SFTP.
type SFTPTarget struct {
	config      SFTPConfig
	mu          sync.Mutex
	ssh         *ssh.Client
	client      *sftp.Client
	fingerprint string
}

func NewSFTPTarget(config SFTPConfig) (*SFTPTarget, error) {
	config.Host = strings.TrimSpace(config.Host)
	config.Username = strings.TrimSpace(config.Username)
	config.Path = strings.TrimSpace(config.Path)
	if config.Port < 1 || config.Port > 65535 {
		if config.Port == 0 {
			config.Port = 22
		} else {
			return nil, errors.New("SFTP port must be between 1 and 65535")
		}
	}
	if config.Host == "" || config.Username == "" || config.Path == "" {
		return nil, errors.New("SFTP host, username, and folder are required")
	}
	if config.Password == "" && config.PrivateKey == "" {
		return nil, errors.New("SFTP password or private key is required")
	}
	if config.PrivateKey != "" {
		if _, err := parseSFTPPrivateKey(config.PrivateKey, config.Passphrase); err != nil {
			return nil, errors.New("SFTP private key is invalid")
		}
	}
	if config.HostKey != "" && !strings.HasPrefix(config.HostKey, "SHA256:") {
		return nil, errors.New("SFTP host key must be a SHA-256 fingerprint")
	}
	if config.HostKey == "" && !config.allowUnpinnedHostKey {
		return nil, errors.New("SFTP host key is not pinned; test the backup target to trust its fingerprint")
	}
	config.Path = pathpkg.Clean(config.Path)
	return &SFTPTarget{config: config}, nil
}

// NewSFTPTargetForProbe allows a single administrative test connection to
// observe a first-use fingerprint. Callers must persist HostKeyFingerprint()
// and use NewSFTPTarget for subsequent backup operations.
func NewSFTPTargetForProbe(config SFTPConfig) (*SFTPTarget, error) {
	config.allowUnpinnedHostKey = true
	return NewSFTPTarget(config)
}

func parseSFTPPrivateKey(private, passphrase string) (ssh.Signer, error) {
	block, _ := pem.Decode([]byte(private))
	if block == nil {
		return nil, errors.New("invalid private key PEM")
	}
	//lint:ignore SA1019 keys in the legacy encrypted PEM form must keep working
	if x509.IsEncryptedPEMBlock(block) {
		if passphrase == "" {
			return nil, errors.New("private key passphrase required")
		}
		//lint:ignore SA1019 keys in the legacy encrypted PEM form must keep working
		plain, err := x509.DecryptPEMBlock(block, []byte(passphrase))
		if err == nil {
			block = &pem.Block{Type: block.Type, Bytes: plain}
		}
	}
	if signer, err := ssh.ParsePrivateKey(pem.EncodeToMemory(block)); err == nil {
		return signer, nil
	}
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase(pem.EncodeToMemory(block), []byte(passphrase))
	}
	return ssh.ParsePrivateKey(block.Bytes)
}

func (t *SFTPTarget) Open(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.client != nil {
		return nil
	}
	auth := make([]ssh.AuthMethod, 0, 2)
	if t.config.Password != "" {
		auth = append(auth, ssh.Password(t.config.Password))
	}
	if t.config.PrivateKey != "" {
		signer, err := parseSFTPPrivateKey(t.config.PrivateKey, t.config.Passphrase)
		if err != nil {
			return errors.New("SFTP private key is invalid")
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	clientConfig := &ssh.ClientConfig{
		User:    t.config.Username,
		Auth:    auth,
		Timeout: sftpReadyTimeout,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			fingerprint := ssh.FingerprintSHA256(key)
			t.fingerprint = fingerprint
			if t.config.HostKey != "" {
				if fingerprint == t.config.HostKey {
					return nil
				}
				return errors.New("SFTP server host key does not match the configured fingerprint")
			}
			if t.config.allowUnpinnedHostKey {
				return nil
			}
			return errors.New("SFTP host key is not pinned; test the backup target to trust its fingerprint")
		},
	}
	address := net.JoinHostPort(t.config.Host, strconv.Itoa(t.config.Port))
	dialer := net.Dialer{Timeout: sftpReadyTimeout, KeepAlive: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("could not connect to SFTP server: %w", err)
	}
	deadline := time.Now().Add(sftpReadyTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
	sshConn, channels, requests, err := ssh.NewClientConn(conn, address, clientConfig)
	if err != nil {
		_ = conn.Close()
		return errors.New("SFTP authentication or host verification failed")
	}
	sshClient := ssh.NewClient(sshConn, channels, requests)
	sftpClient, err := sftp.NewClient(sshClient, sftp.MaxPacket(1<<15))
	if err != nil {
		_ = sshClient.Close()
		return fmt.Errorf("could not start SFTP session: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	if err = sftpClient.MkdirAll(t.config.Path); err != nil {
		_ = sftpClient.Close()
		_ = sshClient.Close()
		return fmt.Errorf("could not prepare SFTP backup folder: %w", err)
	}
	t.ssh, t.client = sshClient, sftpClient
	return nil
}

func (t *SFTPTarget) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var err error
	if t.client != nil {
		err = t.client.Close()
		t.client = nil
	}
	if t.ssh != nil {
		if closeErr := t.ssh.Close(); err == nil {
			err = closeErr
		}
		t.ssh = nil
	}
	return err
}

// HostKeyFingerprint returns the fingerprint observed during a successful or
// attempted SSH handshake, for the administrative first-use pinning flow.
func (t *SFTPTarget) HostKeyFingerprint() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fingerprint
}

func (t *SFTPTarget) sftpClient() (*sftp.Client, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.client == nil {
		return nil, errors.New("SFTP target is not open")
	}
	return t.client, nil
}

func (t *SFTPTarget) remote(key string) (string, error) {
	if err := validTargetKey(key, false); err != nil {
		return "", err
	}
	full := pathpkg.Clean(pathpkg.Join(t.config.Path, key))
	if t.config.Path == "." {
		if pathpkg.IsAbs(full) || full == ".." || strings.HasPrefix(full, "../") {
			return "", errors.New("SFTP backup key escapes configured folder")
		}
		return full, nil
	}
	base := strings.TrimSuffix(t.config.Path, "/")
	if full != t.config.Path && !strings.HasPrefix(full, base+"/") {
		return "", errors.New("SFTP backup key escapes configured folder")
	}
	return full, nil
}

func (t *SFTPTarget) Exists(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	client, err := t.sftpClient()
	if err != nil {
		return false, err
	}
	remote, err := t.remote(key)
	if err != nil {
		return false, err
	}
	info, err := client.Lstat(remote)
	if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

func (t *SFTPTarget) List(ctx context.Context, prefix string) ([]string, error) {
	if err := validTargetKey(prefix, true); err != nil {
		return nil, err
	}
	client, err := t.sftpClient()
	if err != nil {
		return nil, err
	}
	base := t.config.Path
	if prefix != "" {
		base = pathpkg.Join(base, prefix)
	}
	out := make([]string, 0)
	entries := 0
	var walk func(remoteDir, relative string, depth int) error
	walk = func(remoteDir, relative string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > sftpListMaxDepth {
			return errors.New("SFTP backup listing is nested too deeply")
		}
		items, readErr := client.ReadDir(remoteDir)
		if errors.Is(readErr, fs.ErrNotExist) || os.IsNotExist(readErr) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		for _, item := range items {
			name := item.Name()
			if name == "." || name == ".." {
				continue
			}
			if name == "" || strings.ContainsAny(name, "/\\") {
				return errors.New("SFTP listing contains an unsafe name")
			}
			childRemote := pathpkg.Join(remoteDir, name)
			childRelative := name
			if relative != "" {
				childRelative = relative + "/" + name
			}
			if item.Mode()&os.ModeSymlink != 0 {
				continue
			}
			entries++
			if entries > sftpListMaxFiles {
				return errors.New("SFTP backup listing has too many entries")
			}
			if item.IsDir() {
				if err = walk(childRemote, childRelative, depth+1); err != nil {
					return err
				}
				continue
			}
			if strings.HasPrefix(name, ".tmp-") {
				continue
			}
			if prefix != "" {
				out = append(out, prefix+"/"+childRelative)
			} else {
				out = append(out, childRelative)
			}
		}
		return nil
	}
	if err = walk(base, "", 0); err != nil {
		return nil, err
	}
	return out, nil
}

func (t *SFTPTarget) Put(ctx context.Context, key, source string) error {
	remote, err := t.remote(key)
	if err != nil {
		return err
	}
	client, err := t.sftpClient()
	if err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err = client.MkdirAll(pathpkg.Dir(remote)); err != nil {
		return err
	}
	temporary := pathpkg.Join(pathpkg.Dir(remote), ".tmp-"+pathpkg.Base(remote))
	output, err := client.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	if _, err = copyWithContext(ctx, output, input); err != nil {
		_ = output.Close()
		_ = client.Remove(temporary)
		return err
	}
	// The OpenSSH fsync extension is best effort.
	_ = output.Sync()
	if err = output.Close(); err != nil {
		_ = client.Remove(temporary)
		return err
	}
	if _, err = client.Lstat(remote); err == nil {
		if err = client.Remove(remote); err != nil {
			_ = client.Remove(temporary)
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) && !os.IsNotExist(err) {
		_ = client.Remove(temporary)
		return err
	}
	if err = client.Rename(temporary, remote); err != nil {
		_ = client.Remove(temporary)
		return err
	}
	return nil
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func (t *SFTPTarget) Get(ctx context.Context, key, destination string) error {
	remote, err := t.remote(key)
	if err != nil {
		return err
	}
	client, err := t.sftpClient()
	if err != nil {
		return err
	}
	input, err := client.Open(remote)
	if err != nil {
		return err
	}
	defer input.Close()
	if err = os.MkdirAll(pathpkg.Dir(destination), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(pathpkg.Dir(destination), ".neocloud-sftp-download-*")
	if err != nil {
		return err
	}
	tmpName := temporary.Name()
	defer os.Remove(tmpName)
	if _, err = copyWithContext(ctx, temporary, input); err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, 0600); err != nil {
		return err
	}
	return os.Rename(tmpName, destination)
}

func (t *SFTPTarget) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := t.sftpClient()
	if err != nil {
		return err
	}
	remote, err := t.remote(key)
	if err != nil {
		return err
	}
	err = client.Remove(remote)
	if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		return nil
	}
	return err
}

func (t *SFTPTarget) DeletePrefix(ctx context.Context, prefix string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := t.sftpClient()
	if err != nil {
		return err
	}
	remote, err := t.remote(prefix)
	if err != nil {
		return err
	}
	if _, err = client.Lstat(remote); errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	err = client.RemoveAll(remote)
	if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		return nil
	}
	return err
}
