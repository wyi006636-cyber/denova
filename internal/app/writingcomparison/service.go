package writingcomparison

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"denova/config"
	"denova/internal/agents/skills"
	"denova/internal/book/lore"
	"denova/internal/portablepath"
	projectdomain "denova/internal/project"

	"github.com/alfredxw/denova/agent/model/providers"
	"github.com/google/uuid"
)

// Host supplies project-scoped settings without coupling the domain to App.
// Implementations must return a resolved config snapshot, never a live pointer
// that may change while a task is being created.
type Host interface {
	Config(ctx context.Context, layout projectdomain.Layout) (config.Config, error)
	SkillDirectories(ctx context.Context, layout projectdomain.Layout) ([]skills.Directory, error)
}

type ServiceOption func(*Service)

func WithTemplateResolver(resolver WritingSourceResolver) ServiceOption {
	return func(service *Service) { service.templateResolver = resolver }
}

type Service struct {
	host             Host
	templateResolver WritingSourceResolver
}

func NewService(host Host, options ...ServiceOption) *Service {
	service := &Service{host: host}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func (service *Service) CandidateOptions(ctx context.Context, layout projectdomain.Layout) (CandidateOptions, error) {
	cfg, err := service.config(ctx, layout)
	if err != nil {
		return CandidateOptions{}, err
	}
	profiles := profileInputs(&cfg)
	result := CandidateOptions{Profiles: make([]CandidateOption, 0, len(profiles)), MaxCandidates: MaxCandidates}
	for _, profile := range profiles {
		check := CheckCandidateProfile(&cfg, profile.ID)
		result.Profiles = append(result.Profiles, check.Option)
	}
	sort.Slice(result.Profiles, func(i, j int) bool { return result.Profiles[i].ProfileID < result.Profiles[j].ProfileID })
	return result, nil
}

func (service *Service) Create(ctx context.Context, layout projectdomain.Layout, input CreateInput) (Task, error) {
	if service == nil {
		return Task{}, fmt.Errorf("writing comparison service is nil")
	}
	if layout.ProjectID == "" {
		return Task{}, domainError("invalid_request", "Project scope is required", ErrInvalidRequest, nil)
	}
	cfg, err := service.config(ctx, layout)
	if err != nil {
		return Task{}, err
	}
	if len(input.Candidates) < MinCandidates || len(input.Candidates) > MaxCandidates {
		return Task{}, domainError("candidate_count", "Candidate count must be between two and four", ErrInvalidRequest, map[string]any{"min": MinCandidates, "max": MaxCandidates})
	}
	target, err := ValidateTarget(layout, input.TargetPath)
	if err != nil {
		return Task{}, err
	}
	chapterTask := input.ChapterTask
	chapterTask.Text = strings.TrimSpace(chapterTask.Text)
	if chapterTask.Text == "" {
		return Task{}, domainError("invalid_request", "Chapter task text is required", ErrInvalidRequest, map[string]any{"field": "chapter_task.text"})
	}
	if chapterTask.Source == "" {
		chapterTask.Source = "author"
	}
	if input.ReviewModelProfileID == "" || input.FinalWriterProfileID == "" {
		return Task{}, domainError("invalid_request", "Review and final writer Profiles are required", ErrInvalidRequest, map[string]any{"fields": []string{"review_model_profile_id", "final_writer_profile_id"}})
	}
	review, err := checkProfileOrError(&cfg, input.ReviewModelProfileID, "review_model_profile_id")
	if err != nil {
		return Task{}, err
	}
	finalWriter, err := checkProfileOrError(&cfg, input.FinalWriterProfileID, "final_writer_profile_id")
	if err != nil {
		return Task{}, err
	}
	candidates := make([]Candidate, 0, len(input.Candidates))
	for index, candidateInput := range input.Candidates {
		profile, err := checkProfileOrError(&cfg, candidateInput.ProfileID, fmt.Sprintf("candidates[%d].profile_id", index))
		if err != nil {
			if typed := new(Error); errors.As(err, &typed) && typed.Details == nil {
				typed.Details = map[string]any{}
			}
			if typed := new(Error); errors.As(err, &typed) {
				if typed.Details == nil {
					typed.Details = map[string]any{}
				}
				typed.Details["candidate_index"] = index
			}
			return Task{}, err
		}
		source, err := service.resolveSource(ctx, layout, candidateInput.Source)
		if err != nil {
			if typed := new(Error); errors.As(err, &typed) {
				if typed.Details == nil {
					typed.Details = map[string]any{}
				}
				typed.Details["candidate_index"] = index
			}
			return Task{}, err
		}
		candidateID := fmt.Sprintf("candidate-%d", index+1)
		candidates = append(candidates, Candidate{ID: candidateID, Position: index + 1, Profile: profile.Snapshot, Source: source, Requirement: strings.TrimSpace(candidateInput.Requirement), Status: CandidatePending, Attempt: 1})
	}
	createdAt := time.Now().UTC()
	taskID := "comparison-" + uuid.NewString()
	expected := ExpectedCalls{Candidates: len(candidates), Reviews: 1, Syntheses: 1, FinalWrites: 1}
	if input.ExpectedCalls != nil {
		if input.ExpectedCalls.Candidates != 0 && input.ExpectedCalls.Candidates != len(candidates) {
			return Task{}, domainError("invalid_request", "Expected candidate calls do not match candidates", ErrInvalidRequest, nil)
		}
		if input.ExpectedCalls.Reviews > 0 {
			expected.Reviews = input.ExpectedCalls.Reviews
		}
		if input.ExpectedCalls.Syntheses > 0 {
			expected.Syntheses = input.ExpectedCalls.Syntheses
		}
		if input.ExpectedCalls.FinalWrites > 0 {
			expected.FinalWrites = input.ExpectedCalls.FinalWrites
		}
	}
	task := Task{ID: taskID, ProjectID: layout.ProjectID, CreatedAt: createdAt, UpdatedAt: createdAt, Phase: PhaseConfiguring, Execution: ExecutionIdle, Target: target, ChapterTask: chapterTask, Candidates: candidates, ReviewModel: review.Snapshot, FinalWriter: finalWriter.Snapshot, ExpectedCalls: expected}
	return NewStore(layout).Create(ctx, task)
}

func (service *Service) Get(ctx context.Context, layout projectdomain.Layout, id string) (Task, error) {
	if err := projectdomain.ValidateID(id); err != nil {
		return Task{}, ErrNotFound
	}
	return NewStore(layout).Get(ctx, id)
}

func (service *Service) List(ctx context.Context, layout projectdomain.Layout, options ListOptions) ([]TaskSummary, error) {
	if options.Phase != "" && !options.Phase.Valid() {
		return nil, domainError(CodeInvalidRequest, "Comparison task phase is invalid", ErrInvalidRequest, map[string]any{"phase": options.Phase})
	}
	return NewStore(layout).List(ctx, options)
}

func (service *Service) config(ctx context.Context, layout projectdomain.Layout) (config.Config, error) {
	if service == nil || service.host == nil {
		return config.Config{}, fmt.Errorf("writing comparison host is unavailable")
	}
	return service.host.Config(ctx, layout)
}

func (service *Service) resolveSource(ctx context.Context, layout projectdomain.Layout, ref WritingSourceRef) (WritingSourceSnapshot, error) {
	ref.Kind = strings.ToLower(strings.TrimSpace(ref.Kind))
	if ref.Kind == "" {
		ref.Kind = "skill"
	}
	if ref.Kind == "template" {
		if ref.identifier() == "" {
			return WritingSourceSnapshot{}, domainError(CodeTemplateMissing, "Writing template is required", ErrInvalidRequest, nil)
		}
		version := strings.TrimSpace(ref.Version)
		if version == "" || version == "0" {
			return WritingSourceSnapshot{}, domainError(CodeTemplateVersionRequired, "Template content version is required", ErrInvalidRequest, nil)
		}
		if service.templateResolver == nil {
			return WritingSourceSnapshot{}, domainError(CodeTemplateCatalogUnavailable, "Template catalog is unavailable", nil, nil)
		}
		return service.templateResolver.ResolveCandidateSource(ctx, ref)
	}
	if ref.Kind != "skill" {
		return WritingSourceSnapshot{}, domainError(CodeSkillNotUsable, "Writing source kind is not usable", ErrInvalidRequest, nil)
	}
	if ref.identifier() == "" {
		return WritingSourceSnapshot{}, domainError(CodeSkillMissing, "Writing Skill is required", ErrInvalidRequest, nil)
	}
	dirs, err := service.host.SkillDirectories(ctx, layout)
	if err != nil {
		return WritingSourceSnapshot{}, err
	}
	return NewSkillSourceResolver(dirs).ResolveCandidateSource(ctx, ref)
}

func profileInputs(cfg *config.Config) []config.ModelProfileSettings {
	if cfg == nil {
		return nil
	}
	result := make([]config.ModelProfileSettings, 0, len(cfg.ModelProfiles)+1)
	defaultProfile := legacyDefaultProfile(cfg)
	result = append(result, defaultProfile)
	seen := map[string]bool{"default": true}
	for _, profile := range cfg.ModelProfiles {
		id := strings.TrimSpace(profile.ID)
		if id == "" {
			id = strings.TrimSpace(profile.Model)
		}
		if id == "" || seen[id] {
			continue
		}
		profile.ID = id
		seen[id] = true
		result = append(result, profile)
	}
	return result
}

func legacyDefaultProfile(cfg *config.Config) config.ModelProfileSettings {
	profile := config.ModelProfileSettings{ID: "default", Name: "default", Model: cfg.OpenAIModel, Provider: "openai", Protocol: string(providers.ProtocolOpenAIChatCompletions), BaseURL: cfg.OpenAIBaseURL, APIKey: cfg.OpenAIAPIKey}
	for _, stored := range cfg.ModelProfiles {
		id := strings.TrimSpace(stored.ID)
		if id == "" {
			id = strings.TrimSpace(stored.Model)
		}
		if id != "default" {
			continue
		}
		profile = mergeProfileFields(profile, stored)
		profile.ID = "default"
		break
	}
	return profile
}

func mergeProfileFields(base, overlay config.ModelProfileSettings) config.ModelProfileSettings {
	if strings.TrimSpace(overlay.Name) != "" {
		base.Name = overlay.Name
	}
	if overlay.Enabled != nil {
		base.Enabled = overlay.Enabled
	}
	if strings.TrimSpace(overlay.EndpointID) != "" {
		base.EndpointID = overlay.EndpointID
	}
	if strings.TrimSpace(overlay.Model) != "" {
		base.Model = overlay.Model
	}
	if strings.TrimSpace(overlay.Provider) != "" {
		base.Provider = overlay.Provider
	}
	if strings.TrimSpace(overlay.Protocol) != "" {
		base.Protocol = overlay.Protocol
	}
	if strings.TrimSpace(overlay.APIKey) != "" {
		base.APIKey = overlay.APIKey
	}
	if strings.TrimSpace(overlay.BaseURL) != "" {
		base.BaseURL = overlay.BaseURL
	}
	if strings.TrimSpace(overlay.LegacyOpenAIAPIKey) != "" {
		base.LegacyOpenAIAPIKey = overlay.LegacyOpenAIAPIKey
	}
	if strings.TrimSpace(overlay.LegacyOpenAIBaseURL) != "" {
		base.LegacyOpenAIBaseURL = overlay.LegacyOpenAIBaseURL
	}
	if strings.TrimSpace(overlay.LegacyOpenAIModel) != "" {
		base.LegacyOpenAIModel = overlay.LegacyOpenAIModel
	}
	return base
}

func checkProfileOrError(cfg *config.Config, id, field string) (ProfileCheckResult, error) {
	check := CheckCandidateProfile(cfg, id)
	if !check.Option.Eligible {
		return ProfileCheckResult{}, domainError("profile_unavailable", "Selected model Profile is unavailable", ErrInvalidRequest, map[string]any{"field": field, "profile_id": id, "reason": check.Option.Reason})
	}
	return check, nil
}

// CheckCandidateProfile performs every creation-time compatibility gate. The
// result is safe to send to a browser: credentials are deliberately omitted.
func CheckCandidateProfile(cfg *config.Config, profileID string) ProfileCheckResult {
	id := strings.TrimSpace(profileID)
	option := CandidateOption{ProfileID: id, Runtime: string(config.RuntimeNative)}
	if id == "" || cfg == nil || (!config.ModelProfileExists(cfg, id) && id != "default") {
		option.Reason = "profile_missing"
		return ProfileCheckResult{Option: option}
	}
	var raw config.ModelProfileSettings
	if id == "default" {
		raw = legacyDefaultProfile(cfg)
	} else {
		found := false
		for _, candidate := range cfg.ModelProfiles {
			candidateID := strings.TrimSpace(candidate.ID)
			if candidateID == "" {
				candidateID = strings.TrimSpace(candidate.Model)
			}
			if candidateID == id {
				raw, found = candidate, true
				raw.ID = id
				break
			}
		}
		if !found {
			option.Reason = "profile_missing"
			return ProfileCheckResult{Option: option}
		}
	}
	option.Name = firstNonEmpty(strings.TrimSpace(raw.Name), id)
	if raw.Enabled != nil && !*raw.Enabled {
		option.Reason = "profile_disabled"
		return ProfileCheckResult{Option: option}
	}
	// An explicit profile protocol is authoritative even when the endpoint
	// carries a default protocol. Reject it before config inheritance can hide
	// an unsupported value.
	if strings.TrimSpace(raw.Protocol) != "" && !supportedProtocol(raw.Protocol) {
		option.Reason = "protocol_unsupported"
		return ProfileCheckResult{Option: option}
	}
	if raw.EndpointID != "" {
		foundEndpoint := false
		for _, endpoint := range cfg.ModelEndpoints {
			if strings.TrimSpace(endpoint.ID) == strings.TrimSpace(raw.EndpointID) {
				foundEndpoint = true
				break
			}
		}
		if !foundEndpoint && raw.EndpointID != config.DefaultModelEndpointID {
			option.Reason = "endpoint_missing"
			return ProfileCheckResult{Option: option}
		}
	}
	resolved, err := config.ResolveModelProfile(cfg, config.ModelProfileSettings{ID: id})
	if err != nil {
		option.Reason = "profile_missing"
		return ProfileCheckResult{Option: option}
	}
	if strings.TrimSpace(resolved.Model) == "" {
		option.Reason = "model_missing"
		return ProfileCheckResult{Option: option}
	}
	if strings.TrimSpace(resolved.BaseURL) == "" || (strings.TrimSpace(resolved.APIKey) == "" && endpointRequiresAPIKey(resolved.Provider, resolved.Protocol, resolved.BaseURL)) {
		option.Reason = "endpoint_incomplete"
		return ProfileCheckResult{Option: option}
	}
	if !supportedProtocol(resolved.Protocol) {
		option.Reason = "protocol_unsupported"
		return ProfileCheckResult{Option: option}
	}
	snapshot := ModelSnapshot{ProfileID: id, ProfileName: option.Name, Model: resolved.Model, Runtime: string(config.RuntimeNative), EndpointID: raw.EndpointID, Provider: resolved.Provider, Protocol: resolved.Protocol}
	option.Model, option.Eligible = resolved.Model, true
	return ProfileCheckResult{Option: option, Snapshot: snapshot}
}

func supportedProtocol(value string) bool {
	switch providers.ProtocolID(strings.TrimSpace(value)) {
	case providers.ProtocolOpenAIChatCompletions, providers.ProtocolOpenAIResponses, providers.ProtocolAnthropicMessages:
		return true
	default:
		return false
	}
}

func endpointRequiresAPIKey(provider, protocol, baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err == nil && parsed.Hostname() != "" && isLoopbackHost(parsed.Hostname()) {
		return false
	}
	// All currently supported hosted protocols authenticate with an API key.
	// A loopback endpoint is the explicit keyless exception for local model
	// servers such as Ollama and LM Studio.
	return strings.TrimSpace(provider) != "" || strings.TrimSpace(protocol) != ""
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") || host == "::1" || host == "127.0.0.1" {
		return true
	}
	return net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ValidateTarget(layout projectdomain.Layout, targetPath string) (TargetRef, error) {
	targetPath = strings.TrimSpace(targetPath)
	if filepath.IsAbs(targetPath) || strings.Contains(targetPath, `\`) || portablepath.Validate(targetPath) != nil {
		return TargetRef{}, domainError("target_path_invalid", "Target path must be a portable Project-relative file path", ErrInvalidRequest, map[string]any{"path": targetPath})
	}
	if err := portablepath.CheckPath(layout.ContentRoot, targetPath); err != nil {
		return TargetRef{}, domainError("target_path_invalid", "Target path is not valid", err, map[string]any{"path": targetPath})
	}
	fullPath := filepath.Join(layout.ContentRoot, filepath.FromSlash(targetPath))
	info, err := os.Lstat(fullPath)
	if err == nil && info.IsDir() {
		return TargetRef{}, domainError("target_path_invalid", "Target path must be a file", ErrInvalidRequest, nil)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return TargetRef{}, domainError("target_path_invalid", "Target path cannot be inspected", err, nil)
	}
	ref := TargetRef{Path: targetPath, ChapterCursor: lore.ExtractChapterPrefix(targetPath)}
	if errors.Is(err, os.ErrNotExist) {
		ref.BaseRevision, ref.Exists = "missing", false
		return ref, nil
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return TargetRef{}, domainError("target_path_invalid", "Target file cannot be read", err, nil)
	}
	ref.Exists, ref.BaseRevision = true, "sha256:"+AssetSHA256(data)
	return ref, nil
}
