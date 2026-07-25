package yanzhouadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	ContextLoadErrorCode = "context_unavailable"

	contextManifestMaxBytes = 256 * 1024
	contextSectionMaxBytes  = 128 * 1024
	contextTotalMaxBytes    = 256 * 1024
	contextMaxChars         = 65536
	contextMaxIDBytes       = 256
	contextMaxParentIDs     = 32
	contextMaxEntries       = 128
	contextMinTokens        = 256
	contextMaxTokens        = 131072
)

// ContentRefSource is the complete sidecar authority for ContextPack delivery.
// It cannot enumerate, scan, or resolve anything other than an authorized ref.
type ContentRefSource interface {
	Get(context.Context, string) ([]byte, error)
}

// AuthorizedContextRequest carries only logical run authority and the exact CAS ref.
type AuthorizedContextRequest struct {
	BookID         string
	Target         ContextTargetRef
	ContextPackRef string
}

type ContextTargetRef struct {
	SchemaVersion string   `json:"schemaVersion"`
	Kind          string   `json:"kind"`
	BookID        string   `json:"bookId"`
	TargetID      string   `json:"targetId"`
	ParentIDs     []string `json:"parentIds,omitempty"`
}

type ContextSectionKind string

const (
	ContextSectionBookMeta        ContextSectionKind = "book_meta"
	ContextSectionMainOutline     ContextSectionKind = "main_outline"
	ContextSectionVolumeOutline   ContextSectionKind = "volume_outline"
	ContextSectionChapterOutline  ContextSectionKind = "chapter_outline"
	ContextSectionChapterText     ContextSectionKind = "chapter_text"
	ContextSectionAdjacentChapter ContextSectionKind = "adjacent_chapter"
	ContextSectionCharacter       ContextSectionKind = "character"
	ContextSectionSetting         ContextSectionKind = "setting"
	ContextSectionRelationship    ContextSectionKind = "relationship"
	ContextSectionTimeline        ContextSectionKind = "timeline"
	ContextSectionThread          ContextSectionKind = "thread"
	ContextSectionKnowledge       ContextSectionKind = "knowledge"
	ContextSectionStyle           ContextSectionKind = "style"
	ContextSectionSelection       ContextSectionKind = "selection"
	ContextSectionReviewFeedback  ContextSectionKind = "review_feedback"
	ContextSectionGameFact        ContextSectionKind = "game_fact"
)

var contextSectionKinds = []ContextSectionKind{
	ContextSectionBookMeta,
	ContextSectionMainOutline,
	ContextSectionVolumeOutline,
	ContextSectionChapterOutline,
	ContextSectionChapterText,
	ContextSectionAdjacentChapter,
	ContextSectionCharacter,
	ContextSectionSetting,
	ContextSectionRelationship,
	ContextSectionTimeline,
	ContextSectionThread,
	ContextSectionKnowledge,
	ContextSectionStyle,
	ContextSectionSelection,
	ContextSectionReviewFeedback,
	ContextSectionGameFact,
}

func ContextSectionKinds() []ContextSectionKind {
	return append([]ContextSectionKind(nil), contextSectionKinds...)
}

type ContextExclusionReason string

const (
	ContextExclusionNotRequested ContextExclusionReason = "not_requested"
	ContextExclusionBudget       ContextExclusionReason = "budget"
	ContextExclusionStale        ContextExclusionReason = "stale"
	ContextExclusionMissing      ContextExclusionReason = "missing"
	ContextExclusionPermission   ContextExclusionReason = "permission"
)

var contextExclusionReasons = []ContextExclusionReason{
	ContextExclusionNotRequested,
	ContextExclusionBudget,
	ContextExclusionStale,
	ContextExclusionMissing,
	ContextExclusionPermission,
}

func ContextExclusionReasons() []ContextExclusionReason {
	return append([]ContextExclusionReason(nil), contextExclusionReasons...)
}

