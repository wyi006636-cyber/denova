package app

import "errors"

const maxQualityPublicStringRunes = 512

type QualityErrorCode string

const (
	QualityCodeAssetsUnavailable QualityErrorCode = "quality_assets_unavailable"
	QualityCodeProfileNotFound   QualityErrorCode = "quality_profile_not_found"
	QualityCodeNoWorkspace       QualityErrorCode = "quality_no_workspace"
	QualityCodeInvalidRequest    QualityErrorCode = "quality_invalid_request"
	QualityCodeInspectionFailed  QualityErrorCode = "quality_workspace_inspection_failed"
)

// QualityAppError is the stable application-layer error boundary. Cause is
// retained for local diagnostics but API adapters must expose only Code.
type QualityAppError struct {
	Code  QualityErrorCode
	Cause error
}

func (err *QualityAppError) Error() string {
	if err == nil {
		return "<nil>"
	}
	return "quality application error: " + string(err.Code)
}

func (err *QualityAppError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// QualityAppService owns the immutable bundled catalog and delegates current
// workspace reads to the established Workspace Schema v1 package.
type QualityAppService struct {
	app     *App
	items   map[string]qualityCatalogItem
	ordered []string
}

func qualityAssetError(err error) error {
	if err == nil {
		err = errors.New("embedded quality asset is invalid")
	}
	return &QualityAppError{Code: QualityCodeAssetsUnavailable, Cause: err}
}

func (a *App) qualityService() (*QualityAppService, error) {
	a.ensureServices()
	if a.qualityAppErr != nil {
		return nil, a.qualityAppErr
	}
	return a.qualityApp, nil
}

func boundedQualityString(value string) string {
	runes := []rune(value)
	if len(runes) > maxQualityPublicStringRunes {
		runes = runes[:maxQualityPublicStringRunes]
	}
	return string(runes)
}
