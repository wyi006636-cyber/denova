// Package projection implements the disposable SQLite/FTS5 read model derived
// exclusively from bounded workspace source snapshots.
package projection

import (
	"errors"
	"fmt"

	qualityworkspace "denova/internal/quality/workspace"
)

const (
	SchemaVersionV1 = 1

	DatabaseRelativePath         = ".denova/index.db"
	BuildIdentityV1              = "denova-projection-v1"
	DriverModule                 = "modernc.org/sqlite"
	DriverVersion                = "v1.54.0"
	LibcModule                   = "modernc.org/libc"
	LibcVersion                  = "v1.74.1"
	WorkspaceFeatureRangeV1      = ">=1.0.0 <2.0.0"
	WorkspaceFeatureProjectionV1 = "fts_projection"
	WorkspaceFeatureQualityV1    = "quality_harness"

	sqliteDriverName      = "sqlite"
	projectionStagePrefix = "index.db-rebuild-"
	projectionStageSuffix = ".tmp"
)

// WorkspaceSchemaInspectorOptions binds Projection mutation to the actual
// running Denova version and the exact v1 feature contracts understood at this
// boundary. It declares compatibility knowledge only; it does not start or
// integrate the Harness product surface.
func WorkspaceSchemaInspectorOptions(applicationVersion string) qualityworkspace.InspectorOptions {
	return qualityworkspace.InspectorOptions{
		ApplicationVersion: applicationVersion,
		SupportedFeatures: map[string]string{
			WorkspaceFeatureProjectionV1: WorkspaceFeatureRangeV1,
			WorkspaceFeatureQualityV1:    WorkspaceFeatureRangeV1,
		},
	}
}

var (
	// ErrSourceChanged means authoritative bytes changed between the initial
	// snapshot and activation compare-and-swap.
	ErrSourceChanged = errors.New("Projection source snapshot changed")
	// ErrLegacyActive means creating canonical .denova Projection state would
	// disturb a legacy-only workspace before its explicit P1-T02 migration.
	ErrLegacyActive = errors.New("Projection requires the canonical .denova workspace root")
)

// Options configures a standalone Projection service without API/UI wiring.
type Options struct {
	Workspace          string
	SourceOptions      qualityworkspace.ProjectionSourceOptions
	WorkspaceInspector qualityworkspace.InspectorOptions
	BuildIdentity      string
	Hooks              Hooks
}

// State describes whether a Projection can safely serve the current source
// snapshot. A stale Projection remains disposable but is never opened for
// queries as if it represented current author bytes.
type State string

const (
	StateAvailable   State = "available"
	StateUnavailable State = "unavailable"
	StateStale       State = "stale"
)

// Reason gives callers a stable, non-authoritative recovery classification.
type Reason string

const (
	ReasonNone                  Reason = "none"
	ReasonMissing               Reason = "missing"
	ReasonLocked                Reason = "locked"
	ReasonOpenFailed            Reason = "open_failed"
	ReasonCorrupt               Reason = "corrupt"
	ReasonSchemaNewer           Reason = "schema_newer"
	ReasonIdentityMismatch      Reason = "identity_mismatch"
	ReasonIntegrityFailed       Reason = "integrity_failed"
	ReasonSourceChanged         Reason = "source_changed"
	ReasonWorkspaceIncompatible Reason = "workspace_incompatible"
)

// Status is the bounded result of validating a disposable Projection against
// both its own persisted identity and a fresh authoritative source snapshot.
type Status struct {
	State                  State
	Reason                 Reason
	DatabasePath           string
	SchemaVersion          int
	BuildIdentity          string
	DriverModule           string
	DriverVersion          string
	LibcModule             string
	LibcVersion            string
	SQLiteVersion          string
	ProjectionSnapshotHash string
	SourceSnapshotHash     string
	DocumentCount          int
	Detail                 string
}

// UnavailableError reports a typed status without making Projection failure a
// workspace-open failure or elevating SQLite bytes to source authority.
type UnavailableError struct {
	Status Status
	Err    error
}

func (err *UnavailableError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Err != nil {
		return fmt.Sprintf("Projection %s reason=%s: %v", err.Status.State, err.Status.Reason, err.Err)
	}
	return fmt.Sprintf("Projection %s reason=%s", err.Status.State, err.Status.Reason)
}

func (err *UnavailableError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// RebuildResult records the exact activation boundary reached by one rebuild.
type RebuildResult struct {
	DatabasePath       string
	SourceSnapshotHash string
	DocumentCount      int
	SQLiteVersion      string
	Fresh              bool
	Activated          bool
	ParentSynced       bool
	QuarantinePaths    []string
}

// RebuildError distinguishes failures before and after a complete database
// became visible. It never treats a Projection as source authority.
type RebuildError struct {
	Stage     FaultPoint
	Activated bool
	Err       error
}

func (err *RebuildError) Error() string {
	if err == nil {
		return "<nil>"
	}
	return fmt.Sprintf("Projection rebuild stage=%s activated=%t: %v", err.Stage, err.Activated, err.Err)
}

func (err *RebuildError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

type buildRequest struct {
	Path            string
	Snapshot        qualityworkspace.ProjectionSourceSnapshot
	BuildIdentity   string
	FreshActivation bool
	Hooks           Hooks
}

type buildEvidence struct {
	SQLiteVersion  string
	CompileOptions []string
}
