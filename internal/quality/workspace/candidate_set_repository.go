package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"denova/internal/quality/domain"
)

type ArtifactUpdateExpectation struct {
	RawSHA256         string
	WorkspaceRevision int
}

type CandidateSetSnapshot struct {
	Parsed            ParsedCandidateSet
	RelativePath      string
	RawSHA256         string
	WorkspaceRevision int
}

type CandidateSetRepository struct {
	core *recordRepository
}

func NewCandidateSetRepository(config RecordRepositoryConfig) (*CandidateSetRepository, error) {
	core, err := newRecordRepository(config)
	if err != nil {
		return nil, err
	}
	return &CandidateSetRepository{core: core}, nil
}

func (repository *CandidateSetRepository) Create(ctx context.Context, raw []byte) (CandidateSetSnapshot, error) {
	if err := repository.core.requireManagedMutation(ctx); err != nil {
		return CandidateSetSnapshot{}, err
	}
	parsed, managed, err := repository.parseManaged(raw)
	if err != nil {
		return CandidateSetSnapshot{}, err
	}
	rel, err := CandidateSetRelativePath(managed.CandidateSetID)
	if err != nil {
		return CandidateSetSnapshot{}, err
	}
	err = repository.core.mutate(ctx, func(root *os.Root) error {
		scope := repository.core.referenceScope(root)
		if err := repository.validateManaged(ctx, scope, *managed); err != nil {
			return err
		}
		if err := repository.core.authority.VerifyCandidateSetMutation(ctx, nil, *managed); err != nil {
			return err
		}
		if err := rejectPortableRecordCollision(root, candidateSetRecordRoot, managed.CandidateSetID, repository.core.limits.MaxEntries); err != nil {
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
		return CandidateSetSnapshot{}, err
	}
	return candidateSetSnapshot(parsed, rel, raw, managed.Workspace.Revision), nil
}

func (repository *CandidateSetRepository) Read(ctx context.Context, id string) (CandidateSetSnapshot, error) {
	rel, err := CandidateSetRelativePath(id)
	if err != nil {
		return CandidateSetSnapshot{}, err
	}
	root, err := os.OpenRoot(repository.core.workspace)
	if err != nil {
		return CandidateSetSnapshot{}, err
	}
	defer root.Close()
	raw, _, err := readArtifactRecord(root, rel, repository.core.limits.MaxArtifactBytes)
	if err != nil {
		return CandidateSetSnapshot{}, err
	}
	parsed, err := repository.core.decoder.ParseCandidateSet(raw)
	if err != nil {
		return candidateSetSnapshot(parsed, rel, raw, 0), err
	}
	if !parsed.CanManagedMutate() {
		return candidateSetSnapshot(parsed, rel, raw, 0), nil
	}
	managed, err := parsed.Managed()
	if err != nil {
		return candidateSetSnapshot(parsed, rel, raw, 0), err
	}
	if managed.CandidateSetID != id {
		return candidateSetSnapshot(parsed, rel, raw, managed.Workspace.Revision), fmt.Errorf("%w: path ID differs from CandidateSet ID", ErrRecordConflict)
	}
	references, err := repository.core.references.CandidateSetReferences(ctx, repository.core.referenceScope(root), *managed)
	if err == nil {
		err = domain.ValidateCandidateSet(*managed, references)
	}
	return candidateSetSnapshot(parsed, rel, raw, managed.Workspace.Revision), err
}

func (repository *CandidateSetRepository) List(_ context.Context) ([]string, error) {
	root, err := os.OpenRoot(repository.core.workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return listArtifactRecordIDs(root, candidateSetRecordRoot, repository.core.limits.MaxEntries)
}

func (repository *CandidateSetRepository) ListRecovery(_ context.Context) ([]RecordRecoveryEntry, error) {
	root, err := os.OpenRoot(repository.core.workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return listRecordRecovery(root, candidateSetRecordRoot, repository.core.limits.MaxEntries, repository.core.limits.MaxArtifactBytes, func(target string) bool {
		return validArtifactRecordTarget(candidateSetRecordRoot, target)
	})
}

func (repository *CandidateSetRepository) Update(ctx context.Context, id string, raw []byte, expected ArtifactUpdateExpectation) (CandidateSetSnapshot, error) {
	if err := repository.core.requireManagedMutation(ctx); err != nil {
		return CandidateSetSnapshot{}, err
	}
	parsed, managed, err := repository.parseManaged(raw)
	if err != nil {
		return CandidateSetSnapshot{}, err
	}
	if managed.CandidateSetID != id {
		return CandidateSetSnapshot{}, fmt.Errorf("%w: updated CandidateSet ID differs from path ID", ErrRecordConflict)
	}
	rel, err := CandidateSetRelativePath(id)
	if err != nil {
		return CandidateSetSnapshot{}, err
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
		currentParsed, err := repository.core.decoder.ParseCandidateSet(currentRaw)
		if err != nil || !currentParsed.CanManagedMutate() {
			return fmt.Errorf("%w: current record is not exact managed v1", ErrRecordConflict)
		}
		current, err := currentParsed.Managed()
		if err != nil || current.Workspace.Revision != expected.WorkspaceRevision {
			return ErrRecordConflict
		}
		if err := repository.validateManaged(ctx, scope, *current); err != nil {
			return ErrRecordConflict
		}
		if err := validateCandidateSetEvolution(*current, *managed); err != nil {
			return err
		}
		if err := repository.validateManaged(ctx, scope, *managed); err != nil {
			return err
		}
		if err := repository.core.authority.VerifyCandidateSetMutation(ctx, current, *managed); err != nil {
			return err
		}
		return repository.core.publishRecord(root, rel, raw, false, info, currentHash, repository.core.limits.MaxArtifactBytes)
	})
	if err != nil {
		return CandidateSetSnapshot{}, err
	}
	return candidateSetSnapshot(parsed, rel, raw, managed.Workspace.Revision), nil
}

func (repository *CandidateSetRepository) parseManaged(raw []byte) (ParsedCandidateSet, *domain.CandidateSet, error) {
	if int64(len(raw)) > repository.core.limits.MaxArtifactBytes {
		return ParsedCandidateSet{}, nil, ErrRecordTooLarge
	}
	parsed, err := repository.core.decoder.ParseCandidateSet(raw)
	if err != nil {
		return parsed, nil, err
	}
	managed, err := parsed.Managed()
	if err != nil {
		return parsed, nil, err
	}
	return parsed, managed, nil
}

func (repository *CandidateSetRepository) validateManaged(ctx context.Context, scope RecordReferenceScope, record domain.CandidateSet) error {
	references, err := repository.core.references.CandidateSetReferences(ctx, scope, record)
	if err != nil {
		return err
	}
	return domain.ValidateCandidateSet(record, references)
}

func candidateSetSnapshot(parsed ParsedCandidateSet, rel string, raw []byte, revision int) CandidateSetSnapshot {
	return CandidateSetSnapshot{Parsed: parsed, RelativePath: rel, RawSHA256: recordSHA256(raw), WorkspaceRevision: revision}
}

func readArtifactRecord(root *os.Root, rel string, limit int64) ([]byte, os.FileInfo, error) {
	info, err := root.Lstat(filepath.FromSlash(rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, ErrRecordNotFound
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("record path is not a strict regular file: %w", err)
	}
	if info.Size() > limit {
		return nil, nil, ErrRecordTooLarge
	}
	raw, err := readBoundedRootFile(root, rel, info, limit)
	return raw, info, err
}

func listArtifactRecordIDs(root *os.Root, directory string, limit int) ([]string, error) {
	info, err := root.Lstat(filepath.FromSlash(directory))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("record directory is not a strict directory: %w", err)
	}
	handle, err := root.Open(filepath.FromSlash(directory))
	if err != nil {
		return nil, err
	}
	entries, readErr := handle.ReadDir(limit + 1)
	closeErr := handle.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(entries) > limit {
		return nil, ErrRecordTooLarge
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, infoErr := entry.Info()
		target, temporary := recordTemporaryTarget(entry.Name())
		if temporary && validArtifactRecordTarget(directory, target) && infoErr == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if infoErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unsupported record directory entry %q", entry.Name())
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if _, err := artifactRecordRelativePath(directory, id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if collisions := DetectPortablePathCollisions(ids); len(collisions) != 0 {
		return nil, fmt.Errorf("portable record ID collision: %#v", collisions[0])
	}
	sort.Strings(ids)
	return ids, nil
}

func validArtifactRecordTarget(directory, name string) bool {
	if !strings.HasSuffix(name, ".json") {
		return false
	}
	id := strings.TrimSuffix(name, ".json")
	rel, err := artifactRecordRelativePath(directory, id)
	return err == nil && rel == path.Join(directory, name)
}

func rejectPortableRecordCollision(root *os.Root, directory, id string, limit int) error {
	ids, err := listArtifactRecordIDs(root, directory, limit)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return nil
		}
		if _, statErr := root.Lstat(filepath.FromSlash(directory)); errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, existing := range ids {
		if existing == id {
			return ErrRecordExists
		}
	}
	if collisions := DetectPortablePathCollisions(append(ids, id)); len(collisions) != 0 {
		return fmt.Errorf("%w: portable ID collision %#v", ErrRecordConflict, collisions[0])
	}
	return nil
}

func recordSHA256(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
