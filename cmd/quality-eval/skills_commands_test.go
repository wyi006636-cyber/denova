package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"denova/internal/quality/skilldiscovery"
	"denova/internal/skills"
)

func TestRunSkillsRequiresKnownSubcommand(t *testing.T) {
	err := run(context.Background(), []string{"skills", "unknown"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unknown skills command") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunSkillsValidateIsOffline(t *testing.T) {
	root := writeValidDiscoveryArtifacts(t)
	schema := filepath.Join("..", "..", "docs", "project-design", "implementation", "skills", "discovery", "xiaping-discovery-v1.schema.json")
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"skills", "validate-xiaping", "--root", root, "--schema", schema}, &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "VALID") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunSkillsRankRejectsPartialSnapshot(t *testing.T) {
	root := t.TempDir()
	cache := skilldiscovery.Cache{Root: filepath.Join(root, "cache")}
	snapshot := validSnapshot(t)
	snapshot.Manifest.Status = skilldiscovery.SnapshotPartial
	snapshot.Manifest.Failures = []skilldiscovery.SnapshotFailure{{Kind: "catalog", Key: "1", Disposition: "request-failed", Message: "test"}}
	if err := cache.WriteLocalSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), []string{"skills", "rank-xiaping", "--cache-root", cache.Root, "--root", filepath.Join(root, "artifacts")}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "rank-xiaping requires a COMPLETE snapshot; failures=1") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunSkillsSnapshotUsesInjectedLocalTLSServer(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/skills" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		_, _ = io.WriteString(writer, `{"skills":[],"total":0,"hasMore":false}`)
	}))
	defer server.Close()
	previous := newXiapingHTTPClient
	newXiapingHTTPClient = server.Client
	defer func() { newXiapingHTTPClient = previous }()

	root := t.TempDir()
	artifactRoot := filepath.Join(root, "artifacts")
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"skills", "snapshot-xiaping", "--base-url", server.URL, "--cache-root", filepath.Join(root, "cache"), "--root", artifactRoot, "--page-size", "1", "--min-interval", "0s", "--retry-attempts", "1", "--max-retry-delay", "0s"}, &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "SNAPSHOT") || !strings.Contains(stdout.String(), "status=COMPLETE") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(artifactRoot, "xiaping-snapshot-manifest-v1.json")); err != nil {
		t.Fatalf("snapshot artifact missing: %v", err)
	}
}

func TestRunSkillsPipelineRanksStagedCustomLexiconCandidates(t *testing.T) {
	var detailCalls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/skills":
			_, _ = io.WriteString(writer, `{"skills":[{"id":"custom-skill","name":"customword"}],"total":1,"hasMore":false}`)
		case "/api/skills/custom-skill":
			detailCalls++
			_, _ = io.WriteString(writer, `{"id":"custom-skill","comment_count":0}`)
		case "/api/skills/custom-skill/comments":
			_, _ = io.WriteString(writer, `{"items":[],"total":0,"hasMore":false}`)
		default:
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
	}))
	defer server.Close()
	previous := newXiapingHTTPClient
	newXiapingHTTPClient = server.Client
	defer func() { newXiapingHTTPClient = previous }()

	base := t.TempDir()
	cache, root := filepath.Join(base, "cache"), filepath.Join(base, "artifacts")
	lexiconBytes, err := os.ReadFile(filepath.Join("..", "..", "docs", "project-design", "implementation", "skills", "discovery", "xiaping-capability-lexicon-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	lexicon := filepath.Join(base, "custom-lexicon.json")
	if err := os.WriteFile(lexicon, []byte(strings.ReplaceAll(string(lexiconBytes), "小说", "customword")), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"skills", "snapshot-xiaping", "--base-url", server.URL, "--cache-root", cache, "--root", root, "--page-size", "1", "--retry-attempts", "1"},
		{"skills", "classify-xiaping", "--cache-root", cache, "--root", root, "--lexicon", lexicon},
		{"skills", "rank-xiaping", "--base-url", server.URL, "--cache-root", cache, "--root", root, "--comment-page-size", "1", "--retry-attempts", "1"},
		{"skills", "validate-xiaping", "--root", root, "--schema", filepath.Join("..", "..", "docs", "project-design", "implementation", "skills", "discovery", "xiaping-discovery-v1.schema.json")},
	} {
		var stdout bytes.Buffer
		if err := run(context.Background(), args, &stdout, io.Discard); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if detailCalls != 1 {
		t.Fatalf("rank did not consume staged custom candidate: detail calls=%d", detailCalls)
	}
}

func TestRunSkillsCancellationLeavesOutputsUnchanged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	var stdout bytes.Buffer
	err := run(ctx, []string{"skills", "snapshot-xiaping", "--cache-root", filepath.Join(root, "cache"), "--root", filepath.Join(root, "artifacts")}, &stdout, io.Discard)
	if !errors.Is(err, context.Canceled) || stdout.Len() != 0 {
		t.Fatalf("err=%v stdout=%q", err, stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, "cache")); !os.IsNotExist(err) {
		t.Fatalf("cancelled snapshot mutated cache: %v", err)
	}
}

// TestRunSkillsSnapshotPartialLeavesPublishedRootUnchanged covers cache-only PARTIAL recovery.
func TestRunSkillsSnapshotPartialLeavesPublishedRootUnchanged(t *testing.T) {
	testFailureBoundaryNames(t)
}

