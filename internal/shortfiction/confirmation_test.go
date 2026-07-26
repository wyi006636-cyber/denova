package shortfiction

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfirmationResultJSONReportsExactWrittenCheckpoint(t *testing.T) {
	result := ConfirmationResult{
		Status:           ConfirmationWritten,
		CandidateID:      "sha256:candidate",
		WriteRevision:    "sha256:written",
		ChangeGroupID:    "group-1",
		ChangeSetID:      "change-1",
		WorkspaceMutated: true,
		CheckpointStatus: CheckpointCreated,
		Checkpoint: &ConfirmationCheckpoint{
			VersionID: "version-1",
			Source:    "manual",
			Path:      "chapters/short.md",
			Revision:  "sha256:written",
		},
		Retryable: false,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"status":"written","candidate_id":"sha256:candidate","write_revision":"sha256:written","change_group_id":"group-1","change_set_id":"change-1","workspace_mutated":true,"checkpoint_status":"created","checkpoint":{"version_id":"version-1","source":"manual","path":"chapters/short.md","revision":"sha256:written"},"retryable":false}`
	if string(data) != want {
		t.Fatalf("JSON = %s, want %s", data, want)
	}
}

func TestConfirmationResultJSONReportsTruthfulCheckpointFailureWithoutRetryClaim(t *testing.T) {
	result := ConfirmationResult{
		Status:           ConfirmationWrittenCheckpointFailed,
		CandidateID:      "sha256:candidate",
		WriteRevision:    "sha256:written",
		ChangeGroupID:    "group-1",
		ChangeSetID:      "change-1",
		WorkspaceMutated: true,
		CheckpointStatus: CheckpointFailed,
		Retryable:        false,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"status":"written_checkpoint_failed","candidate_id":"sha256:candidate","write_revision":"sha256:written","change_group_id":"group-1","change_set_id":"change-1","workspace_mutated":true,"checkpoint_status":"failed","retryable":false}`
	if string(data) != want {
		t.Fatalf("JSON = %s, want %s", data, want)
	}
	for _, forbidden := range []string{"retry_token", "receipt", "idempot", "rollback"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("JSON contains forbidden claim %q: %s", forbidden, data)
		}
	}
}
