package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"denova/internal/workspacepath"
)

// RunLedger is a durable JSONL trace for one Agent loop run.
// It records bounded metadata only, never full prompts, tool outputs, or thinking.
type RunLedger struct {
	mu          sync.Mutex
	id          string
	path        string
	previewChar int
	file        *os.File
}

type runLedgerRecord struct {
	Type      string         `json:"type"`
	RunID     string         `json:"run_id"`
	CreatedAt time.Time      `json:"created_at"`
	Data      map[string]any `json:"data,omitempty"`
}

type textSummary struct {
	Bytes   int    `json:"bytes"`
	Chars   int    `json:"chars"`
	Hash    string `json:"hash,omitempty"`
	Preview string `json:"preview"`
}

const runLedgerDiagnosticOnlyPreview = "[omitted: explicit developer diagnostics only]"

const (
	maxRunLedgerCollectionItems = 256
	maxRunLedgerDepth           = 16
	maxRunLedgerMetadataBytes   = 512
	maxRunLedgerRecordBytes     = 64 * 1024
)

var knownRunLedgerRecordTypes = map[string]struct{}{
	"agent_run":             {},
	"context_build":         {},
	"context_compaction":    {},
	"context_ledger":        {},
	"context_receipt":       {},
	"event":                 {},
	"llm_call":              {},
	"mutations":             {},
	"post_run_verification": {},
	"run_context":           {},
	"run_created":           {},
	"run_finished":          {},
	"run_started":           {},
	"tool_call":             {},
	"tool_decision":         {},
	"tool_execution":        {},
}

