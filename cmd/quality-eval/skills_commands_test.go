package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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
	base := t.TempDir()
	cache := skilldiscovery.Cache{Root: filepath.Join(base, "cache")}
	snapshot := validSnapshot(t)
	snapshot.Manifest.Status = skilldiscovery.SnapshotPartial
	snapshot.Manifest.Failures = []skilldiscovery.SnapshotFailure{{Kind: "catalog", Key: "1", Disposition: "request-failed", Message: "test"}}
	if err := cache.WriteLocalSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	root := writeValidDiscoveryArtifacts(t)
	before := artifactBytes(t, root)
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"skills", "rank-xiaping", "--cache-root", cache.Root, "--root", root}, &stdout, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "rank-xiaping requires a COMPLETE snapshot; failures=1") {
		t.Fatalf("error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
	assertArtifactBytes(t, root, before)
}

func TestRunSkillsSnapshotUsesInjectedLocalTLSServer(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
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
	if calls != 1 {
		t.Fatalf("catalog calls=%d", calls)
	}
	snapshot, err := (skilldiscovery.Cache{Root: filepath.Join(root, "cache")}).LoadLocalSnapshot()
	if err != nil || snapshot.Manifest.Status != skilldiscovery.SnapshotComplete {
		t.Fatalf("snapshot=%+v err=%v", snapshot.Manifest, err)
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
	outputs := make([]string, 0, 4)
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
		outputs = append(outputs, stdout.String())
	}
	if detailCalls != 1 {
		t.Fatalf("rank did not consume staged custom candidate: detail calls=%d", detailCalls)
	}
	for index, prefix := range []string{"SNAPSHOT ", "CLASSIFIED ", "RANKED ", "VALID "} {
		if !strings.HasPrefix(outputs[index], prefix) {
			t.Fatalf("output[%d]=%q", index, outputs[index])
		}
	}
	if !strings.Contains(outputs[1], "candidates=1") || !strings.Contains(outputs[2], "failures=0") || !strings.Contains(outputs[3], "artifacts=5") {
		t.Fatalf("outputs=%q", outputs)
	}
}

func TestRunSkillsCancellationLeavesOutputsUnchanged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	base := t.TempDir()
	root := writeValidDiscoveryArtifacts(t)
	before := artifactBytes(t, root)
	var stdout bytes.Buffer
	err := run(ctx, []string{"skills", "snapshot-xiaping", "--cache-root", filepath.Join(base, "cache"), "--root", root}, &stdout, io.Discard)
	if !errors.Is(err, context.Canceled) || stdout.Len() != 0 {
		t.Fatalf("err=%v stdout=%q", err, stdout.String())
	}
	if _, err := os.Stat(filepath.Join(base, "cache")); !os.IsNotExist(err) {
		t.Fatalf("cancelled snapshot mutated cache: %v", err)
	}
	assertArtifactBytes(t, root, before)
}

func TestRunSkillsSnapshotPartialLeavesPublishedRootUnchanged(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	previous := newXiapingHTTPClient
	newXiapingHTTPClient = server.Client
	t.Cleanup(func() { newXiapingHTTPClient = previous })

	base := t.TempDir()
	root := writeValidDiscoveryArtifacts(t)
	before := artifactBytes(t, root)
	cacheRoot := filepath.Join(base, "cache")
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"skills", "snapshot-xiaping", "--base-url", server.URL, "--cache-root", cacheRoot, "--root", root, "--page-size", "1", "--retry-attempts", "1", "--max-retry-delay", "0s"}, &stdout, io.Discard)
	if err == nil || stdout.Len() != 0 {
		t.Fatalf("err=%v stdout=%q", err, stdout.String())
	}
	assertArtifactBytes(t, root, before)
	snapshot, loadErr := (skilldiscovery.Cache{Root: cacheRoot}).LoadLocalSnapshot()
	if loadErr != nil || snapshot.Manifest.Status != skilldiscovery.SnapshotPartial || len(snapshot.Manifest.Failures) != 1 || snapshot.Manifest.Pages[0].HTTPStatus != http.StatusBadRequest {
		t.Fatalf("partial cache snapshot=%+v err=%v", snapshot.Manifest, loadErr)
	}
}

