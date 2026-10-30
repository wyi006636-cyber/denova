package handlers

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"denova/internal/writingtemplates"
)

func (h *Handlers) HandleWritingTemplates(ctx context.Context, c *app.RequestContext) {
	items, err := h.app.WritingTemplates().List(ctx, writingtemplates.ListFilter{
		Kind: c.Query("kind"), Task: c.Query("task"), Category: c.Query("category"), Query: c.Query("q"),
	})
	if err != nil {
		writeWritingTemplateError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, map[string]any{"writing_templates": items})
}

func (h *Handlers) HandleWritingTemplateCategories(ctx context.Context, c *app.RequestContext) {
	categories, err := h.app.WritingTemplates().Categories(ctx)
	if err != nil {
		writeWritingTemplateError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, map[string]any{"categories": categories})
}

func (h *Handlers) HandleWritingTemplate(ctx context.Context, c *app.RequestContext) {
	item, err := h.app.WritingTemplates().Get(ctx, c.Param("id"))
	if err != nil {
		writeWritingTemplateError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, item)
}

func (h *Handlers) HandleWritingTemplateCreate(ctx context.Context, c *app.RequestContext) {
	var item writingtemplates.Template
	if err := c.BindJSON(&item); err != nil {
		writeErrorKey(c, consts.StatusBadRequest, "api.common.invalidRequestWithDetail", "detail", err.Error())
		return
	}
	created, err := h.app.WritingTemplates().Create(ctx, item)
	if err != nil {
		writeWritingTemplateError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, created)
}

func (h *Handlers) HandleWritingTemplateUpdate(ctx context.Context, c *app.RequestContext) {
	var body struct {
		writingtemplates.Template
		BaseRevision string `json:"base_revision"`
	}
	if err := c.BindJSON(&body); err != nil {
		writeErrorKey(c, consts.StatusBadRequest, "api.common.invalidRequestWithDetail", "detail", err.Error())
		return
	}
	updated, err := h.app.WritingTemplates().Update(ctx, c.Param("id"), body.Template, body.BaseRevision)
	if err != nil {
		writeWritingTemplateError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, updated)
}

func (h *Handlers) HandleWritingTemplateDelete(ctx context.Context, c *app.RequestContext) {
	if err := h.app.WritingTemplates().Delete(ctx, c.Param("id")); err != nil {
		writeWritingTemplateError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) HandleWritingTemplatePreview(ctx context.Context, c *app.RequestContext) {
	var body struct {
		TargetHint string `json:"target_hint"`
	}
	if err := c.BindJSON(&body); err != nil {
		// An empty POST body is a useful preview request and is equivalent to the
		// default target hint, so only reject malformed non-empty JSON.
		if len(c.Request.Body()) > 0 {
			writeErrorKey(c, consts.StatusBadRequest, "api.common.invalidRequestWithDetail", "detail", err.Error())
			return
		}
	}
	preview, err := h.app.WritingTemplates().Preview(ctx, c.Param("id"), body.TargetHint)
	if err != nil {
		writeWritingTemplateError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, preview)
}

func writeWritingTemplateError(c *app.RequestContext, err error) {
	status := consts.StatusBadRequest
	code := "invalid_request"
	key := "api.common.invalidRequestWithDetail"
	args := []any{"detail", strings.TrimSpace(err.Error())}
	switch {
	case errors.Is(err, writingtemplates.ErrNotFound):
		status, code, key, args = consts.StatusNotFound, "template_not_found", "api.writingTemplates.notFound", nil
	case errors.Is(err, writingtemplates.ErrRevisionConflict):
		status, code, key, args = consts.StatusConflict, "revision_conflict", "api.writingTemplates.revisionConflict", nil
	case errors.Is(err, writingtemplates.ErrVersionRequired):
		code, key, args = "version_required", "api.writingTemplates.versionRequired", nil
	case errors.Is(err, writingtemplates.ErrVersionChanged):
		status, code, key, args = consts.StatusConflict, "version_changed", "api.writingTemplates.versionChanged", nil
	case errors.Is(err, writingtemplates.ErrNotUsableForCandidate):
		code, key, args = "template_not_usable", "api.writingTemplates.notUsableForCandidate", nil
	case errors.Is(err, writingtemplates.ErrDataRootRequired):
		status, code, key, args = consts.StatusInternalServerError, "catalog_unavailable", "api.writingTemplates.catalogUnavailable", nil
	}
	if args == nil {
		writeJSON(c, status, map[string]any{"error": messageKey(c, key), "code": code})
		return
	}
	writeJSON(c, status, map[string]any{"error": messageKey(c, key, args...), "code": code})
}
