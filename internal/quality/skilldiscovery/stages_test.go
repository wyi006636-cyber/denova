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

func TestLoadStagedDiscoveryArtifactsRejectsAllProvenanceFields(t *testing.T) {
	for _, field := range []string{"candidate source", "candidate purpose", "candidate input_sha256", "candidate max_bytes", "proposal source", "proposal purpose", "proposal input_sha256", "proposal max_bytes", "cluster source", "cluster purpose", "cluster input_sha256", "cluster max_bytes", "manifest provenance", "manifest hash"} {
		t.Run(field, func(t *testing.T) {
			root := t.TempDir()
			artifacts := artifactFixture(t)
			if err := WriteStagedDiscoveryArtifacts(root, StagedDiscoveryArtifacts{Manifest: artifacts.Manifest, Candidates: artifacts.Candidates, Proposals: artifacts.Proposals, Clusters: artifacts.Clusters}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "xiaping-writing-candidates-v1.json")
			if strings.HasPrefix(field, "proposal") {
				path = filepath.Join(root, "xiaping-capability-proposals-v1.json")
			}
			if strings.HasPrefix(field, "cluster") {
				path = filepath.Join(root, "xiaping-duplicate-clusters-v1.json")
			}
			if strings.HasPrefix(field, "manifest") {
				path = filepath.Join(root, "xiaping-snapshot-manifest-v1.json")
			}
			var raw map[string]any
			data, _ := os.ReadFile(path)
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatal(err)
			}
			if field == "manifest hash" {
				raw["skill_records_sha256"] = "sha256:" + strings.Repeat("0", 64)
			} else {
				p := raw["provenance"].(map[string]any)
				switch {
				case strings.Contains(field, "source"):
					p["source"] = "wrong"
				case strings.Contains(field, "purpose"):
					p["purpose"] = "wrong"
				case strings.Contains(field, "input_sha256"):
					p["input_sha256"] = "sha256:" + strings.Repeat("0", 64)
				default:
					p["max_bytes"] = float64(1)
				}
			}
			data, _ = json.MarshalIndent(raw, "", "  ")
			if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadStagedDiscoveryArtifacts(root, filepath.Join("..", "..", "..", "docs", "project-design", "implementation", "skills", "discovery", "xiaping-discovery-v1.schema.json")); err == nil {
				t.Fatal("accepted tampered staged artifact")
			}
		})
	}
}
