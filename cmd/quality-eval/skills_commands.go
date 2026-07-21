package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"denova/internal/quality/skilldiscovery"
	"denova/internal/skills"
)

const (
	defaultDiscoveryRoot          = "docs/project-design/implementation/skills/discovery"
	defaultXiapingBaseURL         = "https://xiaping.coze.com"
	defaultXiapingPageSize        = 50
	defaultXiapingCommentPageSize = 50
)

var newXiapingHTTPClient = skills.NewXiapingPublicHTTPClient

func runSkills(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("skills requires snapshot-xiaping, classify-xiaping, rank-xiaping, or validate-xiaping")
	}
	switch args[0] {
	case "snapshot-xiaping":
		return runXiapingSnapshot(ctx, args[1:], stdout, stderr)
	case "classify-xiaping":
		return runXiapingClassify(args[1:], stdout, stderr)
	case "rank-xiaping":
		return runXiapingRank(ctx, args[1:], stdout, stderr)
	case "validate-xiaping":
		return runXiapingValidate(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown skills command %q", args[0])
	}
}

func runXiapingSnapshot(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	flags := flag.NewFlagSet("snapshot-xiaping", flag.ContinueOnError)
	flags.SetOutput(stderr)
	baseURL := flags.String("base-url", defaultXiapingBaseURL, "Xiaping HTTPS base URL")
	cacheRoot := cacheRootFlag(flags)
	root := flags.String("root", defaultDiscoveryRoot, "discovery artifact root")
	pageSize := flags.Int("page-size", defaultXiapingPageSize, "catalog page size")
	minInterval := durationFlag(flags, "min-interval", "0s", "minimum interval between requests")
	retryAttempts := flags.Int("retry-attempts", 3, "retry attempts")
	maxRetryDelay := durationFlag(flags, "max-retry-delay", "30s", "maximum retry delay")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := noPositionalArgs(flags); err != nil {
		return err
	}
	if err := requireRoot(*root); err != nil {
		return err
	}
	options, err := collectorOptions(*baseURL, *cacheRoot, *pageSize, *minInterval, *retryAttempts, *maxRetryDelay)
	if err != nil {
		return fmt.Errorf("snapshot-xiaping: %w", err)
	}
	fmt.Fprintln(stderr, "quality-eval: snapshot-xiaping collecting public catalog")
	snapshot, err := skilldiscovery.NewCollector(newXiapingHTTPClient(), nil).CollectCatalog(ctx, options)
	if err != nil {
		return fmt.Errorf("snapshot-xiaping: %w", err)
	}
	if snapshot.Manifest.SnapshotID == "" {
		return errors.New("snapshot-xiaping: collector returned no snapshot identity")
	}
	if snapshot.Manifest.Status != skilldiscovery.SnapshotComplete {
		return fmt.Errorf("snapshot-xiaping: collector returned %s snapshot", snapshot.Manifest.Status)
	}
	if err := skilldiscovery.WriteSnapshotManifest(*root, snapshot.Manifest); err != nil {
		return fmt.Errorf("snapshot-xiaping: publish manifest: %w", err)
	}
	fmt.Fprintf(stdout, "SNAPSHOT id=%s status=%s skills=%d\n", snapshot.Manifest.SnapshotID, snapshot.Manifest.Status, len(snapshot.Skills))
	return nil
}

func runXiapingClassify(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("classify-xiaping", flag.ContinueOnError)
	flags.SetOutput(stderr)
	cacheRoot := cacheRootFlag(flags)
	root := flags.String("root", defaultDiscoveryRoot, "discovery artifact root")
	lexicon := flags.String("lexicon", defaultLexiconPath(), "capability lexicon")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := noPositionalArgs(flags); err != nil {
		return err
	}
	if err := requireRoot(*root); err != nil {
		return err
	}
	snapshot, err := loadCompleteSnapshot("classify-xiaping", *cacheRoot)
	if err != nil {
		return err
	}
	vocabulary, err := skilldiscovery.LoadLexicon(*lexicon)
	if err != nil {
		return fmt.Errorf("classify-xiaping: %w", err)
	}
	candidates, proposals := skilldiscovery.ClassifyWritingCandidates(snapshot.Skills, vocabulary)
	clusters := skilldiscovery.ClusterCandidates(candidates, .90)
	if err := skilldiscovery.WriteStagedDiscoveryArtifacts(*root, skilldiscovery.StagedDiscoveryArtifacts{Manifest: snapshot.Manifest, Candidates: candidates, Proposals: proposals, Clusters: clusters}); err != nil {
		return fmt.Errorf("classify-xiaping: publish staged artifacts: %w", err)
	}
	matched, ambiguous := capabilityCounts(candidates)
	fmt.Fprintf(stdout, "CLASSIFIED snapshot=%s candidates=%d matched=%d ambiguous=%d proposals=%d clusters=%d coverage=%d\n", snapshot.Manifest.SnapshotID, len(candidates), matched, ambiguous, len(proposals), len(clusters), matched+ambiguous)
	return nil
}

