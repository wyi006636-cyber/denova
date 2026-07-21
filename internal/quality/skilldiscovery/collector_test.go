package skilldiscovery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestCollectCatalogResumesWithoutRefetchingCachedPage(t *testing.T) {
	server, calls := newCatalogTLSServer(t)
	collector := NewCollector(server.Client(), fixedClock())
	opts := CollectorOptions{BaseURL: server.URL, CacheRoot: t.TempDir(), PageSize: 2, MaxRetryDelay: time.Millisecond}

	first, err := collector.CollectCatalog(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	before := calls[1]
	second, err := collector.CollectCatalog(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if calls[1] != before || second.Manifest.SnapshotID != first.Manifest.SnapshotID {
		t.Fatalf("resume refetched page 1 or changed snapshot: calls=%v", calls)
	}
	if got, want := len(second.Skills), 3; got != want {
		t.Fatalf("skills = %d, want %d", got, want)
	}
	if got, want := second.Manifest.Pages[0].ItemCount, 2; got != want {
		t.Fatalf("page 1 item count = %d, want %d", got, want)
	}
}

func TestCollectCatalogDoesNotReuseCacheAcrossOrigins(t *testing.T) {
	first, firstCalls := singlePageTLSServer(t, "first-skill")
	second, secondCalls := singlePageTLSServer(t, "second-skill")
	cacheRoot := t.TempDir()
	collector := NewCollector(first.Client(), fixedClock())
	options := CollectorOptions{BaseURL: first.URL, CacheRoot: cacheRoot, PageSize: 2}
	if _, err := collector.CollectCatalog(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	collector = NewCollector(second.Client(), fixedClock())
	options.BaseURL = second.URL
	got, err := collector.CollectCatalog(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if firstCalls() != 1 || secondCalls() != 1 || len(got.Skills) != 1 || got.Skills[0].ID != "second-skill" {
		t.Fatalf("calls=(%d,%d), skills=%+v; cached bytes crossed origins", firstCalls(), secondCalls(), got.Skills)
	}
}

func TestValidateCatalogOptionsRejectsCredentialBearingOrAmbiguousBaseURL(t *testing.T) {
	for _, rawURL := range []string{
		"https://example.test?access_token=synthetic-secret",
		"https://example.test?X-Amz-Signature=synthetic-signature",
		"https://user:password@example.test",
		"https://example.test#fragment",
	} {
		options := CollectorOptions{BaseURL: rawURL, CacheRoot: t.TempDir(), PageSize: 1, RetryAttempts: 1}
		if _, err := validateCatalogOptions(options); err == nil {
			t.Fatalf("validateCatalogOptions(%q) accepted a credential-bearing or ambiguous base URL", rawURL)
		}
	}
	valid := CollectorOptions{BaseURL: "https://example.test", CacheRoot: t.TempDir(), PageSize: 1, RetryAttempts: 1}
	if _, err := validateCatalogOptions(valid); err != nil {
		t.Fatalf("validateCatalogOptions(valid root) = %v", err)
	}
}

func TestCollectCatalogMarksPartialAfterNonRetryablePage(t *testing.T) {
	collector, options := failingPageCollector(t, http.StatusBadRequest)
	got, err := collector.CollectCatalog(context.Background(), options)
	if err == nil || got.Manifest.Status != SnapshotPartial || len(got.Manifest.Failures) != 1 {
		t.Fatalf("result=%+v error=%v", got.Manifest, err)
	}
	if gotReceipt := got.Manifest.Pages; len(gotReceipt) != 1 || gotReceipt[0].HTTPStatus != http.StatusBadRequest || gotReceipt[0].Error == "" {
		t.Fatalf("failure receipt = %+v, want HTTP failure receipt", gotReceipt)
	}
}

func TestCollectCatalogRecordsReceiptsForResponseFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		body func(page int) string
	}{
		{"invalid JSON", func(int) string { return "{" }},
		{"invalid record", func(int) string { return `{"skills":[{"id":""}],"total":1,"hasMore":false}` }},
		{"repeated page", func(int) string { return `{"skills":[{"id":"same-skill"}],"total":2,"hasMore":true}` }},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, test.body(pageNumber(t, r))) }))
			t.Cleanup(server.Close)
			collector := NewCollector(server.Client(), fixedClock())
			got, err := collector.CollectCatalog(context.Background(), CollectorOptions{BaseURL: server.URL, CacheRoot: t.TempDir(), PageSize: 1, RetryAttempts: 1})
			if err == nil || got.Manifest.Status != SnapshotPartial || len(got.Manifest.Pages) == 0 {
				t.Fatalf("snapshot=%+v error=%v", got.Manifest, err)
			}
			receipt := got.Manifest.Pages[len(got.Manifest.Pages)-1]
			if receipt.HTTPStatus != http.StatusOK || receipt.Error == "" || receipt.SHA256 == "" || receipt.URL == "" {
				t.Fatalf("failure receipt = %+v", receipt)
			}
		})
	}
}

