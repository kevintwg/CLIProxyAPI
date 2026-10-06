package test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestCodexTerminalQuotaCoolsAccountAcrossModels(t *testing.T) {
	for _, transport := range []string{"sse", "websocket-error", "websocket-failed"} {
		t.Run(transport, func(t *testing.T) {
			const model, siblingModel = "gpt-5.4", "gpt-5.4-mini"
			const created = `{"type":"response.created","response":{"id":"quota-test-response"}}`
			const quota = `{"type":"usage_limit_reached","message":"You've hit your usage limit.","resets_in_seconds":3600}`
			const completed = `{"type":"response.completed","response":{"id":"quota-test-success","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`
			attempts := make(chan string, 8)
			upgrader := websocket.Upgrader{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				account := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				attempts <- account
				terminal := completed
				if account == "quota-high" {
					terminal = `{"type":"error","status":429,"error":` + quota + `}`
					if transport == "websocket-failed" {
						terminal = `{"type":"response.failed","response":{"error":` + quota + `}}`
					}
				}
				if transport == "sse" {
					w.Header().Set("Content-Type", "text/event-stream")
					if _, errWrite := fmt.Fprintf(w, "data: %s\n\ndata: %s\n\n", created, terminal); errWrite != nil {
						t.Errorf("write SSE: %v", errWrite)
					}
					return
				}
				conn, errUpgrade := upgrader.Upgrade(w, r, nil)
				if errUpgrade != nil {
					t.Errorf("upgrade websocket: %v", errUpgrade)
					return
				}
				defer func() {
					if errClose := conn.Close(); errClose != nil {
						t.Errorf("close websocket: %v", errClose)
					}
				}()
				if _, _, errRead := conn.ReadMessage(); errRead != nil {
					t.Errorf("read websocket request: %v", errRead)
					return
				}
				for _, event := range []string{created, terminal} {
					if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(event)); errWrite != nil {
						t.Errorf("write websocket event: %v", errWrite)
						return
					}
				}
			}))
			defer server.Close()
			manager := cliproxyauth.NewManager(nil, &cliproxyauth.RoundRobinSelector{}, nil)
			manager.SetRetryConfig(0, 0, 0)
			cfg := &config.Config{}
			if transport == "sse" {
				manager.RegisterExecutor(runtimeexecutor.NewCodexExecutor(cfg))
			} else {
				manager.RegisterExecutor(runtimeexecutor.NewCodexWebsocketsExecutor(cfg))
			}
			highID, lowID := "quota-high-"+transport, "quota-low-"+transport
			for i, id := range []string{highID, lowID} {
				key := "quota-high"
				if i == 1 {
					key = "quota-low"
				}
				registry.GetGlobalRegistry().RegisterClient(id, "codex", []*registry.ModelInfo{{ID: model}, {ID: siblingModel}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
				if _, errRegister := manager.Register(context.Background(), &cliproxyauth.Auth{
					ID: id, Provider: "codex", Status: cliproxyauth.StatusActive,
					Attributes: map[string]string{"priority": fmt.Sprint(4 - i), "base_url": server.URL, "api_key": key},
					Metadata:   map[string]any{"disable_cooling": false},
				}); errRegister != nil {
					t.Fatal(errRegister)
				}
			}
			run := func(requestModel string) ([]byte, error) {
				t.Helper()
				result, errStream := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{
					Model: requestModel, Payload: []byte(fmt.Sprintf(`{"model":%q,"input":"hello"}`, requestModel)),
				}, cliproxyexecutor.Options{Stream: true, SourceFormat: sdktranslator.FromString("openai-response")})
				if errStream != nil {
					t.Fatalf("stream must start before the terminal quota error: %v", errStream)
				}
				var payload []byte
				var terminalErr error
				for chunk := range result.Chunks {
					payload = append(payload, chunk.Payload...)
					if chunk.Err != nil {
						terminalErr = chunk.Err
					}
				}
				return payload, terminalErr
			}
			before := time.Now()
			payload, quotaErr := run(model)
			if !strings.Contains(string(payload), "response.created") || quotaErr == nil {
				t.Fatalf("expected payload then terminal quota error: payload=%s error=%v", payload, quotaErr)
			}
			var scoped interface{ IsCredentialScoped() bool }
			if !errors.As(quotaErr, &scoped) || !scoped.IsCredentialScoped() {
				t.Errorf("real Codex error is missing credential scope: %T %v", quotaErr, quotaErr)
			}
			high, _ := manager.GetByID(highID)
			if high.Quota.Reason != "credential_quota" || high.Quota.NextRecoverAt.Before(before.Add(time.Hour)) {
				t.Errorf("account cooldown missing scope or upstream reset: %+v", high.Quota)
			}
			payload, errSibling := run(siblingModel)
			if errSibling != nil || !strings.Contains(string(payload), "response.completed") {
				t.Errorf("sibling model did not fail over to the healthy account: payload=%s error=%v", payload, errSibling)
			}
			for _, want := range []string{"quota-high", "quota-low"} {
				select {
				case got := <-attempts:
					if got != want {
						t.Errorf("upstream account = %s, want %s", got, want)
					}
				default:
					t.Fatalf("missing upstream attempt for %s", want)
				}
			}
			if len(attempts) != 0 {
				t.Errorf("unexpected extra upstream attempts: %d", len(attempts))
			}
		})
	}
}

