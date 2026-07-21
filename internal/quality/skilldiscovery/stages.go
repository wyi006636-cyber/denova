package skilldiscovery

import (
	"fmt"
	"path/filepath"
)

var stagedArtifactNames = artifactNames[:4]

// StagedDiscoveryArtifacts is the validated handoff from classification to ranking.
type StagedDiscoveryArtifacts struct {
	Manifest   SnapshotManifest
	Candidates []CandidateRecord
	Proposals  []CapabilityProposal
	Clusters   []DuplicateCluster
}

// WriteSnapshotManifest publishes the cache snapshot identity as the first stage.
func WriteSnapshotManifest(root string, manifest SnapshotManifest) error {
	if err := ValidateSnapshotManifest(manifest); err != nil {
		return err
	}
	manifest = normalizedManifest(manifest)
	return publishJSONTransaction(root, map[string]any{"xiaping-snapshot-manifest-v1.json": withProvenance(manifest, manifest, "snapshot manifest")}, []string{"xiaping-snapshot-manifest-v1.json"})
}

// WriteStagedDiscoveryArtifacts atomically advances a COMPLETE snapshot through classification.
func WriteStagedDiscoveryArtifacts(root string, artifacts StagedDiscoveryArtifacts) error {
	if err := ValidateSnapshotManifest(artifacts.Manifest); err != nil {
		return err
	}
	if artifacts.Manifest.Status != SnapshotComplete {
		return fmt.Errorf("complete snapshot required for staged artifacts")
	}
	artifacts.Manifest = normalizedManifest(artifacts.Manifest)
	if err := ValidateSkillRecords(candidateSkills(artifacts.Candidates)); err != nil {
		return err
	}
	values := map[string]any{
		"xiaping-snapshot-manifest-v1.json":    withProvenance(artifacts.Manifest, artifacts.Manifest, "snapshot manifest"),
		"xiaping-writing-candidates-v1.json":   CandidateIndex{Contract: candidateIndexContract, Version: "v1", SnapshotID: artifacts.Manifest.SnapshotID, Candidates: normalizedCandidates(artifacts.Candidates), Provenance: artifactProvenance(artifacts.Manifest, "candidate index")},
		"xiaping-capability-proposals-v1.json": CapabilityProposalIndex{Contract: proposalIndexContract, Version: "v1", SnapshotID: artifacts.Manifest.SnapshotID, Proposals: artifacts.Proposals, Provenance: artifactProvenance(artifacts.Manifest, "capability proposals")},
		"xiaping-duplicate-clusters-v1.json":   DuplicateClusterIndex{Contract: clusterIndexContract, Version: "v1", SnapshotID: artifacts.Manifest.SnapshotID, Clusters: artifacts.Clusters, Provenance: artifactProvenance(artifacts.Manifest, "duplicate clusters")},
	}
	return publishJSONTransaction(root, values, stagedArtifactNames)
}

func normalizedManifest(manifest SnapshotManifest) SnapshotManifest {
	if manifest.Pages == nil {
		manifest.Pages = []PageReceipt{}
	}
	if manifest.Failures == nil {
		manifest.Failures = []SnapshotFailure{}
	}
	return manifest
}

// LoadStagedDiscoveryArtifacts strictly loads the exact classified handoff for ranking.
func LoadStagedDiscoveryArtifacts(root, schema string) (StagedDiscoveryArtifacts, error) {
	paths := make([]string, 0, len(stagedArtifactNames))
	for _, name := range stagedArtifactNames {
		paths = append(paths, filepath.Join(root, name))
	}
	if err := ValidateArtifactSchema(schema, paths); err != nil {
		return StagedDiscoveryArtifacts{}, err
	}
	var manifest SnapshotManifest
	var candidates CandidateIndex
	var proposals CapabilityProposalIndex
	var clusters DuplicateClusterIndex
	if err := LoadStrictJSON(paths[0], &manifest); err != nil {
		return StagedDiscoveryArtifacts{}, err
	}
	if err := LoadStrictJSON(paths[1], &candidates); err != nil {
		return StagedDiscoveryArtifacts{}, err
	}
	if err := LoadStrictJSON(paths[2], &proposals); err != nil {
		return StagedDiscoveryArtifacts{}, err
	}
	if err := LoadStrictJSON(paths[3], &clusters); err != nil {
		return StagedDiscoveryArtifacts{}, err
	}
	if err := ValidateSnapshotManifest(manifest); err != nil || manifest.Status != SnapshotComplete {
		return StagedDiscoveryArtifacts{}, fmt.Errorf("staged manifest is not COMPLETE")
	}
	if err := validateStageProvenance(manifest, manifest.Provenance, "snapshot manifest"); err != nil {
		return StagedDiscoveryArtifacts{}, err
	}
	if candidates.Contract != candidateIndexContract || proposals.Contract != proposalIndexContract || clusters.Contract != clusterIndexContract || candidates.SnapshotID != manifest.SnapshotID || proposals.SnapshotID != manifest.SnapshotID || clusters.SnapshotID != manifest.SnapshotID {
		return StagedDiscoveryArtifacts{}, fmt.Errorf("staged artifact snapshot identity mismatch")
	}
	if err := ValidateSkillRecords(candidateSkills(candidates.Candidates)); err != nil {
		return StagedDiscoveryArtifacts{}, err
	}
	if err := validateStageProvenance(manifest, candidates.Provenance, "candidate index"); err != nil {
		return StagedDiscoveryArtifacts{}, err
	}
	if err := validateStageProvenance(manifest, proposals.Provenance, "capability proposals"); err != nil {
		return StagedDiscoveryArtifacts{}, err
	}
	if err := validateStageProvenance(manifest, clusters.Provenance, "duplicate clusters"); err != nil {
		return StagedDiscoveryArtifacts{}, err
	}
	return StagedDiscoveryArtifacts{Manifest: manifest, Candidates: candidates.Candidates, Proposals: proposals.Proposals, Clusters: clusters.Clusters}, nil
}

func validateStageProvenance(manifest SnapshotManifest, provenance ArtifactProvenance, purpose string) error {
	if provenance.Source != "completed snapshot manifest receipts" || provenance.Purpose != purpose || provenance.InputSHA256 != manifest.SkillRecordsSHA256 || provenance.MaxBytes != artifactMaxBytes {
		return fmt.Errorf("staged %s provenance mismatch", purpose)
	}
	return nil
}
