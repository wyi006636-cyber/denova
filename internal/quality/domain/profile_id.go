package domain

// ProfileID is one exhaustive Quality Profile v1 identity.
type ProfileID string

const (
	ProfileLongSerial     ProfileID = "long_serial"
	ProfileFanqieShort    ProfileID = "fanqie_short"
	ProfileZhihuSaltShort ProfileID = "zhihu_salt_short"
)

var profileIDs = [...]ProfileID{
	ProfileLongSerial,
	ProfileFanqieShort,
	ProfileZhihuSaltShort,
}

// AllProfileIDs returns the stable, exhaustive v1 Profile order.
func AllProfileIDs() []ProfileID {
	result := make([]ProfileID, len(profileIDs))
	copy(result, profileIDs[:])
	return result
}

// ParseProfileID rejects unknown identities; v1 has no default or fallback Profile.
func ParseProfileID(value string) (ProfileID, error) {
	id := ProfileID(value)
	for _, known := range profileIDs {
		if id == known {
			return id, nil
		}
	}
	return "", &ContractError{
		Code:    CodeUnknownProfile,
		Path:    "profile_id",
		Value:   value,
		Message: "Profile v1 has no default or silent fallback",
	}
}
