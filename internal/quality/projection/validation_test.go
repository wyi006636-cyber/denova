package projection

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestRebuildRevalidatesClosedSiblingBeforeSourceCASAndActivation(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(*testing.T, *sql.DB)
	}{
		{
			name: "schema",
			tamper: func(t *testing.T, db *sql.DB) {
				if _, err := db.Exec("PRAGMA user_version = 2"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "document hash",
			tamper: func(t *testing.T, db *sql.DB) {
				if _, err := db.Exec("UPDATE source_documents SET content='closed sibling tamper'"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "external content",
			tamper: func(t *testing.T, db *sql.DB) {
				if _, err := db.Exec("INSERT INTO source_documents_fts(source_documents_fts) VALUES ('delete-all')"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "schema object",
			tamper: func(t *testing.T, db *sql.DB) {
				if _, err := db.Exec("DROP TRIGGER source_documents_au"); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeProjectionTestSource(t, workspace, "chapters/ch1.md", "authoritative closed sibling source")
			service, err := newProjectionTestService(t, Options{
				Workspace: workspace,
				Hooks: Hooks{OnFault: func(point FaultPoint) error {
					if point != FaultAfterConnectionClose {
						return nil
					}
					stagePath := currentProjectionTestStagePath(t, workspace)
					db := openProjectionTestDatabase(t, stagePath)
					test.tamper(t, db)
					if err := db.Close(); err != nil {
						t.Fatal(err)
					}
					return nil
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Rebuild(context.Background())
			if err == nil || result.Activated {
				t.Fatalf("tampered closed sibling result=%#v err=%v", result, err)
			}
			if _, err := os.Lstat(filepath.Join(workspace, filepath.FromSlash(DatabaseRelativePath))); !os.IsNotExist(err) {
				t.Fatalf("tampered sibling became visible: %v", err)
			}
			content, readErr := os.ReadFile(filepath.Join(workspace, "chapters", "ch1.md"))
			if readErr != nil || string(content) != "authoritative closed sibling source" {
				t.Fatalf("source content=%q err=%v", content, readErr)
			}
		})
	}
}
