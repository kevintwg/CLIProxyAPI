package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codex"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// RoutingProfile contains only safe ranking inputs, never authentication material.
type RoutingProfile struct {
	Tier                   *int     `json:"tier,omitempty"`
	TierSource             string   `json:"tier_source"`
	Plan                   string   `json:"plan,omitempty"`
	WeeklyRemainingPercent *float64 `json:"weekly_remaining_percent,omitempty"`
	WeeklyResetAt          string   `json:"weekly_reset_at,omitempty"`
	ResetSource            string   `json:"reset_source"`
	ObservedAt             string   `json:"observed_at,omitempty"`
	BankedResetExpiresAt   string   `json:"banked_reset_expires_at,omitempty"`
	BankedResetObservedAt  string   `json:"banked_reset_observed_at,omitempty"`
	BankedResetCount       *int     `json:"banked_reset_count,omitempty"`
	QuotaReservePercent    *float64 `json:"quota_reserve_percent,omitempty"`
	QuotaReserveBlocked    *bool    `json:"quota_reserve_blocked,omitempty"`
}

// SubscriptionWeeklyReserveThresholdPercent keeps an account out of the
// normal same-tier rotation once its observed weekly capacity falls below this
// threshold. Tier precedence still wins over the reserve rule.
const SubscriptionWeeklyReserveThresholdPercent = 30.0

func ManualRoutingTier(a *Auth) (int, bool) {
	if a == nil {
		return 0, false
	}
	value, ok := a.Metadata["routing_tier"]
	if !ok || value == nil {
		return 0, false
	}
	n, err := routingTierNumber(value)
	return n, err == nil
}

func routingTierNumber(value any) (int, error) {
	var n int64
	switch v := value.(type) {
	case int:
		n = int64(v)
	case int64:
		n = v
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("routing_tier must be an integer from 0 to 1000")
		}
		n = parsed
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) || v < 0 || v > 1000 {
			return 0, fmt.Errorf("routing_tier must be an integer from 0 to 1000")
		}
		n = int64(v)
	default:
		return 0, fmt.Errorf("routing_tier must be an integer from 0 to 1000")
	}
	if n < 0 || n > 1000 {
		return 0, fmt.Errorf("routing_tier must be an integer from 0 to 1000")
	}
	return int(n), nil
}

func ManualRoutingWeeklyReset(a *Auth) string {
	if a == nil {
		return ""
	}
	value, _ := a.Metadata["routing_weekly_reset_at"].(string)
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return ""
	}
	return value
}

// ValidateRoutingField validates the manual overrides before metadata is mutated.
func ValidateRoutingField(field string, value any, now time.Time) error {
	root, _, nested := strings.Cut(field, ".")
	if root != "routing_tier" && root != "routing_weekly_reset_at" {
		return nil
	}
	if nested {
		return fmt.Errorf("%s does not support nested fields", root)
	}
	if value == nil {
		return nil
	}
	if root == "routing_tier" {
		_, err := routingTierNumber(value)
		return err
	}
	raw, ok := value.(string)
	if !ok {
		return fmt.Errorf("routing_weekly_reset_at must be a future RFC3339 timestamp")
	}
	reset, err := time.Parse(time.RFC3339, raw)
	if err != nil || !reset.After(now) {
		return fmt.Errorf("routing_weekly_reset_at must be a future RFC3339 timestamp")
	}
	return nil
}

