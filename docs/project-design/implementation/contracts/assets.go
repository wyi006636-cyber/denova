// Package contracts exposes immutable copies of Denova's normative bundled assets.
package contracts

import (
	"bytes"
	_ "embed"
)

//go:embed quality-event-v1.schema.json
var qualityEventV1Schema []byte

//go:embed profile-v1.schema.json
var profileV1Schema []byte

//go:embed quality-spec-v1.schema.json
var qualitySpecV1Schema []byte

//go:embed examples/long_serial.json
var longSerialProfile []byte

//go:embed examples/fanqie_short.json
var fanqieShortProfile []byte

//go:embed examples/zhihu_salt_short.json
var zhihuSaltShortProfile []byte

func QualityEventV1Schema() []byte { return bytes.Clone(qualityEventV1Schema) }
func ProfileV1Schema() []byte      { return bytes.Clone(profileV1Schema) }
func QualitySpecV1Schema() []byte  { return bytes.Clone(qualitySpecV1Schema) }
func LongSerialProfile() []byte    { return bytes.Clone(longSerialProfile) }
func FanqieShortProfile() []byte   { return bytes.Clone(fanqieShortProfile) }
func ZhihuSaltShortProfile() []byte {
	return bytes.Clone(zhihuSaltShortProfile)
}
