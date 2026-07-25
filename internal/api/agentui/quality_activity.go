package agentui

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"time"
)

var qualityRoutingTokenPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]*$`)

var qualitySummaryArgumentNames = map[string]struct{}{
	"profile_id": {}, "stage_kind": {}, "artifact_kind": {}, "decision_kind": {},
	"preference_kind": {}, "reason_code": {}, "result_code": {}, "item_count": {},
}

func isQualityEventData(data map[string]any) bool {
	contract, ok := anyMap(data["contract"])
	return ok && readBoundedQualityString(contract, "kind", 64) == "denova.quality-event"
}

func qualityActivityData(eventType string, data map[string]any) map[string]any {
	out := make(map[string]any, 10)
	contract, _ := anyMap(data["contract"])
	contractProjection := map[string]any{"kind": "denova.quality-event"}
	if version := readBoundedQualityToken(contract, "version", 32); version != "" {
		contractProjection["version"] = version
	}
	out["contract"] = contractProjection

	if value := boundedQualityToken(eventType, 128); value != "" {
		out["event"] = value
	}
	for _, key := range []string{"event_type", "event_id", "run_id", "stage_id", "artifact_id"} {
		if value := readBoundedQualityToken(data, key, 128); value != "" {
			out[key] = value
		}
	}
	if occurredAt := readBoundedQualityString(data, "occurred_at", 64); validQualityTime(occurredAt) {
		out["occurred_at"] = occurredAt
	}
	if sequence, ok := boundedQualitySequence(data["sequence"]); ok {
		out["sequence"] = sequence
	}
	if summary, ok := qualitySummaryProjection(data["summary"]); ok {
		out["summary"] = summary
	}
	return out
}

func qualityActivityID(data map[string]any, fallback string) string {
	if eventID := readBoundedQualityToken(data, "event_id", 128); eventID != "" {
		return eventID
	}
	if boundedFallback := boundedQualityToken(fallback, 128); boundedFallback != "" {
		fallback = boundedFallback
	} else {
		fallback = "quality-event"
	}
	if runID := readBoundedQualityToken(data, "run_id", 128); runID != "" {
		return fallback + "-" + runID
	}
	return fallback
}

func qualitySummaryProjection(value any) (map[string]any, bool) {
	summary, ok := anyMap(value)
	if !ok {
		return nil, false
	}
	code := readBoundedQualityToken(summary, "code", 256)
	if code == "" {
		return nil, false
	}
	projection := map[string]any{"code": code, "arguments": []any{}}
	arguments, ok := summary["arguments"].([]any)
	if !ok {
		return projection, true
	}
	safeArguments := make([]any, 0, min(len(arguments), 8))
	seen := make(map[string]struct{}, 8)
	for _, value := range arguments {
		if len(safeArguments) == 8 {
			break
		}
		argument, ok := anyMap(value)
		if !ok {
			continue
		}
		name := readBoundedQualityString(argument, "name", 32)
		if _, allowed := qualitySummaryArgumentNames[name]; !allowed {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		argumentValue := readBoundedQualityToken(argument, "value", 128)
		if argumentValue == "" {
			continue
		}
		seen[name] = struct{}{}
		safeArguments = append(safeArguments, map[string]any{"name": name, "value": argumentValue})
	}
	projection["arguments"] = safeArguments
	return projection, true
}

func anyMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out, true
	default:
		return nil, false
	}
}

func readBoundedQualityToken(data map[string]any, key string, limit int) string {
	return boundedQualityToken(readBoundedQualityString(data, key, limit), limit)
}

func boundedQualityToken(value string, limit int) string {
	if value == "" || len(value) > limit || !qualityRoutingTokenPattern.MatchString(value) {
		return ""
	}
	return value
}

func readBoundedQualityString(data map[string]any, key string, limit int) string {
	value, ok := data[key].(string)
	if !ok || len(value) > limit {
		return ""
	}
	return value
}

func validQualityTime(value string) bool {
	if value == "" || !strings.HasSuffix(value, "Z") {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && parsed.Location() == time.UTC
}

func boundedQualitySequence(value any) (any, bool) {
	switch sequence := value.(type) {
	case float64:
		return sequence, sequence >= 1 && sequence <= 1<<53-1 && math.Trunc(sequence) == sequence
	case int:
		return sequence, sequence >= 1
	case int64:
		return sequence, sequence >= 1 && sequence <= 1<<53-1
	case uint64:
		return sequence, sequence >= 1 && sequence <= 1<<53-1
	case json.Number:
		parsed, err := sequence.Int64()
		return parsed, err == nil && parsed >= 1 && parsed <= 1<<53-1
	default:
		return nil, false
	}
}
