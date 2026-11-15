package sync

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// WipedMessage is the status text after the owner signed this device out
// remotely.
const WipedMessage = "Signed out by your account owner"

// onWipeRequested runs on the request that got DEVICE_WIPE_REQUESTED. The wipe
// stops the sync run that request belongs to, so it goes on in the background.
func (e *Engine) onWipeRequested(eraseFiles bool) {
	if !e.wiping.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer e.wiping.Store(false)
		if err := e.WipeLocal(eraseFiles); err != nil {
			e.log.Printf("remote wipe: %v", err)
		}
	}()
}

// WipeLocal is the remote wipe: the token, the sealed key lock and all sync
// state go unconditionally. With eraseFiles the contents of every configured
// sync folder are deleted as well (and the pairs forgotten); without it the
// files and the pairs stay, as with SignOut. Errors from deleting files are
// returned after everything else was done.
func (e *Engine) WipeLocal(eraseFiles bool) error {
	pairs := e.cfg.Get().Pairs
	var errs []error
	err := e.whileStopped(func() error {
		errs = append(errs, e.cfg.Update(func(c *config.Config) error {
			c.Token, c.Username, c.UserID = "", "", 0
			c.KeyLock = nil
			if eraseFiles {
				c.Pairs = nil
			}
			return nil
		}), e.db.Reset())
		if eraseFiles {
			errs = append(errs, e.eraseSyncFolders(pairs))
		}
		return nil
	})
	e.setStatus(func(s *Status) { s.State, s.Message = StateLoggedOut, WipedMessage })
	return errors.Join(append(errs, err)...)
}

func (e *Engine) eraseSyncFolders(pairs []config.Pair) error {
	protected := []string{filepath.Dir(e.cfg.Path())}
	if home, err := os.UserHomeDir(); err == nil {
		protected = append(protected, home)
	}
	var errs []error
	for _, pair := range pairs {
		errs = append(errs, eraseFolderContents(pair.Local, protected...))
	}
	return errors.Join(errs...)
}