func TestCodexModelLevelCoolingPreservesSiblingModel(t *testing.T) {
	for _, transport := range []string{"sse", "websocket-error", "websocket-failed"} {
		t.Run(transport, func(t *testing.T) {
			const model, siblingModel = "gpt-5.3-codex-spark", "gpt-5.6-sol"
			const created = `{"type":"response.created","response":{"id":"quota-test-response"}}`
			const quota = `{"type":"usage_limit_reached","message":"You've hit your usage limit.","resets_in_seconds":3600}`
			const completed = `{"type":"response.completed","response":{"id":"quota-test-success","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`
			attempts := make(chan string, 8)
			upgrader := websocket.Upgrader{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				account := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				attempts <- account
				terminal := completed
				var reqText string
				if transport == "sse" {
					bodyBytes, _ := io.ReadAll(r.Body)
					reqText = string(bodyBytes)
				}
				if transport != "sse" {
					conn, errUpgrade := upgrader.Upgrade(w, r, nil)
					if errUpgrade != nil {
						t.Errorf("upgrade websocket: %v", errUpgrade)
						return
					}
					defer func() {
						if errClose := conn.Close(); errClose != nil {
							t.Errorf("close websocket: %v", errClose)
						}
					}()
					_, msg, errRead := conn.ReadMessage()
					if errRead != nil {
						t.Errorf("read websocket request: %v", errRead)
						return
					}
					reqText = string(msg)
					if account == "quota-high" && strings.Contains(reqText, model) {
						terminal = `{"type":"error","status":429,"error":` + quota + `}`
						if transport == "websocket-failed" {
							terminal = `{"type":"response.failed","response":{"error":` + quota + `}}`
						}
					}
					for _, event := range []string{created, terminal} {
						if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(event)); errWrite != nil {
							t.Errorf("write websocket event: %v", errWrite)
							return
						}
					}
					return
				}
				if account == "quota-high" && strings.Contains(reqText, model) {
					terminal = `{"type":"error","status":429,"error":` + quota + `}`
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if _, errWrite := fmt.Fprintf(w, "data: %s\n\ndata: %s\n\n", created, terminal); errWrite != nil {
					t.Errorf("write SSE: %v", errWrite)
				}
			}))
			defer server.Close()
			manager := cliproxyauth.NewManager(nil, &cliproxyauth.RoundRobinSelector{}, nil)
			manager.SetRetryConfig(0, 0, 0)
			cfg, errParse := config.ParseConfigBytes([]byte("codex:\n  model-level-cooling: true\n"))
			if errParse != nil {
				t.Fatalf("parse config: %v", errParse)
			}
			if transport == "sse" {
				manager.RegisterExecutor(runtimeexecutor.NewCodexExecutor(cfg))
			} else {
				manager.RegisterExecutor(runtimeexecutor.NewCodexWebsocketsExecutor(cfg))
			}
			highID, lowID := "quota-high-"+transport, "quota-low-"+transport
			for i, id := range []string{highID, lowID} {
				key := "quota-high"
				if i == 1 {
					key = "quota-low"
				}
				registry.GetGlobalRegistry().RegisterClient(id, "codex", []*registry.ModelInfo{{ID: model}, {ID: siblingModel}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
				if _, errRegister := manager.Register(context.Background(), &cliproxyauth.Auth{
					ID: id, Provider: "codex", Status: cliproxyauth.StatusActive,
					Attributes: map[string]string{"priority": fmt.Sprint(4 - i), "base_url": server.URL, "api_key": key},
					Metadata:   map[string]any{"disable_cooling": false},
				}); errRegister != nil {
					t.Fatal(errRegister)
				}
			}
			run := func(requestModel string) ([]byte, error) {
				t.Helper()
				result, errStream := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{
					Model: requestModel, Payload: []byte(fmt.Sprintf(`{"model":%q,"input":"hello"}`, requestModel)),
				}, cliproxyexecutor.Options{Stream: true, SourceFormat: sdktranslator.FromString("openai-response")})
				if errStream != nil {
					t.Fatalf("stream must start before the terminal quota error: %v", errStream)
				}
				var payload []byte
				var terminalErr error
				for chunk := range result.Chunks {
					payload = append(payload, chunk.Payload...)
					if chunk.Err != nil {
						terminalErr = chunk.Err
					}
				}
				return payload, terminalErr
			}
			before := time.Now()
			payload, quotaErr := run(model)
			if !strings.Contains(string(payload), "response.created") || quotaErr == nil {
				t.Fatalf("expected payload then terminal quota error: payload=%s error=%v", payload, quotaErr)
			}
			var scoped interface{ IsCredentialScoped() bool }
			if errors.As(quotaErr, &scoped) && scoped.IsCredentialScoped() {
				t.Errorf("model-level cooling must NOT be credential scoped: %T %v", quotaErr, quotaErr)
			}
			high, _ := manager.GetByID(highID)
			if high.Quota.Reason == "credential_quota" {
				t.Errorf("model-level cooling must not set credential_quota reason on auth: %+v", high.Quota)
			}
			if high.Quota.Reason != "quota" || high.Quota.NextRecoverAt.Before(before.Add(time.Hour)) {
				t.Errorf("expected model-level quota cooldown on auth: %+v", high.Quota)
			}
			payload, errSibling := run(siblingModel)
			if errSibling != nil || !strings.Contains(string(payload), "response.completed") {
				t.Errorf("sibling model failed unexpectedly: payload=%s error=%v", payload, errSibling)
			}
			for _, want := range []string{"quota-high", "quota-high"} {
				select {
				case got := <-attempts:
					if got != want {
						t.Errorf("upstream account = %s, want %s", got, want)
					}
				default:
					t.Fatalf("missing upstream attempt for %s", want)
				}
			}
			if len(attempts) != 0 {
				t.Errorf("unexpected extra upstream attempts: %d", len(attempts))
			}
		})
	}
}

