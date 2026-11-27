package externalstorage

import (
	"net/http"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
)

// Copying a file between NeoCloud and a mount. Folders are not copied. Vault
// files stay out: they are encrypted in the browser and make no sense at a
// provider.

// copyIn copies a NeoCloud file into a folder of the mount, renamed if the name
// is taken.
func (h *Handler) copyIn(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	body, err := apijson.Object(w, r)
	if err != nil {
		return 0, nil, err
	}
	nodeID, _ := body["nodeId"].(string)
	node, source, err := h.files.ExternalCopySource(r.Context(), userID, nodeID)
	if err != nil {
		return 0, nil, err
	}
	defer source.Close()
	folder, err := normalizePath(body["folder"])
	if err != nil {
		return 0, nil, err
	}
	name := node.Name
	if given := body["name"]; given != nil {
		name, _ = given.(string)
	}
	// Validates the name before anything is opened at the provider.
	if _, err = joinPath(folder, name); err != nil {
		return 0, nil, err
	}
	s, err := h.session(r.Context(), userID, r.PathValue("id"), true)
	if err != nil {
		return 0, nil, err
	}
	defer s.release()
	target, err := freeName(r.Context(), s.adapter, folder, name)
	if err != nil {
		return 0, nil, err
	}
	if err = s.adapter.write(r.Context(), target, source, node.Size); err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, map[string]string{"path": target}, nil
}

// copyOut copies a file of the mount into a NeoCloud folder, counted against
// the account's quota.
func (h *Handler) copyOut(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	body, err := apijson.Object(w, r)
	if err != nil {
		return 0, nil, err
	}
	parentID, _ := body["parentId"].(string)
	parent, err := h.files.OwnFolder(r.Context(), userID, parentID)
	if err != nil {
		return 0, nil, err
	}
	if parent.VaultID != nil {
		return 0, nil, apperr.New(http.StatusConflict, "Items can’t be moved or copied into or out of a vault.", "VAULT_BOUNDARY")
	}
	path, err := normalizePath(body["path"])
	if err != nil {
		return 0, nil, err
	}
	s, info, err := h.openFile(r.Context(), userID, r.PathValue("id"), path)
	if err != nil {
		return 0, nil, err
	}
	defer s.release()
	stream, err := s.adapter.read(r.Context(), path, nil)
	if err != nil {
		return 0, nil, err
	}
	defer stream.Close()
	stored, err := h.files.ImportCopy(r.Context(), userID, parent.ID, info.name, stream, info.size, "rename")
	return http.StatusCreated, stored, err
}
