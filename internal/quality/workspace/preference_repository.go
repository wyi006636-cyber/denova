package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"denova/internal/quality/domain"
)

type PreferenceAppendExpectation struct {
	PriorRawSHA256 string
}

type PreferenceJournalSnapshot struct {
	Entries   []ParsedPreferenceSignal
	RawSHA256 string
	raw       []byte
}

func (snapshot PreferenceJournalSnapshot) RawBytes() []byte {
	return bytes.Clone(snapshot.raw)
}

type PreferenceMemoryRepository struct {
	core *recordRepository
}

func NewPreferenceMemoryRepository(config RecordRepositoryConfig) (*PreferenceMemoryRepository, error) {
	core, err := newRecordRepository(config)
	if err != nil {
		return nil, err
	}
	return &PreferenceMemoryRepository{core: core}, nil
}

func (repository *PreferenceMemoryRepository) Read(ctx context.Context) (PreferenceJournalSnapshot, error) {
	if ctx == nil {
		return PreferenceJournalSnapshot{}, errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return PreferenceJournalSnapshot{}, err
	}
	root, err := os.OpenRoot(repository.core.workspace)
	if err != nil {
		return PreferenceJournalSnapshot{}, err
	}
	defer root.Close()
	raw, _, err := readPreferenceJournalFile(root, repository.core.limits.MaxJournalBytes)
	if err != nil {
		return PreferenceJournalSnapshot{}, err
	}
	return repository.parseAndValidateJournal(ctx, repository.core.referenceScope(root), raw, true)
}

func (repository *PreferenceMemoryRepository) Append(ctx context.Context, raw []byte, expected PreferenceAppendExpectation) (PreferenceJournalSnapshot, error) {
	if err := repository.core.requireManagedMutation(ctx); err != nil {
		return PreferenceJournalSnapshot{}, err
	}
	if int64(len(raw)+1) > repository.core.limits.MaxJournalBytes {
		return PreferenceJournalSnapshot{}, ErrRecordTooLarge
	}
	if len(raw) == 0 || bytes.ContainsAny(raw, "\r\n") {
		return PreferenceJournalSnapshot{}, &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "$", Message: "appended PreferenceSignal must be one complete JSONL line"}
	}
	parsed, err := repository.core.decoder.ParsePreferenceSignal(raw)
	if err != nil {
		return PreferenceJournalSnapshot{}, err
	}
	managed, err := parsed.Managed()
	if err != nil {
		return PreferenceJournalSnapshot{}, errors.Join(ErrUnknownRecordVersion, err)
	}
	var committed PreferenceJournalSnapshot
	err = repository.core.mutate(ctx, func(root *os.Root) error {
		scope := repository.core.referenceScope(root)
		current, info, err := readPreferenceJournalFile(root, repository.core.limits.MaxJournalBytes)
		if err != nil {
			return err
		}
		currentHash := recordSHA256(current)
		if expected.PriorRawSHA256 == "" || expected.PriorRawSHA256 != currentHash {
			return ErrRecordConflict
		}
		currentSnapshot, err := repository.parseAndValidateJournal(ctx, scope, current, false)
		if err != nil {
			return err
		}
		managedSignals, err := managedPreferenceSignals(currentSnapshot.Entries)
		if err != nil {
			return err
		}
		freshReferences, err := repository.core.references.PreferenceSignalReferences(ctx, scope, *managed)
		if err != nil {
			return err
		}
		if err := domain.ValidatePreferenceSignal(*managed, freshReferences); err != nil {
			return err
		}
		managedSignals = append(managedSignals, *managed)
		if err := domain.ValidatePreferenceJournal(managedSignals); err != nil {
			return err
		}
		if err := repository.core.authority.VerifyPreferenceSignalAppend(ctx, managedSignals[:len(managedSignals)-1], *managed); err != nil {
			return err
		}
		if int64(len(current)+len(raw)+1) > repository.core.limits.MaxJournalBytes {
			return ErrRecordTooLarge
		}
		next := make([]byte, 0, len(current)+len(raw)+1)
		next = append(next, current...)
		next = append(next, raw...)
		next = append(next, '\n')
		create := info == nil
		if err := repository.core.publishRecord(root, preferenceJournalPath, next, create, info, currentHash, repository.core.limits.MaxJournalBytes); err != nil {
			return err
		}
		entries := append(append([]ParsedPreferenceSignal(nil), currentSnapshot.Entries...), parsed)
		committed = preferenceJournalSnapshot(entries, next)
		return nil
	})
	if err != nil {
		return PreferenceJournalSnapshot{}, err
	}
	return committed, nil
}

