package settingsui

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// deviceNameTTL is how often the page's state call re-reads this device's
// name from the server, so a rename made in the web app shows up here.
const deviceNameTTL = time.Minute

// renameDevice renames this computer in the account's device list and keeps
// the name the server stored.
func (s *Server) renameDevice(r *http.Request) (any, error) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	if strings.TrimSpace(body.Name) == "" {
		return nil, errors.New("enter a name for this computer")
	}
	client, err := s.engine.Client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	device, err := client.RenameDevice(ctx, body.Name)
	if err != nil {
		return nil, err
	}
	s.storeDeviceName(device.Name)
	return map[string]string{"deviceName": device.Name}, nil
}

func (s *Server) storeDeviceName(name string) {
	_ = s.cfg.Update(func(c *config.Config) error {
		c.DeviceName = name
		return nil
	})
	s.mu.Lock()
	s.nameAt = time.Now()
	s.mu.Unlock()
}

// refreshDeviceName re-reads the device name in the background, at most once
// per deviceNameTTL; the next state call shows a name changed elsewhere.
func (s *Server) refreshDeviceName() {
	s.mu.Lock()
	if !s.cfg.Get().LoggedIn() || s.nameBusy || time.Since(s.nameAt) < deviceNameTTL {
		s.mu.Unlock()
		return
	}
	s.nameBusy = true
	s.mu.Unlock()
	go func() {
		defer func() { s.mu.Lock(); s.nameBusy, s.nameAt = false, time.Now(); s.mu.Unlock() }()
		client, err := s.engine.Client()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		me, err := client.Me(ctx)
		if err != nil || me.Device.Name == "" || me.Device.Name == s.cfg.Get().DeviceName {
			return
		}
		_ = s.cfg.Update(func(c *config.Config) error {
			c.DeviceName = me.Device.Name
			return nil
		})
	}()
}
