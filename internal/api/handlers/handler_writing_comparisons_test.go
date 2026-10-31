package handlers

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	writingcomparisonapp "denova/internal/app/writingcomparison"
)

func TestWriteWritingComparisonErrorBranches(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"internal", errors.New("boom"), consts.StatusInternalServerError, writingcomparisonapp.CodeInternal},
		{"not found", writingcomparisonapp.ErrNotFound, consts.StatusNotFound, writingcomparisonapp.CodeNotFound},
		{"revision", writingcomparisonapp.ErrRevisionConflict, consts.StatusConflict, writingcomparisonapp.CodeRevisionConflict},
		{"state", writingcomparisonapp.ErrStateConflict, consts.StatusConflict, writingcomparisonapp.CodeStateConflict},
		{"run active", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeRunActive, Cause: writingcomparisonapp.ErrStateConflict}, consts.StatusConflict, writingcomparisonapp.CodeRunActive},
		{"recovery failed", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeRecoveryFailed, Cause: errors.New("unreadable")}, consts.StatusInternalServerError, writingcomparisonapp.CodeRecoveryFailed},
		{"candidate count", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeCandidateCount}, consts.StatusBadRequest, writingcomparisonapp.CodeCandidateCount},
		{"target", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeTargetPathInvalid}, consts.StatusBadRequest, writingcomparisonapp.CodeTargetPathInvalid},
		{"profile", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeProfileUnavailable}, consts.StatusBadRequest, writingcomparisonapp.CodeProfileUnavailable},
		{"template version", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeTemplateVersionRequired}, consts.StatusBadRequest, writingcomparisonapp.CodeTemplateVersionRequired},
		{"template", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeTemplateCatalogUnavailable}, consts.StatusBadRequest, writingcomparisonapp.CodeTemplateCatalogUnavailable},
		{"template missing", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeTemplateMissing}, consts.StatusBadRequest, writingcomparisonapp.CodeTemplateMissing},
		{"skill missing", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeSkillMissing}, consts.StatusBadRequest, writingcomparisonapp.CodeSkillMissing},
		{"skill unusable", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeSkillNotUsable}, consts.StatusBadRequest, writingcomparisonapp.CodeSkillNotUsable},
		{"skill changed", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeSkillVersionChanged}, consts.StatusBadRequest, writingcomparisonapp.CodeSkillVersionChanged},
		{"invalid", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeInvalidRequest}, consts.StatusBadRequest, writingcomparisonapp.CodeInvalidRequest},
		{"asset", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeAssetCorrupt}, consts.StatusConflict, writingcomparisonapp.CodeAssetCorrupt},
		{"context budget", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeContextBudgetExceeded}, consts.StatusBadRequest, writingcomparisonapp.CodeContextBudgetExceeded},
		{"empty output", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeEmptyOutput}, consts.StatusBadGateway, writingcomparisonapp.CodeEmptyOutput},
		{"model error", &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeModelError}, consts.StatusBadGateway, writingcomparisonapp.CodeModelError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := app.NewContext(0)
			writeWritingComparisonError(ctx, test.err)
			if ctx.Response.StatusCode() != test.status {
				t.Fatalf("status=%d body=%s, want %d", ctx.Response.StatusCode(), ctx.Response.Body(), test.status)
			}
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(ctx.Response.Body(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != test.code {
				t.Fatalf("code=%q body=%s, want %q", body.Code, ctx.Response.Body(), test.code)
			}
			if len(body.Code) == 0 || string(ctx.Response.Body()) == "" {
				t.Fatal("error body is empty")
			}
		})
	}
}
