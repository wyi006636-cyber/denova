package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"

	"denova/config"
	"denova/internal/providercompat"
	"denova/internal/quality/evaluation"
)

const defaultManifestPath = "docs/project-design/implementation/evaluation/corpus-manifest-v1.json"

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "quality-eval: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("expected validate, create-run, package-blind, summarize, or skills")
	}
	switch args[0] {
	case "skills":
		return runSkills(ctx, args[1:], stdout, stderr)
	case "validate":
		flags := flag.NewFlagSet("validate", flag.ContinueOnError)
		flags.SetOutput(stderr)
		manifestPath := flags.String("manifest", "", "corpus manifest path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*manifestPath) == "" {
			return errors.New("validate requires --manifest")
		}
		manifest, err := evaluation.LoadManifest(*manifestPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "VALID profiles=3 tasks=%d\n", len(manifest.Tasks))
		return nil
	case "create-run":
		flags := flag.NewFlagSet("create-run", flag.ContinueOnError)
		flags.SetOutput(stderr)
		manifestPath := flags.String("manifest", "", "corpus manifest path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*manifestPath) == "" {
			return errors.New("create-run requires --manifest")
		}
		return createRun(ctx, *manifestPath, stdout, stderr)
	case "package-blind":
		runID, runRoot, err := parseRunCommand(args[0], args[1:], stderr)
		if err != nil {
			return err
		}
		index, err := evaluation.PackageBlind(runRoot, runID)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "PACKAGED run=%s status=%s samples=%d\n", runID, index.Status, len(index.Samples))
		return nil
	case "summarize":
		runID, runRoot, err := parseRunCommand(args[0], args[1:], stderr)
		if err != nil {
			return err
		}
		summary, err := evaluation.Summarize(runRoot, runID)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "SUMMARY run=%s status=%s pairs=%d missing_arms=%d missing_reviews=%d pending_adjudications=%d\n",
			runID, summary.Status, summary.Paired.Total, summary.MissingArms, summary.MissingReviews, summary.PendingAdjudications)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func createRun(ctx context.Context, manifestPath string, stdout, stderr io.Writer) error {
	manifest, err := evaluation.LoadManifest(manifestPath)
	if err != nil {
		return err
	}
	runRoot := evaluation.ResolveRunRoot(manifestPath, manifest)
	cfg, _, configErr := config.LoadWithWorkspace("")
	if configErr != nil {
		return fmt.Errorf("load Provider configuration: %w", configErr)
	}
	resolved := config.ResolveAgentModel(cfg, config.AgentKindIDE)
	blockReason := providerBlockReason(manifest, resolved)
	if blockReason != "" {
		run, err := evaluation.CreateRun(manifestPath, evaluation.CreateRunOptions{
			RunRoot: runRoot, BaselineStatus: evaluation.StatusEnvironmentBlocked,
			HarnessStatus: evaluation.StatusNotReady, BaselineFailureType: blockReason,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(stderr, "quality-eval: run=%s baseline=ENVIRONMENT-BLOCKED reason=%s; H arm remains NOT-READY\n", run.RunID, blockReason)
		fmt.Fprintln(stdout, run.RunID)
		return nil
	}
	runRecord, err := evaluation.CreateRun(manifestPath, evaluation.CreateRunOptions{
		RunRoot: runRoot, BaselineStatus: evaluation.StatusNotReady, HarnessStatus: evaluation.StatusNotReady,
	})
	if err != nil {
		return err
	}
	generator := &einoGenerator{apiKey: resolved.OpenAIAPIKey}
	updated, err := evaluation.ExecuteSingleTurnBaseline(ctx, manifestPath, runRoot, runRecord.RunID, generator)
	if err != nil {
		return err
	}
	if updated.BaselineStatus != evaluation.StatusReady {
		return fmt.Errorf("run %s saved but one or more S-arm model calls failed", updated.RunID)
	}
	fmt.Fprintf(stderr, "quality-eval: run=%s baseline=READY tasks=%d model_calls=%d; H arm remains NOT-READY\n", updated.RunID, len(updated.Tasks), len(updated.Tasks))
	fmt.Fprintln(stdout, updated.RunID)
	return nil
}

func parseRunCommand(name string, args []string, stderr io.Writer) (string, string, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	runID := flags.String("run", "", "stable run ID")
	manifestPath := flags.String("manifest", defaultManifestPath, "corpus manifest path used to locate runs")
	if err := flags.Parse(args); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(*runID) == "" {
		return "", "", fmt.Errorf("%s requires --run", name)
	}
	manifest, err := evaluation.LoadManifest(*manifestPath)
	if err != nil {
		return "", "", err
	}
	return *runID, evaluation.ResolveRunRoot(*manifestPath, manifest), nil
}

func providerBlockReason(manifest evaluation.CorpusManifest, resolved config.ResolvedModelSettings) string {
	if strings.TrimSpace(resolved.OpenAIAPIKey) == "" {
		return "provider_credentials_missing"
	}
	for _, task := range manifest.Tasks {
		model := task.ModelConfigSnapshot
		if model.Provider != providerID(resolved.OpenAIBaseURL) || model.BaseURL != strings.TrimRight(resolved.OpenAIBaseURL, "/") || model.ModelProfileID != resolved.ProfileID || model.Model != resolved.OpenAIModel {
			return "model_configuration_mismatch"
		}
	}
	return ""
}

func providerID(baseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(baseURL))
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case strings.Contains(host, "deepseek"):
		return "deepseek"
	case strings.Contains(host, "openai"):
		return "openai"
	case host != "":
		return host
	default:
		return strings.ToLower(strings.TrimSpace(baseURL))
	}
}