func SubscriptionRoutingProfile(a *Auth, now time.Time, maxAge time.Duration) RoutingProfile {
	profile := RoutingProfile{TierSource: "unknown", ResetSource: "unknown"}
	if a == nil {
		return profile
	}
	if maxAge <= 0 {
		maxAge = 30 * time.Minute
	}
	observed := !a.Quota.ObservedAt.IsZero() && !a.Quota.ObservedAt.After(now)
	fresh := observed && now.Sub(a.Quota.ObservedAt) <= maxAge
	isClaude := isClaudeAuth(a)
	// Claude routes on its last reading at any age; the next response through
	// the account replaces it.
	usable := fresh || (isClaude && observed)
	signals := make(map[string]string, len(a.Quota.Signals))
	for key, value := range a.Quota.Signals {
		signals[strings.ToLower(key)] = value
	}
	profile.Plan = detectedRoutingPlan(a, signals, fresh)
	applyCodexRoutingProfile(&profile, a, now, maxAge)
	if usable && isClaude {
		if remaining, ok := observedClaudeWeeklyRemaining(signals, now); ok {
			profile.WeeklyRemainingPercent = &remaining
		}
	}
	tiers := map[string]int{"free": 0, "go": 1, "plus": 2, "pro": 3}
	if tier, ok := tiers[profile.Plan]; ok {
		profile.Tier = &tier
		profile.TierSource = "plan"
	}
	if usable && (profile.ObservedAt == "" || a.Quota.ObservedAt.After(observedResetTime(profile.ObservedAt))) {
		profile.ObservedAt = a.Quota.ObservedAt.UTC().Format(time.RFC3339)
	}

	if tier, ok := ManualRoutingTier(a); ok {
		profile.Tier = &tier
		profile.TierSource = "manual"
	}
	if raw := ManualRoutingWeeklyReset(a); raw != "" {
		reset, _ := time.Parse(time.RFC3339, raw)
		if reset.After(now) {
			profile.WeeklyResetAt = reset.UTC().Format(time.RFC3339)
			profile.ResetSource = "manual"
			return profile
		}
	}
	if !usable {
		return profile
	}
	reset := observedWeeklyReset(a.Provider, signals, a.Quota.ObservedAt, now)
	if profile.WeeklyResetAt != "" && !a.Quota.ObservedAt.After(codexRoutingObservedAt(a)) {
		return profile
	}
	if reset.After(now) {
		profile.WeeklyResetAt = reset.UTC().Format(time.RFC3339)
		profile.ResetSource = "observed"
	}
	return profile
}

func observedClaudeWeeklyRemaining(signals map[string]string, now time.Time) (float64, bool) {
	raw, ok := signals["anthropic-ratelimit-unified-7d-utilization"]
	if !ok {
		return 0, false
	}
	// A reading taken before the weekly reset no longer applies: the new week starts full.
	if reset := observedResetTime(signals["anthropic-ratelimit-unified-7d-reset"]); !reset.IsZero() && !reset.After(now) {
		return 100, true
	}
	utilization, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(utilization) || math.IsInf(utilization, 0) || utilization < 0 {
		return 0, false
	}
	if utilization > 1 {
		utilization = 1
	}
	return (1 - utilization) * 100, true
}

const (
	weeklyReserveUnread = iota
	weeklyReserveHealthy
	weeklyReserveBelow
)

// weeklyReserveRank orders accounts within a tier. A Claude subscription
// account with no reading yet goes first so its next response supplies one,
// then accounts at or above the weekly reserve, then accounts below it.
// Accounts without a Claude reading otherwise rank as healthy.
func weeklyReserveRank(a *Auth, profile RoutingProfile) int {
	if profile.WeeklyRemainingPercent != nil {
		if *profile.WeeklyRemainingPercent < SubscriptionWeeklyReserveThresholdPercent {
			return weeklyReserveBelow
		}
		return weeklyReserveHealthy
	}
	if isClaudeAuth(a) && a.AuthKind() == AuthKindOAuth {
		return weeklyReserveUnread
	}
	return weeklyReserveHealthy
}