func runXiapingRank(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	flags := flag.NewFlagSet("rank-xiaping", flag.ContinueOnError)
	flags.SetOutput(stderr)
	baseURL := flags.String("base-url", defaultXiapingBaseURL, "Xiaping HTTPS base URL")
	cacheRoot := cacheRootFlag(flags)
	root := flags.String("root", defaultDiscoveryRoot, "discovery artifact root")
	commentPageSize := flags.Int("comment-page-size", defaultXiapingCommentPageSize, "comment page size")
	minInterval := durationFlag(flags, "min-interval", "0s", "minimum interval between requests")
	retryAttempts := flags.Int("retry-attempts", 3, "retry attempts")
	maxRetryDelay := durationFlag(flags, "max-retry-delay", "30s", "maximum retry delay")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := noPositionalArgs(flags); err != nil {
		return err
	}
	if err := requireRoot(*root); err != nil {
		return err
	}
	snapshot, err := loadCompleteSnapshot("rank-xiaping", *cacheRoot)
	if err != nil {
		return err
	}
	staged, err := skilldiscovery.LoadStagedDiscoveryArtifacts(*root, defaultSchemaPath())
	if err != nil {
		return fmt.Errorf("rank-xiaping: load staged artifacts: %w", err)
	}
	if snapshot.Manifest.SnapshotID != staged.Manifest.SnapshotID || snapshot.Manifest.SkillRecordsSHA256 != staged.Manifest.SkillRecordsSHA256 {
		return errors.New("rank-xiaping: staged artifacts do not match cached snapshot")
	}
	options, err := collectorOptions(*baseURL, *cacheRoot, defaultXiapingPageSize, *minInterval, *retryAttempts, *maxRetryDelay)
	if err != nil {
		return fmt.Errorf("rank-xiaping: %w", err)
	}
	if *commentPageSize <= 0 {
		return errors.New("rank-xiaping: comment page size must be positive")
	}
	fmt.Fprintln(stderr, "quality-eval: rank-xiaping collecting bounded candidate evidence")
	details, comments, failures, err := skilldiscovery.NewCollector(newXiapingHTTPClient(), nil).CollectCandidateEvidence(ctx, skilldiscovery.EvidenceCollectionOptions{CollectorOptions: options, CommentPageSize: *commentPageSize}, staged.Candidates)
	if err != nil {
		return fmt.Errorf("rank-xiaping: %w", err)
	}
	reviews := make(map[string]skilldiscovery.ReviewEvidence, len(details))
	for id, detail := range details {
		review := skilldiscovery.SummarizeReviews(detail.OwnerID, comments[id], skilldiscovery.ReviewPolicy{})
		review.EvidenceCacheStatus = "EVIDENCE-CACHE-AVAILABLE"
		reviews[id] = review
	}
	vectors := skilldiscovery.BuildEvidenceVectors(staged.Candidates, reviews, staged.Clusters)
	shortlist, err := skilldiscovery.BuildShortlistFromSnapshot(snapshot, staged.Candidates, vectors, staged.Clusters)
	if err != nil {
		return fmt.Errorf("rank-xiaping: %w", err)
	}
	artifacts := skilldiscovery.DiscoveryArtifacts{Manifest: snapshot.Manifest, Candidates: staged.Candidates, Proposals: staged.Proposals, Clusters: staged.Clusters, Evidence: vectors, Shortlist: shortlist}
	if err := skilldiscovery.WriteDiscoveryArtifacts(*root, artifacts); err != nil {
		return fmt.Errorf("rank-xiaping: %w", err)
	}
	dataRich, exploration := laneCounts(shortlist)
	fmt.Fprintf(stdout, "RANKED snapshot=%s candidates=%d clusters=%d data_rich=%d exploration=%d proposals=%d gaps=%d failures=%d\n", snapshot.Manifest.SnapshotID, len(staged.Candidates), len(staged.Clusters), dataRich, exploration, len(staged.Proposals), len(shortlist.Gaps), len(failures))
	return nil
}

