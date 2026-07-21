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

func TestNormalizeCatalogPageDistinguishesDirectAndEnvelopeResponses(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
		wantErr bool
	}{
		{"direct response", `{"skills":[{"id":"direct-skill"}],"total":1,"hasMore":false}`, false},
		{"valid envelope", `{"success":true,"data":{"skills":[{"id":"enveloped-skill"}],"total":1,"hasMore":false}}`, false},
		{"failed envelope", `{"success":false,"data":{"skills":[],"total":0,"hasMore":false}}`, true},
		{"successful envelope without data", `{"success":true}`, true},
		{"successful envelope with null data", `{"success":true,"data":null}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeCatalogPage([]byte(test.payload))
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalizeCatalogPage(%s) accepted invalid envelope", test.payload)
				}
				return
			}
			if err != nil || len(got.Skills) != 1 || got.Total != 1 {
				t.Fatalf("normalizeCatalogPage() = %+v, %v", got, err)
			}
		})
	}
}

func TestNormalizeCatalogPageRequiresCompleteTypedShape(t *testing.T) {
	validDirect := `{"skills":[],"total":0,"hasMore":false,"upstream":"ignored"}`
	validEnvelope := `{"success":true,"data":{"skills":[],"total":0,"hasMore":false,"upstream":"ignored"}}`
	for _, test := range []struct {
		name    string
		payload string
		wantErr bool
	}{
		{"valid empty direct", validDirect, false},
		{"valid empty envelope", validEnvelope, false},
		{"empty object", `{}`, true},
		{"null root", `null`, true},
		{"array root", `[]`, true},
		{"missing skills", `{"total":0,"hasMore":false}`, true},
		{"missing total", `{"skills":[],"hasMore":false}`, true},
		{"missing hasMore", `{"skills":[],"total":0}`, true},
		{"null skills", `{"skills":null,"total":0,"hasMore":false}`, true},
		{"null total", `{"skills":[],"total":null,"hasMore":false}`, true},
		{"null hasMore", `{"skills":[],"total":0,"hasMore":null}`, true},
		{"wrong skills type", `{"skills":{},"total":0,"hasMore":false}`, true},
		{"wrong total type", `{"skills":[],"total":"0","hasMore":false}`, true},
		{"fractional total", `{"skills":[],"total":0.5,"hasMore":false}`, true},
		{"wrong hasMore type", `{"skills":[],"total":0,"hasMore":"false"}`, true},
		{"negative total", `{"skills":[],"total":-1,"hasMore":false}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeCatalogPage([]byte(test.payload))
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalizeCatalogPage(%s) accepted malformed shape", test.payload)
				}
				return
			}
			if err != nil || got.Skills == nil || got.Total != 0 || got.HasMore {
				t.Fatalf("normalizeCatalogPage() = %+v, %v", got, err)
			}
		})
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
		{"missing catalog fields", func(int) string { return `{}` }},
		{"null catalog skills", func(int) string { return `{"skills":null,"total":0,"hasMore":false}` }},
		{"failed envelope", func(int) string { return `{"success":false,"data":{"skills":[],"total":0,"hasMore":false}}` }},
		{"successful envelope without data", func(int) string { return `{"success":true}` }},
		{"successful envelope with null data", func(int) string { return `{"success":true,"data":null}` }},
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

func TestCollectCandidateEvidenceUsesOnlyCandidatesPaginatesAndResumes(t *testing.T) {
	calls := map[string]int{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls[request.URL.Path+"?"+request.URL.RawQuery]++
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/skills/writing":
			fmt.Fprint(w, `{"code":0,"data":{"id":"writing"}}`)
		case "/api/skills/writing/comments":
			if request.URL.Query().Get("page") == "1" {
				fmt.Fprint(w, `{"success":true,"data":{"items":[{"id":"one","user_id":"reader","stars":5,"content":"续写以后人物说话保持稳定，时间线没有漂移，输出章节可以直接使用。","created_at":"2026-07-02T00:00:00Z"}],"total":2,"hasMore":true}}`)
				return
			}
			fmt.Fprint(w, `{"items":[{"id":"two","user_id":"reader2","stars":4,"content":"输入大纲之后输出章节完整，人物行为符合设定，比较旧稿没有发生冲突。","created_at":"2026-07-03T00:00:00Z"}],"total":2,"hasMore":false}`)
		default:
			t.Fatalf("unexpected request %s", request.URL)
		}
	}))
	t.Cleanup(server.Close)
	collector := NewCollector(server.Client(), fixedClock())
	options := EvidenceCollectionOptions{CollectorOptions: CollectorOptions{BaseURL: server.URL, CacheRoot: t.TempDir(), PageSize: 1, RetryAttempts: 1}, CommentPageSize: 1}
	candidates := []CandidateRecord{{Skill: SkillRecord{ID: "writing"}}}
	details, comments, failures, err := collector.CollectCandidateEvidence(context.Background(), options, candidates)
	if err != nil || len(failures) != 0 || len(details) != 1 || len(comments["writing"]) != 2 {
		t.Fatalf("details=%v comments=%v failures=%v err=%v", details, comments, failures, err)
	}
	before := requestCallTotal(calls)
	if _, _, failures, err = collector.CollectCandidateEvidence(context.Background(), options, candidates); err != nil || len(failures) != 0 || requestCallTotal(calls) != before {
		t.Fatalf("resume calls=%v failures=%v err=%v", calls, failures, err)
	}
}

func requestCallTotal(calls map[string]int) int {
	total := 0
	for _, count := range calls {
		total += count
	}
	return total
}