type ContextSection struct {
	ID              string             `json:"id"`
	Kind            ContextSectionKind `json:"kind"`
	Source          ContextTargetRef   `json:"source"`
	Revision        string             `json:"revision"`
	ContentHash     string             `json:"contentHash"`
	ContentRef      string             `json:"contentRef"`
	Chars           int                `json:"chars"`
	EstimatedTokens int                `json:"estimatedTokens"`
	Truncated       bool               `json:"truncated"`
	ReasonIncluded  string             `json:"reasonIncluded"`
}

type ContextExclusion struct {
	Source ContextTargetRef       `json:"source"`
	Reason ContextExclusionReason `json:"reason"`
}

type ContextBudget struct {
	MaxTokens            int `json:"maxTokens"`
	EstimatedTokens      int `json:"estimatedTokens"`
	ReservedOutputTokens int `json:"reservedOutputTokens"`
}

// ContextPackManifest is exactly the Product ContextPack public identity.
type ContextPackManifest struct {
	SchemaVersion string             `json:"schemaVersion"`
	BookID        string             `json:"bookId"`
	Target        ContextTargetRef   `json:"target"`
	CapabilityID  string             `json:"capabilityId,omitempty"`
	PolicyID      string             `json:"policyId"`
	Sections      []ContextSection   `json:"sections"`
	Exclusions    []ContextExclusion `json:"exclusions"`
	Budget        ContextBudget      `json:"budget"`
}

type AuthorizedContextSection struct {
	Metadata ContextSection
	Content  string
}

type AuthorizedContext struct {
	ContextPackRef string
	Manifest       ContextPackManifest
	Sections       []AuthorizedContextSection
}

// ContextReceipt is the bounded public projection of the exact ContextPack
// that the sidecar successfully loaded. It intentionally excludes section
// content and exposes no filesystem or credential-bearing field.
type ContextReceipt struct {
	SchemaVersion  string             `json:"schemaVersion"`
	ContextPackRef string             `json:"contextPackRef"`
	BookID         string             `json:"bookId"`
	Target         ContextTargetRef   `json:"target"`
	Sections       []ContextSection   `json:"sections"`
	Exclusions     []ContextExclusion `json:"exclusions"`
	Budget         ContextBudget      `json:"budget"`
}

// Receipt returns a detached, content-free receipt only after the full
// manifest and every listed section blob have passed LoadAuthorizedContext.
func (c AuthorizedContext) Receipt() ContextReceipt {
	manifest := cloneContextManifest(c.Manifest)
	return ContextReceipt{
		SchemaVersion:  "1",
		ContextPackRef: c.ContextPackRef,
		BookID:         manifest.BookID,
		Target:         manifest.Target,
		Sections:       manifest.Sections,
		Exclusions:     manifest.Exclusions,
		Budget:         manifest.Budget,
	}
}

type contextSourceSpec struct {
	order      int
	targetKind string
	section    ContextSectionKind
}

