package api

// Node is a file or folder as the server serializes it. Path is only set in
// tree snapshots; VaultID marks end-to-end encrypted folders, which the lite
// client never syncs.
type Node struct {
	ID         string `json:"id"`
	ParentID   string `json:"parentId"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Etag       string `json:"etag"`
	Version    int64  `json:"version"`
	ModifiedAt string `json:"modifiedAt"`
	TrashedAt  string `json:"trashedAt,omitempty"`
	VaultID    string `json:"vaultId,omitempty"`
	Path       string `json:"path,omitempty"`
}

// IsFolder reports whether the node is a folder.
func (n Node) IsFolder() bool { return n.Type == "folder" }

// Tree is GET /api/sync/tree: every live node plus the journal cursor the
// snapshot corresponds to.
type Tree struct {
	Cursor int64  `json:"cursor"`
	RootID string `json:"rootId"`
	Nodes  []Node `json:"nodes"`
}

// Change is one journal entry. Node is the node as it is now, nil once it
// has been deleted for good.
type Change struct {
	Seq    int64  `json:"seq"`
	Op     string `json:"op"`
	NodeID string `json:"nodeId"`
	At     string `json:"at"`
	Node   *Node  `json:"node"`
}

// ChangePage is GET /api/sync/changes.
type ChangePage struct {
	Changes []Change `json:"changes"`
	Cursor  int64    `json:"cursor"`
	HasMore bool     `json:"hasMore"`
}

// User is the account a token belongs to.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Device is a registered client.
type Device struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

// Me is GET /api/devices/me.
type Me struct {
	Device Device `json:"device"`
	User   User   `json:"user"`
	Quota  struct {
		Used  int64 `json:"used"`
		Limit int64 `json:"limit"`
	} `json:"quota"`
}
