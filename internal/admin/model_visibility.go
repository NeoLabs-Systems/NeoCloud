package admin

import (
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/NeoLabs-Systems/neocloud/internal/settings"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const (
	disabledModelsEnv = "NEOCLOUD_DISABLED_MODELS"
	knownModelsEnv    = "NEOCLOUD_KNOWN_MODELS"
	autoEnableEnv     = "NEOCLOUD_AUTO_ENABLE_NEW_MODELS"
)

func (h *Handler) getAdminModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	models := make([]settings.AdminModel, 0)
	if h.adminModelCatalog != nil {
		var err error
		models, err = h.adminModelCatalog(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not load model catalog.", "INTERNAL_ERROR")
			return
		}
	}
	h.modelVisibilityMu.Lock()
	ids := make([]string, 0, len(models))
	for _, model := range models {
		if id := strings.TrimSpace(model.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if err := h.reconcileModelIDs(ids); err != nil {
		h.modelVisibilityMu.Unlock()
		writeError(w, http.StatusInternalServerError, "Could not save discovered model catalog.", "INTERNAL_ERROR")
		return
	}
	disabled := modelIDSet(h.disabledModelIDs)
	for i := range models {
		models[i].Available = !disabled[models[i].ID] && !disabled[models[i].ModelID]
	}
	currentDisabled := append([]string(nil), h.disabledModelIDs...)
	autoEnable := h.autoEnableNewModels
	h.modelVisibilityMu.Unlock()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"models": models, "disabledModels": currentDisabled, "autoEnableNewModels": autoEnable})
}

func (h *Handler) updateAdminModels(w http.ResponseWriter, r *http.Request) {
	_, ok := h.authorize(w, r)
	if !ok {
		return
	}
	body, ok := decodeObject(w, r)
	if !ok {
		return
	}
	raw, exists := body["disabledModels"]
	items, isArray := raw.([]any)
	if !exists || !isArray {
		writeError(w, http.StatusBadRequest, "disabledModels must be an array", "INVALID_VALUE")
		return
	}
	var requested []string
	for _, item := range items {
		if item == nil {
			requested = append(requested, "null")
			continue
		}
		requested = append(requested, fmt.Sprint(item))
	}
	// Anything but a boolean leaves the policy as it is.
	autoEnableRaw := body["autoEnableNewModels"]
	h.modelVisibilityMu.Lock()
	newDisabled := uniqueModelIDs(requested)
	updates := map[string]string{disabledModelsEnv: strings.Join(newDisabled, ",")}
	newAutoEnable := h.autoEnableNewModels
	if value, isBool := autoEnableRaw.(bool); isBool {
		newAutoEnable = value
		if value {
			updates[autoEnableEnv] = "true"
		} else {
			updates[autoEnableEnv] = "false"
		}
	}
	if err := h.persistSettings(updates); err != nil {
		h.modelVisibilityMu.Unlock()
		writeError(w, http.StatusInternalServerError, "Could not save model visibility settings.", "INTERNAL_ERROR")
		return
	}
	h.disabledModelIDs = newDisabled
	h.autoEnableNewModels = newAutoEnable
	if h.onDisabledModelsChanged != nil {
		h.onDisabledModelsChanged(append([]string(nil), newDisabled...))
	}
	currentDisabled := append([]string(nil), h.disabledModelIDs...)
	autoEnable := h.autoEnableNewModels
	h.modelVisibilityMu.Unlock()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "disabledModels": currentDisabled, "autoEnableNewModels": autoEnable})
}

func (h *Handler) reconcileModelIDs(current []string) error {
	current = uniqueModelIDs(current)
	if len(current) == 0 {
		return nil // Empty discovery can be a provider outage; preserve the last-known policy.
	}
	if !h.knownModelsConfigured {
		if err := h.persistSettings(map[string]string{knownModelsEnv: strings.Join(current, ",")}); err != nil {
			return err
		}
		h.knownModelIDs = current
		h.knownModelsConfigured = true
		return nil
	}
	knownIDs := uniqueModelIDs(h.knownModelIDs)
	knownSet := modelIDSet(knownIDs)
	changed := false
	disabledIDs := uniqueModelIDs(h.disabledModelIDs)
	for _, id := range current {
		if knownSet[id] {
			continue
		}
		knownSet[id] = true
		knownIDs = append(knownIDs, id)
		changed = true
		if !h.autoEnableNewModels {
			disabledIDs = append(disabledIDs, id)
		}
	}
	if !changed {
		return nil
	}
	newKnown := knownIDs
	updates := map[string]string{knownModelsEnv: strings.Join(newKnown, ",")}
	newDisabled := h.disabledModelIDs
	if !h.autoEnableNewModels {
		newDisabled = disabledIDs
		updates[disabledModelsEnv] = strings.Join(newDisabled, ",")
	}
	if err := h.persistSettings(updates); err != nil {
		return err
	}
	h.knownModelIDs = newKnown
	h.disabledModelIDs = newDisabled
	if !h.autoEnableNewModels && h.onDisabledModelsChanged != nil {
		h.onDisabledModelsChanged(append([]string(nil), h.disabledModelIDs...))
	}
	return nil
}

func modelIDSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func uniqueModelIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		// A model id is one line of printable text; anything else is not an id
		// any provider serves, and could not be stored in .env either.
		if id != "" && !seen[id] && !strings.ContainsFunc(id, unicode.IsControl) {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}
