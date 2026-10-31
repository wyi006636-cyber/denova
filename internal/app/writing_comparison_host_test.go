package app

import (
	"context"
	"errors"
	"testing"

	writingcomparisonapp "denova/internal/app/writingcomparison"
	"denova/internal/writingtemplates"
)

func TestWritingTemplateResolverMapsCandidateErrors(t *testing.T) {
	resolver := writingTemplateResolver{catalog: writingtemplates.NewCatalog(t.TempDir())}
	tests := []struct {
		name string
		ref  writingcomparisonapp.WritingSourceRef
		code string
	}{
		{name: "missing", ref: writingcomparisonapp.WritingSourceRef{Kind: "template", ID: "draft.missing", Version: "1"}, code: writingcomparisonapp.CodeTemplateMissing},
		{name: "not usable", ref: writingcomparisonapp.WritingSourceRef{Kind: "template", ID: "diagnose.consistency-review", Version: "1"}, code: writingcomparisonapp.CodeTemplateNotUsable},
		{name: "version changed", ref: writingcomparisonapp.WritingSourceRef{Kind: "template", ID: "draft.payoff-climax", Version: "2"}, code: writingcomparisonapp.CodeTemplateVersionChanged},
		{name: "version required", ref: writingcomparisonapp.WritingSourceRef{Kind: "template", ID: "draft.payoff-climax"}, code: writingcomparisonapp.CodeTemplateVersionRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolver.ResolveCandidateSource(context.Background(), test.ref)
			var typed *writingcomparisonapp.Error
			if !errors.As(err, &typed) || typed.Code != test.code {
				t.Fatalf("error=%v code=%q, want %s", err, typedCode(typed), test.code)
			}
		})
	}
	snapshot, err := resolver.ResolveCandidateSource(context.Background(), writingcomparisonapp.WritingSourceRef{Kind: "template", ID: "draft.payoff-climax", Version: "1"})
	if err != nil || snapshot.Scope != "builtin" || snapshot.Title.EN == "" || snapshot.ContentSHA256 == "" || snapshot.Body == "" {
		t.Fatalf("resolved snapshot=%+v err=%v", snapshot, err)
	}
}

func typedCode(err *writingcomparisonapp.Error) string {
	if err == nil {
		return ""
	}
	return err.Code
}