type einoGenerator struct {
	apiKey string
}

func (generator *einoGenerator) Generate(ctx context.Context, request evaluation.BaselineRequest) (evaluation.GenerationResult, error) {
	temperature := float32(request.Model.Parameters.Temperature)
	maxTokens := request.Model.Parameters.MaxOutputTokens
	modelConfig := openai.ChatModelConfig{
		APIKey: generator.apiKey, BaseURL: request.Model.BaseURL, Model: request.Model.Model,
		Temperature: &temperature, MaxTokens: &maxTokens, HTTPClient: providercompat.WrapHTTPClient(nil),
	}
	thinking := false
	extraFields := map[string]any{}
	for key, value := range providercompat.ThinkingExtraFields(modelConfig, &thinking) {
		extraFields[key] = value
	}
	for key, value := range providercompat.ExtraRequestFields(modelConfig) {
		extraFields[key] = value
	}
	if len(extraFields) > 0 {
		modelConfig.ExtraFields = extraFields
	}
	chatModel, err := openai.NewChatModel(ctx, &modelConfig)
	if err != nil {
		return evaluation.GenerationResult{}, fmt.Errorf("task %s profile %s create model: %w", request.TaskID, request.ProfileID, err)
	}
	wrapped := providercompat.Wrap(chatModel, modelConfig)
	goals := append([]string(nil), request.QualityGoals...)
	sort.Strings(goals)
	userMessage := fmt.Sprintf(
		"Task ID: %s\nProfile: %s\nTask type: %s\nLength bucket: %s\nAllowed input classes: %s\nQualitySpec goals:\n- %s\n\nAuthorized task input:\n%s",
		request.TaskID, request.ProfileID, request.TaskType, request.LengthBucket,
		strings.Join(request.AllowedInputs, ", "), strings.Join(goals, "\n- "), request.Input,
	)
	message, err := wrapped.Generate(ctx, []*schema.Message{
		schema.SystemMessage(request.SystemTemplate),
		schema.UserMessage(userMessage),
	})
	if err != nil {
		return evaluation.GenerationResult{}, fmt.Errorf("task %s profile %s single model call: %w", request.TaskID, request.ProfileID, err)
	}
	if message == nil {
		return evaluation.GenerationResult{}, fmt.Errorf("task %s profile %s single model call returned nil", request.TaskID, request.ProfileID)
	}
	result := evaluation.GenerationResult{
		Output: message.Content,
		Usage:  evaluation.UsageRecord{ModelCalls: 1},
		Cost:   evaluation.CostRecord{Status: "NOT-AVAILABLE", Note: "Provider usage is recorded; no verified pricing table is frozen in P0-T07."},
	}
	if message.ResponseMeta != nil && message.ResponseMeta.Usage != nil {
		usage := message.ResponseMeta.Usage
		result.Usage.PromptTokens = usage.PromptTokens
		result.Usage.CompletionTokens = usage.CompletionTokens
		result.Usage.ReasoningTokens = usage.CompletionTokensDetails.ReasoningTokens
		result.Usage.TotalTokens = usage.TotalTokens
	}
	return result, nil
}
