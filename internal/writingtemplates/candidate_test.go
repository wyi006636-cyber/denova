package writingtemplates

import (
	"context"
	"errors"
	"testing"
)

func TestResolveForCandidateSnapshotsAndRejectsInvalidSources(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	snapshot, err := catalog.ResolveForCandidate(context.Background(), "draft.payoff-climax", 1)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ID != "draft.payoff-climax" || snapshot.Kind != KindTemplate || snapshot.Scope != "builtin" || snapshot.Source != "builtin" || snapshot.ContentVersion != 1 || snapshot.Title.EN == "" || snapshot.Body == "" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if len(snapshot.Revision) != len("sha256:")+64 || snapshot.Revision[:len("sha256:")] != "sha256:" {
		t.Fatalf("revision = %q", snapshot.Revision)
	}

	for _, test := range []struct {
		name string
		id   string
		cv   int
		want error
	}{
		{name: "technique", id: "diagnose.consistency-review", cv: 1, want: ErrNotUsableForCandidate},
		{name: "required zero", id: "draft.payoff-climax", cv: 0, want: ErrVersionRequired},
		{name: "required negative", id: "draft.payoff-climax", cv: -1, want: ErrVersionRequired},
		{name: "version changed", id: "draft.payoff-climax", cv: 2, want: ErrVersionChanged},
		{name: "missing", id: "draft.missing", cv: 1, want: ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := catalog.ResolveForCandidate(context.Background(), test.id, test.cv); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestResolveForCandidateRejectsTemplateWithoutChapterTask(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	item := userTemplate()
	item.Tasks = []string{TaskLocalRewrite}
	created, err := catalog.Create(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ResolveForCandidate(context.Background(), created.ID, created.ContentVersion); !errors.Is(err, ErrNotUsableForCandidate) {
		t.Fatalf("error = %v, want %v", err, ErrNotUsableForCandidate)
	}
}

func TestResolveForCandidateTracksBuiltinOverrideVersion(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	builtin, err := catalog.Get(context.Background(), "draft.payoff-climax")
	if err != nil {
		t.Fatal(err)
	}
	changed := builtin
	changed.Body += "\nUse only established facts."
	changed.ContentVersion++
	override, err := catalog.Update(context.Background(), builtin.ID, changed, builtin.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if override.Source != "user" || !override.BuiltinOverridden || override.BuiltinContentVersion != builtin.ContentVersion {
		t.Fatalf("override = %+v", override)
	}
	if _, err := catalog.ResolveForCandidate(context.Background(), builtin.ID, builtin.ContentVersion); !errors.Is(err, ErrVersionChanged) {
		t.Fatalf("old version error = %v, want %v", err, ErrVersionChanged)
	}
	snapshot, err := catalog.ResolveForCandidate(context.Background(), builtin.ID, override.ContentVersion)
	if err != nil || snapshot.Source != "user" || snapshot.Body != changed.Body {
		t.Fatalf("new version snapshot = %+v, err=%v", snapshot, err)
	}
}
