package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestSubscriptionRoutingPatchValidationAndSummary(t *testing.T) {
	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	record := &coreauth.Auth{ID: "subscription.json", FileName: "subscription.json", Provider: "codex", Attributes: map[string]string{"path": "/tmp/subscription.json"}, Metadata: map[string]any{"plan_type": "pro", "routing_tier": 2, "access_token": "synthetic"}}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	patch := func(fields string) int {
		t.Helper()
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodPatch, "/v8/management/credentials/fields", strings.NewReader(`{"name":"subscription.json",`+fields+`}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Set(ConfigV8ContextKey, true)
		h.PatchAuthFileFields(ctx)
		return rec.Code
	}
	for _, fields := range []string{`"routing_tier":-1`, `"routing_tier":1001`, `"routing_tier":1.5`, `"routing_tier":"1"`, `"routing_tier.foo":0`, `"routing_weekly_reset_at":123`, `"routing_weekly_reset_at":"invalid"`, `"routing_weekly_reset_at":"2000-01-01T00:00:00Z"`, `"routing_weekly_reset_at.foo":null`} {
		before, _ := manager.GetByID(record.ID)
		if status := patch(`"note":"must not persist",` + fields); status != http.StatusBadRequest {
			t.Fatalf("invalid fields %s status=%d", fields, status)
		}
		after, _ := manager.GetByID(record.ID)
		if !reflect.DeepEqual(before.Metadata, after.Metadata) {
			t.Fatal("invalid patch mutated runtime metadata")
		}
		persisted, err := store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(persisted) != 1 || !reflect.DeepEqual(persisted[0].Metadata, before.Metadata) {
			t.Fatal("invalid patch reached persistence")
		}
	}
	reset := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if status := patch(`"routing_tier":0,"routing_weekly_reset_at":"` + reset + `"`); status != http.StatusOK {
		t.Fatalf("valid patch status=%d", status)
	}
	a, _ := manager.GetByID(record.ID)
	entry := h.buildAuthFileEntryLocked(a)
	profile, ok := entry["routing_profile"].(coreauth.RoutingProfile)
	if !ok || profile.TierSource != "manual" || profile.ResetSource != "manual" || entry["routing_tier"] != 0 || entry["routing_weekly_reset_at"] != reset {
		t.Fatalf("summary=%+v", profile)
	}
	if _, ok := entry["access_token"]; ok {
		t.Fatal("summary leaked token")
	}
	if status := patch(`"routing_tier":null,"routing_weekly_reset_at":null`); status != http.StatusOK {
		t.Fatalf("clear patch status=%d", status)
	}
	a, _ = manager.GetByID(record.ID)
	profile = coreauth.SubscriptionRoutingProfile(a, time.Now(), time.Minute)
	if profile.TierSource != "plan" || profile.ResetSource != "unknown" || a.Metadata["access_token"] != "synthetic" {
		t.Fatal("clear lost plan or auth metadata")
	}
}
