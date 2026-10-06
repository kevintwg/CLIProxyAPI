package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
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
	if remaining < 29*time.Minute || remaining > 30*time.Minute+time.Second {
		t.Fatalf("credential cooldown = %v, want about 30m", remaining)
	}
	other := existingModelState(updated, canonicalModelKey("gpt-6-astra"))
	if other == nil || other.NextRetryAfter.Before(before.Add(29*time.Minute)) {
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
	if remaining < 29*time.Minute || remaining > 30*time.Minute+time.Second {
		t.Fatalf("marker-only cooldown = %v, want about 30m", remaining)
	}
}
