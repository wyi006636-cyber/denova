package workspace

import (
	"fmt"
	"sort"
	"strings"
)

func verifyMigrationSources(workspace, migrationID string, expected []SourceExpectation, state MigrationState, step MigrationStep) error {
	return verifyMigrationSourcesExcluding(workspace, migrationID, expected, state, step, nil)
}

func verifyMigrationSourcesExcluding(workspace, migrationID string, expected []SourceExpectation, state MigrationState, step MigrationStep, owned []string) error {
	nodes, conflicts, err := scanWorkspaceNodes(workspace)
	if err != nil {
		return &MigrationError{Code: CodeMigrationSourceChanged, MigrationID: migrationID, State: state, Step: step, Durability: DurabilityPending, Recovery: RecoveryRequired, NextAction: MigrationNextResume, Message: "source tree cannot be re-read", Err: err}
	}
	ownPrefix := MigrationRootRelativePath + "/" + migrationID
	expectedByPath := make(map[string]SourceExpectation, len(expected))
	for _, item := range expected {
		expectedByPath[item.Path] = item
	}
	actual := make([]SourceExpectation, 0, len(nodes))
	for _, node := range nodes {
		if node.Path == ownPrefix || strings.HasPrefix(node.Path, ownPrefix+"/") {
			continue
		}
		if isOwnedPublishedPath(node.Path, owned) {
			continue
		}
		if node.Path == MigrationRootRelativePath {
			if _, existedAtAuthorization := expectedByPath[node.Path]; !existedAtAuthorization {
				continue
			}
		}
		actual = append(actual, SourceExpectation{Path: node.Path, NodeType: node.NodeType, Mode: node.Mode, Identity: node.Identity, Size: node.Size, SHA256: node.SHA256})
	}
	for _, conflict := range conflicts {
		if conflict.Path == ownPrefix || strings.HasPrefix(conflict.Path, ownPrefix+"/") {
			continue
		}
		if isOwnedPublishedPath(conflict.Path, owned) {
			continue
		}
		return &MigrationError{Code: CodeMigrationSourceChanged, MigrationID: migrationID, State: state, Step: step, Path: conflict.Path, Durability: DurabilityPending, Recovery: RecoveryRequired, NextAction: MigrationNextResume, Message: conflict.Message}
	}
	sort.Slice(actual, func(i, j int) bool { return actual[i].Path < actual[j].Path })
	actualByPath := make(map[string]SourceExpectation, len(actual))
	for _, item := range actual {
		actualByPath[item.Path] = item
	}
	for _, want := range expected {
		if isOwnedPublishedPath(want.Path, owned) {
			continue
		}
		got, exists := actualByPath[want.Path]
		if !exists {
			return sourceDifferenceError(migrationID, state, step, want.Path, want.SHA256, "missing", "authorized source disappeared")
		}
		if got != want {
			actualHash := got.SHA256
			if actualHash == "" {
				actualHash = fmt.Sprintf("%s:%d", got.NodeType, got.Size)
			}
			expectedHash := want.SHA256
			if expectedHash == "" {
				expectedHash = fmt.Sprintf("%s:%d", want.NodeType, want.Size)
			}
			return sourceDifferenceError(migrationID, state, step, want.Path, expectedHash, actualHash, "authorized source identity or bytes changed")
		}
	}
	for _, got := range actual {
		if isOwnedPublishedPath(got.Path, owned) {
			continue
		}
		if _, exists := expectedByPath[got.Path]; !exists {
			actualHash := got.SHA256
			if actualHash == "" {
				actualHash = fmt.Sprintf("%s:%d", got.NodeType, got.Size)
			}
			return sourceDifferenceError(migrationID, state, step, got.Path, "absent", actualHash, "new source appeared after author confirmation")
		}
	}
	return nil
}

func sourceDifferenceError(migrationID string, state MigrationState, step MigrationStep, path, expected, actual, message string) *MigrationError {
	return &MigrationError{Code: CodeMigrationSourceChanged, MigrationID: migrationID, State: state, Step: step, Path: path, ExpectedSHA256: expected, ActualSHA256: actual, WorkspaceMutated: state != "" && state != MigrationPreviewed, Durability: DurabilityPending, Recovery: RecoveryRequired, NextAction: MigrationNextResume, Message: message}
}
