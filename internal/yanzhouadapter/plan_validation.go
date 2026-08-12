package yanzhouadapter

import (
	"bytes"
	"encoding/json"
	"strings"
)

func validPlanOpaqueObject(raw json.RawMessage) bool {
	if len(raw) == 0 || len(raw) > 64*1024 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return false
	}
	return !planValueContainsForbiddenKey(value)
}

func planValueContainsForbiddenKey(value any) bool {
	forbidden := map[string]bool{
		"bookpath": true, "bookroot": true, "booksdir": true, "workspacepath": true,
		"workspaceroot": true, "cwd": true, "filesystem": true, "shell": true, "directwrite": true,
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
			if forbidden[normalized] || planValueContainsForbiddenKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if planValueContainsForbiddenKey(child) {
				return true
			}
		}
	}
	return false
}
