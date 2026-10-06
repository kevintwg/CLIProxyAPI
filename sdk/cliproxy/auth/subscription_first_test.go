package auth

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestSubscriptionFirstRanking(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	selector := &SubscriptionFirstSelector{nowFunc: func() time.Time { return now }}
	a := &Auth{ID: "a", Provider: "codex", Metadata: map[string]any{"routing_tier": 2, "routing_weekly_reset_at": now.Add(time.Hour).Format(time.RFC3339)}, Attributes: map[string]string{"priority": "99"}}
	b := &Auth{ID: "b", Provider: "codex", Metadata: map[string]any{"routing_tier": 1, "routing_weekly_reset_at": now.Add(2 * time.Hour).Format(time.RFC3339)}}
	pick := func(want string) {
		t.Helper()
		got, err := selector.Pick(context.Background(), "codex", "", cliproxyexecutor.Options{}, []*Auth{a, b})
		if err != nil || got.ID != want {
			t.Fatalf("pick=%v err=%v want=%s", got, err, want)
		}
	}
	pick("b") // Tier dominates both reset and conventional priority.
	a.Metadata["routing_tier"] = 1
	pick("a")
	a.Metadata["routing_weekly_reset_at"] = now.Add(-time.Hour).Format(time.RFC3339)
	pick("b")
	noReset := false
	selector.PreferWeeklyReset = &noReset
	pick("a")
	a.Disabled = true
	pick("b")
	a.Disabled = false
	a.Unavailable = true
	a.NextRetryAfter = now.Add(time.Hour)
	a.Quota = QuotaState{Exceeded: true, NextRecoverAt: now.Add(time.Hour)}
	pick("b")
}

func TestSubscriptionFirstResetOrdering(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	enabled, disabled := true, false
	for _, tt := range []struct {
		name              string
		preferWeeklyReset *bool
		weeklyA, weeklyB  time.Duration
		bankA, bankB      time.Duration
		bankAgeB          time.Duration
		tierA             int
		want              string
	}{
		{name: "default weekly before banked", weeklyA: time.Hour, weeklyB: 2 * time.Hour, bankA: 2 * time.Hour, bankB: time.Hour, tierA: 1, want: "a"},
		{name: "enabled weekly before banked", preferWeeklyReset: &enabled, weeklyA: time.Hour, weeklyB: 2 * time.Hour, bankA: 2 * time.Hour, bankB: time.Hour, tierA: 1, want: "a"},
		{name: "disabled prefers banked", preferWeeklyReset: &disabled, weeklyA: time.Hour, weeklyB: 2 * time.Hour, bankA: 2 * time.Hour, bankB: time.Hour, tierA: 1, want: "b"},
		{name: "equal weekly falls through to banked", weeklyA: time.Hour, weeklyB: time.Hour, bankA: 2 * time.Hour, bankB: time.Hour, tierA: 1, want: "b"},
		{name: "missing weekly falls through to banked", bankA: 2 * time.Hour, bankB: time.Hour, tierA: 1, want: "b"},
		{name: "usable weekly precedes missing weekly", weeklyA: time.Hour, bankA: 2 * time.Hour, bankB: time.Hour, tierA: 1, want: "a"},
		{name: "usable weekly precedes expired weekly", weeklyA: -time.Hour, weeklyB: time.Hour, bankA: time.Hour, bankB: 2 * time.Hour, tierA: 1, want: "b"},
		{name: "usable banked precedes missing banked", bankB: time.Hour, tierA: 1, want: "b"},
		{name: "stale banked ignored", bankA: 2 * time.Hour, bankB: time.Hour, bankAgeB: time.Hour, tierA: 1, want: "a"},
		{name: "expired banked ignored", bankA: 2 * time.Hour, bankB: -time.Hour, tierA: 1, want: "a"},
		{name: "tier still comes first", weeklyA: 2 * time.Hour, weeklyB: time.Hour, bankA: 2 * time.Hour, bankB: time.Hour, tierA: 0, want: "a"},
		{name: "priority breaks reset ties", weeklyA: time.Hour, weeklyB: time.Hour, bankA: time.Hour, bankB: time.Hour, tierA: 1, want: "a"},
		{name: "disabled skips weekly without banked", preferWeeklyReset: &disabled, weeklyA: 2 * time.Hour, weeklyB: time.Hour, tierA: 1, want: "a"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := func(id string, tier int, weekly, bank, bankAge time.Duration) *Auth {
				a := &Auth{ID: id, Provider: "codex", Metadata: map[string]any{"routing_tier": tier}}
				if weekly != 0 {
					a.Metadata["routing_weekly_reset_at"] = now.Add(weekly).Format(time.RFC3339)
				}
				if bank != 0 {
					a.CodexRouting = &CodexRoutingObservation{BankedResetObservedAt: now.Add(-bankAge), BankedResetExpiresAt: now.Add(bank)}
				}
				return a
			}
			a := account("a", tt.tierA, tt.weeklyA, tt.bankA, 0)
			a.Attributes = map[string]string{"priority": "99"}
			b := account("b", 1, tt.weeklyB, tt.bankB, tt.bankAgeB)
			selector := &SubscriptionFirstSelector{PreferWeeklyReset: tt.preferWeeklyReset, nowFunc: func() time.Time { return now }}
			for _, candidates := range [][]*Auth{{a, b}, {b, a}} {
				got, err := selector.Pick(context.Background(), "codex", "", cliproxyexecutor.Options{}, candidates)
				if err != nil || got == nil || got.ID != tt.want {
					t.Fatalf("pick=%v err=%v want=%s", got, err, tt.want)
				}
			}
		})
	}
}

