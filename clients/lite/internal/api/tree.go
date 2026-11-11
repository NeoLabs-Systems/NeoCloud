package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// RootAlias names the account's root folder wherever a folder id is expected.
const RootAlias = "root"

// Tree fetches the full snapshot of live nodes.
func (c *Client) Tree(ctx context.Context) (*Tree, error) {
	var out Tree
	if err := c.doJSON(ctx, http.MethodGet, "/api/sync/tree", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Changes reads one page of the journal after cursor.
func (c *Client) Changes(ctx context.Context, cursor int64, limit int) (*ChangePage, error) {
	query := url.Values{"cursor": {strconv.FormatInt(cursor, 10)}, "limit": {strconv.Itoa(limit)}}
	var out ChangePage
	if err := c.doJSON(ctx, http.MethodGet, "/api/sync/changes", query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateFolder creates name under parentID. An existing folder of that name
// is reused (conflict=replace), which makes the call idempotent.
func (c *Client) CreateFolder(ctx context.Context, parentID, name string) (*Node, error) {
	var out Node
	body := map[string]string{"parentId": parentID, "name": name, "conflict": "replace"}
	if err := c.doJSON(ctx, http.MethodPost, "/api/folders", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EnsureFolderPath creates every missing folder of "a/b/c" below parentID
// and returns the deepest.
func (c *Client) EnsureFolderPath(ctx context.Context, parentID, path string) (*Node, error) {
	var out Node
	body := map[string]string{"parentId": parentID, "path": path}
	if err := c.doJSON(ctx, http.MethodPost, "/api/folders", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Move renames and/or moves a node. Empty arguments are left unchanged.
func (c *Client) Move(ctx context.Context, id, newParentID, newName string) (*Node, error) {
	body := map[string]string{}
	if newParentID != "" {
		body["parentId"] = newParentID
	}
	if newName != "" {
		body["name"] = newName
	}
	var out Node
	if err := c.doJSON(ctx, http.MethodPatch, "/api/nodes/"+url.PathEscape(id), nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Delete moves a node (and a folder's whole subtree) to the trash.
func (c *Client) Delete(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/nodes/"+url.PathEscape(id), nil, nil, nil)
}

// Crumb is one folder on the way from the root to a listed folder.
type Crumb struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// folderPage is one page of GET /api/folders/:id/children.
type folderPage struct {
	Folder     Node    `json:"folder"`
	Path       []Crumb `json:"path"`
	Items      []Node  `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

// Subfolders lists the folders directly inside folderID (RootAlias for the
// root), with the crumbs from the root down to it. Listings put folders
// first, so paging stops at the first file.
func (c *Client) Subfolders(ctx context.Context, folderID string) (Node, []Crumb, []Node, error) {
	var folders []Node
	query := url.Values{"limit": {"2000"}}
	for {
		var page folderPage
		if err := c.doJSON(ctx, http.MethodGet, "/api/folders/"+url.PathEscape(folderID)+"/children", query, nil, &page); err != nil {
			return Node{}, nil, nil, err
		}
		done := page.NextCursor == nil
		for _, item := range page.Items {
			if !item.IsFolder() {
				done = true
				break
			}
			folders = append(folders, item)
		}
		if done {
			return page.Folder, page.Path, folders, nil
		}
		query.Set("cursor", *page.NextCursor)
	}
}
