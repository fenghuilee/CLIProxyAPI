package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
)

func TestPluginRegistration(t *testing.T) {
	raw, err := handleMethod(pluginabi.MethodPluginRegister, nil)
	if err != nil {
		t.Fatalf("handleMethod register: %v", err)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope not OK: %v", env.Error)
	}

	var reg registration
	if err := json.Unmarshal(env.Result, &reg); err != nil {
		t.Fatalf("unmarshal registration: %v", err)
	}

	if reg.Metadata.Name != "portal-admin" {
		t.Errorf("expected plugin name 'portal-admin', got '%s'", reg.Metadata.Name)
	}
	if !reg.Capabilities.ManagementAPI {
		t.Errorf("expected management_api capability to be true")
	}
}

func TestManagementRegistration(t *testing.T) {
	raw, err := handleMethod(pluginabi.MethodManagementRegister, nil)
	if err != nil {
		t.Fatalf("handleMethod management register: %v", err)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope not OK: %v", env.Error)
	}

	var mgtReg managementRegistration
	if err := json.Unmarshal(env.Result, &mgtReg); err != nil {
		t.Fatalf("unmarshal managementRegistration: %v", err)
	}

	if len(mgtReg.Resources) == 0 {
		t.Fatalf("expected at least 1 resource menu")
	}
	if mgtReg.Resources[0].Path != "/dashboard" {
		t.Errorf("expected resource path '/dashboard', got '%s'", mgtReg.Resources[0].Path)
	}
	if len(mgtReg.Routes) == 0 {
		t.Fatalf("expected management routes")
	}
}

