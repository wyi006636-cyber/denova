package config

import (
	"errors"
	"testing"

	"github.com/alfredxw/denova/agent/model/providers"
)

func TestModelProfileRenameKeepsAllReferencesAndDeletionFailsClosed(t *testing.T) {
	cfg := modelProfileRenameFixture()

	language := ResolveAgentModel(cfg, AgentKindIDE)
	if language.ProfileID != "writer-v1" || language.Model != "writer-v2" {
		t.Fatalf("language Agent did not keep the stable profile identity: %#v", language)
	}
	subAgent := ResolveSubAgentModel(cfg, AgentKindGeneral, cfg.SubAgents[0])
	if subAgent.ProfileID != "writer-v1" || subAgent.Model != "writer-v2" {
		t.Fatalf("SubAgent did not keep the stable profile identity: %#v", subAgent)
	}
	runtimeModel, err := ResolveRuntimeModel(cfg, RuntimeSelection{
		Kind:  RuntimeCodex,
		Codex: &CodexRuntimeSettings{ProfileID: "writer-v1"},
	})
	if err != nil || runtimeModel.ProfileID != "writer-v1" || runtimeModel.Model != "writer-v2" {
		t.Fatalf("external runtime profile did not resolve after rename: model=%#v err=%v", runtimeModel, err)
	}

	customLanguage := *cfg
	customLanguage.CustomAgents = append([]CustomAgentConfig(nil), cfg.CustomAgents...)
	if err := ApplyCustomAgentDefinition(&customLanguage, AgentKindIDE, customLanguage.CustomAgents[0], false); err != nil {
		t.Fatal(err)
	}
	customResolved := ResolveAgentModel(&customLanguage, AgentKindIDE)
	if customResolved.ProfileID != "writer-v1" || customResolved.Model != "writer-v2" {
		t.Fatalf("Custom Agent did not keep the stable profile identity: %#v", customResolved)
	}

	image, err := ResolveImageAPIProfile(cfg, "")
	if err != nil || image.ProfileID != "image-v1" || image.Model != "image-v2" {
		t.Fatalf("default image profile did not resolve after rename: profile=%#v err=%v", image, err)
	}
	customImage := *cfg
	customImage.CustomAgents = append([]CustomAgentConfig(nil), cfg.CustomAgents...)
	if err := ApplyCustomAgentDefinition(&customImage, AgentKindImage, customImage.CustomAgents[1], false); err != nil {
		t.Fatal(err)
	}
	customImageResolved, err := ResolveImageAPIProfile(&customImage, "")
	if err != nil || customImageResolved.ProfileID != "image-v1" || customImageResolved.Model != "image-v2" {
		t.Fatalf("Custom Agent image profile did not keep the stable identity: profile=%#v err=%v", customImageResolved, err)
	}

	// A display-name-only edit leaves the same identity and therefore follows
	// the same path without any reference rewrite.
	nameOnly := *cfg
	nameOnly.ModelProfiles = append([]ModelProfileSettings(nil), cfg.ModelProfiles...)
	nameOnly.ModelProfiles[0].Name = "Writer display name"
	if got := ResolveAgentModel(&nameOnly, AgentKindIDE); got.ProfileID != "writer-v1" {
		t.Fatalf("display-name edit changed the profile identity: %#v", got)
	}

	// Deleting the profile is intentionally fail-closed: stale native refs fall
	// back to the normal default resolver, while external/image selections
	// report an unavailable profile instead of silently choosing another model.
	deleted := *cfg
	deleted.ModelProfiles = nil
	deleted.ImageAPIProfiles = nil
	if got := ResolveAgentModel(&deleted, AgentKindIDE); got.ProfileID == "writer-v1" {
		t.Fatalf("deleted language profile remained selectable: %#v", got)
	}
	if _, err := ResolveRuntimeModel(&deleted, RuntimeSelection{
		Kind:  RuntimeCodex,
		Codex: &CodexRuntimeSettings{ProfileID: "writer-v1"},
	}); !errors.Is(err, ErrRuntimeModelProfile) {
		t.Fatalf("deleted external runtime profile error = %v, want ErrRuntimeModelProfile", err)
	}
	if _, err := ResolveImageAPIProfile(&deleted, "image-v1"); !errors.Is(err, ErrImageAPIProfileNotFound) {
		t.Fatalf("deleted image profile error = %v, want ErrImageAPIProfileNotFound", err)
	}
}

func modelProfileRenameFixture() *Config {
	return &Config{
		ModelEndpoints: []ModelEndpointSettings{{
			ID:       "writer-endpoint",
			Provider: string(providers.ProviderOpenAI),
			Protocol: string(providers.ProtocolOpenAIResponses),
			BaseURL:  "https://models.example/v1",
			APIKey:   "test-key",
		}},
		ModelProfiles: []ModelProfileSettings{{
			ID:         "writer-v1",
			Name:       "Writer",
			EndpointID: "writer-endpoint",
			Model:      "writer-v2",
		}},
		AgentModels: AgentModelSettings{IDE: AgentModelOverride{ProfileID: "writer-v1"}},
		AgentRuntimes: AgentRuntimeSettings{IDE: &RuntimePreferences{
			Selected: RuntimeCodex,
			Codex:    &CodexRuntimeSettings{ProfileID: "writer-v1"},
		}},
		SubAgents: []SubAgentConfig{{
			ID: "reviewer", Description: "Review prose", SystemPrompt: "Review the draft.",
			Model: AgentModelOverride{ProfileID: "writer-v1"},
		}},
		ImageAPIEndpoints: []ImageAPIEndpointSettings{{
			ID: "image-endpoint", Provider: ImageProviderCustom,
			Protocol: ImageProtocolOpenAI, BaseURL: "http://localhost:8000/v1",
		}},
		ImageAPIProfiles: []ImageAPIProfileSettings{{
			ID: "image-v1", EndpointID: "image-endpoint", Model: "image-v2",
		}},
		DefaultImageAPIProfileID: "image-v1",
		CustomAgents: []CustomAgentConfig{
			{
				ID: "writer-agent", Name: "Writer Agent", Description: "Writes prose.",
				Contract: AgentContractWritingPrimary,
				Model:    AgentModelOverride{ProfileID: "writer-v1"},
			},
			{
				ID: "image-agent", Name: "Image Agent", Description: "Creates images.",
				Contract:          AgentContractImageCreator,
				ImageAPIProfileID: "image-v1",
			},
		},
	}
}