// This proof uses the real manager, affinity cache, and Codex SSE executor with a loopback upstream.
func TestCodexSessionSwitchesToEarlierWeeklyReset(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("weekly_preference_%t", enabled), func(t *testing.T) {
			const model = "gpt-5.4"
			attempts := make(chan string, 8)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts <- strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				w.Header().Set("Content-Type", "text/event-stream")
				if _, errWrite := fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"weekly-response\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"); errWrite != nil {
					t.Errorf("write SSE: %v", errWrite)
				}
			}))
			defer upstream.Close()
			affinity := cliproxyauth.NewSessionAffinitySelector(&cliproxyauth.SubscriptionFirstSelector{PreferWeeklyReset: &enabled})
			defer affinity.Stop()
			manager := cliproxyauth.NewManager(nil, affinity, nil)
			manager.RegisterExecutor(runtimeexecutor.NewCodexExecutor(&config.Config{}))
			now := time.Now()
			accounts := []*cliproxyauth.Auth{
				{ID: "weekly-bound", Provider: "codex", Status: cliproxyauth.StatusActive, Attributes: map[string]string{"base_url": upstream.URL}, Metadata: map[string]any{"access_token": "synthetic-bound", "routing_tier": 1, "routing_weekly_reset_at": now.Add(2 * time.Hour).Format(time.RFC3339)}},
				{ID: "weekly-early", Provider: "codex", Status: cliproxyauth.StatusActive, Disabled: true, Attributes: map[string]string{"base_url": upstream.URL}, Metadata: map[string]any{"access_token": "synthetic-early", "routing_tier": 1, "routing_weekly_reset_at": now.Add(time.Hour).Format(time.RFC3339)}},
			}
			for _, a := range accounts {
				registry.GetGlobalRegistry().RegisterClient(a.ID, "codex", []*registry.ModelInfo{{ID: model}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(a.ID) })
				if _, err := manager.Register(context.Background(), a); err != nil {
					t.Fatal(err)
				}
			}
			run := func(want string) {
				t.Helper()
				result, err := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model, Payload: []byte(`{"model":"gpt-5.4","input":"hello"}`)}, cliproxyexecutor.Options{Stream: true, SourceFormat: sdktranslator.FromString("openai-response"), Headers: http.Header{"Session_id": []string{"weekly-proof-session"}}})
				if err != nil {
					t.Fatal(err)
				}
				var payload []byte
				for chunk := range result.Chunks {
					if chunk.Err != nil {
						t.Fatal(chunk.Err)
					}
					payload = append(payload, chunk.Payload...)
				}
				if !strings.Contains(string(payload), "response.completed") {
					t.Fatalf("missing completion: %s", payload)
				}
				select {
				case got := <-attempts:
					if got != want {
						t.Fatalf("upstream account=%s want=%s", got, want)
					}
					t.Logf("upstream account=%s", got)
				default:
					t.Fatal("missing upstream request")
				}
				if len(attempts) != 0 {
					t.Fatalf("unexpected retry count=%d", len(attempts))
				}
			}
			run("synthetic-bound")
			accounts[1].Disabled = false
			if _, err := manager.Update(context.Background(), accounts[1]); err != nil {
				t.Fatal(err)
			}
			want := "synthetic-bound"
			if enabled {
				want = "synthetic-early"
			}
			run(want)
			run(want)
		})
	}
}

