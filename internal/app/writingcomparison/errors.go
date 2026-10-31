package writingcomparison

import "errors"

// Stable machine codes returned by the comparison API. They are deliberately
// independent from localized message text so clients can recover by code.
const (
	CodeInvalidRequest             = "invalid_request"
	CodeCandidateCount             = "candidate_count"
	CodeTargetPathInvalid          = "target_path_invalid"
	CodeProfileUnavailable         = "profile_unavailable"
	CodeTemplateCatalogUnavailable = "template_catalog_unavailable"
	CodeTemplateMissing            = "template_missing"
	CodeTemplateNotUsable          = "template_not_usable"
	CodeTemplateVersionChanged     = "template_version_changed"
	CodeTemplateVersionRequired    = "template_version_required"
	CodeSkillMissing               = "skill_missing"
	CodeSkillNotUsable             = "skill_not_usable"
	CodeSkillVersionChanged        = "skill_version_changed"
	CodeContextBudgetExceeded      = "context_budget_exceeded"
	CodeEmptyOutput                = "empty_output"
	CodeModelError                 = "model_error"
	CodeNotFound                   = "not_found"
	CodeRevisionConflict           = "revision_conflict"
	CodeStateConflict              = "state_conflict"
	CodeAssetCorrupt               = "asset_corrupt"
	CodeInternal                   = "internal"
)

// ErrorCode returns the stable public code carried by an application error.
// It is used by the detached SSE adapter, where the diagnostic message must
// remain separate from the machine-readable failure classification.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var typed *Error
	if errors.As(err, &typed) && typed.Code != "" {
		return typed.Code
	}
	if errors.Is(err, ErrAssetCorrupt) {
		return CodeAssetCorrupt
	}
	if errors.Is(err, ErrNotFound) {
		return CodeNotFound
	}
	if errors.Is(err, ErrRevisionConflict) {
		return CodeRevisionConflict
	}
	if errors.Is(err, ErrStateConflict) {
		return CodeStateConflict
	}
	return CodeInternal
}