var (
	contextSHA256Ref  = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	contextSourceID   = regexp.MustCompile(`^context-source-(bookMeta|knowledge|outlines|storyThreads|currentChapter|chapterGoal|adjacentChapters|characters|entityProfiles|dictionary|settings|timelines)-[a-f0-9]{64}$`)
	contextSectionID  = regexp.MustCompile(`^context-section-(bookMeta|knowledge|outlines|storyThreads|currentChapter|chapterGoal|adjacentChapters|characters|entityProfiles|dictionary|settings|timelines)-[a-f0-9]{64}$`)
	contextCredential = regexp.MustCompile(`(?i)(authorization\s*:\s*(?:bearer|basic)\s+\S+|api[\s_-]*key\s*[:=]|runtime[\s_-]*auth\s*[:=]|private[\s_-]*key\s*[:=]|\bsk[-_][A-Za-z0-9_-]{8,})`)
	contextBareAuth   = regexp.MustCompile(`(?i)(?:^|[\s"'=:(\[])(?:bearer|basic)[\t\r\n ]+([A-Za-z0-9._~+/=-]{8,})`)
	contextInteger    = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	contextURIPrefix  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

	contextKindSet = func() map[ContextSectionKind]struct{} {
		result := make(map[ContextSectionKind]struct{}, len(contextSectionKinds))
		for _, kind := range contextSectionKinds {
			result[kind] = struct{}{}
		}
		return result
	}()
	contextExclusionSet = func() map[ContextExclusionReason]struct{} {
		result := make(map[ContextExclusionReason]struct{}, len(contextExclusionReasons))
		for _, reason := range contextExclusionReasons {
			result[reason] = struct{}{}
		}
		return result
	}()
	contextTargetKinds = map[string]struct{}{
		"book": {}, "main_outline": {}, "volume": {}, "volume_outline": {},
		"chapter": {}, "chapter_outline": {}, "text_selection": {}, "character": {},
		"setting": {}, "relationship": {}, "thread": {}, "review_finding": {},
		"game_story": {}, "game_branch": {}, "game_turn": {},
	}
	contextSourceSpecs = map[string]contextSourceSpec{
		"bookMeta":         {order: 0, targetKind: "book", section: ContextSectionBookMeta},
		"knowledge":        {order: 1, targetKind: "book", section: ContextSectionKnowledge},
		"outlines":         {order: 2, targetKind: "main_outline", section: ContextSectionMainOutline},
		"storyThreads":     {order: 3, targetKind: "thread", section: ContextSectionThread},
		"currentChapter":   {order: 4, targetKind: "chapter", section: ContextSectionChapterText},
		"chapterGoal":      {order: 5, targetKind: "chapter_outline", section: ContextSectionChapterOutline},
		"adjacentChapters": {order: 6, targetKind: "chapter", section: ContextSectionAdjacentChapter},
		"characters":       {order: 7, targetKind: "character", section: ContextSectionCharacter},
		"entityProfiles":   {order: 8, targetKind: "setting", section: ContextSectionSetting},
		"dictionary":       {order: 9, targetKind: "setting", section: ContextSectionSetting},
		"settings":         {order: 10, targetKind: "setting", section: ContextSectionSetting},
		"timelines":        {order: 11, targetKind: "book", section: ContextSectionTimeline},
	}
)

func contextLoadFailure() error {
	return fmt.Errorf("%s", ContextLoadErrorCode)
}

// LoadAuthorizedContext resolves one pre-authorized public manifest and only
// the section refs explicitly listed by that manifest, in manifest order.
func LoadAuthorizedContext(ctx context.Context, source ContentRefSource, request AuthorizedContextRequest) (loaded AuthorizedContext, err error) {
	defer func() {
		if recover() != nil {
			loaded = AuthorizedContext{}
			err = contextLoadFailure()
		}
	}()
	return loadAuthorizedContext(ctx, source, request)
}