func runXiapingValidate(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("validate-xiaping", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", defaultDiscoveryRoot, "discovery artifact root")
	schema := flags.String("schema", defaultSchemaPath(), "local discovery schema")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := noPositionalArgs(flags); err != nil {
		return err
	}
	if err := requireRoot(*root); err != nil {
		return err
	}
	paths := make([]string, 0, 5)
	for _, name := range []string{"xiaping-snapshot-manifest-v1.json", "xiaping-writing-candidates-v1.json", "xiaping-capability-proposals-v1.json", "xiaping-duplicate-clusters-v1.json", "xiaping-evidence-shortlist-v1.json"} {
		paths = append(paths, filepath.Join(*root, name))
	}
	if err := skilldiscovery.ValidateArtifactSchema(*schema, paths); err != nil {
		return fmt.Errorf("validate-xiaping: %w", err)
	}
	fmt.Fprintf(stdout, "VALID discovery=xiaping artifacts=%d\n", len(paths))
	return nil
}

func cacheRootFlag(flags *flag.FlagSet) *string {
	root, err := skilldiscovery.DefaultCacheRoot()
	if err != nil {
		root = ""
	}
	return flags.String("cache-root", root, "local Xiaping cache root")
}

func durationFlag(flags *flag.FlagSet, name, value, usage string) *time.Duration {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		panic(err)
	}
	return flags.Duration(name, parsed, usage)
}

func collectorOptions(baseURL, cacheRoot string, pageSize int, minInterval time.Duration, retryAttempts int, maxRetryDelay time.Duration) (skilldiscovery.CollectorOptions, error) {
	if strings.TrimSpace(cacheRoot) == "" {
		return skilldiscovery.CollectorOptions{}, errors.New("cache root is required")
	}
	if pageSize <= 0 || retryAttempts <= 0 || minInterval < 0 || maxRetryDelay < 0 {
		return skilldiscovery.CollectorOptions{}, errors.New("invalid collector flags")
	}
	return skilldiscovery.CollectorOptions{BaseURL: baseURL, CacheRoot: cacheRoot, PageSize: pageSize, MinInterval: minInterval, RetryAttempts: retryAttempts, MaxRetryDelay: maxRetryDelay}, nil
}

func loadCompleteSnapshot(command, cacheRoot string) (skilldiscovery.LocalSnapshot, error) {
	snapshot, err := (skilldiscovery.Cache{Root: cacheRoot}).LoadLocalSnapshot()
	if err != nil {
		return skilldiscovery.LocalSnapshot{}, fmt.Errorf("%s: %w", command, err)
	}
	if snapshot.Manifest.Status != skilldiscovery.SnapshotComplete {
		return skilldiscovery.LocalSnapshot{}, fmt.Errorf("%s requires a COMPLETE snapshot; failures=%d", command, len(snapshot.Manifest.Failures))
	}
	return snapshot, nil
}

func requireRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("artifact root is required")
	}
	return nil
}

func noPositionalArgs(flags *flag.FlagSet) error {
	if flags.NArg() != 0 {
		return fmt.Errorf("%s does not accept positional arguments", flags.Name())
	}
	return nil
}

func capabilityCounts(candidates []skilldiscovery.CandidateRecord) (matched, ambiguous int) {
	for _, candidate := range candidates {
		for _, capability := range candidate.Capabilities {
			if capability.Status == skilldiscovery.MatchMatched {
				matched++
			}
			if capability.Status == skilldiscovery.MatchAmbiguous {
				ambiguous++
			}
		}
	}
	return
}

func laneCounts(shortlist skilldiscovery.Shortlist) (dataRich, exploration int) {
	for _, entry := range shortlist.Entries {
		if entry.Lane == skilldiscovery.LaneDataRich {
			dataRich++
		}
		if entry.Lane == skilldiscovery.LaneExploration {
			exploration++
		}
	}
	return
}

func defaultSchemaPath() string {
	path := filepath.Join(defaultDiscoveryRoot, "xiaping-discovery-v1.schema.json")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return filepath.Join("..", "..", path)
}
func defaultLexiconPath() string {
	path := filepath.Join(defaultDiscoveryRoot, "xiaping-capability-lexicon-v1.json")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return filepath.Join("..", "..", path)
}
