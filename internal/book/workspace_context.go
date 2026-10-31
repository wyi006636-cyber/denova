package book

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"denova/internal/book/lore"
)

const (
	workspaceContextChapterGroupLimit = 2
	workspaceContextChapterLimit      = 12
)

// WorkspaceContext is the complete writing-workspace projection consumed by
// Agent context assembly. File-backed writing state appears only as
// workspace-relative locators. Stable may additionally contain enabled
// resident Lore bodies, while all other Lore bodies stay behind Lore tools.
type WorkspaceContext struct {
	Stable  string
	Dynamic string
}

// Markdown renders the projection for status and diagnostics without changing
// the stable/dynamic placement used by Agent context assembly.
func (c WorkspaceContext) Markdown() string {
	return strings.TrimSpace(strings.Join(nonEmptyContextValues(c.Stable, c.Dynamic), "\n\n"))
}

// WorkspaceContextAccess names how the model reaches the files and Lore listed
// in a workspace projection. It changes only the access instructions; indexed
// paths, Lore groups and inlined resident bodies stay identical.
type WorkspaceContextAccess int

const (
	// WorkspaceContextToolAccess is the ordinary writing Agent contract: file
	// bodies are omitted and loaded on demand with file and Lore tools.
	WorkspaceContextToolAccess WorkspaceContextAccess = iota
	// WorkspaceContextReadOnlySnapshot is the comparison candidate contract:
	// the model has no tools, and the caller inlines selected file bodies in
	// the same frozen snapshot under their paths.
	WorkspaceContextReadOnlySnapshot
)

// workspaceAccessText holds every model-visible sentence that depends on the
// access contract, so one exhaustive switch decides the whole variant.
type workspaceAccessText struct {
	indexSourceNote string
	loreUnavailable string
	loreContext     func(*lore.Store) (string, error)
}

func workspaceAccessTextFor(access WorkspaceContextAccess) workspaceAccessText {
	switch access {
	case WorkspaceContextToolAccess:
		return workspaceAccessText{
			indexSourceNote: "Source: current book workspace snapshot. Entries are workspace-relative discovery paths only; file contents are intentionally omitted. Use `read` for known files, `glob` to discover additional paths, and `grep` for targeted searches. Read only the sources required by the current request and writing workflow. A path's presence does not prove that its file is non-empty or finalized.",
			loreUnavailable: "## Lore Index unavailable\n\nDo not assume setting coverage. Read needed canon with Lore tools before proceeding.",
			loreContext:     (*lore.Store).ProgressiveContextMarkdown,
		}
	case WorkspaceContextReadOnlySnapshot:
		return workspaceAccessText{
			indexSourceNote: "Source: frozen book workspace snapshot for a read-only draft. No file or Lore tools are available. Listed files whose contents fit the snapshot budget are inlined later in this snapshot under sections titled with their path; a listed file without such a section was omitted, so treat its content as unknown. A path's presence does not prove that its file is non-empty or finalized.",
			loreUnavailable: "## Lore Index unavailable\n\nThe Lore Index could not be loaded for this snapshot. Do not assume setting coverage beyond the inlined sources.",
			loreContext:     (*lore.Store).ReadOnlyContextMarkdown,
		}
	default:
		panic(fmt.Sprintf("unknown workspace context access %d", access))
	}
}

// WorkspaceContext returns a compact source index. Content changes do not
// rotate the index, which keeps the model prefix cacheable while every file
// read observes the authoritative workspace state at tool-execution time.
func (s *State) WorkspaceContext() WorkspaceContext {
	return s.WorkspaceContextFor(WorkspaceContextToolAccess)
}

