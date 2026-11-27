// Package documentconvert provides bounded conversion of untrusted office
// documents using an optional local LibreOffice installation.
package documentconvert

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	MaxSourceBytes = int64(100 << 20)
	MaxPDFBytes    = int64(128 << 20)
	Timeout        = 120 * time.Second
)

var supported = map[string]bool{
	".doc": true, ".docx": true, ".odt": true, ".rtf": true,
	".xls": true, ".xlsx": true, ".ods": true,
	".ppt": true, ".pptx": true, ".odp": true,
}

var convertible = map[string]bool{
	".doc": true, ".docx": true, ".odt": true, ".rtf": true, ".txt": true,
	".xls": true, ".xlsx": true, ".ods": true, ".csv": true,
	".ppt": true, ".pptx": true, ".odp": true,
}

var slots = make(chan struct{}, 2)

// Settings is the server's configuration of the converter.
type Settings struct {
	// Disabled turns Office previews off, so that no byte is ever written
	// to disk in plain text.
	Disabled bool
	// Binary names the LibreOffice executable when it is not on PATH.
	Binary string
	// SandboxOff runs the converter without the network-less wrapper.
	SandboxOff bool
}

var settings Settings
var binaryOnce = &sync.Once{}
var binaryPath string
var sandboxOnce = &sync.Once{}
var sandbox []string

// Configure applies the settings. It is called once while the server starts.
func Configure(configured Settings) {
	settings = configured
	binaryOnce, binaryPath = &sync.Once{}, ""
	sandboxOnce, sandbox = &sync.Once{}, nil
}

