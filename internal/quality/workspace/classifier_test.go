package workspace

import (
	"reflect"
	"testing"
)

func TestClassifyPathImplementsExactWorkspaceSchemaV1Order(t *testing.T) {
	tests := []struct {
		path        string
		category    Category
		disposition VersionDisposition
	}{
		// Pending-review and audit records win before similarly named runtime paths.
		{".denova/quality/artifacts/candidate.json", CategoryReviewArtifact, VersionInclude},
		{".denova/quality/artifacts/reviews/issue.json", CategoryReviewArtifact, VersionInclude},
		{".denova/quality/artifacts/runs/output.json", CategoryReviewArtifact, VersionInclude},
		{".denova/quality/decisions/d1.json", CategoryReviewArtifact, VersionInclude},
		{".denova/quality/finalizations/f1.json", CategoryReviewArtifact, VersionInclude},
		{".denova/quality/migration-receipts/m1.json", CategoryReviewArtifact, VersionInclude},

		// Existing runtime/recovery paths are exact beneath both genuine roots.
		{".denova/runs/r1.jsonl", CategoryRuntimeRecovery, VersionExclude},
		{".nova/checkpoints/agent/c1.json", CategoryRuntimeRecovery, VersionExclude},
		{".denova/sessions/s1.jsonl", CategoryRuntimeRecovery, VersionExclude},
		{".nova/backups/b1.json", CategoryRuntimeRecovery, VersionExclude},
		{".denova/messages/m1.json", CategoryRuntimeRecovery, VersionExclude},
		{".nova/changes/ledger.jsonl", CategoryRuntimeRecovery, VersionExclude},
		{".denova/reviews/ledger.jsonl", CategoryRuntimeRecovery, VersionExclude},
		{".nova/interactive/story.json", CategoryRuntimeRecovery, VersionExclude},
		{".denova/automations/inbox.json", CategoryRuntimeRecovery, VersionExclude},
		{".nova/automations/inbox.json", CategoryRuntimeRecovery, VersionExclude},
		{".denova/quality/runs/r1/checkpoint.json", CategoryRuntimeRecovery, VersionExclude},
		{".denova-migration/m1/preview.json", CategoryRuntimeRecovery, VersionExclude},
		{".nova-migration/m1/backup/file", CategoryRuntimeRecovery, VersionExclude},
		{".denova-migrate-m1.tmp", CategoryRuntimeRecovery, VersionExclude},
		{".nova-migrate-m1.tmp", CategoryRuntimeRecovery, VersionExclude},
		{".git/objects/aa/bb", CategoryRuntimeRecovery, VersionExclude},

		// Only the canonical current root owns v1 projections.
		{".denova/index.db", CategoryRebuildableProjection, VersionExclude},
		{".denova/index.db-wal", CategoryRebuildableProjection, VersionExclude},
		{".denova/cache/context.json", CategoryRebuildableProjection, VersionExclude},
		{".denova/quality/projections/fts.json", CategoryRebuildableProjection, VersionExclude},

		// Existing formal and configuration files remain included under both roots.
		{"book.json", CategoryFormalAuthoritative, VersionInclude},
		{"CREATOR.md", CategoryFormalAuthoritative, VersionInclude},
		{"ideas.md", CategoryFormalAuthoritative, VersionInclude},
		{"setting/outline.md", CategoryFormalAuthoritative, VersionInclude},
		{"chapters/第一章.md", CategoryFormalAuthoritative, VersionInclude},
		{".denova/workspace-schema.json", CategoryFormalAuthoritative, VersionInclude},
		{".denova/lore/items.json", CategoryFormalAuthoritative, VersionInclude},
		{".nova/lore/items.json", CategoryFormalAuthoritative, VersionInclude},
		{".denova/profile-lock.json", CategoryFormalAuthoritative, VersionInclude},
		{".denova/quality/specs/project.json", CategoryFormalAuthoritative, VersionInclude},
		{".denova/quality/preferences.jsonl", CategoryFormalAuthoritative, VersionInclude},
		{".nova/chapter_statuses.json", CategoryFormalAuthoritative, VersionInclude},
		{".denova/skills/custom/SKILL.md", CategoryFormalAuthoritative, VersionInclude},
		{".nova/styles/prose.md", CategoryFormalAuthoritative, VersionInclude},
		{".denova/automations/tasks.json", CategoryFormalAuthoritative, VersionInclude},

		// v1-looking legacy inputs are protected unknown data, never v1 authority.
		{".nova/workspace-schema.json", CategoryProtectedLegacyUnknown, VersionInclude},
		{".nova/profile-lock.json", CategoryProtectedLegacyUnknown, VersionInclude},
		{".nova/quality/runs/r1.json", CategoryProtectedLegacyUnknown, VersionInclude},
		{".nova/index.db", CategoryProtectedLegacyUnknown, VersionInclude},
		{".nova/index.db-wal", CategoryProtectedLegacyUnknown, VersionInclude},
		{".nova/cache/context.json", CategoryProtectedLegacyUnknown, VersionInclude},

		// Lookalikes and unknown paths follow default-protect/default-include.
		{".denova/automations/inbox.json.bak", CategoryProtectedLegacyUnknown, VersionInclude},
		{"other/automations/inbox.json", CategoryFormalAuthoritative, VersionInclude},
		{".denova/index.db.bak", CategoryProtectedLegacyUnknown, VersionInclude},
		{".denova/quality/projections-old/index.json", CategoryProtectedLegacyUnknown, VersionInclude},
		{"notes/.denova-migrate-m1.tmp", CategoryFormalAuthoritative, VersionInclude},
		{".denova/quality/unknown.json", CategoryProtectedLegacyUnknown, VersionInclude},
		{".custom/private.bin", CategoryProtectedLegacyUnknown, VersionInclude},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			got, err := ClassifyPath(test.path)
			if err != nil {
				t.Fatalf("ClassifyPath(%q): %v", test.path, err)
			}
			if got.Path != test.path || got.Category != test.category || got.VersionDisposition != test.disposition || got.Rule == "" {
				t.Fatalf("ClassifyPath(%q) = %#v, want category=%q disposition=%q with rule", test.path, got, test.category, test.disposition)
			}
		})
	}
}

func TestWorkspaceSchemaV1CategoriesAreExhaustive(t *testing.T) {
	want := []Category{
		CategoryFormalAuthoritative,
		CategoryReviewArtifact,
		CategoryRuntimeRecovery,
		CategoryRebuildableProjection,
		CategoryProtectedLegacyUnknown,
	}
	if got := AllCategories(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllCategories() = %#v, want %#v", got, want)
	}
}
