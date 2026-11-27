package ai

import (
	"sort"
	"strconv"
	"strings"
)

// Model is a discovered model after provider health, account grants, and
// administrator visibility have been applied. Discovery intentionally lives
// outside the deterministic role router.
type Model struct {
	ID             string
	Provider       string
	ModelID        string
	Kind           string
	Available      bool
	SupportsVision bool
	PriceTier      string
	CreatedAt      int64
}

type roleSpec struct {
	setting   string
	kind      string
	vision    bool
	profile   string
	neorouter []string
}

var roleSpecs = map[string]roleSpec{
	"text":          {setting: "ai_model_text", kind: "chat", profile: "fast", neorouter: []string{"auto:fast", "auto:default"}},
	"smart":         {setting: "ai_model_smart", kind: "chat", profile: "quality", neorouter: []string{"auto:smart", "auto:default"}},
	"vision":        {setting: "ai_model_vision", kind: "chat", vision: true, profile: "balanced", neorouter: []string{"auto:default"}},
	"embedding":     {setting: "ai_model_embedding", kind: "embedding", profile: "balanced", neorouter: []string{"auto:default"}},
	"transcription": {setting: "ai_model_transcription", kind: "transcription", profile: "balanced", neorouter: []string{"auto:default"}},
}

var tierPreferences = map[string][]string{
	"balanced": {"medium", "cheap", "expensive", "free"},
	"fast":     {"cheap", "medium", "free", "expensive"},
	"quality":  {"expensive", "medium", "cheap", "free"},
}

// ResolveRoleModels applies the Node role order: explicit available choice,
// matching NeoRouter auto profile, then best tier/newest discovered model.
// The caller supplies only models already authorized and reachable for the
// account, preventing discovery policy from being bypassed here.
func ResolveRoleModels(models []Model, settings map[string]any) map[string]RoleStatus {
	resolved := make(map[string]RoleStatus, len(roleSpecs))
	for role, spec := range roleSpecs {
		pool := make([]Model, 0)
		for _, model := range models {
			if model.Available && model.Kind == spec.kind && (!spec.vision || model.SupportsVision) {
				pool = append(pool, model)
			}
		}
		requested := "auto"
		if raw, ok := settings[spec.setting].(string); ok && strings.TrimSpace(raw) != "" {
			requested = strings.TrimSpace(raw)
		}
		var chosen *Model
		if requested != "auto" {
			for i := range pool {
				if modelSelectionID(pool[i]) == requested {
					chosen = &pool[i]
					break
				}
			}
			if chosen != nil {
				resolved[role] = statusFor(*chosen, "user", false)
				continue
			}
		}
		requestedUnavailable := requested != "auto"
		for _, target := range spec.neorouter {
			for i := range pool {
				if pool[i].Provider == "neorouter" && pool[i].ModelID == target {
					chosen = &pool[i]
					break
				}
			}
			if chosen != nil {
				break
			}
		}
		if chosen != nil {
			resolved[role] = statusFor(*chosen, "neorouter", requestedUnavailable)
			continue
		}
		sort.SliceStable(pool, func(i, j int) bool {
			ri, rj := tierRank(pool[i].PriceTier, spec.profile), tierRank(pool[j].PriceTier, spec.profile)
			if ri != rj {
				return ri < rj
			}
			return pool[i].CreatedAt > pool[j].CreatedAt
		})
		if len(pool) > 0 {
			resolved[role] = statusFor(pool[0], "auto", requestedUnavailable)
		}
	}
	return resolved
}

func statusFor(model Model, source string, unavailable bool) RoleStatus {
	id := modelSelectionID(model)
	return RoleStatus{Ready: true, Model: &id, Source: &source, RequestedUnavailable: unavailable}
}

func modelSelectionID(model Model) string {
	if model.ID != "" {
		return model.ID
	}
	return strings.ToLower(strings.TrimSpace(model.Provider)) + "::" + strings.TrimSpace(model.ModelID)
}

func tierRank(tier, profile string) int {
	preference := tierPreferences[profile]
	for i, candidate := range preference {
		if tier == candidate {
			return i
		}
	}
	for i, candidate := range preference {
		if candidate == "medium" {
			return i
		}
	}
	return len(preference)
}

// ParseModelCreatedAt accepts the numeric millisecond timestamp used by the
// provider catalog and returns zero for absent or malformed values.
func ParseModelCreatedAt(raw string) int64 {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
