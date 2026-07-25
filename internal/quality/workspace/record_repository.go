package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"denova/internal/quality/domain"
)

var (
	ErrRecordExists           = errors.New("quality record already exists")
	ErrRecordNotFound         = errors.New("quality record not found")
	ErrRecordConflict         = errors.New("quality record compare-and-swap conflict")
	ErrRecordLocked           = errors.New("quality record repository lock is held")
	ErrRecordTooLarge         = errors.New("quality record exceeds configured bound")
	ErrRecordLease            = errors.New("quality record mutation did not execute inside writer lease")
	ErrRecordDurability       = errors.New("quality record durability boundary failed")
	ErrRecordRecoveryRequired = errors.New("quality record requires recovery inspection")
	ErrRecordEvolution        = errors.New("quality record update rewrites immutable history or identity")
	ErrPreferencePartialTail  = errors.New("preference journal has a partial final line")
	ErrUnknownRecordVersion   = errors.New("quality record has no accepted managed migration path")
)

const (
	defaultArtifactRecordBytes int64 = 16 * 1024 * 1024
	defaultJournalBytes        int64 = 128 * 1024 * 1024
	defaultRepositoryEntries         = 100_000
	artifactIDMaxBytes               = 128
)

type RecordRepositoryLimits struct {
	MaxArtifactBytes int64
	MaxJournalBytes  int64
	MaxEntries       int
}

type RepositoryFaultPoint string

const (
	RepositoryFaultAfterWrite            RepositoryFaultPoint = "after_write"
	RepositoryFaultBeforeFileSync        RepositoryFaultPoint = "before_file_sync"
	RepositoryFaultBeforePublish         RepositoryFaultPoint = "before_publish"
	RepositoryFaultAfterCASBeforePublish RepositoryFaultPoint = "after_cas_before_publish"
	RepositoryFaultBeforeParentSync      RepositoryFaultPoint = "before_parent_sync"
)

type RecordRepositoryHooks struct {
	Fail        func(RepositoryFaultPoint) error
	writeFile   func(*os.File, []byte) (int, error)
	syncFile    func(*os.File) error
	linkFile    func(*os.Root, string, string) error
	replaceFile func(*os.Root, string, string, string) (string, error)
	syncParent  func(*os.Root, string, string) error
}

// RecordReferenceProvider resolves stable identities to canonical bytes
// through the writer-pinned workspace root. It does not receive an arbitrary
// filesystem path from repository callers and must not reopen the workspace by
// an independently resolved path.
type RecordReferenceScope struct {
	Root *os.Root
}

type RecordReferenceProvider interface {
	CandidateSetReferences(context.Context, RecordReferenceScope, domain.CandidateSet) (domain.CandidateSetReferences, error)
	ReviewIssueReferences(context.Context, RecordReferenceScope, domain.ReviewIssue) (domain.ReviewIssueReferences, error)
	PreferenceSignalReferences(context.Context, RecordReferenceScope, domain.PreferenceSignal) (domain.PreferenceSignalReferences, error)
}

// RecordAuthorityVerifier is the trusted author-action boundary. Persisted
// actor_type/confirmation fields are evidence to verify, never authority by
// themselves. Implementations must resolve author/review-lead actions from a
// trusted application-owned source rather than model, Skill, reviewer score,
// Automation, or telemetry output.
type RecordAuthorityVerifier interface {
	VerifyCandidateSetMutation(context.Context, *domain.CandidateSet, domain.CandidateSet) error
	VerifyReviewIssueMutation(context.Context, *domain.ReviewIssue, domain.ReviewIssue) error
	VerifyPreferenceSignalAppend(context.Context, []domain.PreferenceSignal, domain.PreferenceSignal) error
}

type RecordRepositoryConfig struct {
	Workspace  string
	Inspector  InspectorOptions
	Decoder    *RecordDecoder
	Lease      WorkspaceWriterLease
	References RecordReferenceProvider
	Authority  RecordAuthorityVerifier
	Limits     RecordRepositoryLimits
	Hooks      RecordRepositoryHooks
}

type recordRepository struct {
	workspace  string
	inspector  *Inspector
	decoder    *RecordDecoder
	lease      WorkspaceWriterLease
	references RecordReferenceProvider
	authority  RecordAuthorityVerifier
	limits     RecordRepositoryLimits
	hooks      RecordRepositoryHooks
}

func (repository *recordRepository) referenceScope(root *os.Root) RecordReferenceScope {
	return RecordReferenceScope{Root: root}
}

