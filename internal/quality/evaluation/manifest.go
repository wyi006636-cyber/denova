package evaluation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,127}$`)
	sha256Pattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	secretPattern = regexp.MustCompile(`(?i)(bearer\s+[a-z0-9._~+/-]{12,}|\bsk-[a-z0-9_-]{12,})`)
)

func LoadManifest(path string) (CorpusManifest, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return CorpusManifest{}, fmt.Errorf("read corpus manifest %s: %w", path, err)
	}
	if err := rejectSensitiveJSON(payload); err != nil {
		return CorpusManifest{}, err
	}
	var manifest CorpusManifest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return CorpusManifest{}, fmt.Errorf("decode corpus manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return CorpusManifest{}, fmt.Errorf("decode corpus manifest: trailing JSON value")
	}
	if err := validateManifest(path, manifest); err != nil {
		return CorpusManifest{}, err
	}
	return manifest, nil
}

func validateManifest(path string, manifest CorpusManifest) error {
	if manifest.Contract != "denova.quality-evaluation-corpus" || manifest.Version != "v1" {
		return fmt.Errorf("unsupported corpus contract %q version %q", manifest.Contract, manifest.Version)
	}
	baseDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("resolve manifest directory: %w", err)
	}
	scopeRoot := repositoryRoot(path)
	if scopeRoot == "" {
		scopeRoot = baseDir
	}
	inputRoot, err := resolveScopedRoot(baseDir, scopeRoot, manifest.InputRoot, "input_root")
	if err != nil {
		return err
	}
	contractRoot, err := resolveScopedRoot(baseDir, scopeRoot, manifest.ContractRoot, "contract_root")
	if err != nil {
		return err
	}
	if err := validateRelativePath(manifest.RunRoot, "run_root", false); err != nil {
		return err
	}
	if err := validateBaseline(baseDir, scopeRoot, manifest.Baseline); err != nil {
		return err
	}
	if len(manifest.Tasks) < 36 {
		return fmt.Errorf("corpus requires at least 36 tasks, got %d", len(manifest.Tasks))
	}
	seen := make(map[string]bool, len(manifest.Tasks))
	profileTasks := make(map[ProfileID][]EvaluationTask, len(AllProfileIDs))
	for index, task := range manifest.Tasks {
		if seen[task.ID] {
			return fmt.Errorf("duplicate task id %q", task.ID)
		}
		seen[task.ID] = true
		if err := validateTask(index, inputRoot, contractRoot, task); err != nil {
			return err
		}
		profileTasks[task.ProfileID] = append(profileTasks[task.ProfileID], task)
	}
	for _, profile := range AllProfileIDs {
		tasks := profileTasks[profile]
		if len(tasks) < 12 {
			return fmt.Errorf("profile %s requires at least 12 tasks, got %d", profile, len(tasks))
		}
		if err := validateProfileStrata(profile, tasks); err != nil {
			return err
		}
	}
	return nil
}

func validateBaseline(baseDir, scopeRoot string, baseline BaselineProtocol) error {
	if baseline.Arm != "S" || strings.TrimSpace(baseline.TemplateVersion) == "" {
		return fmt.Errorf("baseline must define the S arm and a template version")
	}
	if baseline.ModelCallLimit != 1 {
		return fmt.Errorf("baseline S arm model_call_limit must be 1")
	}
	if baseline.HarnessArtifactsAllowed || baseline.ThinkingPersisted {
		return fmt.Errorf("baseline cannot use Harness artifacts or persist thinking")
	}
	if err := validateRelativePath(baseline.TemplateFile, "baseline template_file", true); err != nil {
		return err
	}
	templatePath := filepath.Join(baseDir, filepath.FromSlash(baseline.TemplateFile))
	if !pathWithin(scopeRoot, templatePath) {
		return fmt.Errorf("baseline template_file escapes repository scope")
	}
	return verifyFileHash(templatePath, baseline.TemplateSHA256, "baseline template")
}

func validateTask(index int, inputRoot, contractRoot string, task EvaluationTask) error {
	prefix := fmt.Sprintf("task[%d]", index)
	if !idPattern.MatchString(task.ID) {
		return fmt.Errorf("%s invalid task id %q", prefix, task.ID)
	}
	if !knownProfile(task.ProfileID) {
		return fmt.Errorf("%s unknown profile %q", prefix, task.ProfileID)
	}
	if strings.TrimSpace(task.Genre) == "" || strings.TrimSpace(task.Purpose) == "" {
		return fmt.Errorf("%s genre and purpose are required", prefix)
	}
	if !knownTaskType(task.TaskType) {
		return fmt.Errorf("%s unknown task type %q", prefix, task.TaskType)
	}
	if !knownLengthBucket(task.LengthBucket) {
		return fmt.Errorf("%s unknown length bucket %q", prefix, task.LengthBucket)
	}
	if !knownDataSplit(task.DataSplit) {
		return fmt.Errorf("%s unknown data split %q", prefix, task.DataSplit)
	}
	if len(task.AllowedInputs) == 0 {
		return fmt.Errorf("%s allowed_inputs cannot be empty", prefix)
	}
	if err := validateRelativePath(task.InputFile, prefix+" input_file", true); err != nil {
		return err
	}
	if err := verifyFileHash(filepath.Join(inputRoot, filepath.FromSlash(task.InputFile)), task.InputSHA256, "input"); err != nil {
		return fmt.Errorf("%s input hash mismatch: %w", prefix, err)
	}
	if !knownSourceKind(task.Source.Kind) || strings.TrimSpace(task.Source.Reference) == "" {
		return fmt.Errorf("%s source kind/reference invalid", prefix)
	}
	if !knownLicenseStatus(task.Source.LicenseStatus) || !knownAnonymizationStatus(task.Source.AnonymizationStatus) {
		return fmt.Errorf("%s source license/anonymization status invalid", prefix)
	}
	if err := validateRelativePath(task.QualitySpec.ContractFile, prefix+" quality_spec.contract_file", true); err != nil {
		return err
	}
	if err := verifyFileHash(filepath.Join(contractRoot, filepath.FromSlash(task.QualitySpec.ContractFile)), task.QualitySpec.ContractSHA256, "QualitySpec contract"); err != nil {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	if len(task.QualitySpec.Goals) == 0 {
		return fmt.Errorf("%s QualitySpec goals cannot be empty", prefix)
	}
	model := task.ModelConfigSnapshot
	if strings.TrimSpace(model.Provider) == "" || strings.TrimSpace(model.BaseURL) == "" || strings.TrimSpace(model.ModelProfileID) == "" || strings.TrimSpace(model.Model) == "" || model.CredentialSource != "runtime_only" {
		return fmt.Errorf("%s model snapshot must name provider/base URL/profile/model and use runtime_only credentials", prefix)
	}
	if model.Parameters.MaxOutputTokens <= 0 || model.Parameters.ThinkingEnabled {
		return fmt.Errorf("%s model parameters require a positive output boundary and no persisted thinking", prefix)
	}
	if model.SHA256 != StableSHA256(model.hashPayload()) {
		return fmt.Errorf("%s model config hash mismatch", prefix)
	}
	if err := validateRelativePath(strings.ReplaceAll(task.ActualCostRecord, "{run_id}", "run-id"), prefix+" actual_cost_record", true); err != nil {
		return err
	}
	return nil
}

func validateProfileStrata(profile ProfileID, tasks []EvaluationTask) error {
	genres := map[string]bool{}
	splits := map[DataSplit]bool{}
	coverage := map[string]bool{}
	for _, task := range tasks {
		genres[task.Genre] = true
		splits[task.DataSplit] = true
		switch task.TaskType {
		case TaskOpening:
			coverage["opening"] = true
		case TaskCharacterChoice, TaskDialogue:
			coverage["character_or_dialogue"] = true
		case TaskStructureTurn:
			coverage["structure_turn"] = true
		case TaskEnding, TaskContinuity:
			coverage["ending_or_continuity"] = true
		}
	}
	if len(genres) < 2 {
		return fmt.Errorf("profile %s requires at least 2 genre strata", profile)
	}
	for _, split := range []DataSplit{SplitTuning, SplitRegression, SplitReleaseHoldout} {
		if !splits[split] {
			return fmt.Errorf("profile %s missing data split %s", profile, split)
		}
	}
	for _, required := range []string{"opening", "character_or_dialogue", "structure_turn", "ending_or_continuity"} {
		if !coverage[required] {
			return fmt.Errorf("profile %s missing task coverage %s", profile, required)
		}
	}
	return nil
}

func rejectSensitiveJSON(payload []byte) error {
	if secretPattern.Match(payload) {
		return fmt.Errorf("sensitive credential-like value is forbidden in corpus manifest")
	}
	var raw any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil
	}
	var walk func(any) error
	walk = func(value any) error {
		switch typed := value.(type) {
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				normalized := strings.ToLower(strings.TrimSpace(key))
				switch normalized {
				case "api_key", "authorization", "authorization_header", "password", "secret", "credentials", "full_prompt", "thinking_content", "reasoning_content", "reviewer_id", "raw_comments":
					return fmt.Errorf("sensitive field %q is forbidden in corpus manifest", key)
				}
				if err := walk(typed[key]); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range typed {
				if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(raw)
}

func verifyFileHash(path, want, label string) error {
	if !sha256Pattern.MatchString(want) {
		return fmt.Errorf("%s has invalid SHA-256 %q", label, want)
	}
	got, err := FileSHA256(path)
	if err != nil {
		return fmt.Errorf("read %s %s: %w", label, path, err)
	}
	if got != want {
		return fmt.Errorf("%s hash mismatch: got %s want %s", label, got, want)
	}
	return nil
}

func resolveScopedRoot(baseDir, scopeRoot, relative, field string) (string, error) {
	if strings.TrimSpace(relative) == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("%s must be a non-empty relative path", field)
	}
	resolved := filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(relative)))
	if !pathWithin(scopeRoot, resolved) {
		return "", fmt.Errorf("%s escapes repository scope", field)
	}
	return resolved, nil
}

func validateRelativePath(path, field string, forbidParent bool) error {
	if strings.TrimSpace(path) == "" || filepath.IsAbs(path) {
		return fmt.Errorf("%s must be a non-empty relative path", field)
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || (forbidParent && (clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)))) {
		return fmt.Errorf("%s escapes its declared root", field)
	}
	return nil
}

func pathWithin(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func repositoryRoot(path string) string {
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return ""
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func knownProfile(value ProfileID) bool {
	for _, profile := range AllProfileIDs {
		if value == profile {
			return true
		}
	}
	return false
}

func knownTaskType(value TaskType) bool {
	switch value {
	case TaskOpening, TaskCharacterChoice, TaskDialogue, TaskStructureTurn, TaskEnding, TaskContinuity:
		return true
	default:
		return false
	}
}

func knownLengthBucket(value LengthBucket) bool {
	switch value {
	case LengthScene, LengthChapter, LengthShortStory:
		return true
	default:
		return false
	}
}

func knownDataSplit(value DataSplit) bool {
	switch value {
	case SplitTuning, SplitRegression, SplitReleaseHoldout:
		return true
	default:
		return false
	}
}

func knownSourceKind(value string) bool {
	switch value {
	case "synthetic", "public_domain", "licensed_private", "anonymized_private":
		return true
	default:
		return false
	}
}

func knownLicenseStatus(value string) bool {
	switch value {
	case "project_owned", "public_domain", "licensed", "permission_confirmed":
		return true
	default:
		return false
	}
}

func knownAnonymizationStatus(value string) bool {
	switch value {
	case "not_applicable", "anonymized", "hash_only":
		return true
	default:
		return false
	}
}
