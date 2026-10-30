package writingcomparison

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
	CodeNotFound                   = "not_found"
	CodeRevisionConflict           = "revision_conflict"
	CodeStateConflict              = "state_conflict"
	CodeAssetCorrupt               = "asset_corrupt"
	CodeInternal                   = "internal"
)
