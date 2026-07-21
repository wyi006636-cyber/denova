package evaluation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
)

func StableSHA256(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return bytesSHA256(payload)
}

func FileSHA256(path string) (string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return bytesSHA256(payload), nil
}

func OutputSHA256(output string) string {
	return bytesSHA256([]byte(output))
}

func bytesSHA256(payload []byte) string {
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum)
}
