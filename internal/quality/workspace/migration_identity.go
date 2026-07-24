package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type previewDigestPayload struct {
	Workspace            string              `json:"workspace"`
	WorkspaceKind        WorkspaceKind       `json:"workspace_kind"`
	SourceRoot           string              `json:"source_root"`
	TargetRoot           string              `json:"target_root"`
	CurrentSchemaVersion int                 `json:"current_schema_version"`
	TargetSchemaVersion  int                 `json:"target_schema_version"`
	Features             []PreviewFeature    `json:"features"`
	Entries              []PreviewEntry      `json:"entries"`
	Operations           []PreviewOperation  `json:"operations"`
	Conflicts            []PreviewConflict   `json:"conflicts"`
	Compatibility        inspectionDigest    `json:"compatibility"`
	Sources              []SourceExpectation `json:"sources"`
}

type inspectionDigest struct {
	Workspace               string                `json:"workspace"`
	ActiveRoot              string                `json:"active_root"`
	RootResolution          any                   `json:"root_resolution"`
	MarkerPresent           bool                  `json:"marker_present"`
	Marker                  Marker                `json:"marker"`
	MarkerSHA256            string                `json:"marker_sha256,omitempty"`
	Mode                    CompatibilityMode     `json:"mode"`
	ManagedMutation         ManagedMutationAccess `json:"managed_mutation"`
	Issues                  []CompatibilityIssue  `json:"issues"`
	UnknownOptionalFeatures []string              `json:"unknown_optional_features"`
	LegacyConflicts         []string              `json:"legacy_conflicts"`
}

type authorizationDigestPayload struct {
	Version             int                 `json:"version"`
	MigrationID         string              `json:"migration_id"`
	CanonicalWorkspace  string              `json:"canonical_workspace"`
	CanonicalSourceRoot string              `json:"canonical_source_root"`
	TargetIdentityPath  string              `json:"target_identity_path"`
	WorkspaceIdentity   FilesystemIdentity  `json:"workspace_identity"`
	SourceRootIdentity  FilesystemIdentity  `json:"source_root_identity"`
	TargetIdentity      FilesystemIdentity  `json:"target_identity"`
	PreviewSHA256       string              `json:"preview_sha256"`
	Sources             []SourceExpectation `json:"sources"`
	TargetSchemaVersion int                 `json:"target_schema_version"`
	TargetFeatures      []PreviewFeature    `json:"target_features"`
	Confirmation        AuthorConfirmation  `json:"confirmation"`
}

type recoveryAuthorizationDigestPayload struct {
	Version                      int                     `json:"version"`
	Action                       MigrationRecoveryAction `json:"action"`
	MigrationID                  string                  `json:"migration_id"`
	MigrationAuthorizationSHA256 string                  `json:"migration_authorization_sha256"`
	Confirmation                 AuthorConfirmation      `json:"confirmation"`
}

