package management

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type codexUsageTestExecutor struct {
	mu        sync.Mutex
	responses []codexUsageTestResponse
	requests  []*http.Request
}

type codexUsageTestResponse struct {
	status int
	body   string
}

func (e *codexUsageTestExecutor) Identifier() string { return "codex" }

func (*codexUsageTestExecutor) Execute(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (*codexUsageTestExecutor) ExecuteStream(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (*codexUsageTestExecutor) Refresh(context.Context, *coreauth.Auth) (*coreauth.Auth, error) {
	return nil, nil
}

func (*codexUsageTestExecutor) CountTokens(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (*codexUsageTestExecutor) PrepareRequest(*http.Request, *coreauth.Auth) error { return nil }

func (e *codexUsageTestExecutor) HttpRequest(_ context.Context, _ *coreauth.Auth, request *http.Request) (*http.Response, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.requests = append(e.requests, request.Clone(request.Context()))
	if len(e.responses) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	response := e.responses[0]
	e.responses = e.responses[1:]
	return &http.Response{
		StatusCode: response.status,
		Body:       io.NopCloser(strings.NewReader(response.body)),
		Header:     make(http.Header),
	}, nil
}

func newCodexUsageHandler(t *testing.T, executor *codexUsageTestExecutor) (*Handler, string) {
	t.Helper()
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)
	auth := &coreauth.Auth{
		ID:       "codex-usage-account",
		Provider: "codex",
		Attributes: map[string]string{
			coreauth.AttributeAuthKind: coreauth.AuthKindOAuth,
		},
		Metadata: map[string]any{"access_token": "synthetic-token", "account_id": "workspace-123"},
	}
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	auth.EnsureIndex()
	return NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager), auth.Index
}

func usageRequest(t *testing.T, handler *Handler, path, authIndex string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"auth_index":"`+authIndex+`"}`))
	context.Request.Header.Set("Content-Type", "application/json")
	handler.RedeemCredentialReset(context)
	return recorder
}

func codexUsageBody(resetAt time.Time) string {
	return fmt.Sprintf(`{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":40,"reset_at":%d,"limit_window_seconds":18000}}}`, resetAt.Unix())
}

func TestRedeemCredentialResetUsesCreditReadAndConsumeEndpoint(t *testing.T) {
	now := time.Now().Add(2 * time.Hour).UTC()
	executor := &codexUsageTestExecutor{responses: []codexUsageTestResponse{
		{status: http.StatusOK, body: `{"credits":[{"status":"available","expires_at":"2030-01-03T00:00:00Z"}]}`},
		{status: http.StatusOK, body: `{"code":"reset"}`},
		{status: http.StatusOK, body: codexUsageBody(now)},
		{status: http.StatusOK, body: `{"credits":[]}`},
	}}
	handler, authIndex := newCodexUsageHandler(t, executor)
	recorder := usageRequest(t, handler, "/v8/management/credentials/usage/redeem", authIndex)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(executor.requests) != 4 {
		t.Fatalf("upstream request count=%d, want 4", len(executor.requests))
	}
	if executor.requests[0].Method != http.MethodGet || executor.requests[0].URL.Path != "/backend-api/wham/rate-limit-reset-credits" {
		t.Fatalf("credit lookup=%s %s", executor.requests[0].Method, executor.requests[0].URL.Path)
	}
	if got := executor.requests[0].Header.Get("ChatGPT-Account-ID"); got != "workspace-123" {
		t.Fatalf("credit lookup account header=%q, want workspace-123", got)
	}
	consume := executor.requests[1]
	if consume.Method != http.MethodPost || consume.URL.Path != "/backend-api/wham/rate-limit-reset-credits/consume" {
		t.Fatalf("consume request=%s %s", consume.Method, consume.URL.Path)
	}
	if got := consume.Header.Get("ChatGPT-Account-ID"); got != "workspace-123" {
		t.Fatalf("consume account header=%q, want workspace-123", got)
	}
	var payload map[string]string
	if errDecode := json.NewDecoder(consume.Body).Decode(&payload); errDecode != nil || strings.TrimSpace(payload["redeem_request_id"]) == "" {
		t.Fatalf("consume payload=%q err=%v", payload["redeem_request_id"], errDecode)
	}
	if executor.requests[2].Method != http.MethodGet || executor.requests[2].URL.Path != "/backend-api/wham/usage" {
		t.Fatalf("usage refresh=%s %s", executor.requests[2].Method, executor.requests[2].URL.Path)
	}
	if got := executor.requests[2].Header.Get("ChatGPT-Account-ID"); got != "workspace-123" {
		t.Fatalf("usage refresh account header=%q, want workspace-123", got)
	}
	var response map[string]any
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &response); errDecode != nil {
		t.Fatal(errDecode)
	}
	if response["status"] != "ok" || response["usage"] == nil {
		t.Fatalf("unexpected redemption response: %s", recorder.Body.String())
	}
}

func TestRedeemCredentialResetRejectsMissingCreditWithoutConsuming(t *testing.T) {
	executor := &codexUsageTestExecutor{responses: []codexUsageTestResponse{{
		status: http.StatusOK,
		body:   `{"credits":[{"status":"consumed","expires_at":"2030-01-03T00:00:00Z"}]}`,
	}}}
	handler, authIndex := newCodexUsageHandler(t, executor)
	recorder := usageRequest(t, handler, "/v8/management/credentials/usage/redeem", authIndex)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(executor.requests) != 1 || executor.requests[0].Method != http.MethodGet {
		t.Fatalf("unexpected upstream requests: %d", len(executor.requests))
	}
}

func TestRedeemCredentialResetRejectsUnconfirmedConsumeResponse(t *testing.T) {
	executor := &codexUsageTestExecutor{responses: []codexUsageTestResponse{
		{status: http.StatusOK, body: `{"credits":[{"status":"available","expires_at":"2030-01-03T00:00:00Z"}]}`},
		{status: http.StatusNoContent, body: ""},
	}}
	handler, authIndex := newCodexUsageHandler(t, executor)
	recorder := usageRequest(t, handler, "/v8/management/credentials/usage/redeem", authIndex)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(executor.requests) != 2 {
		t.Fatalf("upstream request count=%d, want 2", len(executor.requests))
	}
}
