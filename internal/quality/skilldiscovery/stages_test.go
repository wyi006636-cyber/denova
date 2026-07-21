package skilldiscovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadStagedDiscoveryArtifactsRejectsSchemaValidTamperedProvenance(t *testing.T) {
	root := t.TempDir()
	artifacts := artifactFixture(t)
	if err := WriteStagedDiscoveryArtifacts(root, StagedDiscoveryArtifacts{Manifest: artifacts.Manifest, Candidates: artifacts.Candidates, Proposals: artifacts.Proposals, Clusters: artifacts.Clusters}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "xiaping-writing-candidates-v1.json")
	var index CandidateIndex
	if err := LoadStrictJSON(path, &index); err != nil {
		t.Fatal(err)
	}
	index.Provenance.InputSHA256 = "sha256:" + strings.Repeat("0", 64)
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadStagedDiscoveryArtifacts(root, filepath.Join("..", "..", "..", "docs", "project-design", "implementation", "skills", "discovery", "xiaping-discovery-v1.schema.json"))
	if err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("error = %v", err)
	}
}
