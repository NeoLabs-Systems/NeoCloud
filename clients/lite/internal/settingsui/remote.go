package settingsui

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

// accountTTL is how long the quota shown on the page is reused.
const accountTTL = 30 * time.Second

// remoteFolder is a server folder as the page's folder browser shows it;
// Path is what a pair's remote setting takes ("" is the root).
type remoteFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// remoteFolders lists the folders inside ?id= (the root by default).
// Vault folders are left out: the lite client never syncs them.
func (s *Server) remoteFolders(r *http.Request) (any, error) {
	client, err := s.engine.Client()
	if err != nil {
		return nil, err
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		id = api.RootAlias
	}
	folder, crumbs, children, err := client.Subfolders(r.Context(), id)
	if err != nil {
		return nil, err
	}
	trail := make([]remoteFolder, 0, len(crumbs))
	for i, crumb := range crumbs {
		trail = append(trail, remoteFolder{ID: crumb.ID, Name: crumb.Name, Path: crumbPath(crumbs[:i+1])})
	}
	here := crumbPath(crumbs)
	folders := []remoteFolder{}
	for _, child := range children {
		if child.VaultID == "" {
			folders = append(folders, remoteFolder{ID: child.ID, Name: child.Name, Path: joinRemote(here, child.Name)})
		}
	}
	return map[string]any{
		"folder":  remoteFolder{ID: folder.ID, Name: folder.Name, Path: here},
		"crumbs":  trail,
		"folders": folders,
	}, nil
}

// crumbPath turns root-first crumbs into a remote path; the first crumb
// is the account's root, which has no name in a path.
func crumbPath(crumbs []api.Crumb) string {
	var names []string
	for _, crumb := range crumbs[min(1, len(crumbs)):] {
		names = append(names, crumb.Name)
	}
	return strings.Join(names, "/")
}

func joinRemote(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

// createRemoteFolder makes { parentId, name } on the server; an existing
// folder of that name is reused.
func (s *Server) createRemoteFolder(r *http.Request) (any, error) {
	var body struct {
		ParentID string `json:"parentId"`
		Name     string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return nil, errors.New("enter a folder name without slashes")
	}
	if body.ParentID == "" {
		body.ParentID = api.RootAlias
	}
	client, err := s.engine.Client()
	if err != nil {
		return nil, err
	}
	node, err := client.CreateFolder(r.Context(), body.ParentID, name)
	if err != nil {
		return nil, err
	}
	return remoteFolder{ID: node.ID, Name: node.Name}, nil
}

// account answers the storage use of the signed-in account, reusing the
// last answer for accountTTL so the page can ask often.
func (s *Server) account(r *http.Request) (any, error) {
	s.mu.Lock()
	cached, at := s.quota, s.quotaAt
	s.mu.Unlock()
	if cached != nil && time.Since(at) < accountTTL {
		return cached, nil
	}
	client, err := s.engine.Client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	me, err := client.Me(ctx)
	if err != nil {
		return nil, err
	}
	quota := map[string]int64{"used": me.Quota.Used, "limit": me.Quota.Limit}
	s.mu.Lock()
	s.quota, s.quotaAt = quota, time.Now()
	s.mu.Unlock()
	return quota, nil
}