func detectedRoutingPlan(a *Auth, signals map[string]string, fresh bool) string {
	if !strings.EqualFold(a.Provider, "codex") {
		return ""
	}
	plan := ""

	if fresh {
		plan = strings.ToLower(strings.TrimSpace(signals["x-codex-plan-type"]))
	}
	if plan == "" {
		plan = strings.ToLower(strings.TrimSpace(a.Attributes["plan_type"]))
	}
	if plan == "" {
		raw, _ := a.Metadata["plan_type"].(string)
		plan = strings.ToLower(strings.TrimSpace(raw))
	}
	if plan == "" {
		raw, _ := a.Metadata["id_token"].(string)
		if claims, err := codex.ParseJWTToken(raw); err == nil && claims != nil {
			plan = strings.ToLower(strings.TrimSpace(claims.CodexAuthInfo.ChatgptPlanType))
		}
	}
	// A detected plan label must be bounded and match the known plan vocabulary.
	switch plan {
	case "free", "go", "plus", "pro", "team", "business", "enterprise", "edu":
	default:
		plan = ""
	}
	return plan
}

func observedWeeklyReset(provider string, signals map[string]string, observedAt, now time.Time) time.Time {
	var reset time.Time
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude":
		reset = observedResetTime(signals["anthropic-ratelimit-unified-7d-reset"])
	case "codex":
		for _, window := range []string{"primary", "secondary"} {
			prefix := "x-codex-" + window + "-"
			if strings.TrimSpace(signals[prefix+"window-minutes"]) != "10080" {
				continue
			}
			candidate := observedResetTime(signals[prefix+"reset-at"])
			if candidate.IsZero() {
				candidate = observedRelativeWeeklyReset(signals[prefix+"reset-after-seconds"], observedAt)
			}

			if candidate.After(now) && (reset.IsZero() || candidate.Before(reset)) {
				reset = candidate
			}
		}
	}
	return reset
}

// Relative weekly resets are anchored to the response observation, never the next pick.
func observedRelativeWeeklyReset(raw string, observedAt time.Time) time.Time {
	const weeklyWindowSeconds = 7 * 24 * 60 * 60
	seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || seconds <= 0 || seconds > weeklyWindowSeconds || observedAt.IsZero() {
		return time.Time{}
	}
	return observedAt.Add(time.Duration(seconds) * time.Second)
}

func observedResetTime(raw string) time.Time {
	if reset, err := time.Parse(time.RFC3339, strings.TrimSpace(raw)); err == nil {
		return reset
	}
	if seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && seconds >= 0 && seconds <= 253402300799 {
		return time.Unix(seconds, 0)
	}
	return time.Time{}
}

// SubscriptionFirstSelector ranks by tier, weekly reserve, weekly reset when enabled, then banked-reset expiry.
// Affinity can move to an earlier weekly reset within a tier, or return to a better tier when enabled.
type SubscriptionFirstSelector struct {
	MaxObservationAge time.Duration
	PreferWeeklyReset *bool
	// ReturnToPreferredTier lets session affinity leave a bound credential once a strictly
	// better tier is available again, instead of keeping a failover binding for its whole TTL.
	ReturnToPreferredTier bool
	nowFunc               func() time.Time
}

func selectorUsesSubscriptionFirst(selector Selector) bool {
	if affinity, ok := selector.(*SessionAffinitySelector); ok {
		selector = affinity.fallback
	}
	_, ok := selector.(*SubscriptionFirstSelector)
	return ok
}

func routingTierFromProfile(profile RoutingProfile) int {
	if profile.Tier != nil {
		return *profile.Tier
	}
	return 1001
}

func (s *SubscriptionFirstSelector) routingTier(a *Auth, now time.Time) int {
	return routingTierFromProfile(SubscriptionRoutingProfile(a, now, s.MaxObservationAge))
}