func TestCodexGenericRateLimitFailsOverWithSubscriptionOrdering(t *testing.T) {
	const primary, sibling = "gpt-5.4", "gpt-5.4-mini"
	attempts := make(chan string, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		attempts <- key
		if key == "limited-plus" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"detail":"Rate limit exceeded"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"test-success\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
	}))
	defer server.Close()
	manager := cliproxyauth.NewManager(nil, &cliproxyauth.SubscriptionFirstSelector{}, nil)
	manager.SetRetryConfig(0, 0, 0)
	manager.RegisterExecutor(runtimeexecutor.NewCodexExecutor(&config.Config{}))
	for i, key := range []string{"limited-plus", "healthy-pro"} {
		registry.GetGlobalRegistry().RegisterClient(key, "codex", []*registry.ModelInfo{{ID: primary}, {ID: sibling}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(key) })
		if _, err := manager.Register(context.Background(), &cliproxyauth.Auth{ID: key, Provider: "codex", Status: cliproxyauth.StatusActive,
			Attributes: map[string]string{"base_url": server.URL, "api_key": key},
			Metadata:   map[string]any{"routing_tier": i + 1, "disable_cooling": false}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, model := range []string{primary, sibling} {
		result, err := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{
			Model: model, Payload: []byte(fmt.Sprintf(`{"model":%q,"input":"hello"}`, model)),
		}, cliproxyexecutor.Options{Stream: true, SourceFormat: sdktranslator.FromString("openai-response")})
		if err != nil {
			t.Fatal(err)
		}
		var output []byte
		for chunk := range result.Chunks {
			if chunk.Err != nil {
				t.Fatal(chunk.Err)
			}
			output = append(output, chunk.Payload...)
		}
		if !strings.Contains(string(output), "response.completed") {
			t.Fatalf("incomplete response: %s", output)
		}
	}
	for _, want := range []string{"limited-plus", "healthy-pro", "healthy-pro"} {
		select {
		case got := <-attempts:
			if got != want {
				t.Fatalf("account=%s want=%s", got, want)
			}
		default:
			t.Fatalf("missing attempt for %s", want)
		}
	}
	if len(attempts) != 0 {
		t.Fatal("unexpected retries")
	}
	limited, _ := manager.GetByID("limited-plus")
	if remaining := time.Until(limited.Quota.NextRecoverAt); limited.Quota.Reason != "credential_quota" || remaining < 55*time.Second || remaining > time.Minute {
		t.Fatalf("quarantine not enforced: %+v", limited.Quota)
	}
	t.Log("Simulated upstream HTTP 429: Plus -> Pro; next model -> Pro; affected credential cooling for 1 minute")
}
