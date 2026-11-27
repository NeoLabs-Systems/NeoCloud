package files

// The WebDAV methods. Each one maps onto the same transaction helpers as the
// REST routes, so quota, versions, trash, the change journal and activity
// behave the same. Vault folders hold ciphertext names and contents, so WebDAV
// never shows them.

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strconv"

	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neocloud/internal/webdav"
)

func davError(status int, message string) error {
	return webdav.HTTPError{Status: status, Message: message}
}

var (
	errDAVNotFound      = davError(http.StatusNotFound, "File or folder not found")
	errDAVParentMissing = davError(http.StatusConflict, "The parent folder does not exist.")
	errDAVLocked        = davError(http.StatusLocked, "The resource is locked.")
)

type davRequest struct {
	w         http.ResponseWriter
	r         *http.Request
	principal webdav.Principal
	target    *webdav.Target
}

// DAVDispatch serves one signed-in WebDAV request below the account's root.
func (h *Handler) DAVDispatch(w http.ResponseWriter, r *http.Request, principal webdav.Principal, target *webdav.Target) error {
	request := davRequest{w: w, r: r, principal: principal, target: target}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return h.davGet(request)
	case "PROPFIND":
		return h.davPropfind(request)
	case "PROPPATCH":
		return h.davProppatch(request)
	case http.MethodPut:
		return h.davPut(request)
	case "MKCOL":
		return h.davMkcol(request)
	case http.MethodDelete:
		return h.davDelete(request)
	case "COPY", "MOVE":
		return h.davCopyMove(request)
	case "LOCK":
		return h.davLock(request)
	case "UNLOCK":
		return h.davUnlock(request)
	}
	return davError(http.StatusMethodNotAllowed, "Method not allowed.")
}

func (h *Handler) davTx(ctx context.Context, work func(*sql.Tx) error) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = work(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// davResolve walks segments from the account root. found is the node at the
// path or nil; parent is the folder that holds or would hold it, nil for the
// root and when that folder is missing.
func (h *Handler) davResolve(ctx context.Context, uid int64, segments []string) (found, parent *node, err error) {
	err = h.davTx(ctx, func(tx *sql.Tx) error {
		current, err := h.root(tx, uid)
		if err != nil {
			return err
		}
		if len(segments) == 0 {
			found = &current
			return nil
		}
		for i, name := range segments {
			child, err := scanNode(tx.QueryRow(nodeSelect+` WHERE n.user_id=? AND n.parent_id=? AND lower(n.name)=lower(?) AND n.trashed_at IS NULL`, uid, current.ID, name))
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			visible := err == nil && child.VaultID == nil
			if i == len(segments)-1 {
				holder := current
				parent = &holder
				if visible {
					found = &child
				}
			} else if !visible || child.Type != "folder" {
				return nil
			}
			current = child
		}
		return nil
	})
	return found, parent, err
}

func (h *Handler) davExisting(ctx context.Context, uid int64, segments []string) (*node, error) {
	found, _, err := h.davResolve(ctx, uid, segments)
	if err == nil && found == nil {
		err = errDAVNotFound
	}
	return found, err
}

// davAssertUnlocked refuses a write when a lock the request does not present
// applies to the path or, with deep, to anything below it.
func (h *Handler) davAssertUnlocked(request davRequest, segments []string, deep bool) error {
	unlocked, err := h.davLocks.Unlocked(request.r.Context(), request.principal.UserID, webdav.LockKey(segments), webdav.SubmittedTokens(request.r.Header.Get("If")), deep)
	if err == nil && !unlocked {
		err = errDAVLocked
	}
	return err
}

func (h *Handler) davGet(request davRequest) error {
	n, err := h.davExisting(request.r.Context(), request.principal.UserID, request.target.Segments)
	if err != nil {
		return err
	}
	if n.Type == "folder" {
		// Browsers that open the URL get a hint instead of an error.
		body := "This is a NeoCloud WebDAV folder. Connect a WebDAV client to browse it.\n"
		request.w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		request.w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		request.w.WriteHeader(http.StatusOK)
		if request.r.Method != http.MethodHead {
			_, _ = io.WriteString(request.w, body)
		}
		return nil
	}
	file := request.r.Clone(requestauth.WithIdentity(request.r.Context(), requestauth.RequestIdentity{UserID: request.principal.UserID}))
	file.URL.RawQuery = ""
	file.SetPathValue("id", n.ID)
	request.w.Header().Set("OC-FileId", n.ID)
	h.download(request.w, file)
	return nil
}
