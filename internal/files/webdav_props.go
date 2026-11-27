package files

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/webdav"
	"golang.org/x/sys/unix"
)

type davName struct{ namespace, name string }

// Every property served. allprop answers the first davAllPropCount (the DAV:
// ones of RFC 4918); quota and the oc:/nc: ones only when asked for by name,
// as Nextcloud clients do.
var davNames = []davName{
	{webdav.DAV, "displayname"}, {webdav.DAV, "getcontentlength"}, {webdav.DAV, "getcontenttype"},
	{webdav.DAV, "getetag"}, {webdav.DAV, "getlastmodified"}, {webdav.DAV, "creationdate"},
	{webdav.DAV, "resourcetype"}, {webdav.DAV, "supportedlock"}, {webdav.DAV, "lockdiscovery"},
	{webdav.DAV, "quota-used-bytes"}, {webdav.DAV, "quota-available-bytes"},
	{webdav.OC, "fileid"}, {webdav.OC, "favorite"}, {webdav.OC, "size"}, {webdav.NC, "has-preview"},
}

const davAllPropCount = 9

type davPropfindRequest struct {
	mode  string // allprop, propname or prop
	names []davName
}

func davRequestedNames(element *webdav.XMLNode) []davName {
	names := make([]davName, 0, len(element.Children))
	for _, child := range element.Children {
		names = append(names, davName{child.Namespace, child.Name})
	}
	return names
}

func davParsePropfind(body []byte) (davPropfindRequest, error) {
	all := davPropfindRequest{mode: "allprop", names: davNames[:davAllPropCount:davAllPropCount]}
	if strings.TrimSpace(string(body)) == "" {
		return all, nil
	}
	root, err := webdav.ParseXML(body)
	if err != nil {
		return all, err
	}
	if !root.Is(webdav.DAV, "propfind") {
		return all, davError(http.StatusBadRequest, "Expected a DAV:propfind body.")
	}
	if root.Child(webdav.DAV, "propname") != nil {
		return davPropfindRequest{mode: "propname", names: davNames}, nil
	}
	if prop := root.Child(webdav.DAV, "prop"); prop != nil {
		return davPropfindRequest{mode: "prop", names: davRequestedNames(prop)}, nil
	}
	if include := root.Child(webdav.DAV, "include"); include != nil {
		all.names = append(all.names, davRequestedNames(include)...)
	}
	return all, nil
}