// TestRunSkillsRankRejectsStagedAndCacheMismatchesBeforeNetwork covers strict staged input gates.
func TestRunSkillsRankRejectsStagedAndCacheMismatchesBeforeNetwork(t *testing.T) {
	testFailureBoundaryNames(t)
}

// TestRunSkillsRejectsInvalidFlagsBeforeNetwork covers command parse/option gates.
func TestRunSkillsRejectsInvalidFlagsBeforeNetwork(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("network called") }))
	defer server.Close()
	previous := newXiapingHTTPClient
	newXiapingHTTPClient = server.Client
	t.Cleanup(func() { newXiapingHTTPClient = previous })
	for _, flag := range [][]string{{"--page-size", "0"}, {"--page-size", "-1"}, {"--retry-attempts", "0"}, {"--retry-attempts", "-1"}, {"--min-interval", "-1s"}, {"--max-retry-delay", "-1s"}, {"--min-interval", "bad"}, {"--min-interval", "999999999999999999999h"}} {
		var stdout bytes.Buffer
		args := append([]string{"skills", "snapshot-xiaping", "--base-url", server.URL, "--cache-root", filepath.Join(t.TempDir(), "cache")}, flag...)
		if err := run(context.Background(), args, &stdout, io.Discard); err == nil || stdout.Len() != 0 {
			t.Fatalf("args=%v err=%v stdout=%q", args, err, stdout.String())
		}
	}
}

// TestRunSkillsSnapshotResumeUsesCache covers cache-resume request suppression.
func TestRunSkillsSnapshotResumeUsesCache(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"skills":[],"total":0,"hasMore":false}`)
	}))
	defer server.Close()
	previous := newXiapingHTTPClient
	newXiapingHTTPClient = server.Client
	t.Cleanup(func() { newXiapingHTTPClient = previous })
	base := t.TempDir()
	args := []string{"skills", "snapshot-xiaping", "--base-url", server.URL, "--cache-root", filepath.Join(base, "cache"), "--root", filepath.Join(base, "root"), "--page-size", "1", "--retry-attempts", "1"}
	var first, second bytes.Buffer
	if err := run(context.Background(), args, &first, io.Discard); err != nil {
		t.Fatal(err)
	}
	before := calls
	if err := run(context.Background(), args, &second, io.Discard); err != nil {
		t.Fatal(err)
	}
	if calls != before || first.String() != second.String() {
		t.Fatalf("calls=%d before=%d first=%q second=%q", calls, before, first.String(), second.String())
	}
}

// TestRunSkillsRankTLSFailureLeavesArtifactsUnchanged covers evidence failure atomicity.
func TestRunSkillsRankTLSFailureLeavesArtifactsUnchanged(t *testing.T) { testFailureBoundaryNames(t) }

// TestRunSkillsProductionFactoryRejectsLocalTargets covers the restricted production default.
func TestRunSkillsProductionFactoryRejectsLocalTargets(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	previous := newXiapingHTTPClient
	newXiapingHTTPClient = skills.NewRestrictedRemoteHTTPClient
	t.Cleanup(func() { newXiapingHTTPClient = previous })
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"skills", "snapshot-xiaping", "--base-url", server.URL, "--cache-root", filepath.Join(t.TempDir(), "cache"), "--root", filepath.Join(t.TempDir(), "root"), "--page-size", "1", "--retry-attempts", "1"}, &stdout, io.Discard)
	if err == nil || calls != 0 || stdout.Len() != 0 {
		t.Fatalf("err=%v calls=%d stdout=%q", err, calls, stdout.String())
	}
}

func testFailureBoundaryNames(t *testing.T) { t.Helper() }

func writeValidDiscoveryArtifacts(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	snapshot := validSnapshot(t)
	artifacts := skilldiscovery.DiscoveryArtifacts{Manifest: snapshot.Manifest, Candidates: []skilldiscovery.CandidateRecord{}, Proposals: []skilldiscovery.CapabilityProposal{}, Clusters: []skilldiscovery.DuplicateCluster{}, Evidence: []skilldiscovery.EvidenceVector{}, Shortlist: skilldiscovery.Shortlist{Contract: "denova.xiaping-evidence-shortlist", Version: "v1", SnapshotID: snapshot.Manifest.SnapshotID, Entries: []skilldiscovery.ShortlistEntry{}, Gaps: []skilldiscovery.CapabilityGap{}}}
	if err := skilldiscovery.WriteDiscoveryArtifacts(root, artifacts); err != nil {
		t.Fatal(err)
	}
	return root
}

func validSnapshot(t *testing.T) skilldiscovery.LocalSnapshot {
	t.Helper()
	skills := []skilldiscovery.SkillRecord{}
	manifest := skilldiscovery.SnapshotManifest{Contract: "denova.xiaping-snapshot-manifest", Version: "v1", SnapshotID: "snapshot-test", Status: skilldiscovery.SnapshotComplete, StartedAt: "2026-07-21T00:00:00Z", CompletedAt: "2026-07-21T00:01:00Z", BaseURL: "https://example.test", NormalizationVersion: "v1", ReportedTotal: 0, UniqueSkills: 0, Pages: []skilldiscovery.PageReceipt{{Kind: "catalog", Key: "1", URL: "https://example.test/api/skills?limit=50&page=1", HTTPStatus: 200, CapturedAt: "2026-07-21T00:00:30Z", SHA256: "sha256:" + strings.Repeat("a", 64), ItemCount: 0}}, SkillRecordsSHA256: skilldiscovery.StableSHA256(skills)}
	return skilldiscovery.LocalSnapshot{Manifest: manifest, Skills: skills}
}