// preferredAffinityPick considers only candidates allowed to replace a healthy binding.
// Banked expiry, priority, and account ID alone never trigger a move.
func (s *SubscriptionFirstSelector) preferredAffinityPick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, bound *Auth, auths []*Auth) *Auth {
	if s == nil || bound == nil {
		return nil
	}
	preferWeekly := s.PreferWeeklyReset == nil || *s.PreferWeeklyReset
	now := time.Now()
	if s.nowFunc != nil {
		now = s.nowFunc()
	}
	profiles := make(map[string]RoutingProfile, len(auths)+1)
	for _, a := range auths {
		if a != nil {
			profiles[a.ID] = SubscriptionRoutingProfile(a, now, s.MaxObservationAge)
		}
	}
	boundProfile, ok := profiles[bound.ID]
	if !ok {
		boundProfile = SubscriptionRoutingProfile(bound, now, s.MaxObservationAge)
		profiles[bound.ID] = boundProfile
	}
	boundBelowReserve := weeklyReserveRank(bound, boundProfile) == weeklyReserveBelow
	boundTier := routingTierFromProfile(boundProfile)
	boundReset := observedResetTime(boundProfile.WeeklyResetAt)
	candidates := []*Auth{bound}
	for _, a := range auths {
		if a == nil || a.ID == bound.ID {
			continue
		}
		profile, ok := profiles[a.ID]
		if !ok {
			profile = SubscriptionRoutingProfile(a, now, s.MaxObservationAge)
		}
		tier := routingTierFromProfile(profile)
		if tier < boundTier && s.ReturnToPreferredTier {
			candidates = append(candidates, a)
		} else if tier == boundTier {
			if boundBelowReserve && weeklyReserveRank(a, profile) != weeklyReserveBelow {
				candidates = append(candidates, a)
				continue
			}
			if !preferWeekly {
				continue
			}
			reset := observedResetTime(profile.WeeklyResetAt)
			if !reset.IsZero() && (boundReset.IsZero() || reset.Before(boundReset)) {
				candidates = append(candidates, a)
			}
		}
	}
	if len(candidates) == 1 {
		return nil
	}
	pick, err := s.Pick(ctx, provider, model, opts, candidates)
	if err != nil || pick == nil || pick.ID == bound.ID {
		return nil
	}
	return pick
}

func (s *SubscriptionFirstSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	now := time.Now()
	if s.nowFunc != nil {
		now = s.nowFunc()
	}
	available, err := getSelectorAvailableAuthsAcrossPriorities(ctx, auths, provider, model, now)
	if err != nil {
		return nil, err
	}
	profiles := make(map[string]RoutingProfile, len(available))
	for _, a := range available {
		profiles[a.ID] = SubscriptionRoutingProfile(a, now, s.MaxObservationAge)
	}
	preferReset := s.PreferWeeklyReset == nil || *s.PreferWeeklyReset
	sort.Slice(available, func(i, j int) bool {
		a, b := available[i], available[j]
		pa, pb := profiles[a.ID], profiles[b.ID]
		ta, tb := 1001, 1001
		if pa.Tier != nil {
			ta = *pa.Tier
		}
		if pb.Tier != nil {
			tb = *pb.Tier
		}
		if ta != tb {
			return ta < tb
		}
		if ra, rb := weeklyReserveRank(a, pa), weeklyReserveRank(b, pb); ra != rb {
			return ra < rb
		}
		if preferReset && pa.WeeklyResetAt != pb.WeeklyResetAt {
			if pa.WeeklyResetAt == "" {
				return false
			}
			if pb.WeeklyResetAt == "" {
				return true
			}
			ra, _ := time.Parse(time.RFC3339, pa.WeeklyResetAt)
			rb, _ := time.Parse(time.RFC3339, pb.WeeklyResetAt)
			return ra.Before(rb)
		}
		if pa.BankedResetExpiresAt != pb.BankedResetExpiresAt {
			if pa.BankedResetExpiresAt == "" {
				return false
			}
			if pb.BankedResetExpiresAt == "" {
				return true
			}
			ra, _ := time.Parse(time.RFC3339Nano, pa.BankedResetExpiresAt)
			rb, _ := time.Parse(time.RFC3339Nano, pb.BankedResetExpiresAt)
			return ra.Before(rb)
		}
		if authPriority(a) != authPriority(b) {
			return authPriority(a) > authPriority(b)
		}
		return a.ID < b.ID
	})
	return available[0], nil
}
