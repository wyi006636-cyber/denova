package workspace

import (
	"reflect"
	"testing"
)

func TestWorkspaceSchemaV1MigrationStatesAreExhaustive(t *testing.T) {
	want := []MigrationState{
		MigrationNotRequired,
		MigrationPreviewed,
		MigrationValidated,
		MigrationBackedUp,
		MigrationStaged,
		MigrationSwitchPending,
		MigrationSwitched,
		MigrationVerifying,
		MigrationCompleted,
		MigrationRollbackPending,
		MigrationRolledBack,
		MigrationNeedsRecovery,
	}
	if got := AllMigrationStates(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllMigrationStates() = %#v, want %#v", got, want)
	}
}
