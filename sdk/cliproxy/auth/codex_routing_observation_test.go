package auth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func codexObservationAuth(t *testing.T, manager *Manager) *Auth {
	t.Helper()
	auth, err := manager.Register(context.Background(), &Auth{ID: "observation-account", Provider: "codex", Status: StatusActive,
		Metadata: map[string]any{"access_token": "synthetic-token", "account_id": "synthetic-account", "email": "synthetic@example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

func TestCodexRoutingCloneAndRuntimeOnly(t *testing.T) {
	auth := &Auth{CodexRouting: &CodexRoutingObservation{Plan: "pro", Primary: &CodexRoutingWindow{UsedPercent: 97}}}
	cloned := auth.Clone()
	cloned.CodexRouting.Primary.UsedPercent = 1
	if auth.CodexRouting.Primary.UsedPercent != 97 {
		t.Fatal("clone shares usage window")
	}
	data, err := json.Marshal(auth)
	if err != nil || strings.Contains(string(data), "UsedPercent") || strings.Contains(string(data), "CodexRouting") {
		t.Fatal("runtime observation persisted")
	}
}

func TestCodexRoutingIndependentObservationOrdering(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	base := codexObservationAuth(t, manager)
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	latest := now.Add(time.Minute)
	manager.UpdateCodexRoutingObservation(base, &CodexRoutingObservation{ObservedAt: now, Plan: "plus",
		Primary: &CodexRoutingWindow{ObservedAt: latest, UsedPercent: 98}, Secondary: &CodexRoutingWindow{UsedPercent: 70},
		BankedResetObservedAt: latest, BankedResetExpiresAt: now.Add(time.Hour)})
	if !manager.UpdateCodexRoutingObservation(base, &CodexRoutingObservation{ObservedAt: now.Add(30 * time.Second), Plan: "pro",
		Primary: &CodexRoutingWindow{UsedPercent: 1}, Secondary: &CodexRoutingWindow{UsedPercent: 80},
		BankedResetObservedAt: now, BankedResetExpiresAt: now.Add(2 * time.Hour)}) {
		t.Fatal("newer independent quota rejected")
	}
	current, _ := manager.GetByID(base.ID)
	if current.CodexRouting.Primary.UsedPercent != 98 || current.CodexRouting.Secondary.UsedPercent != 80 || current.CodexRouting.Plan != "pro" || !current.CodexRouting.BankedResetExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("ordering lost: %+v", current.CodexRouting)
	}
	if manager.UpdateCodexRoutingObservation(base, &CodexRoutingObservation{ObservedAt: now}) {
		t.Fatal("older observation applied")
	}
	manager.UpdateCodexRoutingObservation(base, &CodexRoutingObservation{BankedResetObservedAt: latest.Add(time.Minute)})
	current, _ = manager.GetByID(base.ID)
	if !current.CodexRouting.BankedResetExpiresAt.IsZero() || current.CodexRouting.Primary.UsedPercent != 98 {
		t.Fatal("empty credits damaged quota or failed to clear expiry")
	}
}

func TestCodexRoutingUpdatePreservesSameAccountAndFencesOldToken(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	base := codexObservationAuth(t, manager)
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	manager.UpdateCodexRoutingObservation(base, &CodexRoutingObservation{ObservedAt: now, Primary: &CodexRoutingWindow{UsedPercent: 97}})
	refreshed := base.Clone()
	refreshed.Metadata["access_token"] = "synthetic-new-token"
	updated, err := manager.UpdateRefreshedAuth(context.Background(), base, refreshed)
	if err != nil || updated.CodexRouting == nil || updated.CodexRouting.Primary.UsedPercent != 97 {
		t.Fatalf("refresh lost runtime state: %v", err)
	}
	if manager.UpdateCodexRoutingObservation(base, &CodexRoutingObservation{ObservedAt: now.Add(time.Minute), Primary: &CodexRoutingWindow{UsedPercent: 1}}) {
		t.Fatal("old token probe applied")
	}
	reloaded := updated.Clone()
	reloaded.CodexRouting = nil
	updated, err = manager.Update(context.Background(), reloaded)
	if err != nil || updated.CodexRouting == nil {
		t.Fatal("reload lost observation")
	}
	changed := updated.Clone()
	changed.Metadata["account_id"] = "different-account"
	changed.Metadata["email"] = "different@example.test"
	updated, err = manager.Update(context.Background(), changed)
	if err != nil || updated.CodexRouting != nil {
		t.Fatal("different account inherited observation")
	}
	if manager.UpdateCodexRoutingObservation(reloaded, &CodexRoutingObservation{ObservedAt: now.Add(2 * time.Minute)}) {
		t.Fatal("different account accepted old probe")
	}
}

func TestCodexRoutingRegistrationEpochFence(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	old := codexObservationAuth(t, manager)
	manager.Remove(context.Background(), old.ID)
	replacement := old.Clone()
	replacement.CodexRouting = &CodexRoutingObservation{ObservedAt: time.Now(), Primary: &CodexRoutingWindow{UsedPercent: 99}}
	current, err := manager.Register(context.Background(), replacement)
	if err != nil {
		t.Fatal(err)
	}
	if current.CodexRouting != nil {
		t.Fatal("new registration inherited old runtime observation")
	}
	if old.RegistrationEpoch == current.RegistrationEpoch {
		t.Fatal("registration epoch not advanced")
	}
	if manager.UpdateCodexRoutingObservation(old, &CodexRoutingObservation{ObservedAt: time.Now()}) {
		t.Fatal("old registration probe applied")
	}
}