type contextReceiptLedgerPart struct {
	Source    string `json:"source,omitempty"`
	Purpose   string `json:"purpose,omitempty"`
	Bytes     int    `json:"bytes"`
	Chars     int    `json:"chars"`
	Hash      string `json:"hash,omitempty"`
	Included  bool   `json:"included"`
	Truncated bool   `json:"truncated,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	LimitUnit string `json:"limit_unit,omitempty"`
}

// TraceSink is the durable destination for structured Agent trace spans.
// The default implementation is the local run ledger; external exporters can
// adapt this interface without changing Agent execution.
type TraceSink interface {
	RecordTraceSpan(span TraceSpanRecord) error
}

func newRunLedger(workspace string, policy RunLedgerPolicy) (*RunLedger, error) {
	return newRunLedgerWithOptions(workspace, policy, RunOptions{})
}

func newRunLedgerWithOptions(workspace string, policy RunLedgerPolicy, options RunOptions, authoritativeRunIDs ...string) (*RunLedger, error) {
	traceCfg := traceRuntimeConfigSnapshot()
	if !policy.Enabled || traceCfg.CaptureLevel == TraceCaptureOff || strings.TrimSpace(workspace) == "" {
		return nil, nil
	}
	options = options.normalized(workspace)
	if policy.Directory == "" {
		policy.Directory = defaultRunLedgerDirectory
	}
	if policy.PreviewChars <= 0 {
		policy.PreviewChars = defaultRunLedgerPreviewChars
	}
	if traceCfg.CaptureLevel == TraceCaptureDebug && policy.PreviewChars < defaultDebugRunLedgerPreviewChars {
		policy.PreviewChars = defaultDebugRunLedgerPreviewChars
	}
	id, err := resolveRunLedgerID(authoritativeRunIDs)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(workspace, filepath.FromSlash(policy.Directory))
	if policy.Directory == defaultRunLedgerDirectory {
		dir = workspacepath.Path(workspace, "runs")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create run ledger dir: %w", err)
	}
	path := filepath.Join(dir, id+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open run ledger: %w", err)
	}
	ledger := &RunLedger{id: id, path: path, previewChar: policy.PreviewChars, file: file}
	if err := ledger.Record("run_created", map[string]any{
		"task_id":          options.TaskID,
		"agent_kind":       options.AgentKind,
		"session_id":       options.SessionID,
		"review_thread_id": options.ReviewThreadID,
		"story_id":         options.StoryID,
		"branch_id":        options.BranchID,
		"turn_id":          options.TurnID,
		"maintenance_task": options.MaintenanceTask,
		"mode":             options.Mode,
	}); err != nil {
		_ = file.Close()
		return nil, err
	}
	pruneRunTraceFiles(dir, traceCfg.RetentionRuns, path)
	return ledger, nil
}

func (l *RunLedger) ID() string {
	if l == nil {
		return ""
	}
	return l.id
}

func (l *RunLedger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *RunLedger) RecordContext(parts []ContextLedgerPart) error {
	if l == nil {
		return nil
	}
	return l.Record("context_ledger", map[string]any{
		"parts": boundedContextLedgerParts(parts),
	})
}

// RecordContextReceipt records the model-visible context boundary at a closed
// run phase. It removes preview/title/note text and includes only bounded
// source metadata plus the content-free compaction receipt.
func (l *RunLedger) RecordContextReceipt(phase string, parts []ContextLedgerPart, compaction ContextCompactionReceipt, hasCompaction bool) error {
	if l == nil {
		return nil
	}
	phase = strings.TrimSpace(phase)
	if phase != "before_run" && phase != "after_run" {
		return errors.New("context receipt phase is invalid")
	}
	bounded := boundedContextLedgerParts(parts)
	data := map[string]any{
		"phase":            phase,
		"parts":            bounded,
		"compaction_epoch": 0,
	}
	if hasCompaction {
		if !validContextCompactionReceipt(compaction) {
			return errors.New("context compaction receipt is invalid")
		}
		data["compaction_epoch"] = compaction.Epoch
		data["compaction"] = compaction
	}
	return l.Record("context_receipt", data)
}

func boundedContextLedgerParts(parts []ContextLedgerPart) []contextReceiptLedgerPart {
	bounded := make([]contextReceiptLedgerPart, 0, len(parts))
	for _, part := range parts {
		bounded = append(bounded, contextReceiptLedgerPart{
			Source:    boundedContextReceiptLabel(part.Source),
			Purpose:   boundedContextReceiptLabel(part.Purpose),
			Bytes:     nonnegativeContextReceiptNumber(part.Bytes),
			Chars:     nonnegativeContextReceiptNumber(part.Chars),
			Hash:      boundedContextReceiptHash(part.Hash),
			Included:  part.Included,
			Truncated: part.Truncated,
			Limit:     nonnegativeContextReceiptNumber(part.Limit),
			LimitUnit: boundedContextReceiptLimitUnit(part.LimitUnit),
		})
	}
	return bounded
}

func boundedContextReceiptLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len([]byte(value)) > 128 || strings.ContainsAny(value, `/\`) {
		return ""
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"authorization", "bearer ", "basic ", "api_key", "apikey", "runtimeauth", "credential", "private_key", "stderr"} {
		if strings.Contains(lower, marker) {
			return ""
		}
	}
	for _, character := range value {
		if character < 32 || character == 127 {
			return ""
		}
	}
	return value
}

func boundedContextReceiptHash(value string) string {
	value = strings.TrimSpace(value)
	if validContextReceiptSHA256(value) {
		return value
	}
	// Existing Denova ContextLedger hashes use a bounded 64-bit sha256 prefix.
	if len(value) == len("sha256:")+16 && strings.HasPrefix(value, "sha256:") {
		for _, character := range value[len("sha256:"):] {
			if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
				return ""
			}
		}
		return value
	}
	return ""
}

func boundedContextReceiptLimitUnit(value string) string {
	switch strings.TrimSpace(value) {
	case "bytes", "chars", "tokens":
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func nonnegativeContextReceiptNumber(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func (l *RunLedger) RecordEvent(ev Event) error {
	if l == nil {
		return nil
	}
	if !shouldRecordRunLedgerEvent(ev.Type) {
		return nil
	}
	return l.Record("event", map[string]any{
		"event_type": ev.Type,
		"event_data": l.summarizeEventData(ev.Data),
	})
}

func (l *RunLedger) RecordToolDecision(decision ToolDecision) error {
	if l == nil {
		return nil
	}
	return l.Record("tool_decision", map[string]any{
		"decision": decision,
	})
}

func (l *RunLedger) RecordToolExecution(result ToolExecutionRecord) error {
	if l == nil {
		return nil
	}
	return l.Record("tool_execution", map[string]any{
		"result": result,
	})
}

func (l *RunLedger) RecordMutations(mutations []ToolMutation) error {
	if l == nil || len(mutations) == 0 {
		return nil
	}
	return l.Record("mutations", map[string]any{
		"mutations": mutations,
	})
}

func (l *RunLedger) RecordVerification(verification PostRunVerification) error {
	if l == nil {
		return nil
	}
	return l.Record("post_run_verification", map[string]any{
		"verification": verification,
	})
}

func (l *RunLedger) RecordTraceSpan(span TraceSpanRecord) error {
	if l == nil {
		return nil
	}
	span.Name = strings.TrimSpace(span.Name)
	if span.Name == "" {
		span.Name = "trace_span"
	}
	return l.Record(span.Name, l.traceSpanData(span))
}

func (l *RunLedger) RecordFinish(status, reason string, generatedBytes int) error {
	if l == nil {
		return nil
	}
	return l.Record("run_finished", map[string]any{
		"status":          strings.TrimSpace(status),
		"reason":          strings.TrimSpace(reason),
		"generated_bytes": generatedBytes,
	})
}

func (l *RunLedger) Record(recordType string, data map[string]any) error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	record := runLedgerRecord{
		Type:      recordType,
		RunID:     l.id,
		CreatedAt: time.Now().UTC(),
		Data:      l.sanitizeRecordData(recordType, data),
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(encoded) > maxRunLedgerRecordBytes {
		record.Data = map[string]any{
			"omitted":        true,
			"original_bytes": len(encoded),
		}
		encoded, err = json.Marshal(record)
		if err != nil {
			return err
		}
	}
	if _, err := l.file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
}

func (l *RunLedger) sanitizeRecordData(recordType string, data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	value := l.sanitizeRecordValue(recordType, "", data, 0, false)
	normalized, _ := value.(map[string]any)
	return normalized
}

func (l *RunLedger) sanitizeRecordValue(recordType, key string, value any, depth int, sensitivePath bool) any {
	if depth > maxRunLedgerDepth {
		return map[string]any{"omitted": true}
	}
	switch typed := value.(type) {
	case nil, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64,
		time.Time:
		return typed
	case textSummary:
		return typed
	case string:
		return l.sanitizeRecordString(recordType, key, typed, sensitivePath)
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for childKey := range typed {
			keys = append(keys, childKey)
		}
		sort.Strings(keys)
		limit := len(keys)
		if limit > maxRunLedgerCollectionItems {
			limit = maxRunLedgerCollectionItems
		}
		out := make(map[string]any, limit+1)
		for _, childKey := range keys[:limit] {
			out[childKey] = l.sanitizeRecordValue(recordType, childKey, typed[childKey], depth+1, sensitivePath || sensitiveRunLedgerAncestor(childKey))
		}
		if len(keys) > limit {
			out["omitted_fields"] = len(keys) - limit
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(typed))
		keys := make([]string, 0, len(typed))
		for childKey := range typed {
			keys = append(keys, childKey)
		}
		sort.Strings(keys)
		limit := len(keys)
		if limit > maxRunLedgerCollectionItems {
			limit = maxRunLedgerCollectionItems
		}
		for _, childKey := range keys[:limit] {
			out[childKey] = l.sanitizeRecordString(recordType, childKey, typed[childKey], sensitivePath || sensitiveRunLedgerAncestor(childKey))
		}
		if len(keys) > limit {
			out["omitted_fields"] = len(keys) - limit
		}
		return out
	case []any:
		limit := len(typed)
		if limit > maxRunLedgerCollectionItems {
			limit = maxRunLedgerCollectionItems
		}
		out := make([]any, 0, limit+1)
		for _, item := range typed[:limit] {
			out = append(out, l.sanitizeRecordValue(recordType, key, item, depth+1, sensitivePath))
		}
		if len(typed) > limit {
			out = append(out, map[string]any{"omitted_items": len(typed) - limit})
		}
		return out
	case []string:
		limit := len(typed)
		if limit > maxRunLedgerCollectionItems {
			limit = maxRunLedgerCollectionItems
		}
		out := make([]any, 0, limit+1)
		for _, item := range typed[:limit] {
			out = append(out, l.sanitizeRecordString(recordType, key, item, sensitivePath))
		}
		if len(typed) > limit {
			out = append(out, map[string]any{"omitted_items": len(typed) - limit})
		}
		return out
	default:
		var normalized any
		encoded, err := json.Marshal(typed)
		if err == nil {
			err = json.Unmarshal(encoded, &normalized)
		}
		if err != nil {
			return l.summarizeText(fmt.Sprint(typed))
		}
		return l.sanitizeRecordValue(recordType, key, normalized, depth+1, sensitivePath)
	}
}

func (l *RunLedger) sanitizeRecordString(recordType, key, value string, sensitivePath bool) any {
	value = strings.TrimSpace(value)
	if sensitivePath ||
		shouldSummarizeRunLedgerField(key) ||
		containsCheckpointSensitiveMaterial([]byte(value)) ||
		len([]byte(value)) > maxRunLedgerMetadataBytes ||
		!safeRunLedgerStringField(recordType, key, value) {
		return l.summarizeText(value)
	}
	return value
}

func safeRunLedgerStringField(recordType, key, value string) bool {
	if _, known := knownRunLedgerRecordTypes[strings.TrimSpace(recordType)]; !known {
		return false
	}
	key = strings.ToLower(strings.TrimSpace(key))
	if filesystemLikeRunLedgerValue(key, value) {
		return false
	}
	if key == "reason" {
		switch recordType {
		case "run_finished":
			return stableRunLedgerFinishReason(value)
		case "tool_decision":
			return stableRunLedgerToolDecisionReason(value)
		default:
			return false
		}
	}
	switch key {
	case "schema_version", "schemaversion",
		"task_id", "agent_kind", "session_id", "review_thread_id",
		"story_id", "branch_id", "turn_id", "maintenance_task", "mode",
		"event_type", "id", "name", "type", "role", "source", "purpose",
		"capability", "action", "status", "domain_status", "model_finish_reason",
		"tool_name", "tool_call_id", "idempotency_key", "change_group_id",
		"change_set_id", "review_status", "apply_state", "base_revision", "revision",
		"trace_id", "span_id", "parent_span_id", "phase", "provider_request_id",
		"finish_reason", "model", "provider", "agent_name", "root_agent_name",
		"run_id", "run_path", "subagent_session_id", "subagent_type", "checkpoint_id",
		"writing_skill", "limit_unit", "strategy", "skipped_reason", "hash", "ref",
		"summary_artifact_ref", "summaryartifactref", "delta_hash", "stable_context_hash",
		"stablecontexthash", "operation_id", "request_id", "attempt_id", "parent_attempt_id",
		"execution_run_id", "interruption_id", "origin_run_id", "outcome", "retry_modules",
		"lore_item_ids", "deleted_lore_item_ids":
		return true
	default:
		return false
	}
}

func filesystemLikeRunLedgerValue(key, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "\\") ||
		strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") ||
		strings.HasPrefix(value, "./") || strings.HasPrefix(value, ".\\") ||
		strings.HasPrefix(value, "../") || strings.HasPrefix(value, "..\\") ||
		strings.HasPrefix(strings.ToLower(value), "file://") {
		return true
	}
	if len(value) >= 3 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) && value[1] == ':' && (value[2] == '/' || value[2] == '\\') {
		return true
	}
	if strings.Contains(value, "\\") {
		return true
	}
	if strings.Contains(value, "/") {
		switch key {
		case "model", "provider":
			return false
		default:
			return true
		}
	}
	return false
}

func sensitiveRunLedgerAncestor(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_")
	switch key {
	case "body", "payload", "provider_request", "provider_response", "raw", "raw_frame", "raw_payload", "request", "response", "stderr", "tool_payload":
		return true
	default:
		return false
	}
}

func stableRunLedgerFinishReason(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "completed", "cancelled", "provider_error", "provider_idle_timeout", "panic",
		"budget_exhausted", "run_wall_timeout", "prepare_messages_failed",
		"user_message_commit_failed", "context_compaction_failed", "assistant_persistence_failed",
		"resume_attempt_finish_failed":
		return true
	default:
		return false
	}
}

func stableRunLedgerToolDecisionReason(value string) bool {
	return strings.Contains(value, "参数不是完整 JSON 对象") ||
		strings.Contains(value, "Tool arguments must be a complete JSON object")
}

func (l *RunLedger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *RunLedger) summarizeEventData(data any) any {
	switch typed := data.(type) {
	case map[string]string:
		return l.summarizeStringMap(typed)
	case map[string]interface{}:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = l.summarizeValue(key, value)
		}
		return out
	case string:
		return l.summarizeText(typed)
	default:
		var normalized any
		if encoded, err := json.Marshal(data); err == nil && json.Unmarshal(encoded, &normalized) == nil {
			return normalized
		}
		return fmt.Sprint(data)
	}
}

func (l *RunLedger) summarizeStringMap(values map[string]string) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = l.summarizeValue(key, value)
	}
	return out
}

func (l *RunLedger) summarizeValue(key string, value any) any {
	switch typed := value.(type) {
	case string:
		if shouldSummarizeRunLedgerField(key) {
			return l.summarizeText(typed)
		}
		return typed
	default:
		return typed
	}
}

func (l *RunLedger) summarizeText(content string) textSummary {
	sum := sha256.Sum256([]byte(content))
	preview := ""
	if content != "" {
		preview = runLedgerDiagnosticOnlyPreview
	}
	return textSummary{
		Bytes:   len(content),
		Chars:   utf8.RuneCountInString(content),
		Hash:    fmt.Sprintf("sha256:%x", sum[:]),
		Preview: preview,
	}
}

func (l *RunLedger) traceSpanData(span TraceSpanRecord) map[string]any {
	attrs := make(map[string]any, len(span.Attrs))
	for key, value := range span.Attrs {
		attrs[key] = l.summarizeTraceAttr(key, value)
	}
	data := map[string]any{
		"trace_id":       strings.TrimSpace(span.TraceID),
		"span_id":        strings.TrimSpace(span.SpanID),
		"parent_span_id": strings.TrimSpace(span.ParentSpanID),
		"name":           strings.TrimSpace(span.Name),
		"status":         strings.TrimSpace(span.Status),
		"started_at":     span.StartedAt,
		"ended_at":       span.EndedAt,
		"duration_ms":    span.DurationMS,
		"attrs":          attrs,
	}
	if span.Error != "" {
		data["error"] = l.summarizeText(span.Error)
	}
	return data
}

func (l *RunLedger) summarizeTraceAttr(key string, value any) any {
	switch typed := value.(type) {
	case string:
		if shouldSummarizeRunLedgerField(key) || shouldSummarizeTraceAttrKey(key) {
			return l.summarizeText(typed)
		}
		return typed
	case map[string]any:
		out := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			out[childKey] = l.summarizeTraceAttr(childKey, childValue)
		}
		return out
	default:
		return typed
	}
}

func shouldSummarizeRunLedgerField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "content", "args", "delta", "message", "error", "result", "thinking", "target", "path", "workspace", "stderr", "preview", "note", "title", "request", "response", "body":
		return true
	default:
		return false
	}
}

func shouldSummarizeTraceAttrKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(key, "prompt") ||
		strings.Contains(key, "content") ||
		strings.Contains(key, "message") ||
		strings.Contains(key, "args") ||
		strings.Contains(key, "result") ||
		strings.Contains(key, "thinking")
}

func shouldRecordRunLedgerEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "tool_call", "tool_target", "tool_result", "token_usage", "error", "aborted":
		return true
	default:
		return false
	}
}

func newRunLedgerID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "run-" + time.Now().UTC().Format("20060102T150405.000000000") + "-" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("run-%d", time.Now().UTC().UnixNano())
}

func resolveRunLedgerID(authoritativeRunIDs []string) (string, error) {
	if len(authoritativeRunIDs) == 0 {
		return newRunLedgerID(), nil
	}
	if len(authoritativeRunIDs) != 1 || !validRunLedgerID(authoritativeRunIDs[0]) {
		return "", errors.New("run ledger identity is invalid")
	}
	return authoritativeRunIDs[0], nil
}

func validRunLedgerID(id string) bool {
	if id == "" || id != strings.TrimSpace(id) || len(id) > 128 {
		return false
	}
	for index := 0; index < len(id); index++ {
		char := id[index]
		alphaNumeric := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
		if alphaNumeric {
			continue
		}
		if index == 0 || char != '.' && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func pruneRunTraceFiles(dir string, retention int, keepPath string) {
	if retention <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type traceFile struct {
		path    string
		modTime time.Time
	}
	files := make([]traceFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, traceFile{path: path, modTime: info.ModTime()})
	}
	if len(files) <= retention {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})
	keepPath = filepath.Clean(keepPath)
	kept := 0
	for _, file := range files {
		if kept < retention || filepath.Clean(file.path) == keepPath {
			kept++
			continue
		}
		_ = os.Remove(file.path)
	}
}
