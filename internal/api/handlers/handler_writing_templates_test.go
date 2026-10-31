package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"denova/internal/writingtemplates"
)

func TestWritingTemplateErrorBodiesAreLocalizedAndStable(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "missing", err: writingtemplates.ErrNotFound, status: consts.StatusNotFound, code: "template_missing"},
		{name: "category", err: writingtemplates.ErrCategoryMissing, status: consts.StatusBadRequest, code: "category_missing"},
		{name: "revision", err: writingtemplates.ErrRevisionConflict, status: consts.StatusConflict, code: "revision_conflict"},
		{name: "required", err: writingtemplates.ErrVersionRequired, status: consts.StatusBadRequest, code: "template_version_required"},
		{name: "changed", err: writingtemplates.ErrVersionChanged, status: consts.StatusBadRequest, code: "template_version_changed"},
		{name: "unusable", err: writingtemplates.ErrNotUsableForCandidate, status: consts.StatusBadRequest, code: "template_not_usable"},
		{name: "invalid", err: writingtemplates.ErrInvalidTemplate, status: consts.StatusBadRequest, code: "template_invalid"},
		{name: "unavailable", err: writingtemplates.ErrDataRootRequired, status: consts.StatusInternalServerError, code: "catalog_unavailable"},
	}
	for _, test := range tests {
		for _, locale := range []string{"zh-CN", "en-US"} {
			t.Run(test.name+"/"+locale, func(t *testing.T) {
				c := app.NewContext(0)
				c.Request.Header.Set("X-Denova-Locale", locale)
				writeWritingTemplateError(c, errors.Join(test.err, errors.New("private detail must not leak")))
				var body map[string]json.RawMessage
				if err := json.Unmarshal(c.Response.Body(), &body); err != nil {
					t.Fatal(err)
				}
				if len(body) != 2 {
					t.Fatalf("body has %d fields: %s", len(body), c.Response.Body())
				}
				var code string
				if err := json.Unmarshal(body["code"], &code); err != nil || code != test.code {
					t.Fatalf("code = %q, want %q", code, test.code)
				}
				var message string
				if err := json.Unmarshal(body["error"], &message); err != nil || message == "" {
					t.Fatalf("error message = %q", message)
				}
				if c.Response.StatusCode() != test.status {
					t.Fatalf("status = %d, want %d", c.Response.StatusCode(), test.status)
				}
			})
		}
	}
}

func TestWritingTemplateHandlersRejectMalformedJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(*Handlers, context.Context, *app.RequestContext)
	}{
		{name: "create", call: (*Handlers).HandleWritingTemplateCreate},
		{name: "update", call: (*Handlers).HandleWritingTemplateUpdate},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := &Handlers{}
			c := app.NewContext(0)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("X-Denova-Locale", "en-US")
			c.Request.SetBodyString("{")
			test.call(h, context.Background(), c)
			if c.Response.StatusCode() != consts.StatusBadRequest {
				t.Fatalf("status = %d, want 400", c.Response.StatusCode())
			}
			var body map[string]any
			if err := json.Unmarshal(c.Response.Body(), &body); err != nil {
				t.Fatal(err)
			}
			if body["error"] == nil {
				t.Fatalf("missing error in %s", c.Response.Body())
			}
		})
	}
}