func loadAuthorizedContext(ctx context.Context, source ContentRefSource, request AuthorizedContextRequest) (AuthorizedContext, error) {
	if ctx == nil || source == nil || validateAuthorizedContextRequest(request) != nil {
		return AuthorizedContext{}, contextLoadFailure()
	}
	manifestBytes, err := getContextRef(ctx, source, request.ContextPackRef)
	if err != nil || len(manifestBytes) == 0 || len(manifestBytes) > contextManifestMaxBytes {
		return AuthorizedContext{}, contextLoadFailure()
	}
	if !utf8.Valid(manifestBytes) || contextRef(manifestBytes) != request.ContextPackRef {
		return AuthorizedContext{}, contextLoadFailure()
	}
	root, err := parseStrictContextJSON(manifestBytes)
	if err != nil || validateContextManifestShape(root) != nil {
		return AuthorizedContext{}, contextLoadFailure()
	}
	rootObject := root.(map[string]any)
	if capability, present := rootObject["capabilityId"]; present {
		value, ok := capability.(string)
		if !ok || validateContextOpaque(value) != nil {
			return AuthorizedContext{}, contextLoadFailure()
		}
	}
	canonical, err := canonicalContextJSON(root)
	if err != nil || contextRef(canonical) != request.ContextPackRef {
		return AuthorizedContext{}, contextLoadFailure()
	}
	var manifest ContextPackManifest
	if strictContextDecode(manifestBytes, &manifest) != nil || validateContextManifest(manifest, request) != nil {
		return AuthorizedContext{}, contextLoadFailure()
	}

	sections := make([]AuthorizedContextSection, 0, len(manifest.Sections))
	totalBytes := 0
	totalChars := 0
	for _, section := range manifest.Sections {
		content, getErr := getContextRef(ctx, source, section.ContentRef)
		if getErr != nil || len(content) == 0 || len(content) > contextSectionMaxBytes {
			return AuthorizedContext{}, contextLoadFailure()
		}
		totalBytes += len(content)
		if totalBytes > contextTotalMaxBytes || !utf8.Valid(content) || contextRef(content) != section.ContentRef || section.ContentHash != section.ContentRef {
			return AuthorizedContext{}, contextLoadFailure()
		}
		if strings.TrimSpace(string(content)) == "" || containsContextCredential(content) {
			return AuthorizedContext{}, contextLoadFailure()
		}
		chars := utf8.RuneCount(content)
		estimatedTokens := (len(content) + 2) / 3
		totalChars += chars
		if chars != section.Chars || estimatedTokens != section.EstimatedTokens || totalChars > contextMaxChars {
			return AuthorizedContext{}, contextLoadFailure()
		}
		metadata := section
		metadata.Source = cloneContextTarget(section.Source)
		sections = append(sections, AuthorizedContextSection{
			Metadata: metadata,
			Content:  string(append([]byte(nil), content...)),
		})
	}

	return AuthorizedContext{
		ContextPackRef: request.ContextPackRef,
		Manifest:       cloneContextManifest(manifest),
		Sections:       sections,
	}, nil
}

func getContextRef(ctx context.Context, source ContentRefSource, ref string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, contextLoadFailure()
	}
	value, err := source.Get(ctx, ref)
	if err != nil {
		return nil, contextLoadFailure()
	}
	if err := ctx.Err(); err != nil {
		return nil, contextLoadFailure()
	}
	return append([]byte(nil), value...), nil
}

func validateAuthorizedContextRequest(request AuthorizedContextRequest) error {
	if !contextSHA256Ref.MatchString(request.ContextPackRef) || validateContextOpaque(request.BookID) != nil {
		return contextLoadFailure()
	}
	if validateContextTarget(request.Target, request.BookID) != nil || request.Target.BookID != request.BookID {
		return contextLoadFailure()
	}
	return nil
}

