package files

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/webdav"
)

const davMaxOwnerRunes = 512

func davLockDiscovery(locks []webdav.Lock) string {
	var out strings.Builder
	for _, lock := range locks {
		out.WriteString(davActiveLock(lock))
	}
	return out.String()
}

func davActiveLock(lock webdav.Lock) string {
	element := func(name, content string) string { return webdav.Element(webdav.DAV, name, content) }
	href := func(value string) string { return element("href", webdav.Escape(value)) }
	owner := ""
	if lock.Owner != "" {
		owner = element("owner", href(lock.Owner))
	}
	return element("activelock",
		element("locktype", element("write", ""))+
			element("lockscope", element(lock.Scope, ""))+
			element("depth", lock.Depth)+
			owner+
			element("timeout", "Second-"+strconv.Itoa(int(lock.Timeout.Seconds())))+
			element("locktoken", href(lock.Token))+
			element("lockroot", href(lock.Href)))
}

func davWriteLock(w http.ResponseWriter, lock webdav.Lock, status int) {
	webdav.WriteXML(w, status, webdav.Document("prop", webdav.Element(webdav.DAV, "lockdiscovery", davActiveLock(lock))))
}

func davParseLockInfo(body []byte) (scope, owner string, err error) {
	root, err := webdav.ParseXML(body)
	if err != nil {
		return "", "", err
	}
	if !root.Is(webdav.DAV, "lockinfo") {
		return "", "", davError(http.StatusBadRequest, "Expected a DAV:lockinfo body.")
	}
	wanted := root.Child(webdav.DAV, "lockscope")
	switch {
	case wanted.Child(webdav.DAV, "shared") != nil:
		scope = "shared"
	case wanted.Child(webdav.DAV, "exclusive") != nil:
		scope = "exclusive"
	default:
		return "", "", davError(http.StatusBadRequest, "lockscope must be exclusive or shared.")
	}
	runes := []rune(root.Child(webdav.DAV, "owner").InnerText())
	return scope, string(runes[:min(len(runes), davMaxOwnerRunes)]), nil
}

// davLock takes or refreshes a lock. Locking a path that does not exist yet
// creates an empty file there (RFC 4918 9.10.4), which is how Windows and
// Office start a save.
func (h *Handler) davLock(request davRequest) error {
	r, uid, segments := request.r, request.principal.UserID, request.target.Segments
	body, err := webdav.ReadBody(r)
	if err != nil {
		return err
	}
	timeout := webdav.ParseTimeout(r.Header.Get("Timeout"))
	key := webdav.LockKey(segments)
	if len(bytes.TrimSpace(body)) == 0 {
		lock, ok, err := h.davLocks.Refresh(r.Context(), uid, key, webdav.SubmittedTokens(r.Header.Get("If")), timeout)
		if err != nil {
			return err
		}
		if !ok {
			return davError(http.StatusPreconditionFailed, "No matching lock to refresh.")
		}
		davWriteLock(request.w, lock, http.StatusOK)
		return nil
	}
	scope, owner, err := davParseLockInfo(body)
	if err != nil {
		return err
	}
	found, parent, err := h.davResolve(r.Context(), uid, segments)
	if err != nil {
		return err
	}
	if found == nil && parent == nil {
		return errDAVParentMissing
	}
	depth, folder := "0", found != nil && found.Type == "folder"
	if folder && strings.TrimSpace(r.Header.Get("Depth")) != "0" {
		depth = "infinity"
	}
	lock, ok, err := h.davLocks.Create(r.Context(), uid, webdav.Lock{
		Key: key, Href: webdav.Href(request.target.Base, segments, folder), Depth: depth, Scope: scope, Owner: owner, Timeout: timeout,
	})
	if errors.Is(err, webdav.ErrTooManyLocks) {
		return webdav.HTTPError{Status: http.StatusServiceUnavailable, Message: "Too many locks are held. Try again later."}
	}
	if err != nil {
		return err
	}
	if !ok {
		return errDAVLocked
	}
	status := http.StatusOK
	if found == nil {
		if _, err = h.davStore(r.Context(), uid, parent.ID, segments[len(segments)-1], "fail", "", http.NoBody, 0); err != nil {
			_, removeErr := h.davLocks.Remove(r.Context(), uid, key, lock.Token)
			return errors.Join(err, removeErr)
		}
		status = http.StatusCreated
	}
	request.w.Header().Set("Lock-Token", "<"+lock.Token+">")
	davWriteLock(request.w, lock, status)
	return nil
}

func (h *Handler) davUnlock(request davRequest) error {
	token := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(request.r.Header.Get("Lock-Token")), "<"), ">")
	removed, err := h.davLocks.Remove(request.r.Context(), request.principal.UserID, webdav.LockKey(request.target.Segments), token)
	if err != nil {
		return err
	}
	if !removed {
		return webdav.HTTPError{Status: http.StatusConflict, Message: "No such lock on this resource.", Condition: "lock-token-matches-request-uri"}
	}
	request.w.WriteHeader(http.StatusNoContent)
	return nil
}