func TestSubscriptionRoutingProfileObservations(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	for _, tt := range []struct {
		name, provider, window, reset string
		age                           time.Duration
		want                          bool
	}{
		{"codex-weekly", "codex", "10080", strconv.FormatInt(future.Unix(), 10), time.Minute, true},
		{"codex-hourly", "codex", "300", strconv.FormatInt(future.Unix(), 10), time.Minute, false},
		{"stale", "codex", "10080", strconv.FormatInt(future.Unix(), 10), time.Hour, false},
		{"missing", "codex", "10080", "", time.Minute, false},
		{"past", "codex", "10080", strconv.FormatInt(now.Add(-time.Hour).Unix(), 10), time.Minute, false},
		{"future-observation", "codex", "10080", strconv.FormatInt(future.Unix(), 10), -time.Minute, false},
		{"claude-weekly", "claude", "", future.Format(time.RFC3339), time.Minute, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &Auth{Provider: tt.provider, Quota: QuotaState{ObservedAt: now.Add(-tt.age), NextRecoverAt: future, Signals: map[string]string{"X-Codex-Secondary-Window-Minutes": tt.window, "X-Codex-Secondary-Reset-At": tt.reset, "Anthropic-Ratelimit-Unified-7d-Reset": tt.reset}}}
			p := SubscriptionRoutingProfile(a, now, 30*time.Minute)
			if (p.WeeklyResetAt != "") != tt.want {
				t.Fatalf("profile=%+v wantReset=%v", p, tt.want)
			}
			if p.Tier != nil {
				t.Fatalf("unexpected inferred tier: %+v", p)
			}
		})
	}
	a := &Auth{Provider: "codex", Metadata: map[string]any{"plan_type": "pro", "routing_tier": 0, "routing_weekly_reset_at": future.Format(time.RFC3339)}, Quota: QuotaState{ObservedAt: now.Add(-time.Hour)}}
	p := SubscriptionRoutingProfile(a, now, 30*time.Minute)
	if p.Tier == nil || *p.Tier != 0 || p.TierSource != "manual" || p.ResetSource != "manual" {
		t.Fatalf("manual override=%+v", p)
	}
	delete(a.Metadata, "routing_tier")
	p = SubscriptionRoutingProfile(a, now, time.Minute)
	if p.Tier == nil || *p.Tier != 3 || p.TierSource != "plan" {
		t.Fatalf("detected plan=%+v", p)
	}
	a.Provider = "claude"
	p = SubscriptionRoutingProfile(a, now, time.Minute)
	if p.Tier != nil {
		t.Fatalf("Claude tier inferred=%+v", p)
	}
}