func validateContextManifest(manifest ContextPackManifest, request AuthorizedContextRequest) error {
	if manifest.SchemaVersion != "1" || validateContextOpaque(manifest.BookID) != nil || manifest.BookID != request.BookID {
		return contextLoadFailure()
	}
	if validateContextTarget(manifest.Target, manifest.BookID) != nil || !sameContextTarget(manifest.Target, request.Target) {
		return contextLoadFailure()
	}
	if manifest.PolicyID != "writing.default.v1" {
		return contextLoadFailure()
	}
	if manifest.CapabilityID != "" && manifest.CapabilityID != "writing.create_artifact" && manifest.CapabilityID != "writing.create_proposal" {
		return contextLoadFailure()
	}
	if len(manifest.Sections) > contextMaxEntries || len(manifest.Exclusions) > contextMaxEntries {
		return contextLoadFailure()
	}
	if validateContextBudget(manifest.Budget) != nil {
		return contextLoadFailure()
	}

	sectionIDs := make(map[string]struct{}, len(manifest.Sections))
	includedSources := make(map[string]struct{}, len(manifest.Sections))
	previousOrder := -1
	estimatedTokens := 0
	for _, section := range manifest.Sections {
		key, order, sectionErr := validateContextSection(section, manifest.BookID)
		if sectionErr != nil || order <= previousOrder {
			return contextLoadFailure()
		}
		previousOrder = order
		if _, exists := sectionIDs[section.ID]; exists {
			return contextLoadFailure()
		}
		if _, exists := includedSources[section.Source.TargetID]; exists {
			return contextLoadFailure()
		}
		sectionIDs[section.ID] = struct{}{}
		includedSources[section.Source.TargetID] = struct{}{}
		if section.ReasonIncluded != "required:"+key && section.ReasonIncluded != "requested:"+key {
			return contextLoadFailure()
		}
		estimatedTokens += section.EstimatedTokens
		if estimatedTokens > contextMaxTokens {
			return contextLoadFailure()
		}
	}

	excludedSources := make(map[string]struct{}, len(manifest.Exclusions))
	previousOrder = -1
	for _, exclusion := range manifest.Exclusions {
		key, order, exclusionErr := validateContextSource(exclusion.Source, manifest.BookID)
		if exclusionErr != nil || order <= previousOrder {
			return contextLoadFailure()
		}
		previousOrder = order
		if _, ok := contextSourceSpecs[key]; !ok {
			return contextLoadFailure()
		}
		if _, ok := contextExclusionSet[exclusion.Reason]; !ok {
			return contextLoadFailure()
		}
		if _, exists := excludedSources[exclusion.Source.TargetID]; exists {
			return contextLoadFailure()
		}
		if _, included := includedSources[exclusion.Source.TargetID]; included {
			return contextLoadFailure()
		}
		excludedSources[exclusion.Source.TargetID] = struct{}{}
	}
	if estimatedTokens != manifest.Budget.EstimatedTokens {
		return contextLoadFailure()
	}
	return nil
}

func validateContextSection(section ContextSection, bookID string) (string, int, error) {
	idMatch := contextSectionID.FindStringSubmatch(section.ID)
	if len(idMatch) != 2 || validateContextOpaque(section.ID) != nil {
		return "", 0, contextLoadFailure()
	}
	if _, ok := contextKindSet[section.Kind]; !ok {
		return "", 0, contextLoadFailure()
	}
	key, order, err := validateContextSource(section.Source, bookID)
	if err != nil || key != idMatch[1] {
		return "", 0, contextLoadFailure()
	}
	spec := contextSourceSpecs[key]
	if section.Kind != spec.section || !contextSHA256Ref.MatchString(section.Revision) || !contextSHA256Ref.MatchString(section.ContentHash) || section.ContentRef != section.ContentHash {
		return "", 0, contextLoadFailure()
	}
	if section.Chars < 1 || section.Chars > contextMaxChars || section.EstimatedTokens < 1 || section.EstimatedTokens > contextMaxTokens {
		return "", 0, contextLoadFailure()
	}
	return key, order, nil
}

func validateContextSource(source ContextTargetRef, bookID string) (string, int, error) {
	if validateContextTarget(source, bookID) != nil {
		return "", 0, contextLoadFailure()
	}
	match := contextSourceID.FindStringSubmatch(source.TargetID)
	if len(match) != 2 {
		return "", 0, contextLoadFailure()
	}
	spec, ok := contextSourceSpecs[match[1]]
	if !ok || source.Kind != spec.targetKind {
		return "", 0, contextLoadFailure()
	}
	return match[1], spec.order, nil
}

func validateContextTarget(target ContextTargetRef, bookID string) error {
	if target.SchemaVersion != "1" || target.BookID != bookID {
		return contextLoadFailure()
	}
	if _, ok := contextTargetKinds[target.Kind]; !ok {
		return contextLoadFailure()
	}
	if validateContextOpaque(target.BookID) != nil || validateContextOpaque(target.TargetID) != nil || len(target.ParentIDs) > contextMaxParentIDs {
		return contextLoadFailure()
	}
	seen := make(map[string]struct{}, len(target.ParentIDs))
	for _, parentID := range target.ParentIDs {
		if validateContextOpaque(parentID) != nil {
			return contextLoadFailure()
		}
		if _, exists := seen[parentID]; exists {
			return contextLoadFailure()
		}
		seen[parentID] = struct{}{}
	}
	return nil
}

