package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"

	"denova/config"
	"denova/internal/providercompat"
	"denova/internal/quality/evaluation"
)

const defaultManifestPath = "docs/project-design/implementation/evaluation/corpus-manifest-v1.json"

type evaluationGenerator interface {
	evaluation.TextGenerator
	evaluation.HarnessTextGenerator
}

var newEvaluationGenerator = func(apiKey string) evaluationGenerator {
	return &einoGenerator{apiKey: apiKey}
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "quality-eval: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("expected validate, create-run, execute-harness, package-blind, record-review, summarize, export-run-index, or skills")
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
		splits := flags.String("splits", "", "comma-separated cohort splits: tuning,regression")
		tasks := flags.String("tasks", "", "optional comma-separated task IDs")
		policyPath := flags.String("harness-policy", "", "frozen Harness policy path")
		runRoot := flags.String("run-root", "", "absolute private run root outside the repository")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*manifestPath) == "" {
			return errors.New("create-run requires --manifest")
		}
		if strings.TrimSpace(*splits) == "" {
			if strings.TrimSpace(*tasks) != "" || strings.TrimSpace(*policyPath) != "" || strings.TrimSpace(*runRoot) != "" {
				return errors.New("cohort options require --splits")
			}
			return createRun(ctx, *manifestPath, stdout, stderr)
		}
		return createCohortRun(ctx, *manifestPath, *splits, *tasks, *policyPath, *runRoot, stdout, stderr)
	case "execute-harness":
		flags := flag.NewFlagSet("execute-harness", flag.ContinueOnError)
		flags.SetOutput(stderr)
		runID := flags.String("run", "", "stable cohort run ID")
		manifestPath := flags.String("manifest", "", "corpus manifest path")
		policyPath := flags.String("harness-policy", "", "frozen Harness policy path")
		runRoot := flags.String("run-root", "", "absolute private run root outside the repository")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*runID) == "" || strings.TrimSpace(*manifestPath) == "" || strings.TrimSpace(*policyPath) == "" || strings.TrimSpace(*runRoot) == "" {
			return errors.New("execute-harness requires --run, --manifest, --harness-policy, and --run-root")
		}
		resolvedRunRoot, err := privateRunRoot(*runRoot)
		if err != nil {
			return err
		}
		cfg, _, err := config.LoadWithWorkspace("")
		if err != nil {
			return fmt.Errorf("load Provider configuration: %w", err)
		}
		resolved := config.ResolveAgentModel(cfg, config.AgentKindIDE)
		generator := newEvaluationGenerator(resolved.OpenAIAPIKey)
		updated, err := evaluation.ExecuteOfflineHarness(ctx, *manifestPath, resolvedRunRoot, *runID, *policyPath, generator)
		if err != nil {
			return err
		}
		usage := aggregateHarnessUsage(updated)
		fmt.Fprintf(stdout, "HARNESS run=%s status=%s tasks=%d model_calls=%d tokens=%d\n", updated.RunID, updated.HarnessStatus, len(updated.Tasks), usage.ModelCalls, usage.TotalTokens)
		return nil
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
	case "record-review":
		return recordReview(args[1:], stdout, stderr)
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
	case "export-run-index":
		flags := flag.NewFlagSet("export-run-index", flag.ContinueOnError)
		flags.SetOutput(stderr)
		runRootValue := flags.String("run-root", "", "absolute private run root outside the repository")
		runsValue := flags.String("runs", "", "comma-separated unique stable run IDs")
		output := flags.String("output", "", "bounded run index output path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*runRootValue) == "" || strings.TrimSpace(*runsValue) == "" || strings.TrimSpace(*output) == "" {
			return errors.New("export-run-index requires --run-root, --runs, and --output")
		}
		runRoot, err := privateRunRoot(*runRootValue)
		if err != nil {
			return err
		}
		runIDs, err := parseCSV(*runsValue, "run ID")
		if err != nil {
			return err
		}
		index, err := evaluation.ExportRunIndex(runRoot, runIDs, *output)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "EXPORTED runs=%d output=%s\n", len(index.Runs), filepath.Base(*output))
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func recordReview(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("record-review", flag.ContinueOnError)
	flags.SetOutput(stderr)
	runID := flags.String("run", "", "stable run ID")
	runRootValue := flags.String("run-root", "", "absolute private run root outside the repository")
	inputValue := flags.String("input", "", "absolute private review JSON input")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*runID) == "" || strings.TrimSpace(*runRootValue) == "" || strings.TrimSpace(*inputValue) == "" {
		return errors.New("record-review requires --run, --run-root, and --input")
	}
	runRoot, err := privateRunRoot(*runRootValue)
	if err != nil {
		return errors.New("invalid_input_location")
	}
	runDir, err := evaluation.RunDirectory(runRoot, *runID)
	if err != nil {
		return errors.New("invalid_run")
	}
	input, err := openPrivateReviewInput(*inputValue, filepath.Join(runDir, "private", "review-inbox"))
	if err != nil {
		if errors.Is(err, errReviewInputType) {
			return errors.New("invalid_input_type")
		}
		return errors.New("invalid_input_location")
	}
	defer input.Close()
	review, err := evaluation.DecodeReviewRecord(input)
	if err != nil {
		return errors.New("invalid_json")
	}
	if err := evaluation.SaveReview(runRoot, *runID, review); err != nil {
		var persistence evaluation.ReviewPersistenceError
		if errors.As(err, &persistence) {
			return errors.New("persistence_failure")
		}
		return errors.New("invalid_review")
	}
	fmt.Fprintf(stdout, "REVIEW run=%s sample=%s kind=%s status=RECORDED\n", *runID, review.SampleID, review.Kind)
	return nil
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
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
	generator := newEvaluationGenerator(resolved.OpenAIAPIKey)
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