func TestRunSkillsRankRejectsStagedAndCacheMismatchesBeforeNetwork(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	previous := newXiapingHTTPClient
	newXiapingHTTPClient = server.Client
	t.Cleanup(func() { newXiapingHTTPClient = previous })
	cases := []struct {
		name   string
		mutate func(t *testing.T, cache skilldiscovery.Cache, root string, snapshot skilldiscovery.LocalSnapshot)
	}{
		{"candidate source", mutateProvenance("xiaping-writing-candidates-v1.json", "source", "wrong")},
		{"candidate purpose", mutateProvenance("xiaping-writing-candidates-v1.json", "purpose", "wrong")},
		{"candidate input SHA", mutateProvenance("xiaping-writing-candidates-v1.json", "input_sha256", "sha256:"+strings.Repeat("0", 64))},
		{"candidate max bytes", mutateProvenance("xiaping-writing-candidates-v1.json", "max_bytes", float64(1))},
		{"proposal source", mutateProvenance("xiaping-capability-proposals-v1.json", "source", "wrong")},
		{"proposal purpose", mutateProvenance("xiaping-capability-proposals-v1.json", "purpose", "wrong")},
		{"proposal input SHA", mutateProvenance("xiaping-capability-proposals-v1.json", "input_sha256", "sha256:"+strings.Repeat("0", 64))},
		{"proposal max bytes", mutateProvenance("xiaping-capability-proposals-v1.json", "max_bytes", float64(1))},
		{"cluster source", mutateProvenance("xiaping-duplicate-clusters-v1.json", "source", "wrong")},
		{"cluster purpose", mutateProvenance("xiaping-duplicate-clusters-v1.json", "purpose", "wrong")},
		{"cluster input SHA", mutateProvenance("xiaping-duplicate-clusters-v1.json", "input_sha256", "sha256:"+strings.Repeat("0", 64))},
		{"cluster max bytes", mutateProvenance("xiaping-duplicate-clusters-v1.json", "max_bytes", float64(1))},
		{"cache snapshot ID", func(t *testing.T, cache skilldiscovery.Cache, root string, snapshot skilldiscovery.LocalSnapshot) {
			snapshot.Manifest.SnapshotID = "snapshot-other"
			if err := cache.WriteLocalSnapshot(snapshot); err != nil {
				t.Fatal(err)
			}
		}},
		{"cache snapshot hash", func(t *testing.T, cache skilldiscovery.Cache, root string, snapshot skilldiscovery.LocalSnapshot) {
			snapshot.Skills[0].Name = "different"
			snapshot.Manifest.SkillRecordsSHA256 = skilldiscovery.StableSHA256(snapshot.Skills)
			if err := cache.WriteLocalSnapshot(snapshot); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing candidate", removeStaged("xiaping-writing-candidates-v1.json")},
		{"missing proposal", removeStaged("xiaping-capability-proposals-v1.json")},
		{"missing cluster", removeStaged("xiaping-duplicate-clusters-v1.json")},
		{"malformed JSON", func(t *testing.T, cache skilldiscovery.Cache, root string, snapshot skilldiscovery.LocalSnapshot) {
			if err := os.WriteFile(filepath.Join(root, "xiaping-writing-candidates-v1.json"), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache, root, snapshot := writeRankInputs(t)
			tc.mutate(t, cache, root, snapshot)
			before := artifactBytes(t, root)
			callsBefore := calls
			var stdout bytes.Buffer
			err := run(context.Background(), []string{"skills", "rank-xiaping", "--base-url", server.URL, "--cache-root", cache.Root, "--root", root, "--retry-attempts", "1", "--max-retry-delay", "0s"}, &stdout, io.Discard)
			if err == nil || calls != callsBefore || stdout.Len() != 0 {
				t.Fatalf("err=%v calls=%d calls_before=%d stdout=%q", err, calls, callsBefore, stdout.String())
			}
			assertArtifactBytes(t, root, before)
		})
	}
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
		base := t.TempDir()
		root := writeValidDiscoveryArtifacts(t)
		before := artifactBytes(t, root)
		cacheRoot := filepath.Join(base, "cache")
		args := append([]string{"skills", "snapshot-xiaping", "--base-url", server.URL, "--cache-root", cacheRoot, "--root", root}, flag...)
		if err := run(context.Background(), args, &stdout, io.Discard); err == nil || stdout.Len() != 0 {
			t.Fatalf("args=%v err=%v stdout=%q", args, err, stdout.String())
		}
		if _, err := os.Stat(cacheRoot); !os.IsNotExist(err) {
			t.Fatalf("args=%v mutated cache: %v", args, err)
		}
		assertArtifactBytes(t, root, before)
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
	snapshot, err := (skilldiscovery.Cache{Root: filepath.Join(base, "cache")}).LoadLocalSnapshot()
	if err != nil || snapshot.Manifest.Status != skilldiscovery.SnapshotComplete {
		t.Fatalf("snapshot=%+v err=%v", snapshot.Manifest, err)
	}
}

func TestRunSkillsRankCandidateEvidenceFailurePublishesExploration(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/skills/candidate-1" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	previous := newXiapingHTTPClient
	newXiapingHTTPClient = server.Client
	t.Cleanup(func() { newXiapingHTTPClient = previous })
	cache, root, snapshot := writeRankInputs(t)
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"skills", "rank-xiaping", "--base-url", server.URL, "--cache-root", cache.Root, "--root", root, "--comment-page-size", "1", "--retry-attempts", "1", "--max-retry-delay", "0s"}, &stdout, io.Discard)
	if err != nil || calls != 1 || !strings.Contains(stdout.String(), "RANKED ") || !strings.Contains(stdout.String(), "failures=1") {
		t.Fatalf("err=%v calls=%d stdout=%q", err, calls, stdout.String())
	}
	var shortlist skilldiscovery.Shortlist
	data, readErr := os.ReadFile(filepath.Join(root, "xiaping-evidence-shortlist-v1.json"))
	if readErr != nil || json.Unmarshal(data, &shortlist) != nil || len(shortlist.Entries) != 1 || shortlist.Entries[0].Lane != skilldiscovery.LaneExploration || shortlist.Entries[0].Evidence.EvidenceCacheStatus != "EVIDENCE-CACHE-MISSING" || shortlist.Entries[0].Evidence.PlatformDataRich {
		t.Fatalf("shortlist=%#v readErr=%v", shortlist, readErr)
	}
	loaded, loadErr := cache.LoadLocalSnapshot()
	if loadErr != nil || loaded.Manifest.Status != skilldiscovery.SnapshotComplete || loaded.Manifest.SnapshotID != snapshot.Manifest.SnapshotID {
		t.Fatalf("snapshot=%+v err=%v", loaded.Manifest, loadErr)
	}
}

func TestRunSkillsRankCancellationLeavesArtifactsUnchanged(t *testing.T) {
	cache, root, _ := writeRankInputs(t)
	before := artifactBytes(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout bytes.Buffer
	err := run(ctx, []string{"skills", "rank-xiaping", "--cache-root", cache.Root, "--root", root}, &stdout, io.Discard)
	if !errors.Is(err, context.Canceled) || stdout.Len() != 0 {
		t.Fatalf("err=%v stdout=%q", err, stdout.String())
	}
	assertArtifactBytes(t, root, before)
}

// TestRunSkillsProductionFactoryRejectsLocalTargets covers the restricted production default.
func TestRunSkillsProductionFactoryRejectsLocalTargets(t *testing.T) {
	previous := newXiapingHTTPClient
	newXiapingHTTPClient = skills.NewRestrictedRemoteHTTPClient
	t.Cleanup(func() { newXiapingHTTPClient = previous })
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"skills", "snapshot-xiaping", "--base-url", "https://127.0.0.1:1", "--cache-root", filepath.Join(t.TempDir(), "cache"), "--root", filepath.Join(t.TempDir(), "root"), "--page-size", "1", "--retry-attempts", "1", "--max-retry-delay", "1ms"}, &stdout, io.Discard)
	if err == nil || stdout.Len() != 0 {
		t.Fatalf("err=%v stdout=%q", err, stdout.String())
	}
}

