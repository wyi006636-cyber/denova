package evaluation

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExportRunIndexContainsHashesAndAggregatesOnly(t *testing.T) {
	fixture := writeReadyCohortRun(t, SplitRegression)
	if _, err := PackageBlind(fixture.RunRoot, fixture.RunID); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "index.json")
	index, err := ExportRunIndex(fixture.RunRoot, []string{fixture.RunID}, output)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{fixture.RunRoot, "output_file", "reviewer_id", "raw_comments", "authorization"} {
		if bytes.Contains(payload, []byte(forbidden)) {
			t.Fatalf("bounded index contains %q", forbidden)
		}
	}
	if len(index.Runs) != 1 || index.Runs[0].RunID != fixture.RunID {
		t.Fatalf("index=%#v", index)
	}
	entry := index.Runs[0]
	if entry.Selection.DataSplits[0] != SplitRegression || entry.HarnessPolicySHA256 == "" || entry.BaselineTemplateSHA256 == "" || len(entry.ModelConfigSHA256) == 0 || entry.BlindPackageSHA256 == "" {
		t.Fatalf("entry=%#v", entry)
	}
}