func TestSubscriptionFirstAffinityDoesNotPreempt(t *testing.T) {
	selector := NewSessionAffinitySelector(&SubscriptionFirstSelector{})
	defer selector.Stop()
	a := &Auth{ID: "a", Provider: "codex", Metadata: map[string]any{"routing_tier": 0}}
	b := &Auth{ID: "b", Provider: "codex", Metadata: map[string]any{"routing_tier": 1}, Attributes: map[string]string{"priority": "99"}}
	opts := cliproxyexecutor.Options{Headers: http.Header{"Session_id": []string{"subscription-test"}}}
	pick := func(want string) {
		t.Helper()
		got, err := selector.Pick(context.Background(), "codex", "model", opts, []*Auth{a, b})
		if err != nil || got.ID != want {
			t.Fatalf("got=%v err=%v want=%s", got, err, want)
		}
	}
	pick("a")
	a.Metadata["routing_tier"] = 3
	pick("a")
	a.Disabled = true
	pick("b")
	a.Disabled = false
	a.Metadata["routing_tier"] = 0
	pick("b")
}

func TestSubscriptionFirstAffinityReturnsToPreferredTier(t *testing.T) {
	selector := NewSessionAffinitySelector(&SubscriptionFirstSelector{ReturnToPreferredTier: true})
	defer selector.Stop()
	pro := &Auth{ID: "pro", Provider: "claude", Metadata: map[string]any{"routing_tier": 1}}
	max := &Auth{ID: "max", Provider: "claude", Metadata: map[string]any{"routing_tier": 2}, Attributes: map[string]string{"priority": "99"}}
	pick := func(session, want string) {
		t.Helper()
		opts := cliproxyexecutor.Options{Headers: http.Header{"Session_id": []string{session}}}
		got, err := selector.Pick(context.Background(), "claude", "model", opts, []*Auth{pro, max})
		if err != nil || got.ID != want {
			t.Fatalf("session=%s got=%v err=%v want=%s", session, got, err, want)
		}
	}
	pick("busy", "pro")
	pro.Unavailable = true
	pro.NextRetryAfter = time.Now().Add(time.Hour)
	pro.Quota = QuotaState{Exceeded: true, NextRecoverAt: time.Now().Add(time.Hour)}
	pick("busy", "max") // Limit reached: the bound session fails over.
	pick("busy", "max") // Still limited: the failover binding holds.
	pro.Unavailable = false
	pro.NextRetryAfter = time.Time{}
	pro.Quota = QuotaState{}
	pick("busy", "pro") // Preferred tier recovered: the session returns on its next request.
	pick("busy", "pro")

	// Equal-tier sessions also move when another account has an earlier usable weekly reset.
	now := time.Now()
	early := &Auth{ID: "early", Provider: "codex", Metadata: map[string]any{"routing_tier": 3, "routing_weekly_reset_at": now.Add(time.Hour).Format(time.RFC3339)}}
	late := &Auth{ID: "late", Provider: "codex", Metadata: map[string]any{"routing_tier": 3, "routing_weekly_reset_at": now.Add(2 * time.Hour).Format(time.RFC3339)}}
	equal := func(want string) {
		t.Helper()
		opts := cliproxyexecutor.Options{Headers: http.Header{"Session_id": []string{"equal"}}}
		got, err := selector.Pick(context.Background(), "codex", "model", opts, []*Auth{early, late})
		if err != nil || got.ID != want {
			t.Fatalf("got=%v err=%v want=%s", got, err, want)
		}
	}
	equal("early")
	early.Metadata["routing_weekly_reset_at"] = now.Add(3 * time.Hour).Format(time.RFC3339)
	equal("late")
	equal("late")
}

