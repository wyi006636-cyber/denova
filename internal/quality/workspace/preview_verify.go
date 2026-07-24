package workspace

import "fmt"

// VerifyMigrationPreview re-reads the source tree and rejects any byte,
// topology, or canonical-containment change since BuildMigrationPreview.
func VerifyMigrationPreview(preview MigrationPreview) error {
	canonical, err := canonicalWorkspace(preview.Workspace)
	if err != nil {
		return err
	}
	if canonical != preview.Workspace {
		return &PreviewStaleError{Code: CodePreviewTreeChanged, Path: "", Expected: preview.Workspace, Actual: canonical, Message: "workspace canonical root changed"}
	}
	nodes, conflicts, err := scanWorkspaceNodes(canonical)
	if err != nil {
		return err
	}
	if len(conflicts) != 0 {
		conflict := conflicts[0]
		return &PreviewStaleError{Code: conflict.Code, Path: conflict.Path, Expected: "safe canonical source", Actual: conflict.Value, Message: conflict.Message}
	}
	actual := previewSnapshots(nodes)
	expectedByPath := make(map[string]previewSnapshot, len(preview.snapshot))
	actualByPath := make(map[string]previewSnapshot, len(actual))
	for _, snapshot := range preview.snapshot {
		expectedByPath[snapshot.Path] = snapshot
	}
	for _, snapshot := range actual {
		actualByPath[snapshot.Path] = snapshot
	}
	for _, expected := range preview.snapshot {
		observed, exists := actualByPath[expected.Path]
		if !exists {
			return &PreviewStaleError{Code: CodePreviewTreeChanged, Path: expected.Path, Expected: expected, Actual: "missing", Message: "preview source was removed"}
		}
		if observed != expected {
			code := CodePreviewTreeChanged
			if expected.NodeType == string(PreviewNodeFile) || expected.NodeType == string(PreviewNodeSymlink) {
				code = CodePreviewSourceChanged
			}
			return &PreviewStaleError{Code: code, Path: expected.Path, Expected: expected, Actual: observed, Message: "preview source identity changed"}
		}
	}
	for _, observed := range actual {
		if _, exists := expectedByPath[observed.Path]; !exists {
			return &PreviewStaleError{Code: CodePreviewTreeChanged, Path: observed.Path, Expected: "absent", Actual: fmt.Sprintf("%s:%d:%s", observed.NodeType, observed.Size, observed.SHA256), Message: "new source appeared after preview"}
		}
	}
	return nil
}
