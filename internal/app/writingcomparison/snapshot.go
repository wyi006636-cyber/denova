package writingcomparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"denova/config"
	agentcontext "denova/internal/agents/context"
	"denova/internal/agents/prompts"
	"denova/internal/book"
	"denova/internal/book/lore"
	"denova/internal/portablepath"
	projectdomain "denova/internal/project"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func freezeWritingSnapshot(ctx context.Context, cfg config.Config, layout projectdomain.Layout, task Task, store *Store) (*SnapshotRef, error) {
	state := book.NewState(layout.ContentRoot)
	contexts := prompts.IDEWorkspaceRuntimeContextsForContextWithLore(state, prompts.IDEContextRef{CurrentFile: task.Target.Path}, task.Target.ChapterCursor, lore.LoreScopeAsOf)
	budget := agentcontext.ContextBudgetForAgent(&cfg, config.AgentKindIDE)
	if budget.MaxTotalBytes <= 0 {
		budget = agentcontext.DefaultBudget()
	}
	fragments := []agentcontext.Fragment{
		{ID: "workspace_runtime_stable", Source: "workspace.runtime.stable", Title: contexts.StableTitle, Purpose: "provide stable writing workspace sources", Content: contexts.Stable, Placement: agentcontext.PlacementLeadingMessage, Included: strings.TrimSpace(contexts.Stable) != ""},
		{ID: "workspace_runtime_dynamic", Source: "workspace.runtime.dynamic", Title: contexts.DynamicTitle, Purpose: "provide current writing workspace sources", Content: contexts.Dynamic, Placement: agentcontext.PlacementFinalUserPrefix, Included: strings.TrimSpace(contexts.Dynamic) != ""},
		{ID: "workspace_lore_as_of", Source: "workspace.lore.as_of", Title: "Chapter-scoped Lore for This Turn", Purpose: "provide chapter-scoped lore", Content: contexts.Lore, Placement: agentcontext.PlacementFinalUserPrefix, Included: strings.TrimSpace(contexts.Lore) != ""},
	}
	assembled, err := agentcontext.NewAssembler(budget).Assemble(ctx, agentcontext.AssembleRequest{
		Messages: []*agentschema.Message{agentschema.UserMessage(task.ChapterTask.Text)}, Fragments: fragments,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble writing snapshot context: %w", err)
	}
	snapshotID := fmt.Sprintf("snapshot-%d", time.Now().UTC().UnixNano())
	snapshot := &SnapshotRef{ID: snapshotID, FrozenAt: time.Now().UTC(), TargetRevision: task.Target.BaseRevision, Budget: SnapshotBudget{Bytes: budget.MaxTotalBytes}}
	assetIndex := 0
	for _, fragment := range assembled.Fragments {
		if !fragment.Included || strings.TrimSpace(fragment.Content) == "" {
			continue
		}
		assetIndex++
		relative := fmt.Sprintf("snapshots/%s/%02d-%s.md", snapshotID, assetIndex, assetSlug(fragment.Source))
		assetPath, pathErr := store.AssetPath(task.ID, relative)
		if pathErr != nil {
			return nil, pathErr
		}
		if err := WriteImmutableAsset(assetPath, []byte(fragment.Content)); err != nil {
			return nil, err
		}
		snapshot.Fragments = append(snapshot.Fragments, SnapshotFragment{Source: fragment.Source, Title: fragment.Title, Revision: "sha256:" + AssetSHA256([]byte(fragment.Content)), SHA256: AssetSHA256([]byte(fragment.Content)), Bytes: len(fragment.Content), Truncated: fragment.Truncated, AssetPath: relative})
	}
	usedBytes := 0
	for _, fragment := range snapshot.Fragments {
		usedBytes += fragment.Bytes
	}
	for _, relative := range book.WritingContextPackPaths(state, task.Target.Path) {
		if err := portablepath.Validate(relative); err != nil {
			continue
		}
		path := filepath.Join(layout.ContentRoot, filepath.FromSlash(relative))
		data, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			snapshot.Omitted = append(snapshot.Omitted, OmittedRecord{Source: "workspace.file." + relative, Path: relative, Reason: "missing"})
			continue
		}
		if readErr != nil {
			snapshot.Omitted = append(snapshot.Omitted, OmittedRecord{Source: "workspace.file." + relative, Path: relative, Reason: "unreadable"})
			continue
		}
		originalHash := AssetSHA256(data)
		if usedBytes >= budget.MaxTotalBytes {
			snapshot.Omitted = append(snapshot.Omitted, OmittedRecord{Source: "workspace.file." + relative, Path: relative, Reason: "budget", Bytes: len(data)})
			continue
		}
		limit := budget.MaxFragmentBytes
		if limit <= 0 || limit > budget.MaxTotalBytes-usedBytes {
			limit = budget.MaxTotalBytes - usedBytes
		}
		if len(data) > budget.MaxTotalBytes-usedBytes {
			snapshot.Omitted = append(snapshot.Omitted, OmittedRecord{Source: "workspace.file." + relative, Path: relative, Reason: "budget", Bytes: len(data)})
			continue
		}
		truncated := false
		if len(data) > limit {
			data = []byte(strings.TrimSpace(string(data[:limit])))
			truncated = true
		}
		if len(data) == 0 {
			continue
		}
		assetIndex++
		assetRelative := fmt.Sprintf("snapshots/%s/%02d-%s.md", snapshotID, assetIndex, assetSlug(relative))
		assetPath, pathErr := store.AssetPath(task.ID, assetRelative)
		if pathErr != nil {
			return nil, pathErr
		}
		if err := WriteImmutableAsset(assetPath, data); err != nil {
			return nil, err
		}
		hash := AssetSHA256(data)
		snapshot.Fragments = append(snapshot.Fragments, SnapshotFragment{Source: "workspace.file." + relative, Title: relative, Path: relative, Revision: "sha256:" + originalHash, SHA256: hash, Bytes: len(data), Truncated: truncated, AssetPath: assetRelative})
		usedBytes += len(data)
	}
	snapshotBytes, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(snapshotBytes)
	snapshot.SHA256 = hex.EncodeToString(digest[:])
	return snapshot, nil
}

func assetSlug(value string) string {
	value = strings.Trim(filepath.ToSlash(value), "/")
	value = strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-").Replace(value)
	if value == "" {
		return "context"
	}
	if len(value) > 80 {
		value = value[:80]
	}
	return value
}