func TestSubscriptionFirstAffinityWeeklyReset(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	enabled, disabled := true, false
	for _, tt := range []struct {
		name           string
		prefer         *bool
		candidateReset time.Duration
		boundReset     time.Duration
		candidateTier  int
		unavailable    bool
		reserve        bool
		observed       bool
		stale          bool
		want           string
	}{
		{name: "higher tier never overrides", candidateTier: 2, candidateReset: time.Hour, boundReset: 2 * time.Hour, want: "bound"},
		{name: "lower tier needs return setting", candidateTier: -1, candidateReset: time.Hour, boundReset: 2 * time.Hour, want: "bound"},
		{name: "default switches earlier", candidateReset: time.Hour, boundReset: 2 * time.Hour, want: "candidate"},
		{name: "enabled switches earlier", prefer: &enabled, candidateReset: time.Hour, boundReset: 2 * time.Hour, want: "candidate"},
		{name: "disabled keeps binding", prefer: &disabled, candidateReset: time.Hour, boundReset: 2 * time.Hour, want: "bound"},
		{name: "equal reset keeps binding", candidateReset: 2 * time.Hour, boundReset: 2 * time.Hour, want: "bound"},
		{name: "later reset keeps binding", candidateReset: 3 * time.Hour, boundReset: 2 * time.Hour, want: "bound"},
		{name: "expired reset ignored", candidateReset: -time.Hour, boundReset: 2 * time.Hour, want: "bound"},
		{name: "missing resets keep binding", want: "bound"},
		{name: "known reset precedes unknown", candidateReset: time.Hour, want: "candidate"},
		{name: "unavailable earlier reset ignored", candidateReset: time.Hour, boundReset: 2 * time.Hour, unavailable: true, want: "bound"},
		{name: "quota reserve excludes earlier account", candidateReset: time.Hour, boundReset: 2 * time.Hour, reserve: true, want: "bound"},
		{name: "fresh observed earlier reset switches", candidateReset: time.Hour, boundReset: 2 * time.Hour, observed: true, want: "candidate"},
		{name: "expired bound reset is replaced", candidateReset: time.Hour, boundReset: -time.Hour, want: "candidate"},
		{name: "stale observed reset ignored", candidateReset: time.Hour, boundReset: 2 * time.Hour, stale: true, want: "bound"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			selector := NewSessionAffinitySelector(&SubscriptionFirstSelector{PreferWeeklyReset: tt.prefer, nowFunc: func() time.Time { return now }})
			defer selector.Stop()
			bound := &Auth{ID: "bound", Provider: "codex", Metadata: map[string]any{"routing_tier": 1}}
			candidate := &Auth{ID: "candidate", Provider: "codex", Disabled: true, Metadata: map[string]any{"routing_tier": 1}, Attributes: map[string]string{"priority": "99"}, CodexRouting: &CodexRoutingObservation{BankedResetObservedAt: now, BankedResetExpiresAt: now.Add(time.Hour)}}
			candidate.Metadata["routing_tier"] = 1 + tt.candidateTier
			auths := []*Auth{bound, candidate}
			opts := cliproxyexecutor.Options{Headers: http.Header{"Session_id": []string{"weekly-session"}}}
			pick := func(want string) {
				t.Helper()
				got, err := selector.Pick(context.Background(), "codex", "model", opts, auths)
				if err != nil || got == nil || got.ID != want {
					t.Fatalf("pick=%v err=%v want=%s", got, err, want)
				}
			}
			pick("bound")
			candidate.Disabled = tt.unavailable
			if tt.boundReset != 0 {
				bound.Metadata["routing_weekly_reset_at"] = now.Add(tt.boundReset).Format(time.RFC3339)
			}
			if tt.candidateReset != 0 {
				candidate.Metadata["routing_weekly_reset_at"] = now.Add(tt.candidateReset).Format(time.RFC3339)
			}
			if tt.stale || tt.observed {
				delete(candidate.Metadata, "routing_weekly_reset_at")
				observedAt := now
				if tt.stale {
					observedAt = now.Add(-time.Hour)
				}
				candidate.Quota = QuotaState{ObservedAt: observedAt, Signals: map[string]string{"x-codex-secondary-window-minutes": "10080", "x-codex-secondary-reset-at": now.Add(tt.candidateReset).Format(time.RFC3339)}}
			}
			if tt.reserve {
				candidate.Metadata["access_token"] = "synthetic"
				candidate.CodexRouting.Primary = &CodexRoutingWindow{ObservedAt: time.Now(), UsedPercent: 95}
			}
			pick(tt.want)
			pick(tt.want) // The replacement binding survives the following request.
		})
	}
}

