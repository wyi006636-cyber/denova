package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	contracts "denova/docs/project-design/implementation/contracts"
	"denova/internal/quality/domain"
	"denova/internal/quality/profile"
)

const (
	maxQualityProfileSourceBytes = 1 << 20
	maxQualityProfileDetailBytes = 256 << 10
)

type QualityLocalizedText struct {
	Zh string `json:"zh"`
	En string `json:"en"`
}

type QualitySpecMetadata struct {
	ContractVersion string `json:"contract_version"`
	SpecID          string `json:"spec_id"`
	Revision        int    `json:"revision"`
	SHA256          string `json:"sha256"`
}

type QualityProfileSummary struct {
	ProfileID       string               `json:"profile_id"`
	ContractVersion string               `json:"contract_version"`
	SourceSHA256    string               `json:"source_sha256"`
	QualitySpec     QualitySpecMetadata  `json:"quality_spec"`
	AccessMode      string               `json:"access_mode"`
	Summary         QualityLocalizedText `json:"summary"`
}

type QualityProfileDetail struct {
	QualityProfileSummary
	Profile *profile.Profile `json:"profile"`
}

type qualityCatalogAssets struct {
	profileSchema     []byte
	qualitySpecSchema []byte
	profiles          [][]byte
}

func defaultQualityCatalogAssets() qualityCatalogAssets {
	return qualityCatalogAssets{
		profileSchema:     contracts.ProfileV1Schema(),
		qualitySpecSchema: contracts.QualitySpecV1Schema(),
		profiles:          [][]byte{contracts.LongSerialProfile(), contracts.FanqieShortProfile(), contracts.ZhihuSaltShortProfile()},
	}
}

func (assets qualityCatalogAssets) clone() qualityCatalogAssets {
	clone := qualityCatalogAssets{profileSchema: append([]byte(nil), assets.profileSchema...), qualitySpecSchema: append([]byte(nil), assets.qualitySpecSchema...)}
	clone.profiles = make([][]byte, len(assets.profiles))
	for index := range assets.profiles {
		clone.profiles[index] = append([]byte(nil), assets.profiles[index]...)
	}
	return clone
}

type qualityCatalogItem struct {
	summary QualityProfileSummary
	profile profile.Profile
}

func newQualityAppService(injected *qualityCatalogAssets) (*QualityAppService, error) {
	assets := defaultQualityCatalogAssets()
	if injected != nil {
		assets = injected.clone()
	}
	decoder, err := profile.NewDecoder(assets.profileSchema, assets.qualitySpecSchema)
	if err != nil {
		return nil, qualityAssetError(err)
	}
	if len(assets.profiles) != len(domain.AllProfileIDs()) {
		return nil, qualityAssetError(errors.New("embedded Profile set is not exhaustive"))
	}
	records := make([]profile.ParsedProfile, 0, len(assets.profiles))
	type sourceRecord struct{ raw, qualitySpecRaw []byte }
	sources := make(map[string]sourceRecord, len(assets.profiles))
	for _, raw := range assets.profiles {
		if len(raw) > maxQualityProfileSourceBytes {
			return nil, qualityAssetError(errors.New("embedded Profile source exceeds limit"))
		}
		parsed, parseErr := decoder.Parse(raw)
		if parseErr != nil || !parsed.CanManagedMutate() {
			return nil, qualityAssetError(parseErr)
		}
		managed, managedErr := parsed.Managed()
		if managedErr != nil {
			return nil, qualityAssetError(managedErr)
		}
		var envelope struct {
			QualitySpec json.RawMessage `json:"quality_spec"`
		}
		if decodeErr := json.Unmarshal(raw, &envelope); decodeErr != nil || len(envelope.QualitySpec) == 0 {
			return nil, qualityAssetError(decodeErr)
		}
		if _, specErr := decoder.ParseQualitySpec(envelope.QualitySpec); specErr != nil {
			return nil, qualityAssetError(specErr)
		}
		id := string(managed.ProfileID)
		if _, duplicate := sources[id]; duplicate {
			return nil, qualityAssetError(errors.New("duplicate embedded Profile"))
		}
		sources[id] = sourceRecord{raw: append([]byte(nil), raw...), qualitySpecRaw: append([]byte(nil), envelope.QualitySpec...)}
		records = append(records, parsed)
	}
	registry, err := profile.NewRegistry(records)
	if err != nil {
		return nil, qualityAssetError(err)
	}
	service := &QualityAppService{items: make(map[string]qualityCatalogItem, len(sources))}
	for _, id := range registry.IDs() {
		item, lookupErr := registry.Lookup(id)
		if lookupErr != nil {
			return nil, qualityAssetError(lookupErr)
		}
		source, ok := sources[string(id)]
		if !ok {
			return nil, qualityAssetError(errors.New("embedded Profile source missing"))
		}
		summary := profileSummary(item, source.raw, source.qualitySpecRaw)
		service.items[string(id)] = qualityCatalogItem{summary: summary, profile: item}
		service.ordered = append(service.ordered, string(id))
	}
	return service, nil
}

func profileSummary(item profile.Profile, raw, specRaw []byte) QualityProfileSummary {
	profileDigest := sha256.Sum256(raw)
	specDigest := sha256.Sum256(specRaw)
	return QualityProfileSummary{
		ProfileID: string(item.ProfileID), ContractVersion: item.Contract.Version,
		SourceSHA256: hex.EncodeToString(profileDigest[:]),
		QualitySpec:  QualitySpecMetadata{ContractVersion: item.QualitySpec.Contract.Version, SpecID: string(item.QualitySpec.SpecID), Revision: item.QualitySpec.Revision, SHA256: hex.EncodeToString(specDigest[:])},
		AccessMode:   "read_only_catalog",
		Summary:      QualityLocalizedText{Zh: boundedQualityString(item.DisplayName.ZhCN), En: boundedQualityString(item.DisplayName.En)},
	}
}

func (service *QualityAppService) Profiles() ([]QualityProfileSummary, error) {
	if service == nil {
		return nil, qualityAssetError(errors.New("quality service unavailable"))
	}
	result := make([]QualityProfileSummary, 0, len(service.ordered))
	for _, id := range service.ordered {
		result = append(result, service.items[id].summary)
	}
	return result, nil
}

func (service *QualityAppService) Profile(id string) (QualityProfileDetail, error) {
	item, ok := service.items[id]
	if !ok {
		return QualityProfileDetail{}, &QualityAppError{Code: QualityCodeProfileNotFound}
	}
	clone, err := clonePublicProfile(item.profile)
	if err != nil {
		return QualityProfileDetail{}, qualityAssetError(err)
	}
	detail := QualityProfileDetail{QualityProfileSummary: item.summary, Profile: &clone}
	payload, err := json.Marshal(detail)
	if err != nil || len(payload) > maxQualityProfileDetailBytes {
		return QualityProfileDetail{}, qualityAssetError(errors.New("Profile detail exceeds public response limit"))
	}
	return detail, nil
}

func clonePublicProfile(item profile.Profile) (profile.Profile, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return profile.Profile{}, err
	}
	var clone profile.Profile
	if err := json.Unmarshal(raw, &clone); err != nil {
		return profile.Profile{}, err
	}
	return clone, nil
}

func (a *App) QualityProfiles() ([]QualityProfileSummary, error) {
	service, err := a.qualityService()
	if err != nil {
		return nil, err
	}
	return service.Profiles()
}

func (a *App) QualityProfile(id string) (QualityProfileDetail, error) {
	service, err := a.qualityService()
	if err != nil {
		return QualityProfileDetail{}, err
	}
	return service.Profile(id)
}
