package handlers

import (
	"context"
	"errors"
	"log"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	novaApp "denova/internal/app"
	"denova/internal/shortfiction"
	"denova/internal/workspacechange"
)

type shortFictionErrorResponse struct {
	Error   string         `json:"error"`
	Code    string         `json:"code"`
	Details map[string]any `json:"details,omitempty"`
}

// shortFictionCandidateGenerateRequest is the public wire shape. Source and
// locale remain server-authoritative and are intentionally absent.
type shortFictionCandidateGenerateRequest struct {
	Workspace    string                 `json:"workspace"`
	ProfileID    shortfiction.ProfileID `json:"profile_id"`
	TargetPath   string                 `json:"target_path"`
	BaseRevision string                 `json:"base_revision"`
	Brief        string                 `json:"brief"`
}

// HandleShortFictionCandidateGenerate exposes stateless, no-write candidate generation.
func (h *Handlers) HandleShortFictionCandidateGenerate(ctx context.Context, c *app.RequestContext) {
	var body shortFictionCandidateGenerateRequest
	if err := c.BindJSON(&body); err != nil {
		writeShortFictionError(c, "generate_bind", shortfiction.NewError("invalid_request", "invalid request body", nil))
		return
	}
	req := shortfiction.GenerateRequest{
		ProfileID: body.ProfileID,
		Source: shortfiction.SourcePacket{
			Workspace:    body.Workspace,
			TargetPath:   body.TargetPath,
			BaseRevision: body.BaseRevision,
			Brief:        body.Brief,
		},
	}
	candidate, err := h.app.GenerateShortFictionCandidate(ctx, req, requestLocale(c))
	if err != nil {
		writeShortFictionError(c, "generate", err)
		return
	}
	writeJSON(c, consts.StatusOK, candidate)
}

// HandleShortFictionCandidateConfirm writes only the integrity-bound candidate explicitly returned by the client.
func (h *Handlers) HandleShortFictionCandidateConfirm(ctx context.Context, c *app.RequestContext) {
	var req shortfiction.ConfirmRequest
	if err := c.BindJSON(&req); err != nil {
		writeShortFictionError(c, "confirm_bind", shortfiction.NewError("invalid_request", "invalid request body", nil))
		return
	}
	result, err := h.app.ConfirmShortFictionCandidate(ctx, req)
	if err != nil {
		writeShortFictionError(c, "confirm", err)
		return
	}
	writeJSON(c, consts.StatusOK, result)
}

func writeShortFictionError(c *app.RequestContext, operation string, err error) {
	status, code, messageKey, details := mapShortFictionError(err)
	log.Printf("[short-fiction-api] request failed file=handler_short_fiction.go operation=%s status=%d code=%q", operation, status, code)
	writeJSON(c, status, shortFictionErrorResponse{
		Error:   requestLocalizer(c).T(messageKey),
		Code:    code,
		Details: details,
	})
}

func mapShortFictionError(err error) (int, string, string, map[string]any) {
	details := map[string]any{"workspace_mutated": false}
	var domainErr *shortfiction.Error
	if errors.As(err, &domainErr) {
		for key, value := range domainErr.Details {
			details[key] = value
		}
		switch domainErr.Code {
		case "invalid_request":
			return consts.StatusBadRequest, domainErr.Code, "api.shortFiction.invalidRequest", details
		case shortfiction.ErrorCodeCandidateMismatch:
			return consts.StatusBadRequest, domainErr.Code, "api.shortFiction.candidateMismatch", details
		case shortfiction.ErrorCodeInvalidSource:
			return consts.StatusBadRequest, domainErr.Code, "api.shortFiction.invalidSource", details
		case shortfiction.ErrorCodeInvalidProfile:
			return consts.StatusBadRequest, domainErr.Code, "api.shortFiction.invalidProfile", details
		case shortfiction.ErrorCodeOversized:
			return consts.StatusRequestEntityTooLarge, domainErr.Code, "api.shortFiction.oversized", details
		case "generation_empty":
			return consts.StatusBadGateway, domainErr.Code, "api.shortFiction.generationEmpty", details
		case "candidate_too_large":
			return consts.StatusBadGateway, domainErr.Code, "api.shortFiction.candidateTooLarge", details
		case "generation_failed":
			return consts.StatusBadGateway, domainErr.Code, "api.shortFiction.generationFailed", details
		}
	}

	var changeErr *workspacechange.Error
	if errors.As(err, &changeErr) {
		switch changeErr.Code {
		case workspacechange.ErrorCodeInvalidEdit:
			return consts.StatusBadRequest, changeErr.Code, "api.shortFiction.noChange", details
		case workspacechange.ErrorCodeRevisionConflict:
			return consts.StatusConflict, changeErr.Code, "api.shortFiction.revisionConflict", details
		}
	}
	if errors.Is(err, novaApp.ErrNoWorkspace) || errors.Is(err, novaApp.ErrWorkspaceChanged) {
		return consts.StatusConflict, "workspace_conflict", "api.shortFiction.workspaceConflict", details
	}
	return consts.StatusInternalServerError, "internal_error", "api.shortFiction.internalError", details
}
