package contracts

import (
	"bytes"
	"os"
	"testing"
)

func TestEmbeddedContractAssetsMatchNormativeFilesAndAreImmutable(t *testing.T) {
	tests := []struct {
		path string
		load func() []byte
	}{
		{"quality-event-v1.schema.json", QualityEventV1Schema},
		{"profile-v1.schema.json", ProfileV1Schema},
		{"quality-spec-v1.schema.json", QualitySpecV1Schema},
		{"examples/long_serial.json", LongSerialProfile},
		{"examples/fanqie_short.json", FanqieShortProfile},
		{"examples/zhihu_salt_short.json", ZhihuSaltShortProfile},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			want, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatalf("read normative asset: %v", err)
			}
			first := test.load()
			if !bytes.Equal(first, want) {
				t.Fatal("embedded bytes differ from normative file")
			}
			first[0] ^= 0xff
			if got := test.load(); !bytes.Equal(got, want) {
				t.Fatal("caller mutation changed embedded asset")
			}
		})
	}
}
