package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/ut"

	"denova/config"
	runtimeapp "denova/internal/app"
)

func TestQualityHTTPExactRoutesReturnReadOnlyDTOsWithoutWorkspaceWrites(t *testing.T) {
	application := newTestApplication(t)
	server := NewServer(application, "0")
	before := qualityAPITree(t, application.Workspace())

	profiles := ut.PerformRequest(server.engine.Engine, http.MethodGet, "/api/quality/profiles", nil)
	if profiles.Code != http.StatusOK {
		t.Fatalf("profiles status=%d body=%s", profiles.Code, profiles.Body.String())
	}
	var list []runtimeapp.QualityProfileSummary
	decodeResponse(t, profiles.Body.Bytes(), &list)
	if len(list) != 3 || list[0].ProfileID != "long_serial" || list[0].AccessMode != "read_only_catalog" {
		t.Fatalf("profiles = %#v", list)
	}

	detail := ut.PerformRequest(server.engine.Engine, http.MethodGet, "/api/quality/profiles/long_serial", nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	var profile runtimeapp.QualityProfileDetail
	decodeResponse(t, detail.Body.Bytes(), &profile)
	if profile.ProfileID != "long_serial" || profile.Profile == nil {
		t.Fatalf("detail = %#v", profile)
	}

	project := ut.PerformRequest(server.engine.Engine, http.MethodGet, "/api/quality/project", nil)
	if project.Code != http.StatusOK {
		t.Fatalf("project status=%d body=%s", project.Code, project.Body.String())
	}
	if strings.Contains(project.Body.String(), application.Workspace()) {
		t.Fatalf("project leaked absolute workspace: %s", project.Body.String())
	}

	preview := performQualityRequest(t, server, http.MethodPost, "/api/quality/project/migration-preview", []byte(`{}`), "zh-CN")
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	var migration runtimeapp.QualityMigrationPreviewDTO
	decodeResponse(t, preview.Body.Bytes(), &migration)
	if migration.Page.Offset != 0 || migration.Page.Limit != 100 || migration.Digest == "" {
		t.Fatalf("preview = %#v", migration)
	}
	bounded := performQualityRequest(t, server, http.MethodPost, "/api/quality/project/migration-preview", []byte(`{"offset":0,"limit":1}`), "zh-CN")
	if bounded.Code != http.StatusOK {
		t.Fatalf("bounded preview status=%d body=%s", bounded.Code, bounded.Body.String())
	}
	decodeResponse(t, bounded.Body.Bytes(), &migration)
	if migration.Page.Offset != 0 || migration.Page.Limit != 1 {
		t.Fatalf("bounded preview page = %#v", migration.Page)
	}

	after := qualityAPITree(t, application.Workspace())
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("quality HTTP wrote workspace\nbefore=%#v\nafter=%#v", before, after)
	}

	for _, request := range []struct{ method, path string }{
		{http.MethodPost, "/api/quality/profiles"},
		{http.MethodPut, "/api/quality/profiles/long_serial"},
		{http.MethodPost, "/api/quality/project"},
		{http.MethodGet, "/api/quality/project/migration-preview"},
		{http.MethodPost, "/api/quality/project/run"},
	} {
		response := ut.PerformRequest(server.engine.Engine, request.method, request.path, nil)
		if response.Code == http.StatusOK {
			t.Fatalf("unexpected Quality surface %s %s returned 200", request.method, request.path)
		}
	}
}

func TestQualityHTTPRejectsUnknownProfileAndStrictPreviewBodiesWithStableLocalizedErrors(t *testing.T) {
	application := newTestApplication(t)
	server := NewServer(application, "0")

	unknown := performQualityRequest(t, server, http.MethodGet, "/api/quality/profiles/not-known", nil, "en-US")
	assertQualityHTTPError(t, unknown, http.StatusNotFound, "quality_profile_not_found", "not found")

	tests := []struct {
		name string
		body string
	}{
		{"null", `null`},
		{"array", `[]`},
		{"string", `"preview"`},
		{"number", `1`},
		{"boolean", `true`},
		{"malformed", `{"offset":`},
		{"unknown field", `{"path":"/tmp/secret"}`},
		{"wrong type", `{"offset":"1"}`},
		{"offset null", `{"offset":null}`},
		{"limit null", `{"limit":null}`},
		{"duplicate offset", `{"offset":0,"offset":1}`},
		{"duplicate limit", `{"limit":1,"limit":2}`},
		{"duplicate and unknown", `{"offset":0,"offset":1,"path":"/tmp/secret"}`},
		{"trailing value", `{"offset":0}{}`},
		{"negative offset", `{"offset":-1}`},
		{"zero limit", `{"limit":0}`},
		{"negative limit", `{"limit":-1}`},
		{"large limit", `{"limit":501}`},
		{"execution flag", `{"execute":true}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performQualityRequest(t, server, http.MethodPost, "/api/quality/project/migration-preview", []byte(test.body), "zh-CN")
			assertQualityHTTPError(t, response, http.StatusBadRequest, "quality_invalid_request", "请求")
			if strings.Contains(response.Body.String(), "/tmp/secret") {
				t.Fatalf("error leaked request path: %s", response.Body.String())
			}
		})
	}
}

func TestQualityHTTPNoWorkspaceIsStableConflictInBothLocales(t *testing.T) {
	application, err := runtimeapp.New(context.Background(), &config.Config{NovaDir: t.TempDir(), ResumeLastWorkspace: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(application.Close)
	server := NewServer(application, "0")

	project := performQualityRequest(t, server, http.MethodGet, "/api/quality/project", nil, "en-US")
	assertQualityHTTPError(t, project, http.StatusConflict, "quality_no_workspace", "workspace")
	preview := performQualityRequest(t, server, http.MethodPost, "/api/quality/project/migration-preview", []byte(`{}`), "zh-CN")
	assertQualityHTTPError(t, preview, http.StatusConflict, "quality_no_workspace", "工作区")
}

func performQualityRequest(t *testing.T, server *Server, method, path string, body []byte, locale string) *ut.ResponseRecorder {
	t.Helper()
	var requestBody *ut.Body
	if body != nil {
		requestBody = &ut.Body{Body: bytes.NewReader(body), Len: len(body)}
	}
	headers := []ut.Header{{Key: "X-Denova-Locale", Value: locale}}
	if body != nil {
		headers = append(headers, ut.Header{Key: "Content-Type", Value: "application/json"})
	}
	return ut.PerformRequest(server.engine.Engine, method, path, requestBody, headers...)
}

func assertQualityHTTPError(t *testing.T, response *ut.ResponseRecorder, status int, code, messagePart string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d, want %d (body_bytes=%d)", response.Code, status, len(response.Body.Bytes()))
	}
	var body struct{ Code, Message string }
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v body=%s", err, response.Body.String())
	}
	if body.Code != code || !strings.Contains(strings.ToLower(body.Message), strings.ToLower(messagePart)) {
		t.Fatalf("error = %#v, want code=%q message containing %q", body, code, messagePart)
	}
}

func qualityAPITree(t *testing.T, root string) []string {
	t.Helper()
	result := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			result = append(result, "D "+filepath.ToSlash(rel))
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result = append(result, "F "+filepath.ToSlash(rel)+" "+string(raw))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(result)
	return result
}
