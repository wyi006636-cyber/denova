package writingcomparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"denova/internal/agents/conversationjournal"
	"denova/internal/localfs"
	"denova/internal/portablepath"
	projectdomain "denova/internal/project"
)

const journalFilename = "journal.jsonl"

var immutableAssetMu sync.Mutex

type Store struct {
	layout projectdomain.Layout
	mu     sync.Mutex
}

func NewStore(layout projectdomain.Layout) *Store { return &Store{layout: layout} }

func (store *Store) Layout() projectdomain.Layout {
	if store == nil {
		return projectdomain.Layout{}
	}
	return store.layout
}

func (store *Store) taskDir(id string) string {
	return filepath.Join(store.layout.ComparisonsDir(), filepath.FromSlash(id))
}

func (store *Store) journalPath(id string) string {
	return filepath.Join(store.taskDir(id), journalFilename)
}

func (store *Store) Create(ctx context.Context, task Task) (Task, error) {
	if store == nil {
		return Task{}, fmt.Errorf("comparison store is nil")
	}
	if err := projectdomain.ValidateID(task.ID); err != nil {
		return Task{}, fmt.Errorf("invalid comparison task ID: %w", err)
	}
	if err := projectdomain.ValidateID(task.ProjectID); err != nil {
		return Task{}, fmt.Errorf("invalid Project ID: %w", err)
	}
	if task.ProjectID != store.layout.ProjectID {
		return Task{}, fmt.Errorf("comparison task Project mismatch")
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = task.CreatedAt
	}
	if task.Phase == "" {
		task.Phase = PhaseConfiguring
	}
	if task.Execution == "" {
		task.Execution = ExecutionIdle
	}
	if err := validateTask(task); err != nil {
		return Task{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := os.MkdirAll(store.layout.ComparisonsDir(), 0o700); err != nil {
		return Task{}, fmt.Errorf("create comparison directory: %w", err)
	}
	dir := store.taskDir(task.ID)
	if err := portablepath.CheckPath(store.layout.ComparisonsDir(), task.ID); err != nil {
		return Task{}, err
	}
	if _, err := os.Stat(dir); err == nil {
		return Task{}, &Error{Code: "task_exists", Message: "comparison task already exists", Cause: ErrRevisionConflict}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Task{}, err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return Task{}, err
	}
	journalFile := filepath.Join(dir, journalFilename)
	// conversationjournal requires a syntactically valid first record before it
	// can open a canonical file. Seed the immutable task.created event as a
	// legacy JSONL record; subsequent events use the journal's transactional
	// append path and therefore retain the normal CAS/index guarantees.
	seed, err := marshalEvent(task.ID, Event{Kind: EventTaskCreated, Task: &task})
	if err != nil {
		_ = os.RemoveAll(dir)
		return Task{}, err
	}
	if err := writeSeedJournal(journalFile, append(seed, '\n')); err != nil {
		_ = os.RemoveAll(dir)
		return Task{}, err
	}
	handle, err := store.openLocked(ctx, task.ID)
	if err != nil {
		_ = os.RemoveAll(dir)
		return Task{}, err
	}
	defer handle.Close()
	created := handle.Current()
	if created.ID != task.ID {
		return Task{}, fmt.Errorf("comparison task projection identity mismatch")
	}
	slog.InfoContext(ctx, "writing comparison task created", "task_id", task.ID, "project_id", task.ProjectID)
	return created, nil
}

// Handle is one independent journal view. Callers may keep two handles open
// to exercise the journal CAS behavior; Append refreshes the view before it
// validates and writes a new event.
type Handle struct {
	store      *Store
	id         string
	path       string
	journal    *conversationjournal.Journal
	projection *taskProjection
	mu         sync.Mutex
}

func (store *Store) Open(ctx context.Context, id string) (*Handle, error) {
	if store == nil {
		return nil, fmt.Errorf("comparison store is nil")
	}
	if err := projectdomain.ValidateID(id); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.openLocked(ctx, id)
}

func (store *Store) openLocked(ctx context.Context, id string) (*Handle, error) {
	path := store.journalPath(id)
	if err := portablepath.CheckPath(store.layout.ComparisonsDir(), id+"/"+journalFilename); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	projection := newTaskProjection()
	journal, err := conversationjournal.Open(ctx, path, conversationjournal.Identity{ID: id, Generation: "1"}, projection, conversationjournal.Options{})
	if err != nil {
		return nil, fmt.Errorf("open comparison journal: %w", err)
	}
	if projection.task.ID != "" && projection.task.ProjectID != store.layout.ProjectID {
		_ = journal.Close()
		return nil, fmt.Errorf("comparison task Project mismatch")
	}
	handle := &Handle{store: store, id: id, path: path, journal: journal, projection: projection}
	return handle, nil
}

func (handle *Handle) Current() Task {
	if handle == nil {
		return Task{}
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	return handle.currentLocked()
}

func (handle *Handle) currentLocked() Task {
	if handle == nil || handle.journal == nil || handle.projection == nil {
		return Task{}
	}
	task := cloneTask(handle.projection.task)
	task.Revision = uint64(handle.journal.Head().Cursor)
	return task
}

func (handle *Handle) Append(ctx context.Context, expectedRevision uint64, event Event) (Task, error) {
	if handle == nil {
		return Task{}, fmt.Errorf("comparison handle is nil")
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	return handle.appendLocked(ctx, expectedRevision, event)
}

func (handle *Handle) appendLocked(ctx context.Context, expectedRevision uint64, event Event) (Task, error) {
	if handle.journal == nil {
		return Task{}, fmt.Errorf("comparison handle is closed")
	}
	if err := handle.refreshLocked(ctx); err != nil {
		return Task{}, err
	}
	current := handle.currentLocked()
	if current.ID != "" && current.ID != handle.id {
		return Task{}, fmt.Errorf("comparison task identity changed")
	}
	if event.Kind != EventTaskCreated && current.ID == "" {
		return Task{}, ErrNotFound
	}
	if uint64(handle.journal.Head().Cursor) != expectedRevision {
		return Task{}, &RevisionConflictError{Expected: expectedRevision, Actual: uint64(handle.journal.Head().Cursor)}
	}
	if err := ValidateTransition(current, event); err != nil {
		if errors.Is(err, ErrStateConflict) || errors.Is(err, ErrRevisionConflict) {
			slog.WarnContext(ctx, "writing comparison state transition rejected", "task_id", handle.id, "candidate_id", event.CandidateID, "event", event.Kind, "error", err)
		}
		return Task{}, err
	}
	payload, err := marshalEvent(handle.id, event)
	if err != nil {
		return Task{}, err
	}
	commit, err := handle.journal.Append(ctx, conversationjournal.Guard{Cursor: conversationjournal.Cursor(expectedRevision)}, payload)
	if err != nil {
		if errors.Is(err, conversationjournal.ErrConflict) {
			slog.WarnContext(ctx, "writing comparison append CAS conflict", "task_id", handle.id, "candidate_id", event.CandidateID, "expected_revision", expectedRevision)
			return Task{}, &RevisionConflictError{Expected: expectedRevision, Actual: uint64(handle.journal.Head().Cursor)}
		}
		return Task{}, err
	}
	// Append applies the payload to the reducer. The reducer's state is the
	// canonical projection; only the revision comes from the physical cursor.
	task := cloneTask(handle.projection.task)
	task.Revision = uint64(commit.Head.Cursor)
	return task, nil
}

func (handle *Handle) refreshLocked(ctx context.Context) error {
	if handle.journal == nil {
		return fmt.Errorf("comparison handle is closed")
	}
	// A journal handle can be held over another process' append. Reopen before
	// validating the event so the reducer and CAS guard observe the same head.
	if handle.journal.Head().Cursor == 0 {
		return nil
	}
	freshProjection := newTaskProjection()
	freshJournal, err := conversationjournal.Open(ctx, handle.path, conversationjournal.Identity{ID: handle.id, Generation: "1"}, freshProjection, conversationjournal.Options{})
	if err != nil {
		return err
	}
	old := handle.journal
	handle.journal, handle.projection = freshJournal, freshProjection
	_ = old.Close()
	return nil
}

func (handle *Handle) Close() error {
	if handle == nil {
		return nil
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	if handle.journal == nil {
		return nil
	}
	err := handle.journal.Close()
	handle.journal = nil
	return err
}

func (store *Store) Get(ctx context.Context, id string) (Task, error) {
	handle, err := store.Open(ctx, id)
	if err != nil {
		return Task{}, err
	}
	defer handle.Close()
	task := handle.Current()
	if task.ID == "" {
		return Task{}, ErrNotFound
	}
	return task, nil
}

// OpenCurrent reads the rebuildable current-state projection for one task.
// It deliberately does not expose a write handle; callers that mutate a task
// must use Open/Append so the canonical journal remains the source of truth.
func (store *Store) OpenCurrent(ctx context.Context, id string) (Task, error) {
	if store == nil {
		return Task{}, fmt.Errorf("comparison store is nil")
	}
	if err := projectdomain.ValidateID(id); err != nil {
		return Task{}, ErrNotFound
	}
	path := store.journalPath(id)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return Task{}, ErrNotFound
	} else if err != nil {
		return Task{}, err
	}
	projection := newTaskProjection()
	journal, err := conversationjournal.OpenCurrent(ctx, path, conversationjournal.Identity{ID: id, Generation: "1"}, projection)
	if err != nil {
		return Task{}, err
	}
	defer journal.Close()
	if projection.task.ID == "" {
		return Task{}, ErrNotFound
	}
	task := cloneTask(projection.task)
	task.Revision = uint64(journal.Head().Cursor)
	return task, nil
}

func (store *Store) Append(ctx context.Context, id string, expectedRevision uint64, event Event) (Task, error) {
	handle, err := store.Open(ctx, id)
	if err != nil {
		return Task{}, err
	}
	defer handle.Close()
	return handle.Append(ctx, expectedRevision, event)
}

func (store *Store) List(ctx context.Context, options ListOptions) ([]TaskSummary, error) {
	if store == nil {
		return nil, fmt.Errorf("comparison store is nil")
	}
	entries, err := os.ReadDir(store.layout.ComparisonsDir())
	if errors.Is(err, os.ErrNotExist) {
		return []TaskSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]TaskSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := portablepath.Validate(entry.Name()); err != nil {
			continue
		}
		task, err := store.OpenCurrent(ctx, entry.Name())
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			slog.WarnContext(ctx, "writing comparison task skipped during list", "task_id", entry.Name(), "error", err)
			continue
		}
		if options.Phase != "" && task.Phase != options.Phase {
			continue
		}
		if !options.IncludeApplied && task.Phase == PhaseApplied {
			continue
		}
		result = append(result, task.Summary())
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

func marshalEvent(taskID string, event Event) (json.RawMessage, error) {
	if strings.TrimSpace(taskID) == "" || event.Kind == "" {
		return nil, ErrInvalidRequest
	}
	payload, err := marshalEventPayload(event)
	if err != nil {
		return nil, err
	}
	envelope := journalEnvelope{V: 1, Kind: event.Kind, TaskID: taskID, At: time.Now().UTC(), Payload: payload}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// marshalEventPayload omits the derived journal cursor from task.created. The
// cursor is assigned by the journal commit and must never become a second
// source of truth inside an event payload.
func marshalEventPayload(event Event) ([]byte, error) {
	payload, err := json.Marshal(event)
	if err != nil || event.Task == nil {
		return payload, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		return nil, err
	}
	var task map[string]json.RawMessage
	if raw := object["task"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &task); err != nil {
			return nil, err
		}
		delete(task, "revision")
		object["task"], err = json.Marshal(task)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(object)
}

type RevisionConflictError struct{ Expected, Actual uint64 }

func (err *RevisionConflictError) Error() string {
	return fmt.Sprintf("%v: expected=%d actual=%d", ErrRevisionConflict, err.Expected, err.Actual)
}
func (err *RevisionConflictError) Unwrap() error { return ErrRevisionConflict }

func AssetSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (store *Store) AssetPath(taskID, relative string) (string, error) {
	if err := projectdomain.ValidateID(taskID); err != nil {
		return "", err
	}
	if err := portablepath.Validate(relative); err != nil {
		return "", err
	}
	root := store.taskDir(taskID)
	if err := portablepath.CheckPath(root, relative); err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(relative)), nil
}

func WriteImmutableAsset(path string, content []byte) error {
	immutableAssetMu.Lock()
	defer immutableAssetMu.Unlock()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("asset path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil {
		if AssetSHA256(existing) == AssetSHA256(content) {
			return nil
		}
		return ErrAssetCorrupt
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".asset-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	written, writeErr := file.Write(content)
	if writeErr == nil && written != len(content) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		if existing, readErr := os.ReadFile(path); readErr == nil && AssetSHA256(existing) == AssetSHA256(content) {
			return nil
		}
		return err
	}
	cleanup = false
	return localfs.SyncDirectory(filepath.Dir(path))
}

func writeSeedJournal(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	written, err := file.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := localfs.SyncDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	keep = true
	return nil
}

func ReadVerifiedAsset(path, expectedSHA string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(expectedSHA) != "" && AssetSHA256(data) != expectedSHA {
		return nil, ErrAssetCorrupt
	}
	return data, nil
}
