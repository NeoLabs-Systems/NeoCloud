package ai

import (
	"errors"
	"net/http"
)

// ErrModelsUnavailable says no model can be discovered.
var ErrModelsUnavailable = errors.New("AI models unavailable")

var roles = []string{"text", "smart", "vision", "embedding", "transcription"}

// status is what the apps show: whether AI can run here, whether the person
// opted in, the model each role resolves to, this month's tokens and the file
// type categories. While the admin switch is off no provider is asked.
func (h *Handler) status(r *http.Request, userID int64, _ body) (any, error) {
	settings, err := readSettings(r.Context(), h.db, userID)
	if err != nil {
		return nil, err
	}
	resolved := make(map[string]RoleStatus, len(roles))
	for _, role := range roles {
		resolved[role] = RoleStatus{}
	}
	enabled, _ := h.policy()
	if enabled && h.options.ResolveRoles != nil {
		found, err := h.options.ResolveRoles(r.Context(), userID, settings.settings)
		if err != nil && !errors.Is(err, ErrModelsUnavailable) {
			return nil, err
		}
		for _, role := range roles {
			if status, ok := found[role]; ok {
				resolved[role] = status
			}
		}
	}
	used, limit, err := h.budget(r.Context(), h.db, userID)
	if err != nil {
		return nil, err
	}
	available := false
	for _, role := range resolved {
		available = available || role.Ready
	}
	var shownLimit any
	if limit > 0 {
		shownLimit = limit
	}
	return map[string]any{
		"available": enabled && available,
		"enabled":   enabled,
		"optedIn":   settings.optedIn(),
		"roles":     resolved,
		"budget":    map[string]any{"used": used, "limit": shownLimit},
		"fileTypes": fileTypeCategories,
	}, nil
}
