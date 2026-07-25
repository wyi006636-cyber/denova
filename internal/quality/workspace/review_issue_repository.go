package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"denova/internal/quality/domain"
)

type ReviewIssueSnapshot struct {
	Parsed            ParsedReviewIssue
	RelativePath      string
	RawSHA256         string
	WorkspaceRevision int
}

type ReviewIssueRepository struct {
	core *recordRepository
}

func NewReviewIssueRepository(config RecordRepositoryConfig) (*ReviewIssueRepository, error) {
	core, err := newRecordRepository(config)
	if err != nil {
		return nil, err
	}
	return &ReviewIssueRepository{core: core}, nil
}

func (repository *ReviewIssueRepository) Create(ctx context.Context, raw []byte) (ReviewIssueSnapshot, error) {
	if err := repository.core.requireManagedMutation(ctx); err != nil {
		return ReviewIssueSnapshot{}, err
	}
	parsed, managed, err := repository.parseManaged(raw)
	if err != nil {
		return ReviewIssueSnapshot{}, err
	}
	rel, err := ReviewIssueRelativePath(managed.IssueID)
	if err != nil {
		return ReviewIssueSnapshot{}, err
	}
	err = repository.core.mutate(ctx, func(root *os.Root) error {
		scope := repository.core.referenceScope(root)
		if err := repository.validateManaged(ctx, scope, *managed); err != nil {
			return err
		}
		if err := repository.core.authority.VerifyReviewIssueMutation(ctx, nil, *managed); err != nil {
			return err
		}
		if err := rejectPortableRecordCollision(root, reviewIssueRecordRoot, managed.IssueID, repository.core.limits.MaxEntries); err != nil {
			return err
		}
		if _, err := root.Lstat(filepath.FromSlash(rel)); err == nil {
			return ErrRecordExists
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return repository.core.publishRecord(root, rel, raw, true, nil, "", repository.core.limits.MaxArtifactBytes)
	})
	if err != nil {
		return ReviewIssueSnapshot{}, err
	}
	return reviewIssueSnapshot(parsed, rel, raw, managed.Binding.Workspace.Revision), nil
}

func (repository *ReviewIssueRepository) Read(ctx context.Context, id string) (ReviewIssueSnapshot, error) {
	rel, err := ReviewIssueRelativePath(id)
	if err != nil {
		return ReviewIssueSnapshot{}, err
	}
	root, err := os.OpenRoot(repository.core.workspace)
	if err != nil {
		return ReviewIssueSnapshot{}, err
	}
	defer root.Close()
	raw, _, err := readArtifactRecord(root, rel, repository.core.limits.MaxArtifactBytes)
	if err != nil {
		return ReviewIssueSnapshot{}, err
	}
	parsed, err := repository.core.decoder.ParseReviewIssue(raw)
	if err != nil {
		return reviewIssueSnapshot(parsed, rel, raw, 0), err
	}
	if !parsed.CanManagedMutate() {
		return reviewIssueSnapshot(parsed, rel, raw, 0), nil
	}
	managed, err := parsed.Managed()
	if err != nil {
		return reviewIssueSnapshot(parsed, rel, raw, 0), err
	}
	if managed.IssueID != id {
		return reviewIssueSnapshot(parsed, rel, raw, managed.Binding.Workspace.Revision), fmt.Errorf("%w: path ID differs from ReviewIssue ID", ErrRecordConflict)
	}
	if err := validateReviewedArtifactPath(managed.Location.ArtifactPath); err != nil {
		return reviewIssueSnapshot(parsed, rel, raw, managed.Binding.Workspace.Revision), err
	}
	references, err := repository.core.references.ReviewIssueReferences(ctx, repository.core.referenceScope(root), *managed)
	if err == nil {
		err = domain.ValidateReviewIssue(*managed, references)
	}
	return reviewIssueSnapshot(parsed, rel, raw, managed.Binding.Workspace.Revision), err
}

func (repository *ReviewIssueRepository) List(_ context.Context) ([]string, error) {
	root, err := os.OpenRoot(repository.core.workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return listArtifactRecordIDs(root, reviewIssueRecordRoot, repository.core.limits.MaxEntries)
}

func (repository *ReviewIssueRepository) ListRecovery(_ context.Context) ([]RecordRecoveryEntry, error) {
	root, err := os.OpenRoot(repository.core.workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return listRecordRecovery(root, reviewIssueRecordRoot, repository.core.limits.MaxEntries, repository.core.limits.MaxArtifactBytes, func(target string) bool {
		return validArtifactRecordTarget(reviewIssueRecordRoot, target)
	})
}

func (repository *ReviewIssueRepository) Update(ctx context.Context, id string, raw []byte, expected ArtifactUpdateExpectation) (ReviewIssueSnapshot, error) {
	if err := repository.core.requireManagedMutation(ctx); err != nil {
		return ReviewIssueSnapshot{}, err
	}
	parsed, managed, err := repository.parseManaged(raw)
	if err != nil {
		return ReviewIssueSnapshot{}, err
	}
	if managed.IssueID != id {
		return ReviewIssueSnapshot{}, fmt.Errorf("%w: updated ReviewIssue ID differs from path ID", ErrRecordConflict)
	}
	rel, err := ReviewIssueRelativePath(id)
	if err != nil {
		return ReviewIssueSnapshot{}, err
	}
	err = repository.core.mutate(ctx, func(root *os.Root) error {
		scope := repository.core.referenceScope(root)
		currentRaw, info, err := readArtifactRecord(root, rel, repository.core.limits.MaxArtifactBytes)
		if err != nil {
			return err
		}
		currentHash := recordSHA256(currentRaw)
		if currentHash != expected.RawSHA256 {
			return ErrRecordConflict
		}
		currentParsed, err := repository.core.decoder.ParseReviewIssue(currentRaw)
		if err != nil || !currentParsed.CanManagedMutate() {
			return fmt.Errorf("%w: current record is not exact managed v1", ErrRecordConflict)
		}
		current, err := currentParsed.Managed()
		if err != nil || current.Binding.Workspace.Revision != expected.WorkspaceRevision {
			return ErrRecordConflict
		}
		if err := repository.validateManaged(ctx, scope, *current); err != nil {
			return ErrRecordConflict
		}
		if err := validateReviewIssueEvolution(*current, *managed); err != nil {
			return err
		}
		if err := repository.validateManaged(ctx, scope, *managed); err != nil {
			return err
		}
		if err := repository.core.authority.VerifyReviewIssueMutation(ctx, current, *managed); err != nil {
			return err
		}
		return repository.core.publishRecord(root, rel, raw, false, info, currentHash, repository.core.limits.MaxArtifactBytes)
	})
	if err != nil {
		return ReviewIssueSnapshot{}, err
	}
	return reviewIssueSnapshot(parsed, rel, raw, managed.Binding.Workspace.Revision), nil
}

func (repository *ReviewIssueRepository) validateManaged(ctx context.Context, scope RecordReferenceScope, record domain.ReviewIssue) error {
	if err := validateReviewedArtifactPath(record.Location.ArtifactPath); err != nil {
		return err
	}
	references, err := repository.core.references.ReviewIssueReferences(ctx, scope, record)
	if err != nil {
		return err
	}
	return domain.ValidateReviewIssue(record, references)
}

func (repository *ReviewIssueRepository) parseManaged(raw []byte) (ParsedReviewIssue, *domain.ReviewIssue, error) {
	if int64(len(raw)) > repository.core.limits.MaxArtifactBytes {
		return ParsedReviewIssue{}, nil, ErrRecordTooLarge
	}
	parsed, err := repository.core.decoder.ParseReviewIssue(raw)
	if err != nil {
		return parsed, nil, err
	}
	managed, err := parsed.Managed()
	if err != nil {
		return parsed, nil, err
	}
	if err := validateReviewedArtifactPath(managed.Location.ArtifactPath); err != nil {
		return parsed, nil, err
	}
	return parsed, managed, nil
}

func reviewIssueSnapshot(parsed ParsedReviewIssue, rel string, raw []byte, revision int) ReviewIssueSnapshot {
	return ReviewIssueSnapshot{Parsed: parsed, RelativePath: rel, RawSHA256: recordSHA256(raw), WorkspaceRevision: revision}
}

func validateReviewedArtifactPath(raw string) error {
	validated, err := ValidateRelativePath(raw, PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows})
	if err != nil {
		return err
	}
	if validated != raw {
		return fmt.Errorf("reviewed artifact path must use canonical forward slashes: %q", raw)
	}
	return nil
}