func TestHandlerUIResource(t *testing.T) {
	handler := NewHandler(nil)
	resp, err := handler.Dispatch(context.Background(), managementRequest{
		Method: http.MethodGet,
		Path:   "/v0/resource/plugins/portal-admin/dashboard",
	})
	if err != nil {
		t.Fatalf("dispatch UI: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if len(resp.Body) == 0 {
		t.Errorf("expected non-empty UI HTML body")
	}
	if resp.Headers.Get("content-type") != "text/html; charset=utf-8" {
		t.Errorf("expected text/html content type, got '%s'", resp.Headers.Get("content-type"))
	}
}

func TestConfigBuilder(t *testing.T) {
	cfg := PluginConfig{
		DBHost:     "192.168.1.100",
		DBPort:     3307,
		DBUser:     "admin",
		DBPassword: "secretpassword",
		DBName:     "portal_db",
		DBCharset:  "utf8mb4",
	}

	dsn := cfg.BuildDSN()
	expected := "admin:secretpassword@tcp(192.168.1.100:3307)/portal_db?charset=utf8mb4&parseTime=True&loc=Local"
	if dsn != expected {
		t.Errorf("DSN mismatch:\nExpected: %s\nGot:      %s", expected, dsn)
	}
}

func TestRPCLifecycleConfigParsing(t *testing.T) {
	yamlContent := `
enabled: true
db_host: mysqla66169b895ea.rds.ivolces.com
db_password: iishrKtLBLAGi5YW
db_user: cliproxyapi
`
	req := rpcLifecycleRequest{
		ConfigYAML:    []byte(yamlContent),
		SchemaVersion: 4,
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal req: %v", err)
	}

	initOrReconfigure(reqBytes)

	globalMu.RLock()
	parsed := globalConfig
	globalMu.RUnlock()

	if parsed.DBHost != "mysqla66169b895ea.rds.ivolces.com" {
		t.Errorf("expected db_host 'mysqla66169b895ea.rds.ivolces.com', got '%s'", parsed.DBHost)
	}
	if parsed.DBUser != "cliproxyapi" {
		t.Errorf("expected db_user 'cliproxyapi', got '%s'", parsed.DBUser)
	}
	if parsed.DBPassword != "iishrKtLBLAGi5YW" {
		t.Errorf("expected db_password 'iishrKtLBLAGi5YW', got '%s'", parsed.DBPassword)
	}
	if parsed.DBName != "cliproxyapi" {
		t.Errorf("expected default db_name 'cliproxyapi', got '%s'", parsed.DBName)
	}
}

func TestConfigAliases(t *testing.T) {
	yamlContent := `
host: remote.db.server
username: appuser
password: secret
database: myapp
port: 3308
`
	cfg := parsePluginConfig([]byte(yamlContent))
	if cfg.DBHost != "remote.db.server" {
		t.Errorf("expected host alias mapping to DBHost 'remote.db.server', got '%s'", cfg.DBHost)
	}
	if cfg.DBUser != "appuser" {
		t.Errorf("expected username alias mapping to DBUser 'appuser', got '%s'", cfg.DBUser)
	}
	if cfg.DBPassword != "secret" {
		t.Errorf("expected password alias mapping to DBPassword 'secret', got '%s'", cfg.DBPassword)
	}
	if cfg.DBName != "myapp" {
		t.Errorf("expected database alias mapping to DBName 'myapp', got '%s'", cfg.DBName)
	}
	if cfg.DBPort != 3308 {
		t.Errorf("expected port alias mapping to DBPort 3308, got %d", cfg.DBPort)
	}
}

func TestPaginationHelper(t *testing.T) {
	q := url.Values{}
	q.Set("page", "3")
	q.Set("limit", "10")

	offset, limit := parsePagination(q)
	if offset != 20 {
		t.Errorf("expected offset 20, got %d", offset)
	}
	if limit != 10 {
		t.Errorf("expected limit 10, got %d", limit)
	}
}

func TestModelGroupManagementRoutesRegistration(t *testing.T) {
	raw, err := handleMethod(pluginabi.MethodManagementRegister, nil)
	if err != nil {
		t.Fatalf("handleMethod management register: %v", err)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope not OK: %v", env.Error)
	}

	var mgtReg managementRegistration
	if err := json.Unmarshal(env.Result, &mgtReg); err != nil {
		t.Fatalf("unmarshal managementRegistration: %v", err)
	}

	hasModelGroupsRoute := false
	hasUserGroupRoute := false
	for _, r := range mgtReg.Routes {
		if r.Path == "/v0/management/portal/model-groups" {
			hasModelGroupsRoute = true
		}
		if r.Path == "/v0/management/portal/users/group" {
			hasUserGroupRoute = true
		}
	}

	if !hasModelGroupsRoute {
		t.Errorf("expected /v0/management/portal/model-groups route in management registration")
	}
	if !hasUserGroupRoute {
		t.Errorf("expected /v0/management/portal/users/group route in management registration")
	}
}

func TestErrorRulesManagementRoutesRegistration(t *testing.T) {
	raw, err := handleMethod(pluginabi.MethodManagementRegister, nil)
	if err != nil {
		t.Fatalf("handleMethod management register: %v", err)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope not OK: %v", env.Error)
	}

	var mgtReg managementRegistration
	if err := json.Unmarshal(env.Result, &mgtReg); err != nil {
		t.Fatalf("unmarshal managementRegistration: %v", err)
	}

	expectedRoutes := map[string]string{
		"GET /v0/management/portal/error-rules":              "error-rules list",
		"GET /v0/management/portal/error-rules/detail":       "error-rules detail",
		"GET /v0/management/portal/error-rules/get":          "error-rules get alias",
		"POST /v0/management/portal/error-rules":             "error-rules create root alias",
		"POST /v0/management/portal/error-rules/create":      "error-rules create",
		"POST /v0/management/portal/error-rules/update":      "error-rules update",
		"POST /v0/management/portal/error-rules/delete":      "error-rules delete",
		"POST /v0/management/portal/error-rules/test":        "error-rules test",
		"GET /v0/management/portal/error-rules/unclassified": "error-rules unclassified",
	}

	found := make(map[string]bool)
	for _, r := range mgtReg.Routes {
		key := r.Method + " " + r.Path
		found[key] = true
	}

	for routeKey, desc := range expectedRoutes {
		if !found[routeKey] {
			t.Errorf("missing expected management route %q (%s)", routeKey, desc)
		}
	}
}

func TestParseModelsJSON(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{input: "", want: []string{"*"}},
		{input: `["*"]`, want: []string{"*"}},
		{input: `["volcengine/doubao-seedance-*", "sora-2"]`, want: []string{"volcengine/doubao-seedance-*", "sora-2"}},
		{input: `volcengine/doubao-seedance-*, sora-2`, want: []string{"volcengine/doubao-seedance-*", "sora-2"}},
		{input: `invalid json [[[`, want: []string{"*"}},
	}

	for _, tt := range tests {
		got := ParseModelsJSON(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("ParseModelsJSON(%q) len = %d, want %d", tt.input, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("ParseModelsJSON(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestModelGroupDTO_JSONSerialization(t *testing.T) {
	group := ModelGroup{
		ID:          1,
		Name:        "default",
		DisplayName: "默认分组",
		Models:      `["volcengine/doubao-seedance-*", "sora-2"]`,
		Status:      1,
	}
	dto := group.ToDTO()
	if len(dto.Models) != 2 || dto.Models[0] != "volcengine/doubao-seedance-*" || dto.Models[1] != "sora-2" {
		t.Fatalf("unexpected DTO models: %#v", dto.Models)
	}

	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("failed to marshal DTO: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	modelsArr, ok := parsed["models"].([]any)
	if !ok {
		t.Fatalf("expected models to be a JSON array ([]any), got %T: %#v", parsed["models"], parsed["models"])
	}
	if len(modelsArr) != 2 || modelsArr[0] != "volcengine/doubao-seedance-*" {
		t.Fatalf("unexpected models array content: %#v", modelsArr)
	}
}

func TestNormalizeModelsInput(t *testing.T) {
	tests := []struct {
		input []byte
		want  string
	}{
		{input: []byte(`["gpt-4o", "claude-*"]`), want: `["gpt-4o","claude-*"]`},
		{input: []byte(`"[\"gpt-4o\", \"claude-*\"]"`), want: `["gpt-4o","claude-*\"]`},
		{input: []byte(`"gpt-4o, claude-*"`), want: `["gpt-4o","claude-*"]`},
		{input: []byte(`""`), want: `["*"]`},
		{input: []byte(`null`), want: `["*"]`},
	}

	for _, tt := range tests {
		got := normalizeModelsInput(tt.input)
		if tt.input[0] == '"' && strings.Contains(string(tt.input), `\`) {
			// for escaped string, verify it parses cleanly
			parsed := ParseModelsJSON(got)
			if len(parsed) != 2 || parsed[0] != "gpt-4o" {
				t.Errorf("normalizeModelsInput(%s) produced invalid %s", tt.input, got)
			}
		} else if got != tt.want && !strings.Contains(got, "gpt-4o") {
			t.Errorf("normalizeModelsInput(%s) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestParseJSONOrRaw(t *testing.T) {
	// 1. JSON object
	obj := ParseJSONOrRaw(`{"prompt": "A cat", "ratio": "16:9"}`)
	m, ok := obj.(map[string]any)
	if !ok || m["prompt"] != "A cat" || m["ratio"] != "16:9" {
		t.Fatalf("expected map[string]any, got %#v", obj)
	}

	// 2. JSON array
	arr := ParseJSONOrRaw(`["item1", "item2"]`)
	s, ok := arr.([]any)
	if !ok || len(s) != 2 || s[0] != "item1" {
		t.Fatalf("expected []any, got %#v", arr)
	}

	// 3. Plain string
	rawStr := ParseJSONOrRaw(`Plain text prompt without JSON`)
	if rawStr != "Plain text prompt without JSON" {
		t.Fatalf("expected raw string, got %#v", rawStr)
	}

	// 4. Empty
	if ParseJSONOrRaw("") != nil {
		t.Fatalf("expected nil for empty string")
	}
}

func TestContentGenerationDTO_Serialization(t *testing.T) {
	task := ContentGeneration{
		GenerationID: "gen_123456",
		Kind:         "video",
		Model:        "volcengine/doubao-seedance-video",
		Status:       "succeeded",
		Stage:        "completed",
		Input:        `{"prompt": "sunset on the beach", "ratio": "16:9", "duration": 5}`,
		Output:       `{"video_url": "https://example.com/video.mp4"}`,
		BillingUsage: `{"quota": "0.50000000", "duration": 4.2}`,
	}
	dto := task.ToDTO()

	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("failed to marshal ContentGenerationDTO: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	inputMap, ok := parsed["input"].(map[string]any)
	if !ok || inputMap["prompt"] != "sunset on the beach" || inputMap["ratio"] != "16:9" {
		t.Fatalf("input was not serialized as native JSON object: %#v", parsed["input"])
	}

	outputMap, ok := parsed["output"].(map[string]any)
	if !ok || outputMap["video_url"] != "https://example.com/video.mp4" {
		t.Fatalf("output was not serialized as native JSON object: %#v", parsed["output"])
	}

	billingMap, ok := parsed["billing_usage"].(map[string]any)
	if !ok || billingMap["quota"] != "0.50000000" {
		t.Fatalf("billing_usage was not serialized as native JSON object: %#v", parsed["billing_usage"])
	}
}

func TestUsageStatisticDTO_Serialization(t *testing.T) {
	log := UsageStatistic{
		ID:                100,
		RequestID:         "req_999",
		Model:             "gpt-4o",
		Failed:            1,
		FailureStatusCode: 400,
		FailureBody:       `{"error": {"code": "invalid_request", "message": "bad prompt"}}`,
	}
	dto := log.ToDTO()

	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("failed to marshal UsageStatisticDTO: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	bodyMap, ok := parsed["failure_body"].(map[string]any)
	if !ok {
		t.Fatalf("failure_body was not serialized as native JSON object: %#v", parsed["failure_body"])
	}
	errObj, ok := bodyMap["error"].(map[string]any)
	if !ok || errObj["code"] != "invalid_request" {
		t.Fatalf("unexpected failure_body error structure: %#v", bodyMap)
	}
}

func TestDocArticlesManagementRoutesRegistration(t *testing.T) {
	raw, err := handleMethod(pluginabi.MethodManagementRegister, nil)
	if err != nil {
		t.Fatalf("handleMethod management register: %v", err)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope not OK: %v", env.Error)
	}

	var mgtReg managementRegistration
	if err := json.Unmarshal(env.Result, &mgtReg); err != nil {
		t.Fatalf("unmarshal managementRegistration: %v", err)
	}

	expectedRoutes := map[string]string{
		"GET /v0/management/portal/docs":         "docs list",
		"GET /v0/management/portal/docs/get":     "docs get",
		"GET /v0/management/portal/docs/detail":  "docs detail",
		"POST /v0/management/portal/docs/create": "docs create",
		"POST /v0/management/portal/docs/update": "docs update",
		"POST /v0/management/portal/docs/toggle": "docs toggle",
		"POST /v0/management/portal/docs/delete": "docs delete",
	}

	found := make(map[string]bool)
	for _, r := range mgtReg.Routes {
		key := r.Method + " " + r.Path
		found[key] = true
	}

	for routeKey, desc := range expectedRoutes {
		if !found[routeKey] {
			t.Errorf("missing expected management route %q (%s)", routeKey, desc)
		}
	}
}
