package externalstorage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/outbound"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
)

const (
	maxActivePerUser = 8
	listLimit        = 5000
)

type entry struct {
	name     string
	folder   bool
	size     int64
	modified time.Time
}

type byteRange struct{ start, end int64 }

// adapter is one connection to a provider. Paths are mount-relative and already
// normalised.
type adapter interface {
	open(context.Context) error
	close()
	list(ctx context.Context, path string, limit int) ([]entry, bool, error)
	// stat returns nil without an error when there is nothing at path.
	stat(ctx context.Context, path string) (*entry, error)
	read(ctx context.Context, path string, part *byteRange) (io.ReadCloser, error)
	// write stores body; size is -1 when unknown.
	write(ctx context.Context, path string, body io.Reader, size int64) error
	mkdir(ctx context.Context, path string) error
	move(ctx context.Context, from, to string) error
	remove(ctx context.Context, path string) error
}

func hostBlocked() error {
	return apperr.New(http.StatusForbidden, "This address is on a private network, which the server does not connect to.", "EXTERNAL_HOST_BLOCKED")
}

func unreachable() error {
	return apperr.New(http.StatusBadGateway, "Could not reach the server.", "EXTERNAL_UNREACHABLE")
}

func notFound() error {
	return apperr.New(http.StatusNotFound, "Not found at the provider.", "EXTERNAL_NOT_FOUND")
}

func nameExists() error {
	return apperr.New(http.StatusConflict, "An item with that name already exists.", "NAME_EXISTS")
}

// guard says where this account's connections may go. Hosts are typed by the
// account holder, so a plain account never reaches the server's own network;
// cloud metadata addresses are refused for everyone.
func (h *Handler) guard(ctx context.Context, userID int64) outbound.Guard {
	allowed := h.allowPrivate
	if !allowed && h.isAdmin != nil {
		admin, err := h.isAdmin(ctx, userID)
		allowed = err == nil && admin
	}
	check := outbound.PublicOnly
	if allowed {
		check = outbound.BlockMetadata
	}
	return outbound.Guard{Check: check, Resolve: h.resolveHost}
}

// enter counts the account's running provider operations.
func (h *Handler) enter(userID int64) (func(), error) {
	h.activeMu.Lock()
	defer h.activeMu.Unlock()
	if h.active[userID] >= maxActivePerUser {
		return nil, tooManyTransfers()
	}
	h.active[userID]++
	return func() {
		h.activeMu.Lock()
		defer h.activeMu.Unlock()
		if h.active[userID]--; h.active[userID] <= 0 {
			delete(h.active, userID)
		}
	}, nil
}

// dial opens a connection of its own for one operation. release closes it and
// frees the account's slot.
func (h *Handler) dial(ctx context.Context, userID int64, m mount) (adapter, func(), error) {
	spec, err := providerFor(m.Provider)
	if err != nil {
		return nil, nil, err
	}
	leave, err := h.enter(userID)
	if err != nil {
		return nil, nil, err
	}
	guard := h.guard(ctx, userID)
	if host := spec.host(m.Config); host != "" {
		if _, err = guard.Lookup(ctx, host); err != nil {
			leave()
			if errors.Is(err, outbound.ErrBlocked) {
				return nil, nil, hostBlocked()
			}
			return nil, nil, unreachable()
		}
	}
	var connected adapter
	switch m.Provider {
	case "s3":
		connected = newS3(m.Config, guard)
	case "webdav":
		connected, err = newWebDAV(m.Config, guard)
	case "sftp":
		connected = newSFTP(m.Config, guard)
	}
	if err == nil {
		err = connected.open(ctx)
	}
	if err != nil {
		if connected != nil {
			connected.close()
		}
		leave()
		return nil, nil, err
	}
	return connected, func() { connected.close(); leave() }, nil
}

type probeResult struct {
	items   int
	hostKey string
}

// probe connects, lists the root and closes again.
func (h *Handler) probe(ctx context.Context, userID int64, m mount) (probeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, h.testTimeout)
	defer cancel()
	var result probeResult
	connected, release, err := h.dial(ctx, userID, m)
	if err == nil {
		defer release()
		var entries []entry
		entries, _, err = connected.list(ctx, "", 50)
		result.items = len(entries)
		if identified, ok := connected.(interface{ hostKey() string }); ok {
			result.hostKey = identified.hostKey()
		}
	}
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		err = apperr.New(http.StatusBadGateway, "The server took too long to answer.", "EXTERNAL_UNREACHABLE")
	}
	return result, err
}
