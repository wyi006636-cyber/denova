package handlers

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	writingcomparisonapp "denova/internal/app/writingcomparison"
)

func (h *Handlers) HandleWritingComparisonCandidateOptions(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	options, err := h.app.WritingComparison().CandidateOptions(ctx, scope)
	if err != nil {
		writeWritingComparisonError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, map[string]any{"project_id": scope.ProjectID, "profiles": options.Profiles, "max_candidates": options.MaxCandidates})
}

func (h *Handlers) HandleWritingComparisonCreate(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	var request writingcomparisonapp.CreateInput
	if err := decodeStrictJSONRequest(c.Request.Body(), &request); err != nil {
		writeWritingComparisonError(c, &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeInvalidRequest, Message: "invalid comparison request", Cause: writingcomparisonapp.ErrInvalidRequest})
		return
	}
	task, err := h.app.WritingComparison().Create(ctx, scope, request)
	if err != nil {
		writeWritingComparisonError(c, err)
		return
	}
	writeJSON(c, consts.StatusCreated, map[string]any{"project_id": scope.ProjectID, "task": task})
}

func (h *Handlers) HandleWritingComparisonList(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	options := writingcomparisonapp.ListOptions{Phase: writingcomparisonapp.Phase(strings.TrimSpace(c.Query("phase")))}
	if raw := strings.TrimSpace(c.Query("include_applied")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			writeWritingComparisonError(c, &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeInvalidRequest, Cause: writingcomparisonapp.ErrInvalidRequest})
			return
		}
		options.IncludeApplied = value
	}
	tasks, err := h.app.WritingComparison().List(ctx, scope, options)
	if err != nil {
		writeWritingComparisonError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, map[string]any{"project_id": scope.ProjectID, "tasks": tasks})
}

func (h *Handlers) HandleWritingComparisonDetail(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	task, err := h.app.WritingComparison().Get(ctx, scope, strings.TrimSpace(c.Param("id")))
	if err != nil {
		writeWritingComparisonError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, map[string]any{"project_id": scope.ProjectID, "task": task})
}

func writeWritingComparisonError(c *app.RequestContext, err error) {
	code := writingcomparisonapp.CodeInternal
	status := consts.StatusInternalServerError
	key := "api.writingComparison.internal"
	var typed *writingcomparisonapp.Error
	if errors.As(err, &typed) && typed.Code != "" {
		code = typed.Code
	}
	switch {
	case errors.Is(err, writingcomparisonapp.ErrNotFound):
		status, key, code = consts.StatusNotFound, "api.writingComparison.notFound", writingcomparisonapp.CodeNotFound
	case errors.Is(err, writingcomparisonapp.ErrRevisionConflict):
		status, key, code = consts.StatusConflict, "api.writingComparison.revisionConflict", writingcomparisonapp.CodeRevisionConflict
	case errors.Is(err, writingcomparisonapp.ErrStateConflict):
		status, key, code = consts.StatusConflict, "api.writingComparison.stateConflict", writingcomparisonapp.CodeStateConflict
	case code == writingcomparisonapp.CodeCandidateCount:
		status, key = consts.StatusBadRequest, "api.writingComparison.candidateCount"
	case code == writingcomparisonapp.CodeTargetPathInvalid:
		status, key = consts.StatusBadRequest, "api.writingComparison.targetPathInvalid"
	case code == writingcomparisonapp.CodeProfileUnavailable:
		status, key = consts.StatusBadRequest, "api.writingComparison.profileUnavailable"
	case code == writingcomparisonapp.CodeTemplateUnavailable || code == writingcomparisonapp.CodeTemplateCatalogUnavailable || code == writingcomparisonapp.CodeTemplateMissing || code == writingcomparisonapp.CodeTemplateNotUsable || code == writingcomparisonapp.CodeTemplateVersionChanged:
		status, key = consts.StatusBadRequest, "api.writingComparison.templateUnavailable"
	case code == writingcomparisonapp.CodeSkillMissing || code == writingcomparisonapp.CodeSkillNotUsable || code == writingcomparisonapp.CodeSkillVersionChanged:
		status, key = consts.StatusBadRequest, "api.writingComparison.templateUnavailable"
	case code == writingcomparisonapp.CodeInvalidRequest:
		status, key = consts.StatusBadRequest, "api.writingComparison.invalidRequest"
	case code == writingcomparisonapp.CodeAssetCorrupt:
		status, key = consts.StatusConflict, "api.writingComparison.assetCorrupt"
	}
	details := map[string]any{}
	if typed != nil && typed.Details != nil {
		details = typed.Details
	}
	writeJSON(c, status, map[string]any{"error": messageKey(c, key), "code": key, "reason": code, "details": details})
}
