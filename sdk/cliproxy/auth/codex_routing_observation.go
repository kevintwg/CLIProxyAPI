package auth

import (
	"context"
	"strings"
	"time"
)

// CodexRoutingWindow is a validated read-only usage window.
type CodexRoutingWindow struct {
	ObservedAt    time.Time
	UsedPercent   float64
	ResetsAt      time.Time
	WindowMinutes int
}

// CodexRoutingObservation is an in-memory snapshot, never credential storage.
// Quota and reset-credit timestamps advance independently because either endpoint can fail.
type CodexRoutingObservation struct {
	ObservedAt            time.Time
	Plan                  string
	Primary               *CodexRoutingWindow
	Secondary             *CodexRoutingWindow
	BankedResetObservedAt time.Time
	BankedResetExpiresAt  time.Time
	BankedResetCount      int
}

// Clone returns a snapshot with independent windows.
func (o *CodexRoutingObservation) Clone() *CodexRoutingObservation {
	if o == nil {
		return nil
	}
	cloned := *o
	if o.Primary != nil {
		window := *o.Primary
		cloned.Primary = &window
	}
	if o.Secondary != nil {
		window := *o.Secondary
		cloned.Secondary = &window
	}
	return &cloned
}

func codexAccountIdentity(a *Auth) string {
	if a == nil {
		return ""
	}
	var fields []string
	for _, key := range []string{"account_id", "chatgpt_account_id", "email"} {
		value := authMetadataString(a, key)
		if value == "" {
			value = authAttribute(a, key)
		}
		value = strings.TrimSpace(value)
		if key == "email" {
			value = strings.ToLower(value)
		}
		fields = append(fields, value)
	}
	if strings.Join(fields, "") == "" {
		return ""
	}
	return strings.Join(fields, "\x00")
}

func preserveCodexRouting(existing, incoming *Auth, refresh bool) bool {
	incoming.CodexRouting = nil
	if !codexSubscriptionAuth(existing) && !codexSubscriptionAuth(incoming) {
		return false
	}
	if !strings.EqualFold(existing.Provider, incoming.Provider) || existing.AuthKind() != incoming.AuthKind() {
		return true
	}
	existingIdentity, incomingIdentity := codexAccountIdentity(existing), codexAccountIdentity(incoming)
	if existingIdentity != incomingIdentity {
		return true
	}
	// A manager-owned refresh establishes continuity even without account metadata.
	if existingIdentity == "" && !refresh && CredentialsChanged(existing, incoming) {
		return true
	}
	incoming.CodexRouting = existing.CodexRouting.Clone()
	return false
}

func clearCodexPassiveObservations(auth *Auth) {
	auth.Quota.ObservedAt = time.Time{}
	auth.Quota.Signals = nil
	for model, state := range auth.ModelStates {
		if state == nil {
			continue
		}
		state = state.Clone()
		state.Quota.ObservedAt = time.Time{}
		state.Quota.Signals = nil
		auth.ModelStates[model] = state
	}
}

type codexRoutingObservationContextKey struct{}

func withCodexRoutingObservationAuth(ctx context.Context, auth *Auth) context.Context {
	if !codexSubscriptionAuth(auth) {
		return ctx
	}
	return context.WithValue(ctx, codexRoutingObservationContextKey{}, auth.Clone())
}

func codexRoutingObservationMatches(current, base *Auth) bool {
	return current != nil && base != nil && current.ID == base.ID &&
		strings.EqualFold(current.Provider, base.Provider) && current.AuthKind() == base.AuthKind() &&
		current.RegistrationEpoch == base.RegistrationEpoch &&
		codexAccountIdentity(current) == codexAccountIdentity(base) && !CredentialsChanged(current, base)
}

func codexResponseObservationMatches(ctx context.Context, current *Auth) bool {
	base, _ := ctx.Value(codexRoutingObservationContextKey{}).(*Auth)
	// Direct SDK result reports retain their existing caller-owned identity contract.
	return base == nil || codexRoutingObservationMatches(current, base)
}

// UpdateCodexRoutingObservation applies successful probes only to the credential that
// was read, and publishes a generation-fenced scheduler snapshot without persisting.
func (m *Manager) UpdateCodexRoutingObservation(base *Auth, observation *CodexRoutingObservation) bool {
	if m == nil || base == nil || observation == nil {
		return false
	}
	m.mu.Lock()
	current := m.auths[base.ID]
	if current == nil || current.Disabled || current.Status == StatusDisabled ||
		!strings.EqualFold(current.Provider, "codex") || current.AuthKind() != AuthKindOAuth ||
		!codexRoutingObservationMatches(current, base) {
		m.mu.Unlock()
		return false
	}
	merged := current.CodexRouting.Clone()
	if merged == nil {
		merged = &CodexRoutingObservation{}
	}
	changed := false
	if !observation.ObservedAt.IsZero() && observation.ObservedAt.After(merged.ObservedAt) {
		merged.ObservedAt = observation.ObservedAt
		if observation.Plan != "" {
			merged.Plan = observation.Plan
		}
		changed = true
	}
	for _, pair := range []struct {
		incoming *CodexRoutingWindow
		current  **CodexRoutingWindow
	}{
		{observation.Primary, &merged.Primary}, {observation.Secondary, &merged.Secondary},
	} {
		if pair.incoming == nil {
			continue
		}
		incoming := *pair.incoming
		if incoming.ObservedAt.IsZero() {
			incoming.ObservedAt = observation.ObservedAt
		}
		currentTime := time.Time{}
		if *pair.current != nil {
			currentTime = (*pair.current).ObservedAt
			if currentTime.IsZero() && current.CodexRouting != nil {
				currentTime = current.CodexRouting.ObservedAt
			}
		}
		if !incoming.ObservedAt.IsZero() && incoming.ObservedAt.After(currentTime) {
			*pair.current = &incoming
			changed = true
		}
	}
	if !observation.BankedResetObservedAt.IsZero() && observation.BankedResetObservedAt.After(merged.BankedResetObservedAt) {
		merged.BankedResetObservedAt = observation.BankedResetObservedAt
		merged.BankedResetExpiresAt = observation.BankedResetExpiresAt
		merged.BankedResetCount = observation.BankedResetCount
		changed = true
	}
	if !changed {
		m.mu.Unlock()
		return false
	}
	current.CodexRouting = merged
	current.Generation++
	snapshot := current.Clone()
	m.mu.Unlock()
	if m.scheduler != nil {
		m.scheduler.upsertAuth(snapshot)
	}
	m.structuralEpoch.Add(1)
	return true
}
