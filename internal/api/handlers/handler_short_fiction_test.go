package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	hertzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"denova/internal/workspacechange"
)

func TestShortFictionDurabilityPendingHTTPPreservesSanitizedMutationTruth(t *testing.T) {
	for _, test := range []struct {
		locale  string
		message string
	}{
		{
			locale:  "zh-CN",
			message: "正文可能已经写入目标文件，但写入耐久性或日志恢复仍待完成。请先检查目标文件，不要重试本次确认。",
		},
		{
			locale:  "en-US",
			message: "The manuscript may already be visible, but write durability or journal recovery is still pending. Inspect the target first and do not retry this confirmation.",
		},
	} {
		t.Run(test.locale, func(t *testing.T) {
			server := hertzserver.Default()
			server.POST("/short-fiction-error", func(_ context.Context, c *app.RequestContext) {
				writeShortFictionError(c, "confirm", &workspacechange.Error{
					Code:    workspacechange.ErrorCodeDurabilityPending,
					Message: "internal durability cause must not escape",
					Details: map[string]any{
						"workspace_mutated": true,
						"recovery_pending":  true,
						"retryable":         false,
						"target_path":       "chapters/short.md",
						"write_revision":    "sha256:visible",
						"change_group_id":   "group-durability",
						"change_set_id":     "change-durability",
					},
				})
			})
			response := ut.PerformRequest(
				server.Engine,
				http.MethodPost,
				"/short-fiction-error",
				nil,
				ut.Header{Key: "X-Denova-Locale", Value: test.locale},
			)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			if len(raw) != 3 || raw["error"] == nil || raw["code"] == nil || raw["details"] == nil {
				t.Fatalf("response keys = %#v", raw)
			}
			var payload shortFictionErrorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Code != workspacechange.ErrorCodeDurabilityPending || payload.Error != test.message {
				t.Fatalf("response = %#v", payload)
			}
			for key, want := range map[string]any{
				"workspace_mutated": true,
				"recovery_pending":  true,
				"retryable":         false,
				"target_path":       "chapters/short.md",
				"write_revision":    "sha256:visible",
				"change_group_id":   "group-durability",
				"change_set_id":     "change-durability",
			} {
				if got := payload.Details[key]; got != want {
					t.Fatalf("details[%q] = %#v, want %#v; details=%#v", key, got, want, payload.Details)
				}
			}
			if _, leaked := payload.Details["cause"]; leaked {
				t.Fatalf("details leaked internal cause: %#v", payload.Details)
			}
		})
	}
}
