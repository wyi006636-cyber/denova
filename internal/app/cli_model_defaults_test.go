package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"denova/config"
	appsettings "denova/internal/app/settings"
)

func TestCLIModelChangesReachAppAndStayConversationLocal(t *testing.T) {
	for _, engine := range []config.RuntimeID{config.RuntimeGrok, config.RuntimeAntigravity} {
		t.Run(string(engine), func(t *testing.T) {
			application := newExecutionProfileTestApp(t)
			binding := ConversationConfigBinding{Mode: ConversationModeWriting, ProjectID: application.ProjectID(), SessionID: application.session.ID}
			initial, err := application.ConversationConfig(t.Context(), binding)
			if err != nil {
				t.Fatal(err)
			}
			selection := config.RuntimeSelection{Kind: engine}
			if engine == config.RuntimeGrok {
				selection.Grok = &config.GrokRuntimeSettings{Model: "first-model"}
			} else {
				selection.Antigravity = &config.AntigravityRuntimeSettings{Model: "first-model"}
			}
			seed := initial.Config
			seed.Runtime = &selection
			seeded, err := application.session.SetRuntimeConfig(seed, initial.Revision)
			if err != nil {
				t.Fatal(err)
			}
			var patch ConversationConfigPatch
			if err := json.Unmarshal([]byte(`{"`+string(engine)+`":{"model":"second-model"}}`), &patch); err != nil {
				t.Fatal(err)
			}
			saved, err := application.PatchConversationConfig(t.Context(), binding, patch, seeded.Revision)
			if err != nil {
				t.Fatal(err)
			}
			if engine == config.RuntimeGrok {
				selection.Grok.Model = "second-model"
			} else {
				selection.Antigravity.Model = "second-model"
			}
			if !reflect.DeepEqual(saved.Engine(), selection) {
				t.Fatalf("model change lost: %#v", saved.Engine())
			}
			settings, err := application.SettingsService().Snapshot(appsettings.Global())
			if err != nil {
				t.Fatal(err)
			}
			defaults, err := settings.Effective.AgentRuntimes.ForAgent(config.AgentKindIDE).Selection(config.AgentKindIDE)
			if err != nil {
				t.Fatal(err)
			}
			if defaults.Kind != config.RuntimeNative {
				t.Fatalf("conversation model edit changed new-conversation defaults: %#v", defaults)
			}
		})
	}
}
