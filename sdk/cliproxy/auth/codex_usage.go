package auth

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
)

// CodexAccountID returns the workspace identifier selected for a Codex account.
func CodexAccountID(auth *Auth) string {
	if auth == nil {
		return ""
	}
	for _, key := range []string{"account_id", "chatgpt_account_id"} {
		if value, ok := auth.Metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		if value := strings.TrimSpace(auth.Attributes[key]); value != "" {
			return value
		}
	}
	return ""
}

// CodexUsageURL is the upstream endpoint that reports current usage windows.
const CodexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// CodexResetCreditsURL is the upstream endpoint that reports reset credits.
const CodexResetCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"

// CodexResetCreditConsumeURL is the upstream endpoint that consumes one reset credit.
const CodexResetCreditConsumeURL = CodexResetCreditsURL + "/consume"

type codexUsageWindowPayload struct {
	UsedPercent   *float64 `json:"used_percent"`
	ResetAt       *int64   `json:"reset_at"`
	WindowSeconds *int     `json:"limit_window_seconds"`
}

// ParseCodexUsageObservation validates the provider usage response.
func ParseCodexUsageObservation(data []byte, now time.Time) (*CodexRoutingObservation, error) {
	var payload struct {
		Plan      string `json:"plan_type"`
		RateLimit *struct {
			Primary   *codexUsageWindowPayload `json:"primary_window"`
			Secondary *codexUsageWindowPayload `json:"secondary_window"`
		} `json:"rate_limit"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.RateLimit == nil {
		return nil, errors.New("invalid Codex usage")
	}
	primary, errPrimary := parseCodexUsageWindow(payload.RateLimit.Primary, now)
	secondary, errSecondary := parseCodexUsageWindow(payload.RateLimit.Secondary, now)
	if errPrimary != nil || errSecondary != nil || (primary == nil && secondary == nil) {
		return nil, errors.New("invalid Codex usage windows")
	}
	return &CodexRoutingObservation{ObservedAt: now, Plan: strings.ToLower(strings.TrimSpace(payload.Plan)), Primary: primary, Secondary: secondary}, nil
}

func parseCodexUsageWindow(window *codexUsageWindowPayload, now time.Time) (*CodexRoutingWindow, error) {
	if window == nil {
		return nil, nil
	}
	if window.UsedPercent == nil || window.ResetAt == nil || window.WindowSeconds == nil ||
		math.IsNaN(*window.UsedPercent) || math.IsInf(*window.UsedPercent, 0) || *window.UsedPercent < 0 || *window.UsedPercent > 100 ||
		*window.WindowSeconds < 60 || *window.WindowSeconds%60 != 0 {
		return nil, errors.New("invalid Codex usage window")
	}
	reset := time.Unix(*window.ResetAt, 0)
	if !reset.After(now) {
		return nil, errors.New("expired Codex usage window")
	}
	return &CodexRoutingWindow{ObservedAt: now, UsedPercent: *window.UsedPercent, ResetsAt: reset, WindowMinutes: *window.WindowSeconds / 60}, nil
}

// ParseCodexResetCreditObservation validates the reset-credit response and counts
// every currently available credit supported by the account's plan.
func ParseCodexResetCreditObservation(data []byte, now time.Time) (*CodexRoutingObservation, error) {
	var payload struct {
		Credits json.RawMessage `json:"credits"`
	}
	if json.Unmarshal(data, &payload) != nil || len(payload.Credits) == 0 || string(payload.Credits) == "null" {
		return nil, errors.New("invalid Codex reset credits")
	}
	var credits []struct {
		Status    string          `json:"status"`
		Supported *bool           `json:"is_supported_by_plan"`
		ExpiresAt json.RawMessage `json:"expires_at"`
	}
	if json.Unmarshal(payload.Credits, &credits) != nil {
		return nil, errors.New("invalid Codex reset credits")
	}
	observation := &CodexRoutingObservation{BankedResetObservedAt: now}
	for _, credit := range credits {
		if strings.TrimSpace(credit.Status) == "" {
			return nil, errors.New("invalid Codex reset credit status")
		}
		if credit.Status != "available" || (credit.Supported != nil && !*credit.Supported) {
			continue
		}
		if len(credit.ExpiresAt) == 0 {
			return nil, errors.New("missing Codex reset credit expiry")
		}
		if string(credit.ExpiresAt) == "null" {
			continue
		}
		var expiryText string
		if json.Unmarshal(credit.ExpiresAt, &expiryText) != nil {
			return nil, errors.New("invalid Codex reset credit expiry")
		}
		expiry, errExpiry := time.Parse(time.RFC3339, expiryText)
		if errExpiry != nil {
			return nil, errors.New("invalid Codex reset credit expiry")
		}
		if !expiry.After(now) {
			continue
		}
		observation.BankedResetCount++
		if observation.BankedResetExpiresAt.IsZero() || expiry.Before(observation.BankedResetExpiresAt) {
			observation.BankedResetExpiresAt = expiry
		}
	}
	return observation, nil
}