func newRecordRepository(config RecordRepositoryConfig) (*recordRepository, error) {
	if config.Decoder == nil || nilInterface(config.Lease) || nilInterface(config.References) || nilInterface(config.Authority) {
		return nil, errors.New("record decoder, writer lease, reference provider, and authority verifier are required")
	}
	workspace, err := canonicalWorkspace(config.Workspace)
	if err != nil {
		return nil, err
	}
	inspector, err := NewInspector(config.Inspector)
	if err != nil {
		return nil, err
	}
	limits, err := effectiveRecordLimits(config.Limits)
	if err != nil {
		return nil, err
	}
	return &recordRepository{workspace: workspace, inspector: inspector, decoder: config.Decoder, lease: config.Lease, references: config.References, authority: config.Authority, limits: limits, hooks: config.Hooks}, nil
}

func effectiveRecordLimits(limits RecordRepositoryLimits) (RecordRepositoryLimits, error) {
	if limits.MaxArtifactBytes < 0 || limits.MaxJournalBytes < 0 || limits.MaxEntries < 0 {
		return RecordRepositoryLimits{}, errors.New("record repository limits cannot be negative")
	}
	if limits.MaxArtifactBytes == 0 {
		limits.MaxArtifactBytes = defaultArtifactRecordBytes
	}
	if limits.MaxJournalBytes == 0 {
		limits.MaxJournalBytes = defaultJournalBytes
	}
	if limits.MaxEntries == 0 {
		limits.MaxEntries = defaultRepositoryEntries
	}
	return limits, nil
}

func (repository *recordRepository) mutate(ctx context.Context, fn func(*os.Root) error) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	invoked := false
	err := repository.lease.WithExclusiveWorkspace(ctx, func() error {
		invoked = true
		workspaceInfo, err := os.Lstat(repository.workspace)
		if err != nil || !workspaceInfo.IsDir() || workspaceInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("pin quality record workspace root: %w", err)
		}
		inspection, err := repository.inspector.Inspect(repository.workspace)
		if err != nil {
			return err
		}
		if err := inspection.RequireManagedMutation(); err != nil {
			return err
		}
		root, err := os.OpenRoot(repository.workspace)
		if err != nil {
			return err
		}
		defer root.Close()
		rootInfo, err := root.Stat(".")
		if err != nil || !os.SameFile(workspaceInfo, rootInfo) {
			return ErrRecordConflict
		}
		if err := ensureRootDirectoryTree(root, repository.workspace, recordLockParent, 0o700); err != nil {
			return fmt.Errorf("prepare repository lock parent: %w", err)
		}
		lock, err := acquireRecordRepositoryLock(root, repository.workspace)
		if err != nil {
			return err
		}
		defer lock.Close()
		inspection, err = repository.inspector.Inspect(repository.workspace)
		if err != nil {
			return err
		}
		if err := inspection.RequireManagedMutation(); err != nil {
			return err
		}
		if err := repository.verifyWorkspaceRoot(rootInfo); err != nil {
			return err
		}
		if err := fn(root); err != nil {
			return err
		}
		return repository.verifyWorkspaceRoot(rootInfo)
	})
	if !invoked && err == nil {
		return ErrRecordLease
	}
	return err
}

func (repository *recordRepository) verifyWorkspaceRoot(expected os.FileInfo) error {
	current, err := os.Lstat(repository.workspace)
	if err != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || expected == nil || !os.SameFile(expected, current) {
		return ErrRecordConflict
	}
	return nil
}

func (repository *recordRepository) requireManagedMutation(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	inspection, err := repository.inspector.Inspect(repository.workspace)
	if err != nil {
		return err
	}
	return inspection.RequireManagedMutation()
}

func (repository *recordRepository) fail(point RepositoryFaultPoint) error {
	if repository.hooks.Fail == nil {
		return nil
	}
	if err := repository.hooks.Fail(point); err != nil {
		return fmt.Errorf("%w at %s: %v", ErrRecordDurability, point, err)
	}
	return nil
}

func (repository *recordRepository) writeFile(file *os.File, raw []byte) (int, error) {
	if repository.hooks.writeFile != nil {
		return repository.hooks.writeFile(file, raw)
	}
	return file.Write(raw)
}

func (repository *recordRepository) syncFile(file *os.File) error {
	if repository.hooks.syncFile != nil {
		return repository.hooks.syncFile(file)
	}
	return file.Sync()
}

func (repository *recordRepository) linkFile(root *os.Root, from, to string) error {
	if repository.hooks.linkFile != nil {
		return repository.hooks.linkFile(root, from, to)
	}
	return root.Link(filepath.FromSlash(from), filepath.FromSlash(to))
}

func (repository *recordRepository) replaceFile(root *os.Root, target, replacement string) (string, error) {
	if repository.hooks.replaceFile != nil {
		return repository.hooks.replaceFile(root, repository.workspace, target, replacement)
	}
	return replaceRecordAtomically(root, repository.workspace, target, replacement)
}

func (repository *recordRepository) syncParent(root *os.Root, rel string) error {
	if repository.hooks.syncParent != nil {
		return repository.hooks.syncParent(root, repository.workspace, rel)
	}
	return syncRootDirectory(root, repository.workspace, rel)
}
