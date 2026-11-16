package settingsui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/handoff"
)

var errNoSender = errors.New("sending to your devices is not available here")

// sendFile is a file the page may send: what Prepare found.
type sendFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// sendDevice is a device the page lists. Platform is a hint for the icon.
type sendDevice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Platform string `json:"platform"`
}

// sendState is the "Send to device" part of /api/state.
func (s *Server) sendState() map[string]any {
	if s.opts.Sender == nil {
		return map[string]any{"enabled": false}
	}
	return map[string]any{
		"enabled":   true,
		"transfers": s.opts.Sender.Transfers(),
		"maxFiles":  handoff.MaxFiles,
		"maxBytes":  handoff.MaxBytes,
	}
}

// sendDevices lists the devices a send can go to now.
func (s *Server) sendDevices(r *http.Request) (any, error) {
	if s.opts.Sender == nil {
		return nil, errNoSender
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	devices, err := s.opts.Sender.Devices(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sendDevice, len(devices))
	for i, device := range devices {
		out[i] = sendDevice{ID: device.ID, Name: device.Name, Kind: device.Kind, Platform: device.Platform}
	}
	return map[string]any{"devices": out}, nil
}

// inspectPaths answers { files } for paths that can be sent, or the reason
// they cannot.
func (s *Server) inspectPaths(paths []string) (any, error) {
	items, err := handoff.Prepare(paths)
	if err != nil {
		return nil, err
	}
	files := make([]sendFile, len(items))
	for i, item := range items {
		files[i] = sendFile{Path: item.Path, Name: item.Name, Size: item.Size}
	}
	return map[string]any{"files": files}, nil
}

func (s *Server) inspect(r *http.Request) (any, error) {
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	return s.inspectPaths(body.Paths)
}

// startSend sends the chosen files to a device in the background.
func (s *Server) startSend(r *http.Request) (any, error) {
	if s.opts.Sender == nil {
		return nil, errNoSender
	}
	var body struct {
		DeviceID string   `json:"deviceId"`
		Paths    []string `json:"paths"`
	}
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	items, err := handoff.Prepare(body.Paths)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Temporary = s.isStaged(items[i].Path)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	devices, err := s.opts.Sender.Devices(ctx)
	if err != nil {
		return nil, err
	}
	device, found := api.HandoffDevice{}, false
	for _, candidate := range devices {
		if candidate.ID == body.DeviceID {
			device, found = candidate, true
		}
	}
	if !found {
		return nil, errors.New("that device is not online any more")
	}
	return map[string]string{"id": s.opts.Sender.Start(device, items)}, nil
}

func (s *Server) cancelSend(r *http.Request) (any, error) {
	if s.opts.Sender == nil {
		return nil, errNoSender
	}
	s.opts.Sender.Cancel(r.PathValue("id"))
	return ok(), nil
}

func (s *Server) dismissSend(r *http.Request) (any, error) {
	if s.opts.Sender == nil {
		return nil, errNoSender
	}
	s.opts.Sender.Dismiss(r.PathValue("id"))
	return ok(), nil
}

// stageDrop receives one file dropped on the page. A browser gives the page
// the file's bytes but never its path, so the bytes are kept in a private
// temporary folder until the send has used them.
func (s *Server) stageDrop(r *http.Request) (any, error) {
	name := handoff.SafeName(filepath.Base(r.URL.Query().Get("name"))) // never empty or a path
	root, err := s.stagingRoot()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(root, "f")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	size, err := copyTo(path, http.MaxBytesReader(nil, r.Body, handoff.MaxBytes))
	if err != nil {
		os.RemoveAll(dir)
		return nil, errors.New("the file could not be read")
	}
	return sendFile{Path: path, Name: name, Size: size}, nil
}

func copyTo(path string, from io.Reader) (int64, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	size, err := io.Copy(file, from)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return size, err
}

// discardDrops deletes dropped files the person took off the list.
func (s *Server) discardDrops(r *http.Request) (any, error) {
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	for _, path := range body.Paths {
		if s.isStaged(path) {
			os.RemoveAll(filepath.Dir(path))
		}
	}
	return ok(), nil
}

func (s *Server) stagingRoot() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stageDir == "" {
		dir, err := os.MkdirTemp("", "neocloud-send-")
		if err != nil {
			return "", err
		}
		s.stageDir = dir
	}
	return s.stageDir, nil
}

// isStaged reports whether path is a file the page dropped here.
func (s *Server) isStaged(path string) bool {
	s.mu.Lock()
	root := s.stageDir
	s.mu.Unlock()
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func (s *Server) removeStaging() {
	s.mu.Lock()
	root := s.stageDir
	s.stageDir = ""
	s.mu.Unlock()
	if root != "" {
		os.RemoveAll(root)
	}
}
