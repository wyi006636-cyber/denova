package handlers

import (
	"context"
	"errors"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	casebookapp "denova/internal/app/casebook"
)

func (h *Handlers) HandleProjectCasebook(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	snapshot, err := h.app.Casebook().List(ctx, scope.ProjectID)
	if err != nil {
		writeCasebookError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, map[string]any{
		"project_id": scope.ProjectID,
		"index":      snapshot.Index,
		"entries":    snapshot.Entries,
		"cases":      snapshot.Cases,
	})
}

func (h *Handlers) HandleProjectCasebookToggle(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	var request casebookapp.ToggleRequest
	if err := c.BindJSON(&request); err != nil {
		writeErrorKey(c, consts.StatusBadRequest, "api.casebook.invalidRequest")
		return
	}
	switch c.Param("action") {
	case "favorite":
		request.Collected = true
	case "unfavorite":
		request.Collected = false
	default:
		writeErrorKey(c, consts.StatusBadRequest, "api.casebook.invalidRequest")
		return
	}
	result, err := h.app.Casebook().Toggle(ctx, scope.ProjectID, request)
	if err != nil {
		writeCasebookError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, map[string]any{
		"project_id": scope.ProjectID,
		"case":       result.Case,
		"index":      result.Index,
		"collected":  result.Collected,
	})
}

func writeCasebookError(c *app.RequestContext, err error) {
	code := casebookapp.CaseInvalid
	status := consts.StatusBadRequest
	var typed *casebookapp.Error
	if errors.As(err, &typed) && typed.Code != "" {
		code = typed.Code
	}
	switch code {
	case casebookapp.CaseNotFound, casebookapp.CaseIndexMissing, casebookapp.CaseSourceMissing:
		status = consts.StatusNotFound
	case casebookapp.CaseRevisionConflict:
		status = consts.StatusConflict
	case casebookapp.CaseInvalid, casebookapp.CaseSourceOutsideBook, casebookapp.CaseQuoteMissing, casebookapp.CaseQuoteAmbiguous, casebookapp.CaseQuoteTooLong, casebookapp.CaseQuoteEmpty:
		status = consts.StatusBadRequest
	default:
		status = consts.StatusInternalServerError
	}
	key := "api.casebook.invalid"
	switch code {
	case casebookapp.CaseNotFound:
		key = "api.casebook.notFound"
	case casebookapp.CaseIndexMissing:
		key = "api.casebook.indexMissing"
	case casebookapp.CaseRevisionConflict:
		key = "api.casebook.revisionConflict"
	case casebookapp.CaseQuoteMissing:
		key = "api.casebook.quoteMissing"
	case casebookapp.CaseQuoteAmbiguous:
		key = "api.casebook.quoteAmbiguous"
	case casebookapp.CaseQuoteTooLong:
		key = "api.casebook.quoteTooLong"
	case casebookapp.CaseQuoteEmpty:
		key = "api.casebook.quoteEmpty"
	case casebookapp.CaseSourceMissing:
		key = "api.casebook.sourceMissing"
	case casebookapp.CaseSourceOutsideBook:
		key = "api.casebook.sourceOutsideBook"
	}
	writeJSON(c, status, map[string]any{"error": messageKey(c, key), "code": code})
}
