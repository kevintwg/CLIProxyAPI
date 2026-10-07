package cliproxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const (
	codexUsageURL            = coreauth.CodexUsageURL
	codexResetCreditsURL     = coreauth.CodexResetCreditsURL
	codexObservationInterval = time.Minute
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
	// The repository forbids deadlines after an upstream connection is established.
	// Probes use the service cancellation context and never change generation transports.
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if errRequest != nil {
		return nil, errors.New("invalid Codex observation URL")
	}
	req.Header.Set("Accept", "application/json")
	if accountID := coreauth.CodexAccountID(auth); accountID != "" {
		req.Header.Set("ChatGPT-Account-ID", accountID)
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

func parseCodexUsageObservation(data []byte, now time.Time) (*coreauth.CodexRoutingObservation, error) {
	return coreauth.ParseCodexUsageObservation(data, now)
}

func parseCodexResetCreditObservation(data []byte, now time.Time) (*coreauth.CodexRoutingObservation, error) {
	return coreauth.ParseCodexResetCreditObservation(data, now)
}
