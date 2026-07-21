package skilldiscovery

import (
	"context"
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
}

func TestCollectCatalogMarksPartialAfterNonRetryablePage(t *testing.T) {
	collector, options := failingPageCollector(t, http.StatusBadRequest)
	got, err := collector.CollectCatalog(context.Background(), options)
	if err == nil || got.Manifest.Status != SnapshotPartial || len(got.Manifest.Failures) != 1 {
		t.Fatalf("result=%+v error=%v", got.Manifest, err)
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
