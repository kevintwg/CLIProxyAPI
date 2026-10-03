package cliproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const (
	codexUsageURL            = "https://chatgpt.com/backend-api/wham/usage"
	codexResetCreditsURL     = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	codexObservationInterval = time.Minute
	codexProbeTimeout        = 20 * time.Second
	codexProbeBodyLimit      = 1 << 20
	codexObserverWorkers     = 4
)

type codexRoutingObserver struct {
	manager    *coreauth.Manager
	request    func(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error)
	now        func() time.Time
	usageURL   string
	creditsURL string
	interval   time.Duration
}

func (s *Service) startCodexRoutingObserver(ctx context.Context) {
	if s == nil || s.coreManager == nil {
		return
	}
	s.codexObserverMu.Lock()
	defer s.codexObserverMu.Unlock()
	if s.codexObserverDone != nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.codexObserverCancel, s.codexObserverDone = cancel, done
	observer := &codexRoutingObserver{manager: s.coreManager, request: s.coreManager.HttpRequest,
		now: time.Now, usageURL: codexUsageURL, creditsURL: codexResetCreditsURL, interval: codexObservationInterval}
	go func() { defer close(done); observer.run(ctx) }()
}

func (s *Service) stopCodexRoutingObserver() {
	if s == nil {
		return
	}
	s.codexObserverMu.Lock()
	defer s.codexObserverMu.Unlock()
	if s.codexObserverCancel != nil {
		s.codexObserverCancel()
	}
	if s.codexObserverDone != nil {
		<-s.codexObserverDone
	}
	s.codexObserverCancel, s.codexObserverDone = nil, nil
}

func (o *codexRoutingObserver) run(ctx context.Context) {
	jobs := make(chan *coreauth.Auth)
	var workers, cycle sync.WaitGroup
	for i := 0; i < codexObserverWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for auth := range jobs {
				o.observe(ctx, auth)
				cycle.Done()
			}
		}()
	}
	defer func() { close(jobs); workers.Wait() }()
	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()
	for {
		for _, auth := range o.manager.List() {
			if ctx.Err() != nil {
				break
			}
			if auth.Disabled || auth.Status == coreauth.StatusDisabled || !strings.EqualFold(auth.Provider, "codex") || auth.AuthKind() != coreauth.AuthKindOAuth {
				continue
			}
			cycle.Add(1)
			select {
			case jobs <- auth:
			case <-ctx.Done():
				cycle.Done()
			}
		}
		cycle.Wait()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (o *codexRoutingObserver) observe(ctx context.Context, auth *coreauth.Auth) {
	if ctx.Err() != nil {
		return
	}
	// Separate probes preserve useful quota even if the optional reset-credit endpoint fails.
	observedAt := o.now()
	if data, err := o.fetch(ctx, auth, o.usageURL); err == nil {
		if observation, errParse := parseCodexUsageObservation(data, o.now()); errParse == nil {
			observation.ObservedAt = observedAt
			if observation.Primary != nil {
				observation.Primary.ObservedAt = observedAt
			}
			if observation.Secondary != nil {
				observation.Secondary.ObservedAt = observedAt
			}
			o.manager.UpdateCodexRoutingObservation(auth, observation)
		}
	}
	if ctx.Err() != nil {
		return
	}
	observedAt = o.now()
	if data, err := o.fetch(ctx, auth, o.creditsURL); err == nil {
		if observation, errParse := parseCodexResetCreditObservation(data, o.now()); errParse == nil {
			observation.BankedResetObservedAt = observedAt
			o.manager.UpdateCodexRoutingObservation(auth, observation)
		}
	}
}

func (o *codexRoutingObserver) fetch(ctx context.Context, auth *coreauth.Auth, url string) ([]byte, error) {
	// Read-only background probes have a bounded lifetime so a hung account cannot
	// occupy a recovery worker forever. This does not affect generation requests.
	ctx, cancel := context.WithTimeout(ctx, codexProbeTimeout)
	defer cancel()
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if errRequest != nil {
		return nil, errors.New("invalid Codex observation URL")
	}
	req.Header.Set("Accept", "application/json")
	for _, key := range []string{"account_id", "chatgpt_account_id"} {
		accountID, _ := auth.Metadata[key].(string)
		if strings.TrimSpace(accountID) == "" {
			accountID = auth.Attributes[key]
		}
		if accountID = strings.TrimSpace(accountID); accountID != "" {
			req.Header.Set("ChatGPT-Account-ID", accountID)
			break
		}
	}
	response, errRequest := o.request(ctx, auth, req)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if errRequest != nil {
		return nil, errors.New("Codex observation request failed")
	}
	if response == nil || response.Body == nil || response.StatusCode != http.StatusOK {
		return nil, errors.New("Codex observation unavailable")
	}
	data, errRead := io.ReadAll(io.LimitReader(response.Body, codexProbeBodyLimit+1))
	if errRead != nil || len(data) > codexProbeBodyLimit {
		return nil, errors.New("invalid Codex observation body")
	}
	return data, nil
}

type codexUsageWindow struct {
	UsedPercent   *float64 `json:"used_percent"`
	ResetAt       *int64   `json:"reset_at"`
	WindowSeconds *int     `json:"limit_window_seconds"`
}

func parseCodexUsageObservation(data []byte, now time.Time) (*coreauth.CodexRoutingObservation, error) {
	var payload struct {
		Plan      string `json:"plan_type"`
		RateLimit *struct {
			Primary   *codexUsageWindow `json:"primary_window"`
			Secondary *codexUsageWindow `json:"secondary_window"`
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
	return &coreauth.CodexRoutingObservation{ObservedAt: now, Plan: strings.ToLower(strings.TrimSpace(payload.Plan)), Primary: primary, Secondary: secondary}, nil
}

func parseCodexUsageWindow(window *codexUsageWindow, now time.Time) (*coreauth.CodexRoutingWindow, error) {
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
	return &coreauth.CodexRoutingWindow{ObservedAt: now, UsedPercent: *window.UsedPercent, ResetsAt: reset, WindowMinutes: *window.WindowSeconds / 60}, nil
}

func parseCodexResetCreditObservation(data []byte, now time.Time) (*coreauth.CodexRoutingObservation, error) {
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
	observation := &coreauth.CodexRoutingObservation{BankedResetObservedAt: now}
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
		if observation.BankedResetExpiresAt.IsZero() || expiry.Before(observation.BankedResetExpiresAt) {
			observation.BankedResetExpiresAt = expiry
		}
	}
	return observation, nil
}