func TestSubscriptionFirstReturnUsesFallbackAndLCPBindings(t *testing.T) {
	selector := NewSessionAffinitySelector(&SubscriptionFirstSelector{ReturnToPreferredTier: true})
	defer selector.Stop()
	pro := &Auth{ID: "pro", Provider: "claude", Metadata: map[string]any{"routing_tier": 1}, Disabled: true}
	max := &Auth{ID: "max", Provider: "claude", Metadata: map[string]any{"routing_tier": 2}}
	auths := []*Auth{pro, max}

	// A subagent that inherits its parent's failover binding returns too.
	parent := cliproxyexecutor.Options{Headers: http.Header{"X-Claude-Code-Session-Id": []string{"root"}}, Metadata: map[string]any{}}
	if got, err := selector.Pick(context.Background(), "claude", "model", parent, auths); err != nil || got.ID != "max" {
		t.Fatalf("parent got=%v err=%v", got, err)
	}

	// A conversation matched only by its content returns too, and its new binding sticks.
	lcp := func(body string) *Auth {
		t.Helper()
		opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, OriginalRequest: []byte(body),
			Metadata: map[string]any{cliproxyexecutor.CallerScopeMetadataKey: "caller"}}
		got, err := selector.Pick(context.Background(), "claude", "model", opts, auths)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	first := `{"messages":[{"role":"system","content":"stable"},{"role":"user","content":"first"}]}`
	grown := `{"messages":[{"role":"system","content":"stable"},{"role":"user","content":"first"},{"role":"assistant","content":"answer"},{"role":"user","content":"next"}]}`
	if got := lcp(first); got.ID != "max" {
		t.Fatalf("lcp first=%s", got.ID)
	}
	if got := lcp(grown); got.ID != "max" {
		t.Fatalf("lcp kept=%s", got.ID)
	}
	pro.Disabled = false
	if got := lcp(grown + " "); got.ID != "pro" {
		t.Fatalf("lcp return=%s", got.ID)
	}
	if got := lcp(grown); got.ID != "pro" {
		t.Fatalf("lcp after return=%s", got.ID)
	}
	subagent := cliproxyexecutor.Options{Headers: http.Header{"X-Claude-Code-Session-Id": []string{"root"}, "X-Claude-Code-Agent-Id": []string{"child"}}, Metadata: map[string]any{}}
	if got, err := selector.Pick(context.Background(), "claude", "model", subagent, auths); err != nil || got.ID != "pro" {
		t.Fatalf("subagent return got=%v err=%v", got, err)
	}
}

func TestSubscriptionFirstWeeklyResetUsesFallbackAndLCPBindings(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	selector := NewSessionAffinitySelector(&SubscriptionFirstSelector{nowFunc: func() time.Time { return now }})
	defer selector.Stop()
	bound := &Auth{ID: "bound", Provider: "claude", Metadata: map[string]any{"routing_tier": 1, "routing_weekly_reset_at": now.Add(2 * time.Hour).Format(time.RFC3339)}}
	early := &Auth{ID: "early", Provider: "claude", Disabled: true, Metadata: map[string]any{"routing_tier": 1, "routing_weekly_reset_at": now.Add(time.Hour).Format(time.RFC3339)}}
	lower := &Auth{ID: "lower", Provider: "claude", Disabled: true, Metadata: map[string]any{"routing_tier": 0}}
	auths := []*Auth{bound, early, lower}
	parent := cliproxyexecutor.Options{Headers: http.Header{"X-Claude-Code-Session-Id": []string{"weekly-root"}}, Metadata: map[string]any{}}
	if got, err := selector.Pick(context.Background(), "claude", "model", parent, auths); err != nil || got.ID != "bound" {
		t.Fatalf("parent=%v err=%v", got, err)
	}
	first := `{"messages":[{"role":"system","content":"stable"},{"role":"user","content":"first"}]}`
	grown := `{"messages":[{"role":"system","content":"stable"},{"role":"user","content":"first"},{"role":"assistant","content":"answer"},{"role":"user","content":"next"}]}`
	lcp := func(body, want string) {
		t.Helper()
		opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, OriginalRequest: []byte(body), Metadata: map[string]any{cliproxyexecutor.CallerScopeMetadataKey: "weekly-caller"}}
		got, err := selector.Pick(context.Background(), "claude", "model", opts, auths)
		if err != nil || got == nil || got.ID != want {
			t.Fatalf("lcp=%v err=%v want=%s", got, err, want)
		}
		if opts.Metadata[cliproxyexecutor.CanonicalSessionIDMetadataKey] == "" {
			t.Fatal("LCP binding lost canonical session ID")
		}
	}
	lcp(first, "bound")
	lcp(grown, "bound")
	early.Disabled = false
	lower.Disabled = false
	lcp(grown, "early")
	lcp(grown, "early")
	child := cliproxyexecutor.Options{Headers: http.Header{"X-Claude-Code-Session-Id": []string{"weekly-root"}, "X-Claude-Code-Agent-Id": []string{"child"}}, Metadata: map[string]any{}}
	for range 2 {
		if got, err := selector.Pick(context.Background(), "claude", "model", child, auths); err != nil || got.ID != "early" {
			t.Fatalf("child=%v err=%v", got, err)
		}
	}
}