func validateContextBudget(budget ContextBudget) error {
	if budget.MaxTokens < contextMinTokens || budget.MaxTokens > contextMaxTokens || budget.EstimatedTokens < 0 || budget.ReservedOutputTokens < 0 || budget.ReservedOutputTokens >= budget.MaxTokens {
		return contextLoadFailure()
	}
	if budget.EstimatedTokens+budget.ReservedOutputTokens > budget.MaxTokens {
		return contextLoadFailure()
	}
	return nil
}

func validateContextOpaque(value string) error {
	if value == "" || strings.TrimSpace(value) != value || len([]byte(value)) > contextMaxIDBytes || !utf8.ValidString(value) || containsContextCredential([]byte(value)) {
		return contextLoadFailure()
	}
	if value == "." || value == ".." || strings.HasPrefix(value, "~") || strings.ContainsAny(value, `/\`) || contextURIPrefix.MatchString(value) {
		return contextLoadFailure()
	}
	for _, character := range value {
		if character < 32 || character == 127 {
			return contextLoadFailure()
		}
	}
	return nil
}

func containsContextCredential(value []byte) bool {
	if contextCredential.Match(value) {
		return true
	}
	for _, match := range contextBareAuth.FindAllSubmatch(value, -1) {
		if len(match) == 2 {
			return true
		}
	}
	return false
}

func sameContextTarget(left, right ContextTargetRef) bool {
	if left.SchemaVersion != right.SchemaVersion || left.Kind != right.Kind || left.BookID != right.BookID || left.TargetID != right.TargetID || len(left.ParentIDs) != len(right.ParentIDs) {
		return false
	}
	for index := range left.ParentIDs {
		if left.ParentIDs[index] != right.ParentIDs[index] {
			return false
		}
	}
	return true
}

func cloneContextTarget(target ContextTargetRef) ContextTargetRef {
	result := target
	result.ParentIDs = append([]string(nil), target.ParentIDs...)
	return result
}

func cloneContextManifest(manifest ContextPackManifest) ContextPackManifest {
	result := manifest
	result.Target = cloneContextTarget(manifest.Target)
	result.Sections = append([]ContextSection(nil), manifest.Sections...)
	for index := range result.Sections {
		result.Sections[index].Source = cloneContextTarget(manifest.Sections[index].Source)
	}
	result.Exclusions = append([]ContextExclusion(nil), manifest.Exclusions...)
	for index := range result.Exclusions {
		result.Exclusions[index].Source = cloneContextTarget(manifest.Exclusions[index].Source)
	}
	return result
}

func contextRef(value []byte) string {
	digest := sha256.Sum256(value)
	return fmt.Sprintf("sha256:%x", digest[:])
}

func parseStrictContextJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := parseContextJSONValue(decoder)
	if err != nil {
		return nil, contextLoadFailure()
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, contextLoadFailure()
	}
	return value, nil
}

func parseContextJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil || token == nil {
		return nil, contextLoadFailure()
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := map[string]any{}
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			key, ok := keyToken.(string)
			if keyErr != nil || !ok {
				return nil, contextLoadFailure()
			}
			if _, duplicate := object[key]; duplicate {
				return nil, contextLoadFailure()
			}
			child, childErr := parseContextJSONValue(decoder)
			if childErr != nil {
				return nil, contextLoadFailure()
			}
			object[key] = child
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim('}') {
			return nil, contextLoadFailure()
		}
		return object, nil
	case '[':
		array := []any{}
		for decoder.More() {
			child, childErr := parseContextJSONValue(decoder)
			if childErr != nil {
				return nil, contextLoadFailure()
			}
			array = append(array, child)
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim(']') {
			return nil, contextLoadFailure()
		}
		return array, nil
	default:
		return nil, contextLoadFailure()
	}
}

func strictContextDecode(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return contextLoadFailure()
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return contextLoadFailure()
	}
	return nil
}

func validateContextManifestShape(value any) error {
	root, ok := value.(map[string]any)
	if !ok || validateContextObjectKeys(root, []string{"schemaVersion", "bookId", "target", "policyId", "sections", "exclusions", "budget"}, []string{"capabilityId"}) != nil {
		return contextLoadFailure()
	}
	if validateContextTargetShape(root["target"]) != nil {
		return contextLoadFailure()
	}
	sections, ok := root["sections"].([]any)
	if !ok {
		return contextLoadFailure()
	}
	for _, value := range sections {
		section, sectionOK := value.(map[string]any)
		if !sectionOK || validateContextObjectKeys(section, []string{"id", "kind", "source", "revision", "contentHash", "contentRef", "chars", "estimatedTokens", "truncated", "reasonIncluded"}, nil) != nil || validateContextTargetShape(section["source"]) != nil {
			return contextLoadFailure()
		}
	}
	exclusions, ok := root["exclusions"].([]any)
	if !ok {
		return contextLoadFailure()
	}
	for _, value := range exclusions {
		exclusion, exclusionOK := value.(map[string]any)
		if !exclusionOK || validateContextObjectKeys(exclusion, []string{"source", "reason"}, nil) != nil || validateContextTargetShape(exclusion["source"]) != nil {
			return contextLoadFailure()
		}
	}
	budget, ok := root["budget"].(map[string]any)
	if !ok || validateContextObjectKeys(budget, []string{"maxTokens", "estimatedTokens", "reservedOutputTokens"}, nil) != nil {
		return contextLoadFailure()
	}
	return nil
}

func validateContextTargetShape(value any) error {
	target, ok := value.(map[string]any)
	if !ok {
		return contextLoadFailure()
	}
	return validateContextObjectKeys(target, []string{"schemaVersion", "kind", "bookId", "targetId"}, []string{"parentIds"})
}

func validateContextObjectKeys(object map[string]any, required, optional []string) error {
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = struct{}{}
		if _, exists := object[key]; !exists {
			return contextLoadFailure()
		}
	}
	for _, key := range optional {
		allowed[key] = struct{}{}
	}
	for key := range object {
		if _, ok := allowed[key]; !ok {
			return contextLoadFailure()
		}
	}
	return nil
}

func canonicalContextJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	if err := appendCanonicalContextJSON(&output, value); err != nil {
		return nil, contextLoadFailure()
	}
	return output.Bytes(), nil
}

func appendCanonicalContextJSON(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonicalContextString(output, key); err != nil {
				return err
			}
			output.WriteByte(':')
			if err := appendCanonicalContextJSON(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	case []any:
		output.WriteByte('[')
		for index, child := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonicalContextJSON(output, child); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case string:
		return appendCanonicalContextString(output, typed)
	case json.Number:
		if !contextInteger.MatchString(typed.String()) {
			return contextLoadFailure()
		}
		output.WriteString(typed.String())
	case bool:
		if typed {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	default:
		return contextLoadFailure()
	}
	return nil
}

func appendCanonicalContextString(output *bytes.Buffer, value string) error {
	const hexadecimal = "0123456789abcdef"
	output.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"', '\\':
			output.WriteByte('\\')
			output.WriteRune(character)
		case '\b':
			output.WriteString(`\b`)
		case '\f':
			output.WriteString(`\f`)
		case '\n':
			output.WriteString(`\n`)
		case '\r':
			output.WriteString(`\r`)
		case '\t':
			output.WriteString(`\t`)
		default:
			if character < 32 {
				output.WriteString(`\u00`)
				output.WriteByte(hexadecimal[byte(character)>>4])
				output.WriteByte(hexadecimal[byte(character)&0x0f])
			} else {
				output.WriteRune(character)
			}
		}
	}
	output.WriteByte('"')
	return nil
}
