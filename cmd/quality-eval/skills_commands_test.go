package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"denova/internal/quality/skilldiscovery"
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
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"skills", "snapshot-xiaping", "--base-url", server.URL, "--cache-root", filepath.Join(root, "cache"), "--root", filepath.Join(root, "artifacts"), "--page-size", "1", "--min-interval", "0s", "--retry-attempts", "1", "--max-retry-delay", "0s"}, &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "SNAPSHOT") || !strings.Contains(stdout.String(), "status=COMPLETE") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

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
