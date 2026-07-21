package skilldiscovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"denova/internal/skills"
)

const (
	snapshotContract     = "denova.xiaping-snapshot-manifest"
	snapshotVersion      = "v1"
	normalizationVersion = "v1"
	catalogPageKind      = "catalog"
)

// CollectorOptions configure a public, resumable Xiaping catalog request.
type CollectorOptions struct {
	BaseURL       string
	CacheRoot     string
	PageSize      int
	MinInterval   time.Duration
	RetryAttempts int
	MaxRetryDelay time.Duration
}

// Collector retrieves public catalog pages through a restricted HTTP client.
type Collector struct {
	client *http.Client
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
}

// NewCollector creates a collector. A nil client uses the shared restricted transport.
func NewCollector(client *http.Client, now func() time.Time) *Collector {
	if client == nil {
		client = skills.NewRestrictedRemoteHTTPClient()
	}
	if now == nil {
		now = time.Now
	}
	return &Collector{client: client, now: now, sleep: sleepContext}
}

// CollectCatalog obtains and checkpoints every page in the public Skill catalog.
func (collector *Collector) CollectCatalog(ctx context.Context, options CollectorOptions) (LocalSnapshot, error) {
	if options.RetryAttempts == 0 {
		options.RetryAttempts = 3
	}
	baseURL, err := validateCatalogOptions(options)
	if err != nil {
		return LocalSnapshot{}, err
	}
	cache := Cache{Root: options.CacheRoot}
	if err := cache.Initialize(); err != nil {
		return LocalSnapshot{}, err
	}
	started := collector.now().UTC()
	manifest := SnapshotManifest{
		Contract: snapshotContract, Version: snapshotVersion, Status: SnapshotComplete,
		StartedAt: started.Format(time.RFC3339), BaseURL: baseURL.String(),
		NormalizationVersion: normalizationVersion,
	}
	if previous, err := cache.LoadLocalSnapshot(); err == nil {
		manifest.PreviousSnapshotSHA256 = StableSHA256(previous.Manifest)
	} else if !os.IsNotExist(unwrapPathError(err)) {
		return LocalSnapshot{}, err
	}

	records := make(map[string]SkillRecord)
	pageHashes := make(map[string]struct{})
	var lastRequest time.Time
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return LocalSnapshot{}, err
		}
		key := strconv.Itoa(page)
		payload, receipt, readErr := cache.ReadPage(catalogPageKind, key)
		if readErr != nil {
			pageURL := catalogPageURL(baseURL, options.PageSize, page)
			payload, receipt, readErr = collector.fetchPage(ctx, pageURL, options, &lastRequest)
			if readErr != nil {
				receipt.Kind, receipt.Key, receipt.Error = catalogPageKind, key, safeError(readErr)
				manifest.Status = SnapshotPartial
				if receipt.HTTPStatus >= 100 && receipt.HTTPStatus <= 599 {
					manifest.Pages = append(manifest.Pages, receipt)
				}
				manifest.Failures = append(manifest.Failures, SnapshotFailure{Kind: catalogPageKind, Key: key, Disposition: "request-failed", Message: safeError(readErr)})
				result, finishErr := collector.finishSnapshot(cache, manifest, records)
				return result, errors.Join(fmt.Errorf("collect catalog page %d: %w", page, readErr), finishErr)
			}
			receipt.Kind, receipt.Key = catalogPageKind, key
			if err := cache.WritePage(catalogPageKind, key, payload, receipt); err != nil {
				return LocalSnapshot{}, err
			}
		}
		response, err := normalizeCatalogPage(payload)
		if err != nil {
			manifest.Status = SnapshotPartial
			manifest.Failures = append(manifest.Failures, SnapshotFailure{Kind: catalogPageKind, Key: key, Disposition: "invalid-response", Message: safeError(err)})
			result, finishErr := collector.finishSnapshot(cache, manifest, records)
			return result, errors.Join(fmt.Errorf("normalize catalog page %d: %w", page, err), finishErr)
		}
		if len(response.Skills) > 0 {
			if _, seen := pageHashes[receipt.SHA256]; seen {
				manifest.Status = SnapshotPartial
				manifest.Failures = append(manifest.Failures, SnapshotFailure{Kind: catalogPageKind, Key: key, Disposition: "repeated-page", Message: "repeated nonempty page hash"})
				result, finishErr := collector.finishSnapshot(cache, manifest, records)
				return result, errors.Join(fmt.Errorf("catalog page %d repeats a nonempty page", page), finishErr)
			}
			pageHashes[receipt.SHA256] = struct{}{}
		}
		if err := ValidateSkillRecords(response.Skills); err != nil {
			manifest.Status = SnapshotPartial
			manifest.Failures = append(manifest.Failures, SnapshotFailure{Kind: catalogPageKind, Key: key, Disposition: "invalid-record", Message: safeError(err)})
			result, finishErr := collector.finishSnapshot(cache, manifest, records)
			return result, errors.Join(err, finishErr)
		}
		for _, record := range response.Skills {
			records[record.ID] = record
		}
		manifest.Pages = append(manifest.Pages, receipt)
		manifest.ReportedTotal = response.Total
		if !response.HasMore {
			return collector.finishSnapshot(cache, manifest, records)
		}
	}
}

type catalogPage struct {
	Skills  []SkillRecord `json:"skills"`
	Total   int           `json:"total"`
	HasMore bool          `json:"hasMore"`
}