func createCohortRun(ctx context.Context, manifestPath, splitsValue, tasksValue, policyPath, runRootValue string, stdout, stderr io.Writer) error {
	if strings.TrimSpace(policyPath) == "" {
		return errors.New("cohort create-run requires --harness-policy")
	}
	if strings.TrimSpace(runRootValue) == "" {
		return errors.New("cohort create-run requires --run-root")
	}
	runRoot, err := privateRunRoot(runRootValue)
	if err != nil {
		return err
	}
	selection, err := parseRunSelection(splitsValue, tasksValue)
	if err != nil {
		return err
	}
	manifest, err := evaluation.LoadManifest(manifestPath)
	if err != nil {
		return err
	}
	selectedTasks, err := evaluation.SelectTasks(manifest, selection)
	if err != nil {
		return err
	}
	policy, err := evaluation.LoadHarnessPolicy(policyPath)
	if err != nil {
		return err
	}
	if err := evaluation.ValidateHarnessPolicyModelAgreement(policy, selectedTasks); err != nil {
		return err
	}
	selectedManifest := manifest
	selectedManifest.Tasks = selectedTasks
	cfg, _, err := config.LoadWithWorkspace("")
	if err != nil {
		return fmt.Errorf("load Provider configuration: %w", err)
	}
	resolved := config.ResolveAgentModel(cfg, config.AgentKindIDE)
	if blockReason := providerBlockReason(selectedManifest, resolved); blockReason != "" {
		return fmt.Errorf("cohort provider is unavailable: %s", blockReason)
	}
	runRecord, err := evaluation.CreateRun(manifestPath, evaluation.CreateRunOptions{
		RunRoot: runRoot, BaselineStatus: evaluation.StatusNotReady, HarnessStatus: evaluation.StatusNotReady,
		Selection: &selection, HarnessPolicyID: policy.PolicyID, HarnessPolicySHA256: evaluation.HarnessPolicySHA256(policy),
	})
	if err != nil {
		return err
	}
	generator := newEvaluationGenerator(resolved.OpenAIAPIKey)
	updated, err := evaluation.ExecuteSingleTurnBaseline(ctx, manifestPath, runRoot, runRecord.RunID, generator)
	if err != nil {
		return err
	}
	if updated.BaselineStatus != evaluation.StatusReady {
		return fmt.Errorf("run %s saved but one or more S-arm model calls failed", updated.RunID)
	}
	fmt.Fprintf(stderr, "quality-eval: run=%s baseline=READY tasks=%d model_calls=%d\n", updated.RunID, len(updated.Tasks), len(updated.Tasks))
	fmt.Fprintln(stdout, updated.RunID)
	return nil
}

func parseRunSelection(splitsValue, tasksValue string) (evaluation.RunSelection, error) {
	splits, err := parseCSV(splitsValue, "split")
	if err != nil {
		return evaluation.RunSelection{}, err
	}
	taskIDs, err := parseCSV(tasksValue, "task ID")
	if err != nil {
		return evaluation.RunSelection{}, err
	}
	dataSplits := make([]evaluation.DataSplit, len(splits))
	for index, split := range splits {
		dataSplits[index] = evaluation.DataSplit(split)
	}
	return evaluation.NewRunSelection(dataSplits, taskIDs)
}

func parseCSV(value, label string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	values := strings.Split(value, ",")
	parsed := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s list contains an empty value", label)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("%s list contains duplicate %q", label, value)
		}
		seen[value] = struct{}{}
		parsed = append(parsed, value)
	}
	return parsed, nil
}

