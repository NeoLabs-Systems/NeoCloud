// Package instance keeps the tray app to one copy per config folder. The
// first copy holds instance.lock and listens on a random loopback port; it
// writes the port and a random token to instance.json (0600, in the 0700
// config folder). A second launch finds the lock taken, sends the token
// and "open" to that port, and exits: the running copy shows its window.
// Other local users can reach the port but cannot read the token.
package instance

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/filelock"
)

// ErrRunning means another copy holds the lock.
var ErrRunning = errors.New("NeoCloud is already running")

const (
	lockName = "instance.lock"
	infoName = "instance.json"
	// waitForInfo covers a first copy that holds the lock but has not
	// written its port yet (two launches in quick succession).
	waitForInfo = 5 * time.Second
	ioTimeout   = 5 * time.Second
)

// info is instance.json.
type info struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// Instance is the running copy's lock and listener.
type Instance struct {
	lock     *os.File
	listener net.Listener
	token    string
	infoPath string
}

// Acquire takes the lock in dir and starts listening. It returns
// ErrRunning when another copy already holds the lock.
func Acquire(dir string) (*Instance, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.TryLock(lock); err != nil {
		lock.Close()
		return nil, ErrRunning
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		lock.Close()
		return nil, err
	}
	inst := &Instance{
		lock:     lock,
		listener: listener,
		token:    randomToken(),
		infoPath: filepath.Join(dir, infoName),
	}
	err = writeInfo(inst.infoPath, info{Port: listener.Addr().(*net.TCPAddr).Port, Token: inst.token})
	if err != nil {
		inst.Close()
		return nil, err
	}
	return inst, nil
}

// Serve calls open for every valid request until Close. Requests that
// arrive before Serve wait in the listener's backlog.
func (i *Instance) Serve(open func()) {
	for {
		conn, err := i.listener.Accept()
		if err != nil {
			return // closed
		}
		go i.handle(conn, open)
	}
}

func (i *Instance) handle(conn net.Conn, open func()) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(ioTimeout))
	line, err := bufio.NewReader(io.LimitReader(conn, 256)).ReadString('\n')
	if err != nil {
		return
	}
	command, token, _ := strings.Cut(strings.TrimSpace(line), " ")
	if command != "open" || subtle.ConstantTimeCompare([]byte(token), []byte(i.token)) != 1 {
		return
	}
	fmt.Fprint(conn, "ok\n")
	open()
}

// Close stops listening and releases the lock.
func (i *Instance) Close() error {
	os.Remove(i.infoPath)
	err := i.listener.Close()
	i.lock.Close()
	return err
}

// Show asks the copy that holds the lock in dir to open its window.
func Show(dir string) error {
	path := filepath.Join(dir, infoName)
	deadline := time.Now().Add(waitForInfo)
	for {
		err := sendOpen(path)
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func sendOpen(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var running info
	if err := json.Unmarshal(raw, &running); err != nil {
		return err
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", running.Port), ioTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(ioTimeout))
	if _, err := fmt.Fprintf(conn, "open %s\n", running.Token); err != nil {
		return err
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return fmt.Errorf("the running copy did not answer: %w", err)
	}
	if reply != "ok\n" {
		return errors.New("the running copy refused the request")
	}
	return nil
}

// writeInfo replaces instance.json atomically, readable by its owner only.
func writeInfo(path string, value info) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".instance-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err) // crypto/rand never fails on supported systems
	}
	return hex.EncodeToString(buf)
}
