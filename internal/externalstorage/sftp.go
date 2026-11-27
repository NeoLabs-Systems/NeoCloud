package externalstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/outbound"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// A folder on another machine over SFTP, password or private-key login. The
// server's host key is remembered when the mount is first tested (a hex SHA-256
// fingerprint) and must match from then on. Symbolic links are neither listed
// nor followed, so a link can never lead out of the attached folder.

const (
	handshakeTimeout = 20 * time.Second
	maxRemoveDepth   = 256
)

var errHostKeyChanged = errors.New("host key changed")

type sftpAdapter struct {
	config      settings
	guard       outbound.Guard
	base        string
	fingerprint string
	ssh         *ssh.Client
	client      *sftp.Client
	stop        func() bool
}

func newSFTP(config settings, guard outbound.Guard) *sftpAdapter {
	return &sftpAdapter{config: config, guard: guard, base: path.Clean("/" + config.text("path"))}
}

func (a *sftpAdapter) hostKey() string { return a.fingerprint }

func authFailed() error {
	return apperr.New(http.StatusBadGateway, "The server refused the user name, password or key.", "EXTERNAL_AUTH_FAILED")
}

func (a *sftpAdapter) authMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if key := a.config.text("privateKey"); key != "" {
		signer, err := ssh.ParsePrivateKey([]byte(key))
		var locked *ssh.PassphraseMissingError
		if errors.As(err, &locked) {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(key), []byte(a.config.text("passphrase")))
		}
		if err != nil {
			return nil, authFailed()
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if password := a.config.text("password"); password != "" {
		methods = append(methods, ssh.Password(password))
	}
	return methods, nil
}

func translate(err error) error {
	if _, ok := apperr.As(err); ok || err == nil {
		return err
	}
	switch {
	case errors.Is(err, outbound.ErrBlocked):
		return hostBlocked()
	case errors.Is(err, errHostKeyChanged):
		return apperr.New(http.StatusBadGateway, "The server’s identity changed. Remove and add the connection again if you trust the change.", "EXTERNAL_HOST_KEY")
	case errors.Is(err, fs.ErrNotExist):
		return notFound()
	case errors.Is(err, fs.ErrPermission):
		return apperr.New(http.StatusBadGateway, "The server denied access.", "EXTERNAL_FORBIDDEN")
	case strings.Contains(err.Error(), "unable to authenticate"):
		return authFailed()
	}
	return unreachable()
}

func (a *sftpAdapter) open(ctx context.Context) error {
	methods, err := a.authMethods()
	if err != nil {
		return err
	}
	port := 22
	if value, ok := a.config["port"].(float64); ok {
		port = int(value)
	}
	address := net.JoinHostPort(a.config.text("host"), strconv.Itoa(port))
	conn, err := a.guard.DialContext(ctx, "tcp", address)
	if err != nil {
		return translate(err)
	}
	// Ending the request ends every pending operation.
	a.stop = context.AfterFunc(ctx, func() { _ = conn.Close() })
	handshake := time.AfterFunc(handshakeTimeout, func() { _ = conn.Close() })
	defer handshake.Stop()
	pinned := a.config.text("hostKey")
	sshConn, channels, requests, err := ssh.NewClientConn(conn, address, &ssh.ClientConfig{
		User: a.config.text("username"),
		Auth: methods,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			sum := sha256.Sum256(key.Marshal())
			a.fingerprint = hex.EncodeToString(sum[:])
			if pinned != "" && pinned != a.fingerprint {
				return errHostKeyChanged
			}
			return nil
		},
	})
	if err != nil {
		_ = conn.Close()
		return translate(err)
	}
	a.ssh = ssh.NewClient(sshConn, channels, requests)
	if a.client, err = sftp.NewClient(a.ssh); err != nil {
		return translate(err)
	}
	if _, err = a.client.Stat(a.base); errors.Is(err, fs.ErrNotExist) {
		return apperr.New(http.StatusNotFound, "That folder does not exist on the server.", "EXTERNAL_NOT_FOUND")
	}
	return translate(err)
}

func (a *sftpAdapter) close() {
	if a.stop != nil {
		a.stop()
	}
	if a.client != nil {
		_ = a.client.Close()
	}
	if a.ssh != nil {
		_ = a.ssh.Close()
	}
}

