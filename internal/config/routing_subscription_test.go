package config

import (
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSubscriptionFirstConfigRoundTrip(t *testing.T) {
	data := []byte("config-version: 8\nrouting:\n  strategy: subscription-first\n  subscription-first-max-observation-age: 45m\n  subscription-first-prefer-weekly-reset: false\n")
	if err := ValidateV8Config(data); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfigBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routing.Strategy != "subscription-first" || cfg.Routing.SubscriptionFirstObservationAge() != 45*time.Minute || cfg.Routing.SubscriptionFirstWeeklyResetEnabled() {
		t.Fatalf("routing=%+v", cfg.Routing)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfigPreserveComments(path, cfg, true); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	readback, err := ParseConfigBytes(saved)
	if err != nil {
		t.Fatal(err)
	}
	if readback.Routing.Strategy != "subscription-first" || readback.Routing.SubscriptionFirstObservationAge() != 45*time.Minute || readback.Routing.SubscriptionFirstWeeklyResetEnabled() {
		t.Fatal("persistence lost subscription routing settings")
	}
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = ParseConfigBytes(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routing.SubscriptionFirstWeeklyResetEnabled() {
		t.Fatal("false lost on save/read")
	}
	if (RoutingConfig{}).SubscriptionFirstObservationAge() != 30*time.Minute || !(RoutingConfig{}).SubscriptionFirstWeeklyResetEnabled() {
		t.Fatal("incorrect defaults")
	}
	for _, raw := range []string{"0m", "bad", "-1h"} {
		cfg := RoutingConfig{Strategy: "subscription-first", SubscriptionFirstMaxObservationAge: raw}
		if cfg.Validate() == nil {
			t.Fatalf("accepted age %q", raw)
		}
	}
	if (RoutingConfig{Strategy: "unknown"}).ValidateStrategy() == nil {
		t.Fatal("accepted invalid strategy")
	}
}
