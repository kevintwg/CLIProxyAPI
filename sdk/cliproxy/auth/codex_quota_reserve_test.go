package auth

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestCodexReserveBoundaryAndRecovery(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name   string
		used   float64
		window string
		want   bool
	}{
		{"headroom available", 94.9, "primary", false}, {"exact cutoff", 95, "primary", true},
		{"weekly cutoff", 95, "secondary", true}, {"credits cannot reopen", 100, "primary", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &Auth{ID: "account", Provider: "codex", Metadata: map[string]any{"access_token": "synthetic"}}
			headers := http.Header{"X-Codex-Credits-Has-Credits": []string{"true"}}
			headers.Set("X-Codex-"+tt.window+"-Used-Percent", strconv.FormatFloat(tt.used, 'f', -1, 64))
			observeCodexRoutingHeaders(a, headers, now)
			if blocked, _, _ := isAuthBlockedForModel(a, "model", now); blocked != tt.want {
				t.Fatalf("blocked=%v want=%v", blocked, tt.want)
			}
			if tt.want {
				observeCodexRoutingHeaders(a, http.Header{"X-Codex-Credits-Balance": []string{"100"}}, now.Add(time.Minute))
				if blocked, _, _ := isAuthBlockedForModel(a, "model", now.Add(24*time.Hour)); !blocked {
					t.Fatal("missing percentages or elapsed time reopened exhausted account")
				}
				headers.Set("X-Codex-"+tt.window+"-Used-Percent", "10")
				observeCodexRoutingHeaders(a, headers, now.Add(24*time.Hour))
				if blocked, _, _ := isAuthBlockedForModel(a, "model", now.Add(24*time.Hour)); blocked {
					t.Fatal("fresh recovered quota stayed blocked")
				}
			}
		})
	}
	a := &Auth{Provider: "codex", Attributes: map[string]string{AttributeAPIKey: "synthetic"}, Quota: QuotaState{ObservedAt: now, Signals: map[string]string{"X-Codex-Primary-Used-Percent": "100"}}}
	if blocked, _, _ := isAuthBlockedForModel(a, "", now); blocked {
		t.Fatal("subscription gate affected API key")
	}
}

func TestCodexReserveManagerSessionFailover(t *testing.T) {
	now := time.Now()
	selector := NewSessionAffinitySelector(&SubscriptionFirstSelector{})
	defer selector.Stop()
	manager := NewManager(nil, selector, nil)
	const model = "reserve-failover-model"
	accounts := []*Auth{
		{ID: "generated-low-tier", Provider: "codex", Status: StatusActive, Metadata: map[string]any{"routing_tier": 0, "access_token": "synthetic-a"}},
		{ID: "generated-high-tier", Provider: "codex", Status: StatusActive, Metadata: map[string]any{"routing_tier": 3, "access_token": "synthetic-b"}},
	}
	for _, a := range accounts {
		registry.GetGlobalRegistry().RegisterClient(a.ID, "codex", []*registry.ModelInfo{{ID: model}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(a.ID) })
		if _, err := manager.Register(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	var selected []string
	manager.RegisterExecutor(&customStreamMockExecutor{identifier: "codex", streamFn: func(_ context.Context, a *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
		selected = append(selected, a.ID)
		chunks := make(chan cliproxyexecutor.StreamChunk, 1)
		chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("data: {}\n\n")}
		close(chunks)
		return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
	}})
	opts := cliproxyexecutor.Options{Stream: true, Headers: http.Header{"Session_id": []string{"generated-quota-session"}}}
	execute := func(want string) {
		t.Helper()
		stream, err := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, opts)
		if err != nil {
			t.Fatal(err)
		}
		for range stream.Chunks {
		}
		if selected[len(selected)-1] != want {
			t.Fatalf("selected=%v want=%s", selected, want)
		}
	}
	snapshot := func(id string) *Auth {
		for _, auth := range manager.List() {
			if auth.ID == id {
				return auth
			}
		}
		t.Fatalf("missing auth %s", id)
		return nil
	}
	execute(accounts[0].ID)
	if !manager.UpdateCodexRoutingObservation(snapshot(accounts[0].ID), &CodexRoutingObservation{ObservedAt: now, Primary: &CodexRoutingWindow{UsedPercent: 95, ObservedAt: now}}) {
		t.Fatal("quota observation was rejected")
	}
	execute(accounts[1].ID)
	if !manager.UpdateCodexRoutingObservation(snapshot(accounts[1].ID), &CodexRoutingObservation{ObservedAt: now, Secondary: &CodexRoutingWindow{UsedPercent: 100, ObservedAt: now}}) {
		t.Fatal("quota observation was rejected")
	}
	calls := len(selected)
	if _, err := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, opts); err == nil {
		t.Fatal("all exhausted accounts must stop routing")
	}
	if len(selected) != calls {
		t.Fatal("upstream called when all accounts exhausted")
	}
}
