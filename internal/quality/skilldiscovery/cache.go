package skilldiscovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const cacheOwnershipMarker = "denova.quality-eval.xiaping-cache/v1"

// Cache owns the local-only Xiaping collection checkpoints.
type Cache struct{ Root string }

type cachedPage struct {
	Payload []byte      `json:"payload"`
	Receipt PageReceipt `json:"receipt"`
}

// DefaultCacheRoot returns the user-local cache root for Xiaping snapshots.
func DefaultCacheRoot() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("get user cache directory: %w", err)
	}
	return filepath.Join(root, "denova", "quality-eval", "xiaping"), nil
}

// Initialize creates the cache and records its ownership contract.
func (cache Cache) Initialize() error {
	if cache.Root == "" {
		return fmt.Errorf("cache root is required")
	}
	info, err := os.Stat(cache.Root)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("inspect cache root: %w", err)
		}
		if err := os.MkdirAll(cache.Root, 0o700); err != nil {
			return fmt.Errorf("create cache root: %w", err)
		}
	} else if !info.IsDir() {
		return fmt.Errorf("cache root is not a directory")
	}
	markerPath := filepath.Join(cache.Root, "OWNER")
	marker, markerErr := os.ReadFile(markerPath)
	if markerErr == nil {
		if string(marker) != cacheOwnershipMarker+"\n" {
			return fmt.Errorf("cache ownership marker mismatch")
		}
	} else if !os.IsNotExist(markerErr) {
		return fmt.Errorf("read cache ownership marker: %w", markerErr)
	} else {
		entries, err := os.ReadDir(cache.Root)
		if err != nil {
			return fmt.Errorf("inspect cache root contents: %w", err)
		}
		if len(entries) != 0 {
			return fmt.Errorf("cache ownership marker is missing from a non-empty root")
		}
		if err := writeCacheFile(markerPath, []byte(cacheOwnershipMarker+"\n")); err != nil {
			return fmt.Errorf("write cache ownership marker: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(cache.Root, "pages"), 0o700); err != nil {
		return fmt.Errorf("create cache pages directory: %w", err)
	}
	return nil
}

// ReadPage loads a checkpointed page and verifies its receipt hash.
func (cache Cache) ReadPage(kind, key string) ([]byte, PageReceipt, error) {
	data, err := os.ReadFile(cache.pagePath(kind, key))
	if err != nil {
		return nil, PageReceipt{}, fmt.Errorf("read cached page: %w", err)
	}
	var page cachedPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, PageReceipt{}, fmt.Errorf("decode cached page: %w", err)
	}
	if page.Receipt.Kind != kind || page.Receipt.Key != key || page.Receipt.SHA256 != payloadSHA256(page.Payload) {
		return nil, PageReceipt{}, fmt.Errorf("cached page checkpoint failed validation")
	}
	return page.Payload, page.Receipt, nil
}

// WritePage atomically persists one raw public response and its receipt.
func (cache Cache) WritePage(kind, key string, payload []byte, receipt PageReceipt) error {
	if err := cache.Initialize(); err != nil {
		return err
	}
	if receipt.Kind != kind || receipt.Key != key || receipt.SHA256 != payloadSHA256(payload) {
		return fmt.Errorf("cached page receipt failed validation")
	}
	encoded, err := json.Marshal(cachedPage{Payload: payload, Receipt: receipt})
	if err != nil {
		return fmt.Errorf("encode cached page: %w", err)
	}
	return writeCacheFile(cache.pagePath(kind, key), encoded)
}

// WriteLocalSnapshot atomically persists the latest complete or partial result.
func (cache Cache) WriteLocalSnapshot(snapshot LocalSnapshot) error {
	if err := cache.Initialize(); err != nil {
		return err
	}
	if err := ValidateSnapshotManifest(snapshot.Manifest); err != nil {
		return fmt.Errorf("validate local snapshot: %w", err)
	}
	if err := ValidateSkillRecords(snapshot.Skills); err != nil {
		return fmt.Errorf("validate local snapshot skills: %w", err)
	}
	if snapshot.Manifest.SkillRecordsSHA256 != StableSHA256(snapshot.Skills) {
		return fmt.Errorf("local snapshot skill hash does not match records")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode local snapshot: %w", err)
	}
	return writeCacheFile(filepath.Join(cache.Root, "snapshot.json"), encoded)
}

// LoadLocalSnapshot loads the last persisted collection result.
func (cache Cache) LoadLocalSnapshot() (LocalSnapshot, error) {
	var snapshot LocalSnapshot
	if err := LoadStrictJSON(filepath.Join(cache.Root, "snapshot.json"), &snapshot); err != nil {
		return LocalSnapshot{}, fmt.Errorf("load local snapshot: %w", err)
	}
	if err := ValidateSnapshotManifest(snapshot.Manifest); err != nil {
		return LocalSnapshot{}, err
	}
	if err := ValidateSkillRecords(snapshot.Skills); err != nil {
		return LocalSnapshot{}, err
	}
	if snapshot.Manifest.SkillRecordsSHA256 != StableSHA256(snapshot.Skills) {
		return LocalSnapshot{}, fmt.Errorf("local snapshot skill hash does not match records")
	}
	return snapshot, nil
}

func (cache Cache) pagePath(kind, key string) string {
	digest := sha256.Sum256([]byte(kind + "\n" + key))
	return filepath.Join(cache.Root, "pages", hex.EncodeToString(digest[:])+".json")
}

func payloadSHA256(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func writeCacheFile(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".xiaping-*")
	if err != nil {
		return fmt.Errorf("create cache temp file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write cache temp file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync cache temp file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close cache temp file: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("rename cache temp file: %w", err)
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open cache directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync cache directory: %w", err)
	}
	return nil
}