func normalizeCatalogPage(payload []byte) (catalogPage, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return catalogPage{}, fmt.Errorf("decode catalog JSON: %w", err)
	}
	decoded := payload
	if len(envelope.Data) != 0 && string(envelope.Data) != "null" {
		decoded = envelope.Data
	}
	var page catalogPage
	if err := json.Unmarshal(decoded, &page); err != nil {
		return catalogPage{}, fmt.Errorf("decode normalized catalog response: %w", err)
	}
	if page.Total < 0 {
		return catalogPage{}, fmt.Errorf("catalog total must not be negative")
	}
	return page, nil
}

func (collector *Collector) fetchPage(ctx context.Context, target *urlpkg.URL, options CollectorOptions, lastRequest *time.Time) ([]byte, PageReceipt, error) {
	for retry := 0; retry <= options.RetryAttempts; retry++ {
		if !lastRequest.IsZero() && options.MinInterval > 0 {
			remaining := options.MinInterval - collector.now().Sub(*lastRequest)
			if remaining > 0 {
				if err := collector.sleep(ctx, remaining); err != nil {
					return nil, PageReceipt{}, err
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, PageReceipt{}, err
		}
		*lastRequest = collector.now()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return nil, PageReceipt{}, err
		}
		req.Header.Set("Accept", "application/json")
		response, err := collector.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, PageReceipt{}, ctx.Err()
			}
			if retry == options.RetryAttempts {
				return nil, PageReceipt{}, fmt.Errorf("request catalog page: %w", err)
			}
			if err := collector.sleep(ctx, retryDelay(retry, options.MaxRetryDelay)); err != nil {
				return nil, PageReceipt{}, err
			}
			continue
		}
		payload, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			return nil, PageReceipt{}, fmt.Errorf("read catalog response: %w", readErr)
		}
		receipt := PageReceipt{URL: target.String(), HTTPStatus: response.StatusCode, CapturedAt: collector.now().UTC().Format(time.RFC3339), SHA256: payloadSHA256(payload)}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return payload, receipt, nil
		}
		if !retryableStatus(response.StatusCode) || retry == options.RetryAttempts {
			return nil, receipt, fmt.Errorf("catalog request returned HTTP %d", response.StatusCode)
		}
		delay := retryDelay(retry, options.MaxRetryDelay)
		if response.StatusCode == http.StatusTooManyRequests {
			if retryAfter, ok := parseRetryAfter(response.Header.Get("Retry-After"), collector.now()); ok {
				delay = retryAfter
			}
		}
		if err := collector.sleep(ctx, delay); err != nil {
			return nil, PageReceipt{}, err
		}
	}
	return nil, PageReceipt{}, fmt.Errorf("catalog retry loop exhausted")
}

func (collector *Collector) finishSnapshot(cache Cache, manifest SnapshotManifest, records map[string]SkillRecord) (LocalSnapshot, error) {
	snapshot := LocalSnapshot{Manifest: manifest}
	for _, record := range records {
		snapshot.Skills = append(snapshot.Skills, record)
	}
	sort.Slice(snapshot.Skills, func(i, j int) bool { return snapshot.Skills[i].ID < snapshot.Skills[j].ID })
	snapshot.Manifest.UniqueSkills = len(snapshot.Skills)
	snapshot.Manifest.SkillRecordsSHA256 = StableSHA256(snapshot.Skills)
	snapshot.Manifest.CompletedAt = collector.now().UTC().Format(time.RFC3339)
	snapshot.Manifest.SnapshotID = "snapshot-" + strings.TrimPrefix(StableSHA256(struct {
		BaseURL string
		Records []SkillRecord
	}{snapshot.Manifest.BaseURL, snapshot.Skills}), "sha256:")[:16]
	if err := cache.WriteLocalSnapshot(snapshot); err != nil {
		return snapshot, fmt.Errorf("persist local snapshot: %w", err)
	}
	return snapshot, nil
}

func validateCatalogOptions(options CollectorOptions) (*urlpkg.URL, error) {
	baseURL, err := urlpkg.Parse(strings.TrimSpace(options.BaseURL))
	if err != nil || baseURL.Scheme != "https" || baseURL.Hostname() == "" || baseURL.User != nil {
		return nil, fmt.Errorf("catalog base URL must be an absolute https:// URL")
	}
	if options.CacheRoot == "" || options.PageSize <= 0 || options.RetryAttempts <= 0 || options.MinInterval < 0 || options.MaxRetryDelay < 0 {
		return nil, fmt.Errorf("invalid catalog collector options")
	}
	return baseURL, nil
}

func catalogPageURL(base *urlpkg.URL, limit, page int) *urlpkg.URL {
	target := *base
	target.Path = strings.TrimRight(base.Path, "/") + "/api/skills"
	query := target.Query()
	query.Set("limit", strconv.Itoa(limit))
	query.Set("page", strconv.Itoa(page))
	target.RawQuery = query.Encode()
	return &target
}
func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500 && status <= 599
}
func retryDelay(retry int, cap time.Duration) time.Duration {
	delay := time.Second << retry
	if cap > 0 && delay > cap {
		return cap
	}
	return delay
}
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := when.Sub(now); delay > 0 {
			return delay, true
		}
		return 0, true
	}
	return 0, false
}
func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func safeError(err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(err.Error(), "\n", " ")
}
func unwrapPathError(err error) error {
	for {
		if pathErr, ok := err.(*os.PathError); ok {
			return pathErr
		}
		unwrap, ok := err.(interface{ Unwrap() error })
		if !ok || unwrap.Unwrap() == nil {
			return err
		}
		err = unwrap.Unwrap()
	}
}