// WorkspaceContextFor returns the source index described for one access
// contract. Callers that give the model file tools must use
// WorkspaceContextToolAccess; read-only callers must inline the bodies they
// promise in the same model input.
func (s *State) WorkspaceContextFor(access WorkspaceContextAccess) WorkspaceContext {
	if s == nil {
		return WorkspaceContext{}
	}
	text := workspaceAccessTextFor(access)

	stableFiles := make([]workspaceContextPath, 0, 2)
	if s.hasMeaningfulIdeas() {
		stableFiles = append(stableFiles, workspaceContextPath{Purpose: "Creative ideas and current direction", Path: IdeasFileName})
	}
	if s.hasNonEmptyFile(filepath.Join("setting", "outline.md")) {
		stableFiles = append(stableFiles, workspaceContextPath{Purpose: "Long-term outline", Path: "setting/outline.md"})
	}

	dynamicFiles := make([]workspaceContextPath, 0, 3)
	if s.hasNonEmptyFile(filepath.Join("setting", "progress.md")) {
		dynamicFiles = append(dynamicFiles, workspaceContextPath{Purpose: "Established writing progress", Path: "setting/progress.md"})
	}
	if s.hasNonEmptyFile(filepath.Join("setting", CharacterStatesFileName)) {
		dynamicFiles = append(dynamicFiles, workspaceContextPath{Purpose: "Current character continuity state", Path: "setting/" + CharacterStatesFileName})
	}
	if s.hasNonEmptyFile(filepath.Join("setting", "story-ledger.md")) {
		dynamicFiles = append(dynamicFiles, workspaceContextPath{Purpose: "Story threads and foreshadowing ledger", Path: "setting/story-ledger.md"})
	}
	if s.hasNonEmptyFile(filepath.Join("setting", "case-index.md")) {
		dynamicFiles = append(dynamicFiles, workspaceContextPath{Purpose: "Writing method case index", Path: "setting/case-index.md"})
	}

	stable := renderWorkspacePathIndex("Workspace Source Index", text.indexSourceNote, []workspaceContextPathSection{
		{Title: "Canonical writing files", Paths: stableFiles},
	})
	if loreContext := s.progressiveLoreContext(text); loreContext != "" {
		stable = strings.TrimSpace(strings.Join(nonEmptyContextValues(stable, loreContext), "\n\n"))
	}

	dynamic := renderWorkspacePathIndex("Current Writing Source Index", text.indexSourceNote, []workspaceContextPathSection{
		{Title: "Current writing state", Paths: dynamicFiles},
		{Title: "Recent chapter-group plans", Paths: contextPaths("Chapter-group plan", s.recentChapterGroupPaths(workspaceContextChapterGroupLimit))},
		{Title: "Recent chapter paths", Paths: contextPaths("Chapter path", s.recentChapterPaths(workspaceContextChapterLimit))},
	})

	return WorkspaceContext{Stable: stable, Dynamic: dynamic}
}

type workspaceContextPath struct {
	Purpose string
	Path    string
}

type workspaceContextPathSection struct {
	Title string
	Paths []workspaceContextPath
}

func renderWorkspacePathIndex(title, sourceNote string, sections []workspaceContextPathSection) string {
	entryCount := 0
	for _, section := range sections {
		entryCount += len(section.Paths)
	}
	if entryCount == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s\n\n", title)
	sb.WriteString(sourceNote)
	sb.WriteString("\n")
	for _, section := range sections {
		if len(section.Paths) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\n### %s\n\n", section.Title)
		for _, source := range section.Paths {
			fmt.Fprintf(&sb, "- %s: %s\n", source.Purpose, strconv.Quote(filepath.ToSlash(source.Path)))
		}
	}
	return strings.TrimSpace(sb.String())
}

func contextPaths(purpose string, paths []string) []workspaceContextPath {
	result := make([]workspaceContextPath, 0, len(paths))
	for _, path := range paths {
		result = append(result, workspaceContextPath{Purpose: purpose, Path: path})
	}
	return result
}

func (s *State) hasMeaningfulIdeas() bool {
	info, err := os.Stat(s.IdeasPath())
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return false
	}
	template := []byte(IdeasTemplate)
	if info.Size() != int64(len(template)) {
		return true
	}
	data, err := os.ReadFile(s.IdeasPath())
	if err != nil {
		return false
	}
	content := bytes.TrimSpace(data)
	return len(content) > 0 && !bytes.Equal(content, bytes.TrimSpace(template))
}

func (s *State) hasNonEmptyFile(relativePath string) bool {
	info, err := os.Stat(filepath.Join(s.workspace, filepath.FromSlash(relativePath)))
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func (s *State) recentChapterGroupPaths(limit int) []string {
	entries, err := os.ReadDir(s.ChapterGroupDir())
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".md" && ext != ".txt" {
			continue
		}
		paths = append(paths, filepath.ToSlash(filepath.Join("setting", "chapter-groups", name)))
	}
	sort.Strings(paths)
	return latestContextPaths(paths, limit)
}

func (s *State) recentChapterPaths(limit int) []string {
	entries := s.chapterPaths()
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	return latestContextPaths(paths, limit)
}

func latestContextPaths(paths []string, limit int) []string {
	if limit <= 0 || len(paths) <= limit {
		return paths
	}
	return paths[len(paths)-limit:]
}

func (s *State) progressiveLoreContext(text workspaceAccessText) string {
	context, err := text.loreContext(lore.NewStore(s.workspace))
	if err != nil {
		slog.Error("[lore] workspace index context unavailable", "error", err)
		return text.loreUnavailable
	}
	return strings.TrimSpace(context)
}

func nonEmptyContextValues(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
