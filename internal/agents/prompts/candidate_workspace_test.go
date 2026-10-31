package prompts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"denova/internal/book"
	"denova/internal/book/lore"
)

func writingContextFixture(t *testing.T) *book.State {
	t.Helper()
	dir := t.TempDir()
	state := book.NewState(dir)
	if err := state.InitWorkspace(); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		state.IdeasPath(): "A revenge story on a flooded planet.",
		filepath.Join(state.SettingDir(), "outline.md"):      "outline",
		filepath.Join(state.SettingDir(), "progress.md"):     "progress",
		filepath.Join(dir, "chapters", "ch00001-opening.md"): "prose",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := lore.NewStore(dir)
	if _, err := store.Create(lore.ItemInput{ID: "smithy", Type: "location", Name: "Smithy", Importance: "major", LoadMode: lore.LoadModeResident, Content: "The smith keeps every tool polished.\nThe forge opens at dawn."}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(lore.ItemInput{ID: "archive", Type: "location", Name: "Hidden Archive", LoadMode: lore.LoadModeManual, BriefDescription: "Sealed records", Content: "ON_DEMAND_ARCHIVE_BODY"}); err != nil {
		t.Fatal(err)
	}
	return state
}

// candidateForbiddenInstruction matches every tool or interactive onboarding
// instruction that a read-only, tool-less candidate must never receive.
var candidateForbiddenInstruction = regexp.MustCompile("`read`|`glob`|`grep`|query_lore_items|(?s:confirm.*with the author)|update CREATOR\\.md|generate setting/outline\\.md|use a tool|with Lore tools")

// The ordinary writing Agent output was captured from the implementation
// before the read-only candidate variant existed; it must stay byte-identical.
func TestOrdinaryWritingContextTextIsUnchanged(t *testing.T) {
	state := writingContextFixture(t)
	got := IDEWorkspaceRuntimeContextsForContextWithLore(state, IDEContextRef{CurrentFile: "chapters/ch00002.md"}, "ch00002", lore.LoreScopeAsOf)
	wantStable := "## Workspace Source Index\n\nSource: current book workspace snapshot. Entries are workspace-relative discovery paths only; file contents are intentionally omitted. Use `read` for known files, `glob` to discover additional paths, and `grep` for targeted searches. Read only the sources required by the current request and writing workflow. A path's presence does not prove that its file is non-empty or finalized.\n\n### Canonical writing files\n\n- Creative ideas and current direction: \"ideas.md\"\n- Long-term outline: \"setting/outline.md\"\n\n# Lore Index\n\nEntry metadata is category / importance / detail. Importance indicates narrative prominence, not authority or required use. name and brief omit the body; full includes the complete body. Read missing bodies with `query_lore_items` using names and detail=full. Search with `query_lore_items` keywords; `group_names` filters by heading. Explore existing material before inventing new entities.\n\n## Resident\n\n### Smithy (Location / major / full)\n\nThe smith keeps every tool polished.\nThe forge opens at dawn.\n\n## Location · Manual\n\n- Hidden Archive (Location / important / name)"
	wantDynamic := "## Current Writing Source Index\n\nSource: current book workspace snapshot. Entries are workspace-relative discovery paths only; file contents are intentionally omitted. Use `read` for known files, `glob` to discover additional paths, and `grep` for targeted searches. Read only the sources required by the current request and writing workflow. A path's presence does not prove that its file is non-empty or finalized.\n\n### Current writing state\n\n- Established writing progress: \"setting/progress.md\"\n\n### Recent chapter paths\n\n- Chapter path: \"chapters/ch00001-opening.md\"\n\n## Current IDE State (provided by the frontend request; paths only; at most 20 open files)\n\nSource: frontend IDE request state. This contains only paths for open and focused files, never file contents.\n- Focused file: chapters/ch00002.md\n- Constraint: use a tool to read any needed file explicitly by path. Do not assume this context contains current file contents."
	if got.Stable != wantStable {
		t.Fatalf("ordinary stable context changed:\n%q", got.Stable)
	}
	if got.Dynamic != wantDynamic {
		t.Fatalf("ordinary dynamic context changed:\n%q", got.Dynamic)
	}
	if got.Lore != "" {
		t.Fatalf("ordinary lore context = %q", got.Lore)
	}
	if existing := state.WorkspaceContext(); got.Stable != strings.TrimSpace(existing.Stable) {
		t.Fatalf("ordinary context diverges from State.WorkspaceContext")
	}

	empty := book.NewState(t.TempDir())
	if err := empty.InitWorkspace(); err != nil {
		t.Fatal(err)
	}
	if got := IDEWorkspaceRuntimeContextsForState(empty); got.Stable != EmptyIDEStateHint() || !strings.Contains(got.Stable, "with the author") {
		t.Fatalf("ordinary new-work onboarding changed: %q", got.Stable)
	}
}

func TestCandidateWorkspaceContextIsReadOnlyAndKeepsLoreBodies(t *testing.T) {
	state := writingContextFixture(t)
	got := CandidateWorkspaceRuntimeContexts(state, "chapters/ch00002.md", "ch00002", lore.LoreScopeAsOf)
	all := strings.Join([]string{got.Stable, got.Dynamic, got.Lore}, "\n")
	if match := candidateForbiddenInstruction.FindString(all); match != "" {
		t.Fatalf("candidate context contains tool instruction %q:\n%s", match, all)
	}
	for _, required := range []string{
		"The smith keeps every tool polished.\nThe forge opens at dawn.",
		`"ideas.md"`,
		`"setting/outline.md"`,
		`"setting/progress.md"`,
		"Hidden Archive",
		"No file or Lore tools are available.",
		"cannot be loaded",
		"## Candidate Target",
		"chapters/ch00002.md",
	} {
		if !strings.Contains(all, required) {
			t.Fatalf("candidate context missing %q:\n%s", required, all)
		}
	}
	if strings.Contains(all, "ON_DEMAND_ARCHIVE_BODY") {
		t.Fatalf("candidate context inlined an on-demand Lore body:\n%s", all)
	}
}

func TestCandidateWorkspaceContextForNewWorkIsReadOnly(t *testing.T) {
	state := book.NewState(t.TempDir())
	if err := state.InitWorkspace(); err != nil {
		t.Fatal(err)
	}
	got := CandidateWorkspaceRuntimeContexts(state, "chapters/ch00001.md", "ch00001", lore.LoreScopeAsOf)
	if got.Stable != candidateEmptyWorkspaceHint {
		t.Fatalf("candidate new-work context = %q", got.Stable)
	}
	all := strings.Join([]string{got.Stable, got.Dynamic, got.Lore}, "\n")
	if match := candidateForbiddenInstruction.FindString(all); match != "" {
		t.Fatalf("candidate new-work context contains interactive instruction %q:\n%s", match, all)
	}
}