func TestNormalizeSkillDetailDecodesAndValidatesReturnedID(t *testing.T) {
	candidate := SkillRecord{ID: "wanted", Name: "catalog"}
	got, err := normalizeSkillDetail([]byte(`{"success":true,"data":{"id":"wanted","name":"detail","weighted_score":3.5,"security_report":"clear"}}`), candidate)
	if err != nil || got.Name != "detail" || got.WeightedScore != 3.5 || got.SecurityReport != "clear" {
		t.Fatalf("detail=%#v err=%v", got, err)
	}
	if _, err := normalizeSkillDetail([]byte(`{"id":"other"}`), candidate); err == nil {
		t.Fatal("accepted mismatched detail id")
	}
}

func TestCollectCandidateEvidenceOmitsPartialCandidateAndRejectsUnsafeID(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/skills/good" {
			fmt.Fprint(w, `{"id":"good"}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	collector := NewCollector(server.Client(), fixedClock())
	options := EvidenceCollectionOptions{CollectorOptions: CollectorOptions{BaseURL: server.URL, CacheRoot: t.TempDir(), PageSize: 1, RetryAttempts: 1, MaxRetryDelay: time.Millisecond}, CommentPageSize: 1}
	details, comments, failures, err := collector.CollectCandidateEvidence(context.Background(), options, []CandidateRecord{{Skill: SkillRecord{ID: "good"}}})
	if err != nil || len(failures) != 1 || len(details) != 0 {
		t.Fatalf("details=%v comments=%v failures=%v err=%v", details, comments, failures, err)
	}
	if _, ok := comments["good"]; ok {
		t.Fatalf("partial comments must be omitted: %#v", comments)
	}
	if _, _, _, err = collector.CollectCandidateEvidence(context.Background(), options, []CandidateRecord{{Skill: SkillRecord{ID: "bad/id"}}}); err == nil {
		t.Fatal("accepted unsafe candidate id")
	}
}

func TestNormalizeCommentPageRejectsMissingEnvelopeDataAndPaginationFields(t *testing.T) {
	for _, payload := range []string{`{"success":true}`, `{"success":true,"data":null}`, `{"items":[]}`} {
		if _, err := normalizeCommentPage([]byte(payload)); err == nil {
			t.Fatalf("accepted incomplete comments payload %s", payload)
		}
	}
}

func TestCollectCandidateEvidenceBoundsPerpetualPaginationByReportedTotal(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls++
		if request.URL.Path == "/api/skills/bounded" {
			fmt.Fprint(w, `{"id":"bounded","comment_count":2}`)
			return
		}
		if request.URL.Path == "/api/skills/bounded/comments" {
			fmt.Fprint(w, `{"items":[{"id":"`+request.URL.Query().Get("page")+`","user_id":"u`+request.URL.Query().Get("page")+`","stars":5,"content":"synthetic observation content has enough unique detail for pagination testing","created_at":"2026-07-01T00:00:00Z"}],"total":2,"hasMore":true}`)
			return
		}
		t.Fatalf("unexpected request %s", request.URL)
	}))
	t.Cleanup(server.Close)
	collector := NewCollector(server.Client(), fixedClock())
	options := EvidenceCollectionOptions{CollectorOptions: CollectorOptions{BaseURL: server.URL, CacheRoot: t.TempDir(), PageSize: 1, RetryAttempts: 1}, CommentPageSize: 1}
	details, comments, failures, err := collector.CollectCandidateEvidence(context.Background(), options, []CandidateRecord{{Skill: SkillRecord{ID: "bounded"}}})
	if err != nil || len(details) != 0 || len(comments) != 0 || len(failures) != 1 || calls != 3 {
		t.Fatalf("details=%v comments=%v failures=%v calls=%d err=%v", details, comments, failures, calls, err)
	}
}

func TestNormalizeCommentPageAcceptsDirectShapeAndRejectsFailedEnvelope(t *testing.T) {
	page, err := normalizeCommentPage([]byte(`{"items":[],"total":0,"hasMore":false}`))
	if err != nil || page.Total != 0 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	if _, err := normalizeCommentPage([]byte(`{"success":false,"data":{"items":[],"total":0,"hasMore":false}}`)); err == nil {
		t.Fatal("accepted unsuccessful envelope")
	}
	if _, err := normalizeSkillDetail([]byte(`{"success":false,"data":{"id":"wanted"}}`), SkillRecord{ID: "wanted"}); err == nil {
		t.Fatal("accepted unsuccessful detail envelope")
	}
}

func TestCollectCandidateEvidenceStopsRepeatedCommentPage(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls++
		if request.URL.Path == "/api/skills/repeated" {
			fmt.Fprint(w, `{"id":"repeated","comment_count":10}`)
			return
		}
		if request.URL.Path == "/api/skills/repeated/comments" {
			fmt.Fprint(w, `{"items":[{"id":"same","user_id":"same","stars":5,"content":"synthetic comment response for repeated hash detection only","created_at":"2026-07-01T00:00:00Z"}],"total":10,"hasMore":true}`)
			return
		}
		t.Fatalf("unexpected %s", request.URL)
	}))
	t.Cleanup(server.Close)
	collector := NewCollector(server.Client(), fixedClock())
	options := EvidenceCollectionOptions{CollectorOptions: CollectorOptions{BaseURL: server.URL, CacheRoot: t.TempDir(), PageSize: 1, RetryAttempts: 1}, CommentPageSize: 1}
	details, comments, failures, err := collector.CollectCandidateEvidence(context.Background(), options, []CandidateRecord{{Skill: SkillRecord{ID: "repeated"}}})
	if err != nil || len(details) != 0 || len(comments) != 0 || len(failures) != 1 || calls != 3 {
		t.Fatalf("details=%v comments=%v failures=%v calls=%d err=%v", details, comments, failures, calls, err)
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