func (repository *PreferenceMemoryRepository) Resolve(ctx context.Context, query domain.PreferenceQuery) (domain.PreferenceResolution, error) {
	snapshot, err := repository.Read(ctx)
	if err != nil {
		return domain.PreferenceResolution{}, err
	}
	signals, err := managedPreferenceSignals(snapshot.Entries)
	if err != nil {
		return domain.PreferenceResolution{}, err
	}
	return domain.ResolvePreferences(signals, query)
}

func (repository *PreferenceMemoryRepository) ListRecovery(_ context.Context) ([]RecordRecoveryEntry, error) {
	root, err := os.OpenRoot(repository.core.workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return listRecordRecovery(root, path.Dir(preferenceJournalPath), repository.core.limits.MaxEntries, repository.core.limits.MaxJournalBytes, func(target string) bool {
		return target == path.Base(preferenceJournalPath)
	})
}

func (repository *PreferenceMemoryRepository) parseAndValidateJournal(ctx context.Context, scope RecordReferenceScope, raw []byte, allowUnknown bool) (PreferenceJournalSnapshot, error) {
	entries, err := repository.parseJournal(raw)
	if err != nil {
		return PreferenceJournalSnapshot{}, err
	}
	managed := make([]domain.PreferenceSignal, 0, len(entries))
	for _, parsed := range entries {
		if !parsed.CanManagedMutate() {
			if allowUnknown {
				continue
			}
			return PreferenceJournalSnapshot{}, ErrUnknownRecordVersion
		}
		signal, err := parsed.Managed()
		if err != nil {
			return PreferenceJournalSnapshot{}, err
		}
		references, err := repository.core.references.PreferenceSignalReferences(ctx, scope, *signal)
		if err != nil {
			return PreferenceJournalSnapshot{}, err
		}
		if err := domain.ValidatePreferenceSignal(*signal, references); err != nil {
			return PreferenceJournalSnapshot{}, err
		}
		managed = append(managed, *signal)
	}
	if len(managed) == len(entries) {
		if err := domain.ValidatePreferenceJournal(managed); err != nil {
			return PreferenceJournalSnapshot{}, err
		}
	}
	return preferenceJournalSnapshot(entries, raw), nil
}

func (repository *PreferenceMemoryRepository) parseJournal(raw []byte) ([]ParsedPreferenceSignal, error) {
	if len(raw) == 0 {
		return []ParsedPreferenceSignal{}, nil
	}
	if raw[len(raw)-1] != '\n' {
		return nil, ErrPreferencePartialTail
	}
	lines := bytes.Split(raw[:len(raw)-1], []byte{'\n'})
	if len(lines) > repository.core.limits.MaxEntries {
		return nil, ErrRecordTooLarge
	}
	entries := make([]ParsedPreferenceSignal, 0, len(lines))
	for index, line := range lines {
		if len(line) == 0 || bytes.ContainsRune(line, '\r') {
			return nil, &domain.ContractError{Code: domain.CodeMalformedRecord, Path: fmt.Sprintf("line[%d]", index+1), Message: "PreferenceMemory JSONL line is empty or contains CR"}
		}
		parsed, err := repository.core.decoder.ParsePreferenceSignal(line)
		if err != nil {
			return nil, fmt.Errorf("preference journal line %d: %w", index+1, err)
		}
		entries = append(entries, parsed)
	}
	return entries, nil
}

func readPreferenceJournalFile(root *os.Root, limit int64) ([]byte, os.FileInfo, error) {
	info, err := root.Lstat(filepath.FromSlash(preferenceJournalPath))
	if errors.Is(err, os.ErrNotExist) {
		return []byte{}, nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("preference journal is not a strict regular file: %w", err)
	}
	if info.Size() > limit {
		return nil, nil, ErrRecordTooLarge
	}
	raw, err := readBoundedRootFile(root, preferenceJournalPath, info, limit)
	return raw, info, err
}

func managedPreferenceSignals(entries []ParsedPreferenceSignal) ([]domain.PreferenceSignal, error) {
	signals := make([]domain.PreferenceSignal, 0, len(entries))
	for _, entry := range entries {
		signal, err := entry.Managed()
		if err != nil {
			return nil, errors.Join(ErrUnknownRecordVersion, err)
		}
		signals = append(signals, *signal)
	}
	return signals, nil
}

func preferenceJournalSnapshot(entries []ParsedPreferenceSignal, raw []byte) PreferenceJournalSnapshot {
	return PreferenceJournalSnapshot{Entries: append([]ParsedPreferenceSignal(nil), entries...), RawSHA256: recordSHA256(raw), raw: bytes.Clone(raw)}
}