func TestSubscriptionFirstManagerExecution(t *testing.T) {
	manager := NewManager(nil, &SubscriptionFirstSelector{}, nil)
	const model = "subscription-execution-model"
	a := &Auth{ID: "subscription-low", Provider: "codex", Status: StatusActive, Metadata: map[string]any{"routing_tier": 0}}
	b := &Auth{ID: "subscription-high", Provider: "codex", Status: StatusActive, Metadata: map[string]any{"routing_tier": 3}, Attributes: map[string]string{"priority": "99"}}
	for _, a := range []*Auth{a, b} {
		registry.GetGlobalRegistry().RegisterClient(a.ID, "codex", []*registry.ModelInfo{{ID: model}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(a.ID) })
		if _, err := manager.Register(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	var selected string
	manager.RegisterExecutor(&customStreamMockExecutor{identifier: "codex", streamFn: func(_ context.Context, a *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
		selected = a.ID
		chunks := make(chan cliproxyexecutor.StreamChunk, 1)
		chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("data: {}\n\n")}
		close(chunks)
		return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
	}})
	execute := func(want string) {
		t.Helper()
		_, err := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{Stream: true})
		if err != nil || selected != want {
			t.Fatalf("selected=%s err=%v want=%s", selected, err, want)
		}
	}
	execute(a.ID)
	a.Disabled = true
	if _, err := manager.Update(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	execute(b.ID)
	a.Disabled = false
	if _, err := manager.Update(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	registry.GetGlobalRegistry().UnregisterClient(a.ID)
	manager.RefreshSchedulerEntry(a.ID)
	execute(b.ID)
}

func TestSubscriptionRoutingMetadataSurvivesRefresh(t *testing.T) {
	base := &Auth{ID: "a", Metadata: map[string]any{"routing_tier": 2, "routing_weekly_reset_at": "2026-10-05T00:00:00Z", "access_token": "old"}}
	updated := base.Clone()
	delete(updated.Metadata, "routing_tier")
	delete(updated.Metadata, "routing_weekly_reset_at")
	updated.Metadata["access_token"] = "refreshed"
	current := base.Clone()
	current.Metadata["routing_tier"] = 0
	merged := MergeRefreshedAuth(base, current, updated)
	if merged.Metadata["routing_tier"] != 0 || merged.Metadata["routing_weekly_reset_at"] != base.Metadata["routing_weekly_reset_at"] || merged.Metadata["access_token"] != "refreshed" {
		t.Fatal("refresh lost manual ranking or token refresh")
	}
}

func TestSubscriptionRoutingProfileWeeklyPrimaryAndOverrides(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	early, late := now.Add(time.Hour), now.Add(2*time.Hour)
	a := &Auth{Provider: "codex", Metadata: map[string]any{"routing_weekly_reset_at": now.Add(-time.Hour).Format(time.RFC3339)}, Quota: QuotaState{ObservedAt: now, Signals: map[string]string{
		"X-Codex-Primary-Window-Minutes": "10080", "X-Codex-Primary-Reset-At": strconv.FormatInt(late.Unix(), 10),
		"X-Codex-Secondary-Window-Minutes": "10080", "X-Codex-Secondary-Reset-At": strconv.FormatInt(early.Unix(), 10),
	}}}
	p := SubscriptionRoutingProfile(a, now, time.Minute)
	if p.WeeklyResetAt != early.Format(time.RFC3339) || p.ResetSource != "observed" {
		t.Fatal("expired manual override must fall back to earliest observed weekly window")
	}
	a.Quota.Signals["X-Codex-Secondary-Window-Minutes"] = "0"
	p = SubscriptionRoutingProfile(a, now, time.Minute)
	if p.WeeklyResetAt != late.Format(time.RFC3339) {
		t.Fatal("weekly primary window was ignored")
	}
	a.Quota.ObservedAt = time.Time{}
	if p := SubscriptionRoutingProfile(a, now, time.Minute); p.WeeklyResetAt != "" {
		t.Fatal("missing observation timestamp was trusted")
	}
	for plan, want := range map[string]int{"free": 0, "go": 1, "plus": 2, "pro": 3} {
		a.Metadata["plan_type"] = plan
		p := SubscriptionRoutingProfile(a, now, time.Minute)
		if p.Tier == nil || *p.Tier != want || p.TierSource != "plan" {
			t.Fatalf("plan %s profile=%+v", plan, p)
		}
	}
	a.Metadata["plan_type"] = "team"
	if p := SubscriptionRoutingProfile(a, now, time.Minute); p.Tier != nil || p.TierSource != "unknown" {
		t.Fatal("unknown plan ranked as known tier")
	}
}

func TestSubscriptionRoutingRelativeWeeklyReset(t *testing.T) {
	observed := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	expected := observed.Add(time.Hour).Format(time.RFC3339)
	for _, tt := range []struct {
		name, window, relative, absolute string
		elapsed, maxAge                  time.Duration
		want                             string
	}{
		{"primary relative", "10080", "3600", "", 0, 30 * time.Minute, expected},
		{"elapsed time keeps anchor", "10080", "3600", "", 20 * time.Minute, 30 * time.Minute, expected},
		{"stale", "10080", "3600", "", 31 * time.Minute, 30 * time.Minute, ""},
		{"expired", "10080", "3600", "", time.Hour, time.Hour, ""},
		{"hourly", "300", "3600", "", 0, 30 * time.Minute, ""},
		{"missing window", "", "3600", "", 0, 30 * time.Minute, ""},
		{"missing relative", "10080", "", "", 0, 30 * time.Minute, ""},
		{"malformed", "10080", "tomorrow", "", 0, 30 * time.Minute, ""},
		{"fractional", "10080", "1.5", "", 0, 30 * time.Minute, ""},
		{"zero", "10080", "0", "", 0, 30 * time.Minute, ""},
		{"negative", "10080", "-1", "", 0, 30 * time.Minute, ""},
		{"beyond weekly window", "10080", "604801", "", 0, 30 * time.Minute, ""},
		{"duration overflow", "10080", "9223372036854775807", "", 0, 30 * time.Minute, ""},
		{"integer overflow", "10080", "9223372036854775808", "", 0, 30 * time.Minute, ""},
		{"absolute wins", "10080", "3600", observed.Add(2 * time.Hour).Format(time.RFC3339), 0, 30 * time.Minute, observed.Add(2 * time.Hour).Format(time.RFC3339)},
		{"malformed absolute fallback", "10080", "3600", "invalid", 0, 30 * time.Minute, expected},
		{"expired absolute stays expired", "10080", "3600", observed.Add(-time.Minute).Format(time.RFC3339), 0, 30 * time.Minute, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &Auth{Provider: "codex", Quota: QuotaState{ObservedAt: observed}}
			// Exercise the production passive-header collector, including normalization.
			a.Quota.ObserveResponseHeadersForProvider("codex", http.Header{
				"X-Codex-Primary-Window-Minutes":      []string{tt.window},
				"X-Codex-Primary-Reset-After-Seconds": []string{tt.relative},
				"X-Codex-Primary-Reset-At":            []string{tt.absolute},
			}, observed)
			p := SubscriptionRoutingProfile(a, observed.Add(tt.elapsed), tt.maxAge)
			if p.WeeklyResetAt != tt.want {
				t.Fatalf("reset=%q want=%q", p.WeeklyResetAt, tt.want)
			}
			if tt.want != "" && p.ResetSource != "observed" {
				t.Fatalf("source=%q", p.ResetSource)
			}
		})
	}
	a := &Auth{Provider: "codex", Quota: QuotaState{ObservedAt: observed, Signals: map[string]string{
		"X-Codex-Secondary-Window-Minutes": "10080", "X-Codex-Secondary-Reset-After-Seconds": "604800",
	}}}
	if p := SubscriptionRoutingProfile(a, observed, 30*time.Minute); p.WeeklyResetAt != observed.Add(7*24*time.Hour).Format(time.RFC3339) {
		t.Fatal("secondary weekly relative reset missing")
	}
	a.Quota.ObservedAt = time.Time{}
	if p := SubscriptionRoutingProfile(a, observed, 30*time.Minute); p.WeeklyResetAt != "" {
		t.Fatal("missing observation trusted")
	}
}
