package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	novaapp "denova/internal/app"
)

const maxQualityPreviewRequestBytes = 4096

type qualityHTTPError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (h *Handlers) HandleQualityProfiles(_ context.Context, c *app.RequestContext) {
	profiles, err := h.app.QualityProfiles()
	if err != nil {
		h.writeQualityError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, profiles)
}

func (h *Handlers) HandleQualityProfile(_ context.Context, c *app.RequestContext) {
	detail, err := h.app.QualityProfile(c.Param("profile_id"))
	if err != nil {
		h.writeQualityError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, detail)
}

func (h *Handlers) HandleQualityProject(_ context.Context, c *app.RequestContext) {
	project, err := h.app.QualityProject()
	if err != nil {
		h.writeQualityError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, project)
}

func (h *Handlers) HandleQualityMigrationPreview(_ context.Context, c *app.RequestContext) {
	body := bytes.TrimSpace(c.Request.Body())
	if len(body) == 0 || len(body) > maxQualityPreviewRequestBytes {
		h.writeQualityError(c, &novaapp.QualityAppError{Code: novaapp.QualityCodeInvalidRequest})
		return
	}
	request, ok := decodeQualityPreviewRequest(body)
	if !ok {
		h.writeQualityError(c, &novaapp.QualityAppError{Code: novaapp.QualityCodeInvalidRequest})
		return
	}
	preview, err := h.app.QualityMigrationPreview(request)
	if err != nil {
		h.writeQualityError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, preview)
}

func decodeQualityPreviewRequest(body []byte) (novaapp.QualityMigrationPreviewRequest, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return novaapp.QualityMigrationPreviewRequest{}, false
	}
	request := novaapp.QualityMigrationPreviewRequest{}
	seen := make(map[string]struct{}, 2)
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, isString := keyToken.(string)
		if err != nil || !isString {
			return novaapp.QualityMigrationPreviewRequest{}, false
		}
		if _, duplicate := seen[key]; duplicate {
			return novaapp.QualityMigrationPreviewRequest{}, false
		}
		seen[key] = struct{}{}
		if key != "offset" && key != "limit" {
			return novaapp.QualityMigrationPreviewRequest{}, false
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return novaapp.QualityMigrationPreviewRequest{}, false
		}
		var value int
		if err := json.Unmarshal(raw, &value); err != nil {
			return novaapp.QualityMigrationPreviewRequest{}, false
		}
		if key == "offset" {
			request.Offset = value
			continue
		}
		if value < 1 || value > 500 {
			return novaapp.QualityMigrationPreviewRequest{}, false
		}
		request.Limit = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return novaapp.QualityMigrationPreviewRequest{}, false
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return novaapp.QualityMigrationPreviewRequest{}, false
	}
	return request, true
}

func (h *Handlers) writeQualityError(c *app.RequestContext, err error) {
	status := consts.StatusInternalServerError
	code := novaapp.QualityCodeAssetsUnavailable
	var appErr *novaapp.QualityAppError
	if errors.As(err, &appErr) {
		code = appErr.Code
	}
	switch code {
	case novaapp.QualityCodeProfileNotFound:
		status = consts.StatusNotFound
	case novaapp.QualityCodeNoWorkspace:
		status = consts.StatusConflict
	case novaapp.QualityCodeInvalidRequest:
		status = consts.StatusBadRequest
	case novaapp.QualityCodeAssetsUnavailable, novaapp.QualityCodeInspectionFailed:
		status = consts.StatusInternalServerError
	default:
		code = novaapp.QualityCodeAssetsUnavailable
	}
	writeJSON(c, status, qualityHTTPError{Code: string(code), Message: qualityErrorMessage(requestLocale(c), code)})
}

func qualityErrorMessage(locale string, code novaapp.QualityErrorCode) string {
	english := strings.HasPrefix(strings.ToLower(locale), "en")
	if english {
		switch code {
		case novaapp.QualityCodeProfileNotFound:
			return "Quality Profile not found."
		case novaapp.QualityCodeNoWorkspace:
			return "No workspace is currently open."
		case novaapp.QualityCodeInvalidRequest:
			return "The Quality request is invalid."
		case novaapp.QualityCodeInspectionFailed:
			return "The current workspace could not be inspected safely."
		default:
			return "The Quality catalog is temporarily unavailable."
		}
	}
	switch code {
	case novaapp.QualityCodeProfileNotFound:
		return "未找到质量档案。"
	case novaapp.QualityCodeNoWorkspace:
		return "当前没有打开工作区。"
	case novaapp.QualityCodeInvalidRequest:
		return "质量功能请求无效。"
	case novaapp.QualityCodeInspectionFailed:
		return "无法安全检查当前工作区。"
	default:
		return "质量档案目录暂时不可用。"
	}
}
