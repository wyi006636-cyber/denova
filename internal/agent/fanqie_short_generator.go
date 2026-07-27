package agent

import (
	"context"
	"log"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"

	"denova/config"
	"denova/internal/providercompat"
	"denova/internal/shortfiction"
)

type fanqieShortGenerator struct {
	cfg *config.Config
}

// NewFanqieShortGenerator creates the isolated, tool-free generator used for Fanqie previews.
func NewFanqieShortGenerator(cfg *config.Config) shortfiction.Generator {
	return &fanqieShortGenerator{cfg: cfg}
}

func (g *fanqieShortGenerator) Generate(ctx context.Context, source shortfiction.SourcePacket) (shortfiction.Generation, error) {
	resolved := config.ResolveAgentModel(g.cfg, config.AgentKindIDE)
	modelCfg := chatModelConfigFromResolved(resolved)
	cm, err := openai.NewChatModel(ctx, &modelCfg)
	if err != nil {
		log.Printf("[short-fiction] create configured model failed profile=%q model=%q class=model_initialization", resolved.ProfileID, resolved.OpenAIModel)
		return shortfiction.Generation{}, shortfiction.NewError("generation_failed", "configured model is unavailable", nil)
	}
	chatModel := providercompat.Wrap(cm, modelCfg)
	message, err := chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage(shortfiction.FanqieSystemPrompt()),
		schema.UserMessage(shortfiction.FormatSourcePacket(source)),
	})
	if err != nil {
		log.Printf("[short-fiction] configured model generation failed profile=%q model=%q class=provider_request", resolved.ProfileID, resolved.OpenAIModel)
		return shortfiction.Generation{}, shortfiction.NewError("generation_failed", "configured model generation failed", nil)
	}
	if message == nil || strings.TrimSpace(message.Content) == "" {
		log.Printf("[short-fiction] configured model returned empty candidate profile=%q model=%q", resolved.ProfileID, resolved.OpenAIModel)
		return shortfiction.Generation{}, shortfiction.NewError("generation_empty", "configured model returned an empty candidate", nil)
	}
	if len(message.Content) > shortfiction.MaxCandidateBytes {
		log.Printf("[short-fiction] configured model returned oversized candidate profile=%q model=%q bytes=%d", resolved.ProfileID, resolved.OpenAIModel, len(message.Content))
		return shortfiction.Generation{}, shortfiction.NewError("candidate_too_large", "configured model returned an oversized candidate", map[string]any{"max_bytes": shortfiction.MaxCandidateBytes})
	}
	return shortfiction.Generation{
		PreviewMarkdown: message.Content,
		ModelProfileID:  resolved.ProfileID,
		Model:           resolved.OpenAIModel,
	}, nil
}
