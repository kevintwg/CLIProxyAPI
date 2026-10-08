package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestClaudeUsageResponseMapsObservedRateLimitHeaders(t *testing.T) {
	observedAt := time.Unix(1_800_000_000, 0).UTC()
	quota := coreauth.QuotaState{
		ObservedAt: observedAt,
		Signals: map[string]string{
			"anthropic-ratelimit-unified-5h-utilization": "0.23",
			"Anthropic-Ratelimit-Unified-5h-Reset":       "1800010800",
			"Anthropic-Ratelimit-Unified-7d-Utilization": "1.02",
			"Anthropic-Ratelimit-Unified-7d-Reset":       "2030-01-08T00:00:00Z",
		},
	}

	response, ok := claudeUsageResponse(quota)
	if !ok {
		t.Fatal("claudeUsageResponse() reported no usable observation")
	}

	primary, ok := response["primary"].(gin.H)
	if !ok {
		t.Fatalf("primary = %#v, want usage window", response["primary"])
	}
	if got := primary["used_percent"]; got != 23.0 {
		t.Fatalf("primary used_percent = %#v, want 23", got)
	}
	if got := primary["remaining_percent"]; got != 77.0 {
		t.Fatalf("primary remaining_percent = %#v, want 77", got)
	}
	if got := primary["window_minutes"]; got != 300 {
		t.Fatalf("primary window_minutes = %#v, want 300", got)
	}

	secondary, ok := response["secondary"].(gin.H)
	if !ok {
		t.Fatalf("secondary = %#v, want usage window", response["secondary"])
	}
	if got := secondary["used_percent"]; got != 102.0 {
		t.Fatalf("secondary used_percent = %#v, want 102", got)
	}
	if got := secondary["remaining_percent"]; got != 0.0 {
		t.Fatalf("secondary remaining_percent = %#v, want 0", got)
	}
	if got := response["observed_at"]; got != observedAt {
		t.Fatalf("observed_at = %#v, want %v", got, observedAt)
	}
}

func TestFetchCredentialUsageClaudeUsesPassiveObservation(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:       "claude-observed",
		FileName: "claude.json",
		Provider: "claude",
		Quota: coreauth.QuotaState{
			ObservedAt: time.Unix(1_800_000_000, 0).UTC(),
			Signals: map[string]string{
				"Anthropic-Ratelimit-Unified-5h-Utilization": "0.40",
				"Anthropic-Ratelimit-Unified-5h-Reset":       "1800010800",
			},
		},
	}
	authIndex := auth.EnsureIndex()
	if _, errRegister := manager.Register(t.Context(), auth); errRegister != nil {
		t.Fatalf("register Claude auth: %v", errRegister)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v8/management/credentials/usage/fetch", strings.NewReader("{\"auth_index\":\""+authIndex+"\",\"provider\":\"claude\"}"))
	h.FetchCredentialUsage(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &response); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	primary, ok := response["primary"].(map[string]any)
	if !ok {
		t.Fatalf("primary = %#v, want usage window", response["primary"])
	}
	if got := primary["used_percent"]; got != 40.0 {
		t.Fatalf("used_percent = %#v, want 40", got)
	}
}

func TestFetchCredentialUsageClaudeRequiresObservation(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{ID: "claude-empty", FileName: "claude.json", Provider: "claude"}
	auth.EnsureIndex()
	if _, errRegister := manager.Register(t.Context(), auth); errRegister != nil {
		t.Fatalf("register Claude auth: %v", errRegister)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v8/management/credentials/usage/fetch", strings.NewReader("{\"auth_index\":\""+auth.Index+"\"}"))
	h.FetchCredentialUsage(ctx)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "send a Claude request first") {
		t.Fatalf("error = %s, want observation guidance", recorder.Body.String())
	}
}

func TestListAuthFilesClaudeIncludesPassiveUsage(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:         "claude-listed",
		FileName:   "claude.json",
		Provider:   "claude",
		Attributes: map[string]string{"runtime_only": "true"},
		Quota: coreauth.QuotaState{
			ObservedAt: time.Unix(1_800_000_000, 0).UTC(),
			Signals: map[string]string{
				"Anthropic-Ratelimit-Unified-7d-Utilization": "0.69",
				"Anthropic-Ratelimit-Unified-7d-Reset":       "1800604800",
			},
		},
	}
	auth.EnsureIndex()
	if _, errRegister := manager.Register(t.Context(), auth); errRegister != nil {
		t.Fatalf("register Claude auth: %v", errRegister)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v8/management/credentials", nil)
	h.ListAuthFiles(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Files []map[string]any `json:"files"`
	}
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &response); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if len(response.Files) != 1 {
		t.Fatalf("files = %#v, want one Claude credential", response.Files)
	}
	entry := response.Files[0]
	if supports, ok := entry["supports_quota"].(bool); !ok || !supports {
		t.Fatalf("supports_quota = %#v, want true", entry["supports_quota"])
	}
	usage, ok := entry["usage_limits"].(map[string]any)
	if !ok {
		t.Fatalf("usage_limits = %#v, want passive usage snapshot", entry["usage_limits"])
	}
	if _, ok := usage["secondary"]; !ok {
		t.Fatalf("usage_limits = %#v, want weekly window", usage)
	}
}