func TestCollectCatalogRecordsHonestReceiptForRequestFailure(t *testing.T) {
	collector := NewCollector(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic transport failure") })}, fixedClock())
	got, err := collector.CollectCatalog(context.Background(), CollectorOptions{BaseURL: "https://example.test", CacheRoot: t.TempDir(), PageSize: 1, RetryAttempts: 1, MaxRetryDelay: time.Millisecond})
	if err == nil || len(got.Manifest.Pages) != 1 {
		t.Fatalf("snapshot=%+v error=%v", got.Manifest, err)
	}
	receipt := got.Manifest.Pages[0]
	if receipt.HTTPStatus != 0 || receipt.Error == "" || receipt.SHA256 != payloadSHA256(nil) || receipt.ItemCount != 0 {
		t.Fatalf("request failure receipt = %+v", receipt)
	}
	if err := ValidateSnapshotManifest(got.Manifest); err != nil {
		t.Fatalf("ValidateSnapshotManifest() = %v", err)
	}
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC) }
}

func newCatalogTLSServer(t *testing.T) (*httptest.Server, map[int]int) {
	t.Helper()
	calls := map[int]int{}
	pageTwoAttempts := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/skills" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		calls[page]++
		w.Header().Set("Content-Type", "application/json")
		if page == 1 {
			fmt.Fprint(w, `{"success":true,"data":{"skills":[{"id":"00000000-0000-0000-0000-000000000001","name":"小说助手"},{"id":"00000000-0000-0000-0000-000000000002","name":"对白助手"}],"total":3,"hasMore":true}}`)
			return
		}
		pageTwoAttempts++
		if pageTwoAttempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"skills":[{"id":"00000000-0000-0000-0000-000000000003","name":"大纲助手"}],"total":3,"hasMore":false}`)
	}))
	t.Cleanup(server.Close)
	return server, calls
}

func failingPageCollector(t *testing.T, status int) (*Collector, CollectorOptions) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	t.Cleanup(server.Close)
	return NewCollector(server.Client(), fixedClock()), CollectorOptions{BaseURL: server.URL, CacheRoot: t.TempDir(), PageSize: 2, RetryAttempts: 1, MaxRetryDelay: time.Millisecond}
}

func singlePageTLSServer(t *testing.T, id string) (*httptest.Server, func() int) {
	t.Helper()
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		fmt.Fprintf(w, `{"skills":[{"id":%q}],"total":1,"hasMore":false}`, id)
	}))
	t.Cleanup(server.Close)
	return server, func() int { return calls }
}

func pageNumber(t *testing.T, request *http.Request) int {
	t.Helper()
	page, err := strconv.Atoi(request.URL.Query().Get("page"))
	if err != nil {
		t.Fatal(err)
	}
	return page
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }
