package ai

import "testing"

func TestResolveRoleModelsFollowsUserNeoRouterAndAutomaticOrder(t *testing.T) {
	settings := map[string]any{"ai_model_text": "openai::chosen", "ai_model_smart": "openai::missing"}
	models := []Model{
		{Provider: "openai", ModelID: "chosen", Kind: "chat", Available: true, PriceTier: "cheap"},
		{Provider: "openai", ModelID: "new", Kind: "chat", Available: true, PriceTier: "expensive", CreatedAt: 30},
		{Provider: "openai", ModelID: "cheap", Kind: "chat", Available: true, PriceTier: "cheap", CreatedAt: 10},
		{Provider: "neorouter", ModelID: "auto:smart", Kind: "chat", Available: true},
		{Provider: "openai", ModelID: "image-only", Kind: "chat", Available: true, SupportsVision: false},
		{Provider: "openai", ModelID: "vision", Kind: "chat", Available: true, SupportsVision: true},
		{Provider: "openai", ModelID: "embedding", Kind: "embedding", Available: true},
	}
	got := ResolveRoleModels(models, settings)
	if !got["text"].Ready || *got["text"].Model != "openai::chosen" || *got["text"].Source != "user" || got["text"].RequestedUnavailable {
		t.Fatalf("text role: %#v", got["text"])
	}
	if !got["smart"].Ready || *got["smart"].Model != "neorouter::auto:smart" || *got["smart"].Source != "neorouter" || !got["smart"].RequestedUnavailable {
		t.Fatalf("smart role: %#v", got["smart"])
	}
	if !got["vision"].Ready || *got["vision"].Model != "openai::vision" {
		t.Fatalf("vision role: %#v", got["vision"])
	}
	if !got["embedding"].Ready || *got["embedding"].Model != "openai::embedding" {
		t.Fatalf("embedding role: %#v", got["embedding"])
	}
	if got["transcription"].Ready {
		t.Fatalf("unexpected transcription model: %#v", got["transcription"])
	}
}

func TestResolveRoleModelsPicksCheapestFastAndNewestWithinTier(t *testing.T) {
	models := []Model{
		{Provider: "openai", ModelID: "old", Kind: "chat", Available: true, PriceTier: "cheap", CreatedAt: 4},
		{Provider: "openai", ModelID: "new", Kind: "chat", Available: true, PriceTier: "cheap", CreatedAt: 8},
		{Provider: "openai", ModelID: "quality", Kind: "chat", Available: true, PriceTier: "expensive", CreatedAt: 20},
	}
	got := ResolveRoleModels(models, nil)
	if *got["text"].Model != "openai::new" || *got["text"].Source != "auto" {
		t.Fatalf("text role: %#v", got["text"])
	}
	if *got["smart"].Model != "openai::quality" {
		t.Fatalf("smart role: %#v", got["smart"])
	}
}

func TestResolveRoleModelsExcludesUnavailableAndWrongKind(t *testing.T) {
	models := []Model{
		{Provider: "openai", ModelID: "offline", Kind: "chat", Available: false, PriceTier: "cheap"},
		{Provider: "openai", ModelID: "wrong-kind", Kind: "embedding", Available: true, PriceTier: "cheap"},
	}
	got := ResolveRoleModels(models, map[string]any{"ai_model_text": "openai::offline"})
	if got["text"].Ready || got["text"].Model != nil || got["text"].Source != nil || got["text"].RequestedUnavailable {
		t.Fatalf("role status: %#v", got["text"])
	}
}
