package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestManager_MarkResult_CodexRateLimitWithoutHintQuarantinesCredential(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	manager := NewManager(nil, nil, nil)
	auth := &Auth{ID: "codex-rate-limit-cooldown", Provider: "codex"}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{
		{ID: "gpt-6.1-sol", Created: time.Now().Unix()},
		{ID: "gpt-6-astra", Created: time.Now().Unix()},
	})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-6-astra",
		Success:  true,
	})
	before := time.Now()
	manager.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-6.1-sol",
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "Rate limit exceeded"},
	})

	updated, _ := manager.GetByID(auth.ID)
	if !updated.Unavailable || updated.Quota.Reason != "credential_quota" {
		t.Fatalf("expected credential quarantine, got unavailable=%v quota=%+v", updated.Unavailable, updated.Quota)
	}
	remaining := time.Until(updated.NextRetryAfter)
	if remaining < 55*time.Second || remaining > time.Minute+time.Second {
		t.Fatalf("credential cooldown = %v, want about 1m", remaining)
	}
	other := existingModelState(updated, canonicalModelKey("gpt-6-astra"))
	if other == nil || other.NextRetryAfter.Before(before.Add(55*time.Second)) {
		t.Fatalf("sibling model was not quarantined: %+v", other)
	}
}

func TestManager_MarkResult_CodexUsageLimitMessageKeepsQuotaBackoff(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	manager := NewManager(nil, nil, nil)
	auth := &Auth{ID: "codex-usage-limit-message", Provider: "codex"}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{
		{ID: "gpt-6.1-sol", Created: time.Now().Unix()},
	})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-6.1-sol",
		Success:  false,
		Error: &Error{
			HTTPStatus: http.StatusTooManyRequests,
			Message:    `{"error":{"type":"usage_limit_reached","message":"Rate limit exceeded"}}`,
		},
	})

	updated, _ := manager.GetByID(auth.ID)
	state := existingModelState(updated, canonicalModelKey("gpt-6.1-sol"))
	if state == nil || state.Quota.Reason != "quota" || state.Quota.BackoffLevel != 1 {
		t.Fatalf("usage-limit payload changed quota backoff: auth=%+v state=%+v", updated.Quota, state)
	}
	if updated.Quota.Reason == "credential_quota" {
		t.Fatalf("usage-limit payload was misclassified as generic credential rate limit: %+v", updated.Quota)
	}
}

func TestManager_MarkResult_CodexRateLimitMarkerWithoutMessageQuarantinesCredential(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	manager := NewManager(nil, nil, nil)
	auth := &Auth{ID: "codex-rate-limit-marker", Provider: "codex"}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{
		{ID: "gpt-6.1-sol", Created: time.Now().Unix()},
	})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-6.1-sol",
		Success:  false,
		Error: &Error{
			HTTPStatus: http.StatusTooManyRequests,
			Message:    `{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded"}}`,
		},
	})

	updated, _ := manager.GetByID(auth.ID)
	if !updated.Unavailable || updated.Quota.Reason != "credential_quota" {
		t.Fatalf("marker-only rate limit was not quarantined: unavailable=%v quota=%+v", updated.Unavailable, updated.Quota)
	}
	remaining := time.Until(updated.NextRetryAfter)
	if remaining < 55*time.Second || remaining > time.Minute+time.Second {
		t.Fatalf("marker-only cooldown = %v, want about 1m", remaining)
	}
}

func TestCodexRepeatedRateLimitUpdatesWarmSiblingScheduler(t *testing.T) {
	withQuotaCooldownEnabled(t)
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	oldDeadline := time.Now().Add(10 * time.Second)
	candidate := &Auth{ID: "repeated-codex-rate-limit", Provider: "codex", Unavailable: true,
		NextRetryAfter: oldDeadline, Quota: QuotaState{Exceeded: true, Reason: "credential_quota", NextRecoverAt: oldDeadline}}
	models := []string{"repeated-rate-limit-a", "repeated-rate-limit-b"}
	registry.GetGlobalRegistry().RegisterClient(candidate.ID, candidate.Provider, []*registry.ModelInfo{{ID: models[0]}, {ID: models[1]}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(candidate.ID) })
	if _, err := manager.Register(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		if _, err := manager.scheduler.pickSingle(context.Background(), "codex", model, cliproxyexecutor.Options{}, nil); err == nil {
			t.Fatal("precondition: credential should already be cooling")
		}
	}
	manager.MarkResult(context.Background(), Result{AuthID: candidate.ID, Provider: "codex", Model: models[0], Error: &Error{HTTPStatus: 429, Message: `{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded"}}`}})
	current, _ := manager.GetByID(candidate.ID)
	manager.scheduler.mu.Lock()
	defer manager.scheduler.mu.Unlock()
	for _, model := range models {
		entry := manager.scheduler.providers["codex"].modelShards[model].entries[candidate.ID]
		if !entry.nextRetryAt.Equal(current.Quota.NextRecoverAt) {
			t.Fatalf("%s stale scheduler deadline: %s, want %s", model, entry.nextRetryAt, current.Quota.NextRecoverAt)
		}
		if blocked, _, _ := isAuthBlockedForModel(entry.auth, model, oldDeadline.Add(20*time.Second)); !blocked {
			t.Fatalf("%s became eligible before renewed quarantine expires", model)
		}
	}
}