func davStoredTime(value string) (time.Time, bool) {
	for _, layout := range []string{time.DateTime, time.RFC3339Nano} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func davSupportedLock() string {
	entries := ""
	for _, scope := range []string{"exclusive", "shared"} {
		entries += webdav.Element(webdav.DAV, "lockentry",
			webdav.Element(webdav.DAV, "lockscope", webdav.Element(webdav.DAV, scope, ""))+
				webdav.Element(webdav.DAV, "locktype", webdav.Element(webdav.DAV, "write", "")))
	}
	return entries
}

type davItem struct {
	node         node
	segments     []string
	href         string
	usedBytes    int64
	availability *int64
	folderBytes  int64
	locks        []webdav.Lock
	displayName  string
}

// davValue is one property as XML content; found is false when the node has
// no such property.
func davValue(item davItem, name davName) (value string, found bool) {
	n := item.node
	file := n.Type == "file"
	modified, _ := davStoredTime(n.ModifiedAt)
	switch name.namespace + " " + name.name {
	case "DAV: displayname":
		return webdav.Escape(item.displayName), true
	case "DAV: getcontentlength":
		return strconv.FormatInt(n.Size, 10), file
	case "DAV: getcontenttype":
		return webdav.Escape(mimeOrDefault(n.Mime)), file
	case "DAV: getetag":
		// A folder's etag changes whenever something directly inside it does,
		// so clients notice new and removed children.
		etag := n.Etag
		if !file {
			etag += "-" + strconv.FormatInt(modified.Unix(), 10)
		}
		return webdav.Escape(`"` + etag + `"`), true
	case "DAV: getlastmodified":
		return modified.Format(http.TimeFormat), true
	case "DAV: creationdate":
		created, _ := davStoredTime(n.CreatedAt)
		return created.Format("2006-01-02T15:04:05Z"), true
	case "DAV: resourcetype":
		if file {
			return "", true
		}
		return webdav.Element(webdav.DAV, "collection", ""), true
	case "DAV: supportedlock":
		return davSupportedLock(), true
	case "DAV: lockdiscovery":
		return davLockDiscovery(webdav.Covering(item.locks, webdav.LockKey(item.segments))), true
	case "DAV: quota-used-bytes":
		return strconv.FormatInt(item.usedBytes, 10), !file
	case "DAV: quota-available-bytes":
		if file || item.availability == nil {
			return "", false
		}
		return strconv.FormatInt(*item.availability, 10), true
	case webdav.OC + " fileid":
		return webdav.Escape(n.ID), true
	case webdav.OC + " favorite":
		if n.Favorite {
			return "1", true
		}
		return "0", true
	case webdav.OC + " size":
		if file {
			return strconv.FormatInt(n.Size, 10), true
		}
		return strconv.FormatInt(item.folderBytes, 10), true
	case webdav.NC + " has-preview":
		return "false", true
	}
	return "", false
}

func davPropstat(status int, content string) string {
	return webdav.Element(webdav.DAV, "propstat",
		webdav.Element(webdav.DAV, "prop", content)+
			webdav.Element(webdav.DAV, "status", "HTTP/1.1 "+strconv.Itoa(status)+" "+http.StatusText(status)))
}

func davResponse(href, propstats string) string {
	return webdav.Element(webdav.DAV, "response", webdav.Element(webdav.DAV, "href", webdav.Escape(href))+propstats)
}

func davPropfindResponse(item davItem, request davPropfindRequest) string {
	var found, missing strings.Builder
	for _, name := range request.names {
		value, ok := davValue(item, name)
		switch {
		case !ok:
			// allprop lists only what the node has; a missing named prop is a 404.
			if request.mode != "allprop" {
				missing.WriteString(webdav.Element(name.namespace, name.name, ""))
			}
		case request.mode == "propname":
			found.WriteString(webdav.Element(name.namespace, name.name, ""))
		default:
			found.WriteString(webdav.Element(name.namespace, name.name, value))
		}
	}
	propstats := ""
	if found.Len() > 0 || missing.Len() == 0 {
		propstats = davPropstat(http.StatusOK, found.String())
	}
	if missing.Len() > 0 {
		propstats += davPropstat(http.StatusNotFound, missing.String())
	}
	return davResponse(item.href, propstats)
}

func (h *Handler) davChildren(ctx context.Context, uid int64, folderID string) ([]node, error) {
	rows, err := h.db.QueryContext(ctx, nodeSelect+` WHERE n.user_id=? AND n.parent_id=? AND n.trashed_at IS NULL AND n.vault_id IS NULL ORDER BY (n.type='folder') DESC,lower(n.name),n.id`, uid, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	children := []node{}
	for rows.Next() {
		child, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	return children, rows.Err()
}

func (h *Handler) davFolderBytes(ctx context.Context, uid int64, folderID string) (int64, error) {
	var total int64
	err := h.db.QueryRowContext(ctx, `WITH RECURSIVE down(id) AS (
		SELECT id FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.id FROM nodes n JOIN down ON n.parent_id=down.id WHERE n.trashed_at IS NULL
	) SELECT COALESCE(SUM(nodes.size),0) FROM down JOIN nodes ON nodes.id=down.id WHERE nodes.type='file' AND nodes.vault_id IS NULL`, folderID, uid).Scan(&total)
	return total, err
}

// diskRoom is what the storage disk takes before uploads are refused.
func (h *Handler) diskRoom() (int64, bool) {
	var stats unix.Statfs_t
	if unix.Statfs(h.blobDir, &stats) != nil {
		return 0, false
	}
	return max(int64(stats.Bavail)*int64(stats.Bsize)-h.minFreeDiskBytes, 0), true
}

// davQuota is what the account uses and may still store: its quota, and never
// more than the disk takes before uploads are refused. available is nil when
// neither is known.
func (h *Handler) davQuota(ctx context.Context, uid int64) (used int64, available *int64, err error) {
	var quota sql.NullInt64
	var reserved int64
	err = h.db.QueryRowContext(ctx, `SELECT quota_bytes,used_bytes,(SELECT COALESCE(SUM(total_size),0) FROM upload_sessions WHERE user_id=users.id AND datetime(expires_at)>datetime('now')) FROM users WHERE id=?`, uid).Scan(&quota, &used, &reserved)
	if err != nil {
		return 0, nil, err
	}
	limit := h.storagePolicy().DefaultQuotaBytes
	if quota.Valid {
		limit = quota.Int64
	}
	if limit >= 0 {
		remaining := max(limit-used-reserved, 0)
		available = &remaining
	}
	if disk, known := h.diskRoom(); known && (available == nil || disk < *available) {
		available = &disk
	}
	return used, available, nil
}

func (h *Handler) davPropfind(request davRequest) error {
	r, uid, target := request.r, request.principal.UserID, request.target
	depth := strings.ToLower(strings.TrimSpace(r.Header.Get("Depth")))
	switch depth {
	case "infinity":
		// RFC 4918 9.1: say why, so clients fall back to Depth 1.
		return webdav.HTTPError{Status: http.StatusForbidden, Message: "Depth infinity is not supported.", Condition: "propfind-finite-depth"}
	case "", "0", "1":
	default:
		return davError(http.StatusBadRequest, "Depth must be 0, 1 or infinity.")
	}
	body, err := webdav.ReadBody(r)
	if err != nil {
		return err
	}
	asked, err := davParsePropfind(body)
	if err != nil {
		return err
	}
	root, err := h.davExisting(r.Context(), uid, target.Segments)
	if err != nil {
		return err
	}
	used, available, err := h.davQuota(r.Context(), uid)
	if err != nil {
		return err
	}
	locks, err := h.davLocks.All(r.Context(), uid)
	if err != nil {
		return err
	}
	wantsSize := false
	for _, name := range asked.names {
		wantsSize = wantsSize || asked.mode != "propname" && name == davName{webdav.OC, "size"}
	}
	nodes := []node{*root}
	if depth != "0" && root.Type == "folder" {
		children, err := h.davChildren(r.Context(), uid, root.ID)
		if err != nil {
			return err
		}
		nodes = append(nodes, children...)
	}
	var responses strings.Builder
	for i, n := range nodes {
		segments := target.Segments
		if i > 0 {
			segments = append(append([]string(nil), target.Segments...), n.Name)
		}
		item := davItem{node: n, segments: segments, href: webdav.Href(target.Base, segments, n.Type == "folder"),
			usedBytes: used, availability: available, locks: locks, displayName: n.Name}
		if n.ParentID == nil {
			item.displayName = request.principal.Username
		}
		if wantsSize && n.Type == "folder" {
			if item.folderBytes, err = h.davFolderBytes(r.Context(), uid, n.ID); err != nil {
				return err
			}
		}
		responses.WriteString(davPropfindResponse(item, asked))
	}
	webdav.WriteXML(request.w, http.StatusMultiStatus, webdav.Document("multistatus", responses.String()))
	return nil
}

type davUpdate struct {
	name   davName
	set    bool
	value  string
	status int
}

func davParseProppatch(body []byte) ([]davUpdate, error) {
	root, err := webdav.ParseXML(body)
	if err != nil {
		return nil, err
	}
	if !root.Is(webdav.DAV, "propertyupdate") {
		return nil, davError(http.StatusBadRequest, "Expected a DAV:propertyupdate body.")
	}
	updates := []davUpdate{}
	for _, instruction := range root.Children {
		if !instruction.Is(webdav.DAV, "set") && !instruction.Is(webdav.DAV, "remove") {
			continue
		}
		for _, prop := range instruction.Children {
			if !prop.Is(webdav.DAV, "prop") {
				continue
			}
			for _, property := range prop.Children {
				updates = append(updates, davUpdate{name: davName{property.Namespace, property.Name}, set: instruction.Name == "set",
					value: strings.TrimSpace(property.Text), status: http.StatusOK})
			}
		}
	}
	return updates, nil
}

// davProppatch keeps file times. Everything else (Windows' Win32* attributes,
// editor metadata) is accepted and dropped.
func (h *Handler) davProppatch(request davRequest) error {
	r, uid, segments := request.r, request.principal.UserID, request.target.Segments
	body, err := webdav.ReadBody(r)
	if err != nil {
		return err
	}
	updates, err := davParseProppatch(body)
	if err != nil {
		return err
	}
	n, err := h.davExisting(r.Context(), uid, segments)
	if err != nil {
		return err
	}
	if err = h.davAssertUnlocked(request, segments, false); err != nil {
		return err
	}
	var mtime time.Time
	invalid := false
	for i, update := range updates {
		if !update.set || update.name.namespace != webdav.DAV || update.name.name != "getlastmodified" && update.name.name != "lastmodified" {
			continue
		}
		if parsed, ok := davParseTime(update.value); ok {
			mtime = parsed
		} else {
			updates[i].status, invalid = http.StatusConflict, true
		}
	}
	// PROPPATCH is all or nothing: one bad value fails the others with 424.
	statuses := []int{}
	byStatus := map[int]string{}
	for _, update := range updates {
		status := update.status
		if invalid && status == http.StatusOK {
			status = http.StatusFailedDependency
		}
		if _, seen := byStatus[status]; !seen {
			statuses = append(statuses, status)
		}
		byStatus[status] += webdav.Element(update.name.namespace, update.name.name, "")
	}
	if !invalid && !mtime.IsZero() {
		if err = h.davTx(r.Context(), func(tx *sql.Tx) error { return setModifiedAtTx(tx, uid, n.ID, mtime) }); err != nil {
			return err
		}
	}
	propstats := ""
	for _, status := range statuses {
		propstats += davPropstat(status, byStatus[status])
	}
	href := webdav.Href(request.target.Base, segments, n.Type == "folder")
	webdav.WriteXML(request.w, http.StatusMultiStatus, webdav.Document("multistatus", davResponse(href, propstats)))
	return nil
}
