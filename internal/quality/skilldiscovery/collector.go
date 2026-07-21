package skilldiscovery

import (
	"bytes"
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

// EvidenceCollectionOptions configures local-only evidence collection for already matched candidates.
type EvidenceCollectionOptions struct {
	CollectorOptions
	CommentPageSize int
}

// SkillDetail is the non-identity detail used while calculating evidence.
type SkillDetail struct {
	SkillRecord
	WeightedScore  float64 `json:"weighted_score"`
	SecurityReport string  `json:"security_report"`
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
		pageURL := catalogPageURL(baseURL, options.PageSize, page)
		key := pageURL.String()
		payload, receipt, readErr := cache.ReadPage(catalogPageKind, key)
		if readErr != nil {
			payload, receipt, readErr = collector.fetchPage(ctx, pageURL, options, &lastRequest)
			if readErr != nil {
				if receipt.URL == "" {
					receipt = requestFailureReceipt(pageURL.String(), collector.now(), readErr)
				}
				receipt.Kind, receipt.Key, receipt.Error = catalogPageKind, key, safeError(readErr)
				manifest.Status = SnapshotPartial
				manifest.Pages = append(manifest.Pages, receipt)
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
			receipt.Error = safeError(err)
			manifest.Status = SnapshotPartial
			manifest.Pages = append(manifest.Pages, receipt)
			manifest.Failures = append(manifest.Failures, SnapshotFailure{Kind: catalogPageKind, Key: key, Disposition: "invalid-response", Message: safeError(err)})
			result, finishErr := collector.finishSnapshot(cache, manifest, records)
			return result, errors.Join(fmt.Errorf("normalize catalog page %d: %w", page, err), finishErr)
		}
		if len(response.Skills) > 0 {
			receipt.ItemCount = len(response.Skills)
			if _, seen := pageHashes[receipt.SHA256]; seen {
				receipt.Error = "repeated nonempty page hash"
				manifest.Status = SnapshotPartial
				manifest.Pages = append(manifest.Pages, receipt)
				manifest.Failures = append(manifest.Failures, SnapshotFailure{Kind: catalogPageKind, Key: key, Disposition: "repeated-page", Message: "repeated nonempty page hash"})
				result, finishErr := collector.finishSnapshot(cache, manifest, records)
				return result, errors.Join(fmt.Errorf("catalog page %d repeats a nonempty page", page), finishErr)
			}
			pageHashes[receipt.SHA256] = struct{}{}
		}
		if err := ValidateSkillRecords(response.Skills); err != nil {
			receipt.Error = safeError(err)
			manifest.Status = SnapshotPartial
			manifest.Pages = append(manifest.Pages, receipt)
			manifest.Failures = append(manifest.Failures, SnapshotFailure{Kind: catalogPageKind, Key: key, Disposition: "invalid-record", Message: safeError(err)})
			result, finishErr := collector.finishSnapshot(cache, manifest, records)
			return result, errors.Join(err, finishErr)
		}
		for _, record := range response.Skills {
			records[record.ID] = record
		}
		if receipt.ItemCount == 0 {
			receipt.ItemCount = len(response.Skills)
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
	fields, err := catalogObjectFields(payload)
	if err != nil {
		return catalogPage{}, err
	}
	if successRaw, enveloped := fields["success"]; enveloped {
		var success bool
		if isNullJSON(successRaw) || json.Unmarshal(successRaw, &success) != nil {
			return catalogPage{}, fmt.Errorf("catalog envelope has invalid success field")
		}
		if !success {
			return catalogPage{}, fmt.Errorf("catalog envelope reports unsuccessful response")
		}
		dataRaw, exists := fields["data"]
		if !exists || isNullJSON(dataRaw) {
			return catalogPage{}, fmt.Errorf("successful catalog envelope requires a data object")
		}
		return decodeCatalogObject(dataRaw)
	}
	return decodeCatalogFields(fields)
}

func decodeCatalogObject(payload []byte) (catalogPage, error) {
	fields, err := catalogObjectFields(payload)
	if err != nil {
		return catalogPage{}, fmt.Errorf("successful catalog envelope requires a data object: %w", err)
	}
	return decodeCatalogFields(fields)
}

func catalogObjectFields(payload []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("catalog payload must be a JSON object")
	}
	return fields, nil
}

func decodeCatalogFields(fields map[string]json.RawMessage) (catalogPage, error) {
	skillsRaw, skillsPresent := fields["skills"]
	totalRaw, totalPresent := fields["total"]
	hasMoreRaw, hasMorePresent := fields["hasMore"]
	if !skillsPresent || !totalPresent || !hasMorePresent || isNullJSON(skillsRaw) || isNullJSON(totalRaw) || isNullJSON(hasMoreRaw) {
		return catalogPage{}, fmt.Errorf("catalog payload requires skills, total, and hasMore")
	}
	var page catalogPage
	if err := json.Unmarshal(skillsRaw, &page.Skills); err != nil || page.Skills == nil {
		return catalogPage{}, fmt.Errorf("catalog skills must be an array")
	}
	if err := json.Unmarshal(totalRaw, &page.Total); err != nil || page.Total < 0 {
		return catalogPage{}, fmt.Errorf("catalog total must be a non-negative integer")
	}
	if err := json.Unmarshal(hasMoreRaw, &page.HasMore); err != nil {
		return catalogPage{}, fmt.Errorf("catalog hasMore must be a boolean")
	}
	return page, nil
}

func isNullJSON(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

func requestFailureReceipt(target string, capturedAt time.Time, err error) PageReceipt {
	return PageReceipt{URL: target, HTTPStatus: 0, CapturedAt: capturedAt.UTC().Format(time.RFC3339), SHA256: payloadSHA256(nil), ItemCount: 0, Error: safeError(err)}
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
	if err != nil || baseURL.Scheme != "https" || baseURL.Hostname() == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.ForceQuery || baseURL.Fragment != "" || baseURL.RawFragment != "" || (baseURL.Path != "" && baseURL.Path != "/") || baseURL.RawPath != "" {
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

// CollectCandidateEvidence checkpoints raw detail and comment responses locally and returns only normalized values.
func (collector *Collector) CollectCandidateEvidence(ctx context.Context, options EvidenceCollectionOptions, candidates []CandidateRecord) (map[string]SkillDetail, map[string][]ReviewRecord, []SnapshotFailure, error) {
	if options.CommentPageSize <= 0 {
		return nil, nil, nil, fmt.Errorf("comment page size must be positive")
	}
	base, err := validateCatalogOptions(options.CollectorOptions)
	if err != nil {
		return nil, nil, nil, err
	}
	cache := Cache{Root: options.CacheRoot}
	if err := cache.Initialize(); err != nil {
		return nil, nil, nil, err
	}
	details, comments, failures := map[string]SkillDetail{}, map[string][]ReviewRecord{}, []SnapshotFailure{}
	var lastRequest time.Time
	for _, candidate := range coalesceCandidates(candidates) {
		if !safeCandidateID(candidate.Skill.ID) {
			return details, comments, failures, fmt.Errorf("invalid candidate id")
		}
		if err := ctx.Err(); err != nil {
			return details, comments, failures, err
		}
		detailURL := evidenceDetailURL(base, candidate.Skill.ID)
		payload, receipt, readErr := cache.ReadPage("skill-detail", detailURL.String())
		if readErr != nil {
			payload, receipt, readErr = collector.fetchPage(ctx, detailURL, options.CollectorOptions, &lastRequest)
			if readErr == nil {
				receipt.Kind, receipt.Key = "skill-detail", detailURL.String()
				readErr = cache.WritePage("skill-detail", detailURL.String(), payload, receipt)
			}
		}
		if readErr != nil {
			failures = append(failures, evidenceFailure("skill-detail", detailURL.String(), readErr))
			continue
		}
		detail, err := normalizeSkillDetail(payload, candidate.Skill)
		if err != nil {
			failures = append(failures, evidenceFailure("skill-detail", detailURL.String(), err))
			continue
		}
		pageHashes := map[string]struct{}{}
		collected := []ReviewRecord{}
		complete := true
		for page := 1; ; page++ {
			pageURL := evidenceCommentsURL(base, candidate.Skill.ID, options.CommentPageSize, page)
			payload, receipt, readErr = cache.ReadPage("skill-comments", pageURL.String())
			if readErr != nil {
				payload, receipt, readErr = collector.fetchPage(ctx, pageURL, options.CollectorOptions, &lastRequest)
				if readErr == nil {
					receipt.Kind, receipt.Key = "skill-comments", pageURL.String()
					readErr = cache.WritePage("skill-comments", pageURL.String(), payload, receipt)
				}
			}
			if readErr != nil {
				failures = append(failures, evidenceFailure("skill-comments", pageURL.String(), readErr))
				complete = false
				break
			}
			response, err := normalizeCommentPage(payload)
			if err != nil {
				failures = append(failures, evidenceFailure("skill-comments", pageURL.String(), err))
				complete = false
				break
			}
			if len(response.Reviews) > 0 {
				if _, seen := pageHashes[receipt.SHA256]; seen {
					failures = append(failures, evidenceFailure("skill-comments", pageURL.String(), fmt.Errorf("repeated nonempty comments page")))
					complete = false
					break
				}
				pageHashes[receipt.SHA256] = struct{}{}
			}
			if response.HasMore && len(response.Reviews) == 0 {
				failures = append(failures, evidenceFailure("skill-comments", pageURL.String(), fmt.Errorf("empty comments page reports hasMore")))
				complete = false
				break
			}
			collected = append(collected, response.Reviews...)
			if !response.HasMore {
				break
			}
		}
		if complete {
			details[candidate.Skill.ID] = detail
			comments[candidate.Skill.ID] = collected
		} else {
			delete(details, candidate.Skill.ID)
			delete(comments, candidate.Skill.ID)
		}
	}
	return details, comments, failures, nil
}

func evidenceFailure(kind, key string, err error) SnapshotFailure {
	return SnapshotFailure{Kind: kind, Key: key, Disposition: "evidence-cache-missing", Message: safeError(err)}
}
func safeCandidateID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func evidenceDetailURL(base *urlpkg.URL, id string) *urlpkg.URL {
	target := *base
	target.Path = strings.TrimRight(base.Path, "/") + "/api/skills/" + urlpkg.PathEscape(id)
	return &target
}
func evidenceCommentsURL(base *urlpkg.URL, id string, limit, page int) *urlpkg.URL {
	target := evidenceDetailURL(base, id)
	target.Path += "/comments"
	query := target.Query()
	query.Set("limit", strconv.Itoa(limit))
	query.Set("page", strconv.Itoa(page))
	target.RawQuery = query.Encode()
	return target
}

func normalizeSkillDetail(payload []byte, fallback SkillRecord) (SkillDetail, error) {
	fields, err := catalogObjectFields(payload)
	if err != nil {
		return SkillDetail{}, err
	}
	if success, ok := fields["success"]; ok {
		var value bool
		if json.Unmarshal(success, &value) != nil || !value {
			return SkillDetail{}, fmt.Errorf("detail envelope reports unsuccessful response")
		}
		fields, err = catalogObjectFields(fields["data"])
		if err != nil {
			return SkillDetail{}, err
		}
	}
	if _, hasID := fields["id"]; !hasID {
		if data, ok := fields["data"]; ok {
			fields, err = catalogObjectFields(data)
			if err != nil {
				return SkillDetail{}, err
			}
		}
	}
	var detail SkillDetail
	encoded, err := json.Marshal(fields)
	if err != nil {
		return SkillDetail{}, err
	}
	if err := json.Unmarshal(encoded, &detail); err != nil {
		return SkillDetail{}, err
	}
	if detail.ID == "" || detail.ID != fallback.ID {
		return SkillDetail{}, fmt.Errorf("detail id does not match requested candidate")
	}
	if detail.Name == "" {
		detail.Name = fallback.Name
	}
	if detail.Description == "" {
		detail.Description = fallback.Description
	}
	if detail.OwnerID == "" {
		detail.OwnerID = fallback.OwnerID
	}
	return detail, nil
}

type commentPage struct {
	Reviews []ReviewRecord
	HasMore bool
}

func normalizeCommentPage(payload []byte) (commentPage, error) {
	fields, err := catalogObjectFields(payload)
	if err != nil {
		return commentPage{}, err
	}
	if success, ok := fields["success"]; ok {
		var value bool
		if json.Unmarshal(success, &value) != nil || !value {
			return commentPage{}, fmt.Errorf("comments envelope reports unsuccessful response")
		}
		fields, err = catalogObjectFields(fields["data"])
		if err != nil {
			return commentPage{}, err
		}
	}
	raw, ok := fields["items"]
	if !ok {
		raw = fields["comments"]
	}
	if raw == nil {
		return commentPage{}, fmt.Errorf("comments payload requires items")
	}
	var api []apiReview
	if err := json.Unmarshal(raw, &api); err != nil {
		return commentPage{}, fmt.Errorf("comments must be an array")
	}
	reviews := make([]ReviewRecord, 0, len(api))
	for _, review := range api {
		reviews = append(reviews, normalizeAPIReview(review))
	}
	rawMore, ok := fields["hasMore"]
	if !ok || isNullJSON(rawMore) {
		return commentPage{}, fmt.Errorf("comments payload requires hasMore")
	}
	more := false
	if err := json.Unmarshal(rawMore, &more); err != nil {
		return commentPage{}, fmt.Errorf("comments hasMore must be boolean")
	}
	return commentPage{Reviews: reviews, HasMore: more}, nil
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
