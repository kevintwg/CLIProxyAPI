package auth

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CodexQuotaReservePercent leaves room for reporting lag and in-flight requests.
const CodexQuotaReservePercent = 5.0

func codexSubscriptionAuth(a *Auth) bool {
	return a != nil && strings.EqualFold(strings.TrimSpace(a.Provider), "codex") && a.AuthKind() != AuthKindAPIKey
}

func lowercaseQuotaSignals(signals map[string]string) map[string]string {
	result := make(map[string]string, len(signals))
	for key, value := range signals {
		result[strings.ToLower(key)] = value
	}
	return result
}

func codexWindowFromSignals(signals map[string]string, name string, observedAt time.Time) *CodexRoutingWindow {
	prefix := "x-codex-" + name + "-"
	used, err := strconv.ParseFloat(strings.TrimSpace(signals[prefix+"used-percent"]), 64)
	if err != nil || math.IsNaN(used) || math.IsInf(used, 0) || used < 0 || used > 100 || observedAt.IsZero() {
		return nil
	}
	minutes, _ := strconv.Atoi(signals[prefix+"window-minutes"])
	reset := observedResetTime(signals[prefix+"reset-at"])
	if reset.IsZero() {
		seconds, err := strconv.ParseInt(signals[prefix+"reset-after-seconds"], 10, 64)
		if err == nil && seconds >= 0 && seconds <= 7*24*60*60 {
			reset = observedAt.Add(time.Duration(seconds) * time.Second)
		}
	}
	return &CodexRoutingWindow{UsedPercent: used, WindowMinutes: minutes, ResetsAt: reset, ObservedAt: observedAt}
}

// Called under the manager lock. Partial headers cannot erase an exhausted window.
func observeCodexRoutingHeaders(a *Auth, headers http.Header, observedAt time.Time) {
	if !codexSubscriptionAuth(a) {
		return
	}
	signals := lowercaseQuotaSignals(collectQuotaSignals("codex", headers))
	primary := codexWindowFromSignals(signals, "primary", observedAt)
	secondary := codexWindowFromSignals(signals, "secondary", observedAt)
	if primary == nil && secondary == nil {
		return
	}
	if a.CodexRouting == nil {
		a.CodexRouting = &CodexRoutingObservation{}
	}
	if primary != nil {
		a.CodexRouting.Primary = primary
	}
	if secondary != nil {
		a.CodexRouting.Secondary = secondary
	}
	if plan := signals["x-codex-plan-type"]; plan != "" {
		a.CodexRouting.Plan = plan
	}
	a.CodexRouting.ObservedAt = observedAt
}

func codexRoutingObservedAt(a *Auth) time.Time {
	if a.CodexRouting == nil {
		return time.Time{}
	}
	return a.CodexRouting.ObservedAt
}

// Exhausted observations stay blocked until refreshed quota confirms recovery.
// A reset deadline or an old observation alone is not evidence of recovery.
func codexQuotaReserveState(a *Auth, now time.Time) (known, blocked bool) {
	if !codexSubscriptionAuth(a) {
		return false, false
	}
	signals := lowercaseQuotaSignals(a.Quota.Signals)
	for _, name := range []string{"primary", "secondary"} {
		var window *CodexRoutingWindow
		if a.CodexRouting != nil {
			window = a.CodexRouting.Primary
			if name == "secondary" {
				window = a.CodexRouting.Secondary
			}
		}
		passive := codexWindowFromSignals(signals, name, a.Quota.ObservedAt)
		observed := codexRoutingObservedAt(a)
		if window != nil && !window.ObservedAt.IsZero() {
			observed = window.ObservedAt
		}
		if passive != nil && !passive.ObservedAt.After(now) && (window == nil || passive.ObservedAt.After(observed)) {
			window, observed = passive, passive.ObservedAt
		}
		if window == nil || observed.IsZero() || observed.After(now) || math.IsNaN(window.UsedPercent) || math.IsInf(window.UsedPercent, 0) || window.UsedPercent < 0 || window.UsedPercent > 100 {
			continue
		}
		known = true
		if window.UsedPercent >= 100-CodexQuotaReservePercent {
			blocked = true
		}
	}
	return known, blocked
}

func applyCodexRoutingProfile(profile *RoutingProfile, a *Auth, now time.Time, maxAge time.Duration) {
	if !codexSubscriptionAuth(a) {
		return
	}
	reserve := CodexQuotaReservePercent
	profile.QuotaReservePercent = &reserve
	if known, blocked := codexQuotaReserveState(a, now); known {
		profile.QuotaReserveBlocked = &blocked
	}
	observation := a.CodexRouting
	if observation == nil {
		return
	}
	if observationFresh(observation.BankedResetObservedAt, now, maxAge) {
		profile.BankedResetObservedAt = observation.BankedResetObservedAt.UTC().Format(time.RFC3339Nano)
		count := observation.BankedResetCount
		profile.BankedResetCount = &count
		if observation.BankedResetExpiresAt.After(now) {
			profile.BankedResetExpiresAt = observation.BankedResetExpiresAt.UTC().Format(time.RFC3339Nano)
		}
	}
	if !observationFresh(observation.ObservedAt, now, maxAge) {
		return
	}
	if observation.Plan != "" && (profile.Plan == "" || !a.Quota.ObservedAt.After(observation.ObservedAt)) {
		profile.Plan = observation.Plan
	}
	for _, window := range []*CodexRoutingWindow{observation.Primary, observation.Secondary} {
		if window == nil || window.WindowMinutes != 10080 || !window.ResetsAt.After(now) {
			continue
		}
		observed := window.ObservedAt
		if observed.IsZero() {
			observed = observation.ObservedAt
		}
		if !observationFresh(observed, now, maxAge) {
			continue
		}
		if profile.WeeklyResetAt == "" || window.ResetsAt.Before(observedResetTime(profile.WeeklyResetAt)) {
			profile.WeeklyResetAt = window.ResetsAt.UTC().Format(time.RFC3339)
			profile.ResetSource = "observed"
			profile.ObservedAt = observed.UTC().Format(time.RFC3339Nano)
		}
	}
}

func observationFresh(observed, now time.Time, maxAge time.Duration) bool {
	return !observed.IsZero() && !observed.After(now) && now.Sub(observed) <= maxAge
}