// ValidateMigrationID rejects any identifier that cannot be represented as one
// unchanged, portable, Windows-safe path segment.
func ValidateMigrationID(migrationID string) error {
	if migrationID == "." || migrationID == ".." || strings.ContainsAny(migrationID, `/\`) {
		return migrationIDError(migrationID, "migration ID must be one safe path segment", nil)
	}
	_, err := ValidateRelativePath(migrationID, PathOptions{
		Intent:   PathIntentNew,
		Platform: PathPlatformWindows,
		Limits:   PathLimits{MaxPathBytes: MaxMigrationIDBytes, MaxSegmentBytes: MaxMigrationIDBytes},
	})
	if err != nil {
		return migrationIDError(migrationID, "migration ID is not a portable path segment", err)
	}
	return nil
}

func migrationIDError(migrationID, message string, err error) *MigrationError {
	return &MigrationError{
		Code:        CodeMigrationIDInvalid,
		MigrationID: migrationID,
		Step:        MigrationStepAuthorize,
		Durability:  DurabilityNotStarted,
		Recovery:    RecoveryNotRequired,
		NextAction:  MigrationNextNone,
		Message:     message,
		Err:         err,
	}
}

// PreviewDigest computes the complete canonical seal used by author
// authorization. It includes the private source snapshot and marker raw hash.
func PreviewDigest(preview MigrationPreview) (string, error) {
	sources := sourceExpectations(preview)
	markerRaw := preview.Compatibility.Marker.RawBytes()
	markerHash := ""
	if len(markerRaw) != 0 {
		markerHash = sha256Hex(markerRaw)
	}
	payload := previewDigestPayload{
		Workspace:            preview.Workspace,
		WorkspaceKind:        preview.Kind,
		SourceRoot:           preview.SourceRoot,
		TargetRoot:           preview.TargetRoot,
		CurrentSchemaVersion: preview.CurrentSchemaVersion,
		TargetSchemaVersion:  preview.TargetSchemaVersion,
		Features:             append([]PreviewFeature(nil), preview.Features...),
		Entries:              append([]PreviewEntry(nil), preview.Entries...),
		Operations:           append([]PreviewOperation(nil), preview.Operations...),
		Conflicts:            append([]PreviewConflict(nil), preview.Conflicts...),
		Compatibility: inspectionDigest{
			Workspace:               preview.Compatibility.Workspace,
			ActiveRoot:              preview.Compatibility.ActiveRoot,
			RootResolution:          preview.Compatibility.RootResolution,
			MarkerPresent:           preview.Compatibility.Marker.Present,
			Marker:                  cloneMarker(preview.Compatibility.Marker.Contract),
			MarkerSHA256:            markerHash,
			Mode:                    preview.Compatibility.Mode,
			ManagedMutation:         preview.Compatibility.ManagedMutation,
			Issues:                  append([]CompatibilityIssue(nil), preview.Compatibility.Issues...),
			UnknownOptionalFeatures: append([]string(nil), preview.Compatibility.UnknownOptionalFeatures...),
			LegacyConflicts:         append([]string(nil), preview.Compatibility.LegacyConflicts...),
		},
		Sources: sources,
	}
	return canonicalSHA256(payload)
}

// BuildMigrationAuthorization creates a complete author-bound request. The
// executor recomputes every field under the writer lease before its first write.
func BuildMigrationAuthorization(preview MigrationPreview, migrationID string, confirmation AuthorConfirmation) (MigrationAuthorization, error) {
	if err := ValidateMigrationID(migrationID); err != nil {
		return MigrationAuthorization{}, err
	}
	if strings.TrimSpace(confirmation.ID) == "" || strings.TrimSpace(confirmation.Evidence) == "" {
		return MigrationAuthorization{}, &MigrationError{
			Code:        CodeMigrationAuthorizationRequired,
			MigrationID: migrationID,
			Step:        MigrationStepAuthorize,
			Durability:  DurabilityNotStarted,
			Recovery:    RecoveryNotRequired,
			NextAction:  MigrationNextNone,
			Message:     "author confirmation ID and evidence are required",
		}
	}
	canonical, err := canonicalWorkspace(preview.Workspace)
	if err != nil {
		return MigrationAuthorization{}, &MigrationError{
			Code:        CodeMigrationAuthorizationMismatch,
			MigrationID: migrationID,
			Step:        MigrationStepAuthorize,
			Path:        preview.Workspace,
			Durability:  DurabilityNotStarted,
			Recovery:    RecoveryNotRequired,
			NextAction:  MigrationNextNone,
			Message:     "preview workspace identity cannot be canonicalized",
			Err:         err,
		}
	}
	if canonical != preview.Workspace {
		return MigrationAuthorization{}, &MigrationError{
			Code:        CodeMigrationAuthorizationMismatch,
			MigrationID: migrationID,
			Step:        MigrationStepAuthorize,
			Path:        preview.Workspace,
			Durability:  DurabilityNotStarted,
			Recovery:    RecoveryNotRequired,
			NextAction:  MigrationNextNone,
			Message:     "preview workspace is not the canonical workspace identity",
		}
	}
	previewHash, err := PreviewDigest(preview)
	if err != nil {
		return MigrationAuthorization{}, &MigrationError{
			Code:        CodeMigrationAuthorizationMismatch,
			MigrationID: migrationID,
			Step:        MigrationStepAuthorize,
			Durability:  DurabilityNotStarted,
			Recovery:    RecoveryNotRequired,
			NextAction:  MigrationNextNone,
			Message:     "preview cannot be canonically hashed",
			Err:         err,
		}
	}
	canonicalSourceRoot, targetIdentityPath, workspaceIdentity, sourceIdentity, targetIdentity, err := previewFilesystemBinding(preview)
	if err != nil {
		return MigrationAuthorization{}, &MigrationError{Code: CodeMigrationAuthorizationMismatch, MigrationID: migrationID, Step: MigrationStepAuthorize, Path: preview.Workspace, Durability: DurabilityNotStarted, Recovery: RecoveryNotRequired, NextAction: MigrationNextNone, Message: "preview filesystem identity cannot be pinned", Err: err}
	}
	authorization := MigrationAuthorization{
		Version:             MigrationAuthorizationVersionV1,
		MigrationID:         migrationID,
		CanonicalWorkspace:  canonical,
		CanonicalSourceRoot: canonicalSourceRoot,
		TargetIdentityPath:  targetIdentityPath,
		WorkspaceIdentity:   workspaceIdentity,
		SourceRootIdentity:  sourceIdentity,
		TargetIdentity:      targetIdentity,
		PreviewSHA256:       previewHash,
		Sources:             sourceExpectations(preview),
		TargetSchemaVersion: preview.TargetSchemaVersion,
		TargetFeatures:      append([]PreviewFeature(nil), preview.Features...),
		Confirmation:        confirmation,
	}
	authorization.PayloadSHA256, err = authorizationPayloadDigest(authorization)
	if err != nil {
		return MigrationAuthorization{}, &MigrationError{
			Code:        CodeMigrationAuthorizationMismatch,
			MigrationID: migrationID,
			Step:        MigrationStepAuthorize,
			Durability:  DurabilityNotStarted,
			Recovery:    RecoveryNotRequired,
			NextAction:  MigrationNextNone,
			Message:     "authorization payload cannot be canonically hashed",
			Err:         err,
		}
	}
	return authorization, nil
}

func validateMigrationAuthorization(preview MigrationPreview, authorization MigrationAuthorization) error {
	if authorization.Version != MigrationAuthorizationVersionV1 {
		return authorizationMismatch(authorization.MigrationID, "authorization.version", fmt.Sprint(MigrationAuthorizationVersionV1), fmt.Sprint(authorization.Version))
	}
	previewHash, err := PreviewDigest(preview)
	if err != nil {
		return authorizationMismatch(authorization.MigrationID, "authorization.preview_sha256", authorization.PreviewSHA256, "unhashable")
	}
	wantSources := sourceExpectations(preview)
	if authorization.CanonicalWorkspace != preview.Workspace || authorization.PreviewSHA256 != previewHash || authorization.TargetSchemaVersion != preview.TargetSchemaVersion || !reflect.DeepEqual(authorization.TargetFeatures, preview.Features) || !reflect.DeepEqual(authorization.Sources, wantSources) || !validFilesystemIdentity(authorization.WorkspaceIdentity) || !validFilesystemIdentity(authorization.SourceRootIdentity) || !validFilesystemIdentity(authorization.TargetIdentity) || authorization.CanonicalSourceRoot == "" || authorization.TargetIdentityPath == "" {
		return authorizationMismatch(authorization.MigrationID, "authorization.preview_binding", previewHash, authorization.PreviewSHA256)
	}
	payloadHash, err := authorizationPayloadDigest(authorization)
	if err != nil || payloadHash != authorization.PayloadSHA256 {
		return authorizationMismatch(authorization.MigrationID, "authorization.payload_sha256", payloadHash, authorization.PayloadSHA256)
	}
	return nil
}

func authorizationMismatch(migrationID, field, expected, actual string) *MigrationError {
	return &MigrationError{
		Code:           CodeMigrationAuthorizationMismatch,
		MigrationID:    migrationID,
		Step:           MigrationStepAuthorize,
		Path:           field,
		ExpectedSHA256: expected,
		ActualSHA256:   actual,
		Durability:     DurabilityNotStarted,
		Recovery:       RecoveryNotRequired,
		NextAction:     MigrationNextNone,
		Message:        "authorization does not match the complete canonical preview payload",
	}
}

func authorizationPayloadDigest(authorization MigrationAuthorization) (string, error) {
	payload := authorizationDigestPayload{
		Version:             authorization.Version,
		MigrationID:         authorization.MigrationID,
		CanonicalWorkspace:  authorization.CanonicalWorkspace,
		CanonicalSourceRoot: authorization.CanonicalSourceRoot,
		TargetIdentityPath:  authorization.TargetIdentityPath,
		WorkspaceIdentity:   authorization.WorkspaceIdentity,
		SourceRootIdentity:  authorization.SourceRootIdentity,
		TargetIdentity:      authorization.TargetIdentity,
		PreviewSHA256:       authorization.PreviewSHA256,
		Sources:             append([]SourceExpectation(nil), authorization.Sources...),
		TargetSchemaVersion: authorization.TargetSchemaVersion,
		TargetFeatures:      append([]PreviewFeature(nil), authorization.TargetFeatures...),
		Confirmation:        authorization.Confirmation,
	}
	return canonicalSHA256(payload)
}

// BuildMigrationRecoveryAuthorization records a separate, explicit author
// choice for roll-forward or rollback.
func BuildMigrationRecoveryAuthorization(migration MigrationAuthorization, action MigrationRecoveryAction, confirmation AuthorConfirmation) (MigrationRecoveryAuthorization, error) {
	if err := ValidateMigrationID(migration.MigrationID); err != nil {
		return MigrationRecoveryAuthorization{}, err
	}
	if !validSHA256(migration.PayloadSHA256) || strings.TrimSpace(confirmation.ID) == "" || strings.TrimSpace(confirmation.Evidence) == "" {
		return MigrationRecoveryAuthorization{}, &MigrationError{Code: CodeMigrationAuthorizationRequired, MigrationID: migration.MigrationID, Step: MigrationStepAuthorize, Durability: DurabilityNotStarted, Recovery: RecoveryNotRequired, NextAction: MigrationNextNone, Message: "recovery requires the original authorization and fresh author confirmation evidence"}
	}
	if action != RecoveryActionRollForward && action != RecoveryActionRollback {
		return MigrationRecoveryAuthorization{}, &MigrationError{Code: CodeMigrationAuthorizationRequired, MigrationID: migration.MigrationID, Step: MigrationStepAuthorize, Durability: DurabilityNotStarted, Recovery: RecoveryNotRequired, NextAction: MigrationNextNone, Message: "recovery action must be explicitly roll_forward or rollback"}
	}
	authorization := MigrationRecoveryAuthorization{
		Version:                      MigrationRecoveryAuthorizationVersionV1,
		Action:                       action,
		MigrationID:                  migration.MigrationID,
		MigrationAuthorizationSHA256: migration.PayloadSHA256,
		Confirmation:                 confirmation,
	}
	var err error
	authorization.PayloadSHA256, err = recoveryAuthorizationPayloadDigest(authorization)
	if err != nil {
		return MigrationRecoveryAuthorization{}, err
	}
	return authorization, nil
}

func validateMigrationRecoveryAuthorization(migration MigrationAuthorization, authorization MigrationRecoveryAuthorization, requiredAction MigrationRecoveryAction) error {
	if authorization.Version == 0 && authorization.Action == "" && authorization.MigrationID == "" && authorization.PayloadSHA256 == "" {
		return &MigrationError{Code: CodeMigrationAuthorizationRequired, MigrationID: migration.MigrationID, Step: MigrationStepAuthorize, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextNone, Message: "separate author recovery authorization is required"}
	}
	expected, err := BuildMigrationRecoveryAuthorization(migration, authorization.Action, authorization.Confirmation)
	if err != nil {
		return err
	}
	if authorization.Action != requiredAction || !reflect.DeepEqual(expected, authorization) {
		return &MigrationError{Code: CodeMigrationAuthorizationMismatch, MigrationID: migration.MigrationID, Step: MigrationStepAuthorize, ExpectedSHA256: expected.PayloadSHA256, ActualSHA256: authorization.PayloadSHA256, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextNone, Message: "recovery authorization does not match the original migration and explicit action"}
	}
	return nil
}

func recoveryAuthorizationPayloadDigest(authorization MigrationRecoveryAuthorization) (string, error) {
	return canonicalSHA256(recoveryAuthorizationDigestPayload{
		Version:                      authorization.Version,
		Action:                       authorization.Action,
		MigrationID:                  authorization.MigrationID,
		MigrationAuthorizationSHA256: authorization.MigrationAuthorizationSHA256,
		Confirmation:                 authorization.Confirmation,
	})
}

func sourceExpectations(preview MigrationPreview) []SourceExpectation {
	expectations := make([]SourceExpectation, 0, len(preview.snapshot))
	for _, snapshot := range preview.snapshot {
		expectations = append(expectations, SourceExpectation{
			Path:     snapshot.Path,
			NodeType: snapshot.NodeType,
			Mode:     snapshot.Mode,
			Identity: snapshot.Identity,
			Size:     snapshot.Size,
			SHA256:   snapshot.SHA256,
		})
	}
	sort.Slice(expectations, func(i, j int) bool { return expectations[i].Path < expectations[j].Path })
	return expectations
}

func previewFilesystemBinding(preview MigrationPreview) (canonicalSourceRoot, targetIdentityPath string, workspaceIdentity, sourceIdentity, targetIdentity FilesystemIdentity, err error) {
	workspaceIdentity, err = pathFilesystemIdentity(preview.Workspace)
	if err != nil {
		return "", "", FilesystemIdentity{}, FilesystemIdentity{}, FilesystemIdentity{}, err
	}
	canonicalSourceRoot = preview.Workspace
	if preview.SourceRoot != "" {
		source, resolveErr := ResolveCanonicalPath(preview.Workspace, preview.SourceRoot, CanonicalOptions{})
		if resolveErr != nil {
			return "", "", FilesystemIdentity{}, FilesystemIdentity{}, FilesystemIdentity{}, resolveErr
		}
		canonicalSourceRoot = source.Absolute
	}
	sourceIdentity, err = pathFilesystemIdentity(canonicalSourceRoot)
	if err != nil {
		return "", "", FilesystemIdentity{}, FilesystemIdentity{}, FilesystemIdentity{}, err
	}
	target, resolveErr := ResolveCanonicalPath(preview.Workspace, preview.TargetRoot, CanonicalOptions{AllowMissing: true})
	if resolveErr != nil {
		return "", "", FilesystemIdentity{}, FilesystemIdentity{}, FilesystemIdentity{}, resolveErr
	}
	targetIdentityPath = target.Absolute
	if !target.Exists {
		targetIdentityPath = filepath.Dir(target.Absolute)
	}
	targetIdentity, err = pathFilesystemIdentity(targetIdentityPath)
	return canonicalSourceRoot, targetIdentityPath, workspaceIdentity, sourceIdentity, targetIdentity, err
}

func verifyAuthorizationFilesystemBinding(preview MigrationPreview, authorization MigrationAuthorization) error {
	workspaceIdentity, err := pathFilesystemIdentity(authorization.CanonicalWorkspace)
	if err != nil || workspaceIdentity != authorization.WorkspaceIdentity {
		return authorizationFilesystemMismatch(authorization.MigrationID, authorization.CanonicalWorkspace, authorization.WorkspaceIdentity, workspaceIdentity, err)
	}
	sourceIdentity, err := pathFilesystemIdentity(authorization.CanonicalSourceRoot)
	if err != nil || sourceIdentity != authorization.SourceRootIdentity {
		return authorizationFilesystemMismatch(authorization.MigrationID, authorization.CanonicalSourceRoot, authorization.SourceRootIdentity, sourceIdentity, err)
	}
	targetIdentity, err := pathFilesystemIdentity(authorization.TargetIdentityPath)
	if err != nil || targetIdentity != authorization.TargetIdentity {
		return authorizationFilesystemMismatch(authorization.MigrationID, authorization.TargetIdentityPath, authorization.TargetIdentity, targetIdentity, err)
	}
	if err := verifyMigrationSources(preview.Workspace, authorization.MigrationID, authorization.Sources, "", MigrationStepAuthorize); err != nil {
		return &MigrationError{Code: CodeMigrationAuthorizationMismatch, MigrationID: authorization.MigrationID, Step: MigrationStepAuthorize, Path: migrationErrorPath(err), Durability: DurabilityNotStarted, Recovery: RecoveryNotRequired, NextAction: MigrationNextNone, Message: "authorized source identity, mode, or bytes changed", Err: err}
	}
	return nil
}

func authorizationFilesystemMismatch(migrationID, path string, expected, actual FilesystemIdentity, err error) *MigrationError {
	return &MigrationError{Code: CodeMigrationAuthorizationMismatch, MigrationID: migrationID, Step: MigrationStepAuthorize, Path: path, ExpectedSHA256: fmt.Sprintf("%s:%s", expected.Volume, expected.FileID), ActualSHA256: fmt.Sprintf("%s:%s", actual.Volume, actual.FileID), Durability: DurabilityNotStarted, Recovery: RecoveryNotRequired, NextAction: MigrationNextNone, Message: "canonical filesystem identity changed", Err: err}
}

func migrationErrorPath(err error) string {
	var migrationErr *MigrationError
	if errors.As(err, &migrationErr) {
		return migrationErr.Path
	}
	return ""
}

func canonicalSHA256(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

func sha256Hex(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func cloneMarker(marker Marker) Marker {
	features := make(map[string]FeatureContract, len(marker.Features))
	for id, feature := range marker.Features {
		features[id] = feature
	}
	marker.Features = features
	return marker
}

func canonicalMigrationRoot(workspace, migrationID string) (CanonicalPath, error) {
	return ResolveCanonicalPath(workspace, filepath.ToSlash(filepath.Join(MigrationRootRelativePath, migrationID)), CanonicalOptions{AllowMissing: true})
}