// target is the remote path of a mount path. A symbolic link on the way is
// refused.
func (a *sftpAdapter) target(relative string) (string, error) {
	full := path.Join(a.base, relative)
	if full != a.base && !strings.HasPrefix(full, strings.TrimSuffix(a.base, "/")+"/") {
		return "", invalidPath()
	}
	current := a.base
	segments := strings.Split(relative, "/")
	for _, segment := range segments[:len(segments)-1] {
		current = path.Join(current, segment)
		info, err := a.client.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return "", translate(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", invalidPath()
		}
	}
	return full, nil
}

// lstat is nil without an error when nothing is there.
func (a *sftpAdapter) lstat(full string) (os.FileInfo, error) {
	info, err := a.client.Lstat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return info, translate(err)
}

func sftpEntry(name string, info os.FileInfo) entry {
	found := entry{name: name, folder: info.IsDir(), modified: info.ModTime()}
	if !found.folder {
		found.size = info.Size()
	}
	return found
}

func (a *sftpAdapter) list(_ context.Context, relative string, limit int) ([]entry, bool, error) {
	full, err := a.target(relative)
	if err != nil {
		return nil, false, err
	}
	// ReadDir would follow a link that was named directly.
	if relative != "" {
		info, err := a.lstat(full)
		if err != nil {
			return nil, false, err
		}
		if info == nil || !info.IsDir() {
			return nil, false, notFound()
		}
	}
	infos, err := a.client.ReadDir(full)
	if err != nil {
		return nil, false, translate(err)
	}
	entries := []entry{}
	for _, info := range infos {
		if info.Mode()&os.ModeSymlink != 0 || !safeListedName(info.Name()) {
			continue
		}
		if len(entries) == limit {
			return entries, true, nil
		}
		entries = append(entries, sftpEntry(info.Name(), info))
	}
	return entries, false, nil
}

func (a *sftpAdapter) stat(_ context.Context, relative string) (*entry, error) {
	full, err := a.target(relative)
	if err != nil {
		return nil, err
	}
	info, err := a.lstat(full)
	if err != nil || info == nil || info.Mode()&os.ModeSymlink != 0 {
		return nil, err
	}
	found := sftpEntry(baseName(relative), info)
	return &found, nil
}

func (a *sftpAdapter) read(_ context.Context, relative string, part *byteRange) (io.ReadCloser, error) {
	full, err := a.target(relative)
	if err != nil {
		return nil, err
	}
	file, err := a.client.Open(full)
	if err != nil {
		return nil, translate(err)
	}
	if part == nil {
		return file, nil
	}
	return struct {
		io.Reader
		io.Closer
	}{io.NewSectionReader(file, part.start, part.end-part.start+1), file}, nil
}

func (a *sftpAdapter) write(_ context.Context, relative string, body io.Reader, _ int64) error {
	full, err := a.target(relative)
	if err != nil {
		return err
	}
	info, err := a.lstat(full)
	if err != nil {
		return err
	}
	// Opening a link would write wherever it points.
	if info != nil && info.Mode()&os.ModeSymlink != 0 {
		return invalidPath()
	}
	file, err := a.client.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return translate(err)
	}
	_, err = io.Copy(file, body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return translate(err)
}

func (a *sftpAdapter) mkdir(_ context.Context, relative string) error {
	full, err := a.target(relative)
	if err != nil {
		return err
	}
	return translate(a.client.Mkdir(full))
}

func (a *sftpAdapter) move(_ context.Context, from, to string) error {
	source, err := a.target(from)
	if err != nil {
		return err
	}
	destination, err := a.target(to)
	if err != nil {
		return err
	}
	existing, err := a.lstat(destination)
	if err != nil {
		return err
	}
	if existing != nil {
		return nameExists()
	}
	return translate(a.client.Rename(source, destination))
}

func (a *sftpAdapter) remove(ctx context.Context, relative string) error {
	full, err := a.target(relative)
	if err != nil {
		return err
	}
	info, err := a.lstat(full)
	if err != nil {
		return err
	}
	if info == nil {
		return notFound()
	}
	return translate(a.removeAll(ctx, full, info, 0))
}

func (a *sftpAdapter) removeAll(ctx context.Context, full string, info os.FileInfo, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !info.IsDir() {
		return a.client.Remove(full)
	}
	if depth > maxRemoveDepth {
		return apperr.New(http.StatusRequestEntityTooLarge, "This folder holds too many items to change in one go.", "EXTERNAL_TOO_LARGE")
	}
	children, err := a.client.ReadDir(full)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err = a.removeAll(ctx, path.Join(full, child.Name()), child, depth+1); err != nil {
			return err
		}
	}
	return a.client.RemoveDirectory(full)
}