// profileSettings stop a document from reaching out: macros off, no linked or
// remote content, no link or external data refresh. Writer's Link is 0 for
// "never"; Calc numbers it differently (0 always, 1 never, 2 on request).
var profileSettings = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<oor:items xmlns:oor="http://openoffice.org/2001/registry" xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
<item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="MacroSecurityLevel" oor:op="fuse"><value>3</value></prop></item>
<item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="DisableMacrosExecution" oor:op="fuse"><value>true</value></prop></item>
<item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="BlockUntrustedRefererLinks" oor:op="fuse"><value>true</value></prop></item>
<item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="DisableActiveContent" oor:op="fuse"><value>true</value></prop></item>
<item oor:path="/org.openoffice.Office.Writer/Content/Update"><prop oor:name="Link" oor:op="fuse"><value>0</value></prop></item>
<item oor:path="/org.openoffice.Office.Calc/Content/Update"><prop oor:name="Link" oor:op="fuse"><value>1</value></prop></item>
<item oor:path="/org.openoffice.Office.Jobs/Jobs/org.openoffice.Office.Jobs:Job['UpdateCheck']/Arguments"><prop oor:name="AutoCheckEnabled" oor:op="fuse"><value>false</value></prop></item>
</oor:items>`)

var ErrUnavailable = errors.New("office conversion unavailable")
var ErrUnsupported = errors.New("unsupported office document")
var ErrTooLarge = errors.New("office document exceeds conversion limit")
var ErrConversion = errors.New("office document conversion failed")

// Supports reports whether filename uses an extension accepted by LibreOffice.
func Supports(filename string) bool { return supported[strings.ToLower(filepath.Ext(filename))] }

// Available reports whether conversion is enabled and a LibreOffice binary
// can be found without launching the converter.
func Available() bool { return !settings.Disabled && findBinary() != "" }

func findBinary() string {
	binaryOnce.Do(func() {
		candidates := []string{settings.Binary, "soffice", "libreoffice"}
		if runtime.GOOS == "darwin" {
			candidates = append(candidates, "/Applications/LibreOffice.app/Contents/MacOS/soffice")
		}
		if runtime.GOOS == "windows" {
			candidates = append(candidates, `C:\Program Files\LibreOffice\program\soffice.exe`)
		}
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			if path, err := exec.LookPath(candidate); err == nil {
				binaryPath = path
				return
			}
		}
	})
	return binaryPath
}

// ConvertToPDF reads one supported document and returns a bounded PDF.
// LibreOffice runs in a private profile and temporary directory with a minimal
// environment. On Linux, network access is removed when bwrap or unshare is
// usable; otherwise the hardened LibreOffice profile is still used.
func ConvertToPDF(ctx context.Context, name string, src io.Reader) ([]byte, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if !convertible[ext] {
		return nil, ErrUnsupported
	}
	if src == nil {
		return nil, ErrUnsupported
	}
	binary := findBinary()
	if binary == "" || !Available() {
		return nil, ErrUnavailable
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	work, err := os.MkdirTemp("", "neocloud-office-")
	if err != nil {
		return nil, ErrConversion
	}
	defer os.RemoveAll(work)
	if err = os.Chmod(work, 0700); err != nil {
		return nil, ErrConversion
	}
	source := filepath.Join(work, "document"+ext)
	f, err := os.OpenFile(source, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, ErrConversion
	}
	written, copyErr := io.Copy(f, io.LimitReader(src, MaxSourceBytes+1))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		return nil, ErrConversion
	}
	if written < 1 || written > MaxSourceBytes {
		return nil, ErrTooLarge
	}
	profile := filepath.Join(work, "profile", "user")
	if err = os.MkdirAll(profile, 0700); err != nil {
		return nil, ErrConversion
	}
	if err = os.WriteFile(filepath.Join(profile, "registrymodifications.xcu"), profileSettings, 0600); err != nil {
		return nil, ErrConversion
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	args := []string{"--headless", "--norestore", "-env:UserInstallation=" + fileURL(filepath.Join(work, "profile")), "--convert-to", "pdf", "--outdir", work, source}
	command, args := commandFor(binary, args, work)
	env := []string{"PATH=" + minimalPath(), "LANG=C.UTF-8", "HOME=" + work, "TMPDIR=" + work}
	if runtime.GOOS == "windows" {
		env = append(env, "SystemRoot="+os.Getenv("SystemRoot"), "TEMP="+work, "TMP="+work)
	}
	// Output goes to the null device: a pipe would keep Wait waiting for any
	// helper the converter leaves behind.
	cmd := exec.Command(command, args...)
	cmd.Dir = work
	cmd.Env = env
	cmd.SysProcAttr = processGroupAttr()
	if err = cmd.Start(); err != nil {
		return nil, ErrUnavailable
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	finished, monitorErr := monitorOutput(ctx, filepath.Join(work, "document.pdf"), wait)
	// Take whatever it started with it, and only free the slot once it is gone.
	killProcessGroup(cmd)
	if !finished {
		<-wait
	}
	if monitorErr != nil {
		return nil, ErrConversion
	}
	result, err := os.ReadFile(filepath.Join(work, "document.pdf"))
	if err != nil || int64(len(result)) < 5 || int64(len(result)) > MaxPDFBytes || !bytes.HasPrefix(result, []byte("%PDF-")) {
		return nil, ErrConversion
	}
	return result, nil
}

func fileURL(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	return u.String()
}

func minimalPath() string {
	if p := os.Getenv("PATH"); p != "" {
		return p
	}
	return "/usr/bin:/bin"
}

func monitorOutput(ctx context.Context, output string, wait <-chan error) (bool, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-wait:
			if err != nil {
				return true, err
			}
			info, statErr := os.Stat(output)
			if statErr != nil || info.Size() < 5 || info.Size() > MaxPDFBytes {
				return true, ErrConversion
			}
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		case <-ticker.C:
			if info, err := os.Stat(output); err == nil && info.Size() > MaxPDFBytes {
				return false, ErrTooLarge
			}
		}
	}
}

// commandFor wraps the converter so that it has no network, on Linux hosts
// that allow it.
func commandFor(binary string, args []string, work string) (string, []string) {
	wrapper := findSandbox()
	if len(wrapper) == 0 {
		return binary, args
	}
	if filepath.Base(wrapper[0]) == "bwrap" {
		return wrapper[0], append(bubblewrapArgs(binary, work), args...)
	}
	return wrapper[0], append(append(append([]string{}, wrapper[1:]...), binary), args...)
}

// findSandbox probes the wrappers once, by running `true` through them.
func findSandbox() []string {
	if settings.SandboxOff || runtime.GOOS != "linux" {
		return nil
	}
	sandboxOnce.Do(func() {
		if wrapper, err := exec.LookPath("bwrap"); err == nil && probeSandbox(wrapper, bubblewrapArgs("true", os.TempDir())) {
			sandbox = []string{wrapper}
			return
		}
		if wrapper, err := exec.LookPath("unshare"); err == nil {
			if probeSandbox(wrapper, []string{"--net", "--map-root-user", "--", "true"}) {
				sandbox = []string{wrapper, "--net", "--map-root-user", "--"}
				return
			}
		}
		slog.Warn("Office previews run with network access: neither bwrap nor unshare can drop it on this host.")
	})
	return sandbox
}

// bubblewrapArgs makes the host filesystem read-only and overlays only the
// private conversion directory as writable. LibreOffice still needs to read
// installed libraries and data, so this limits impact on host state rather
// than hiding host files from a compromised converter.
func bubblewrapArgs(command, work string) []string {
	return []string{"--ro-bind", "/", "/", "--dev", "/dev", "--bind", work, work, "--unshare-net", "--die-with-parent", "--", command}
}

func probeSandbox(wrapper string, args []string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, wrapper, args...)
	probe.Env = []string{"PATH=" + minimalPath()}
	return probe.Run() == nil
}

func killProcessGroup(cmd *exec.Cmd) {
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
		return
	}
	_ = platformKillGroup(cmd.Process.Pid)
}