func writeRankInputs(t *testing.T) (skilldiscovery.Cache, string, skilldiscovery.LocalSnapshot) {
	t.Helper()
	base := t.TempDir()
	cache := skilldiscovery.Cache{Root: filepath.Join(base, "cache")}
	snapshot := validSnapshotWithCandidate(t)
	if err := cache.WriteLocalSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "artifacts")
	candidates := []skilldiscovery.CandidateRecord{{Skill: snapshot.Skills[0], Capabilities: []skilldiscovery.CapabilityMatch{{CapabilityID: "style.revise-prose", Status: skilldiscovery.MatchMatched, Evidence: []skilldiscovery.FieldEvidence{{Field: "name", Term: "candidate"}}}}}}
	shortlist, err := skilldiscovery.BuildShortlist(snapshot.Manifest.SnapshotID, candidates, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := skilldiscovery.DiscoveryArtifacts{Manifest: snapshot.Manifest, Candidates: candidates, Proposals: []skilldiscovery.CapabilityProposal{}, Clusters: []skilldiscovery.DuplicateCluster{}, Evidence: []skilldiscovery.EvidenceVector{}, Shortlist: shortlist}
	if err := skilldiscovery.WriteDiscoveryArtifacts(root, artifacts); err != nil {
		t.Fatal(err)
	}
	return cache, root, snapshot
}

func validSnapshotWithCandidate(t *testing.T) skilldiscovery.LocalSnapshot {
	t.Helper()
	snapshot := validSnapshot(t)
	snapshot.Skills = []skilldiscovery.SkillRecord{{ID: "candidate-1", Name: "candidate"}}
	snapshot.Manifest.ReportedTotal, snapshot.Manifest.UniqueSkills = 1, 1
	snapshot.Manifest.SkillRecordsSHA256 = skilldiscovery.StableSHA256(snapshot.Skills)
	return snapshot
}

func artifactBytes(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		files[strings.TrimPrefix(path, root+string(os.PathSeparator))] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func assertArtifactBytes(t *testing.T, root string, want map[string][]byte) {
	t.Helper()
	if got := artifactBytes(t, root); !reflect.DeepEqual(got, want) {
		t.Fatalf("artifact bytes changed: got=%v want=%v", got, want)
	}
}

func mutateProvenance(name, field string, value any) func(*testing.T, skilldiscovery.Cache, string, skilldiscovery.LocalSnapshot) {
	return func(t *testing.T, cache skilldiscovery.Cache, root string, snapshot skilldiscovery.LocalSnapshot) {
		t.Helper()
		path := filepath.Join(root, name)
		var raw map[string]any
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		raw["provenance"].(map[string]any)[field] = value
		data, err = json.MarshalIndent(raw, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func removeStaged(name string) func(*testing.T, skilldiscovery.Cache, string, skilldiscovery.LocalSnapshot) {
	return func(t *testing.T, cache skilldiscovery.Cache, root string, snapshot skilldiscovery.LocalSnapshot) {
		t.Helper()
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
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