func privateRunRoot(value string) (string, error) {
	if !filepath.IsAbs(value) {
		return "", errors.New("--run-root must be an absolute path outside the repository")
	}
	runRoot, err := resolvePathForContainment(value)
	if err != nil {
		return "", fmt.Errorf("resolve --run-root: %w", err)
	}
	repositoryRoot, err := findRepositoryRoot()
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	repositoryRoot, err = resolvePathForContainment(repositoryRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	relative, err := filepath.Rel(repositoryRoot, runRoot)
	if err != nil {
		if filepath.VolumeName(repositoryRoot) != filepath.VolumeName(runRoot) {
			return runRoot, nil
		}
		return "", fmt.Errorf("compare --run-root to repository: %w", err)
	}
	if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return "", errors.New("--run-root must be outside the repository")
	}
	return runRoot, nil
}

func resolvePathForContainment(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	missing := make([]string, 0)
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			return filepath.Join(append([]string{resolved}, missing...)...), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		missing = append([]string{filepath.Base(path)}, missing...)
		path = parent
	}
}

func findRepositoryRoot() (string, error) {
	directory, err := filepath.Abs(".")
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, ".git")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("repository root not found")
		}
		directory = parent
	}
}

func aggregateHarnessUsage(run evaluation.RunRecord) evaluation.UsageRecord {
	usage := evaluation.UsageRecord{}
	for _, task := range run.Tasks {
		arm := task.Arms["H"]
		usage.PromptTokens += arm.Usage.PromptTokens
		usage.CompletionTokens += arm.Usage.CompletionTokens
		usage.ReasoningTokens += arm.Usage.ReasoningTokens
		usage.TotalTokens += arm.Usage.TotalTokens
		usage.ModelCalls += arm.Usage.ModelCalls
	}
	return usage
}

func parseRunCommand(name string, args []string, stderr io.Writer) (string, string, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	runID := flags.String("run", "", "stable run ID")
	manifestPath := flags.String("manifest", defaultManifestPath, "corpus manifest path used to locate runs")
	runRoot := flags.String("run-root", "", "explicit private run root")
	if err := flags.Parse(args); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(*runID) == "" {
		return "", "", fmt.Errorf("%s requires --run", name)
	}
	if strings.TrimSpace(*runRoot) != "" {
		resolved, err := privateRunRoot(*runRoot)
		if err != nil {
			return "", "", err
		}
		return *runID, resolved, nil
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
	goals := append([]string(nil), request.QualityGoals...)
	sort.Strings(goals)
	userMessage := fmt.Sprintf(
		"Task ID: %s\nProfile: %s\nTask type: %s\nLength bucket: %s\nAllowed input classes: %s\nQualitySpec goals:\n- %s\n\nAuthorized task input:\n%s",
		request.TaskID, request.ProfileID, request.TaskType, request.LengthBucket,
		strings.Join(request.AllowedInputs, ", "), strings.Join(goals, "\n- "), request.Input,
	)
	return generator.generate(ctx, request.Model, request.SystemTemplate, userMessage)
}

func (generator *einoGenerator) GenerateHarness(ctx context.Context, request evaluation.HarnessRequest) (evaluation.GenerationResult, error) {
	return generator.generate(ctx, request.Model, request.SystemTemplate, request.UserInput)
}

func (generator *einoGenerator) generate(ctx context.Context, model evaluation.ModelConfigSnapshot, systemTemplate, userInput string) (evaluation.GenerationResult, error) {
	temperature := float32(model.Parameters.Temperature)
	maxTokens := model.Parameters.MaxOutputTokens
	modelConfig := openai.ChatModelConfig{
		APIKey: generator.apiKey, BaseURL: model.BaseURL, Model: model.Model,
		Temperature: &temperature, MaxTokens: &maxTokens, HTTPClient: providercompat.WrapHTTPClient(nil),
	}
	extraFields := map[string]any{}
	for key, value := range evaluationThinkingFields(model) {
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
		return evaluation.GenerationResult{}, errors.New("create evaluation model failed")
	}
	wrapped := providercompat.Wrap(chatModel, modelConfig)
	message, err := wrapped.Generate(ctx, []*schema.Message{
		schema.SystemMessage(systemTemplate),
		schema.UserMessage(userInput),
	})
	if err != nil {
		return evaluation.GenerationResult{}, errors.New("evaluation model call failed")
	}
	if message == nil {
		return evaluation.GenerationResult{}, errors.New("evaluation model call returned nil")
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

func evaluationThinkingFields(model evaluation.ModelConfigSnapshot) map[string]any {
	if strings.EqualFold(strings.TrimSpace(model.Provider), "deepseek") && strings.HasPrefix(strings.ToLower(strings.TrimSpace(model.Model)), "deepseek-v4") {
		mode := "disabled"
		if model.Parameters.ThinkingEnabled {
			mode = "enabled"
		}
		return map[string]any{"thinking": map[string]any{"type": mode}}
	}
	thinking := model.Parameters.ThinkingEnabled
	return providercompat.ThinkingExtraFields(openai.ChatModelConfig{BaseURL: model.BaseURL, Model: model.Model}, &thinking)
}
