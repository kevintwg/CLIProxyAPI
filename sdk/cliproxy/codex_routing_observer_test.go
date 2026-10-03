package cliproxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestParseCodexUsageObservation(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	valid := fmt.Sprintf(`{"plan_type":"PRO","rate_limit":{"primary_window":{"used_percent":95,"reset_at":%d,"limit_window_seconds":18000},"secondary_window":{"used_percent":80,"reset_at":%d,"limit_window_seconds":604800}}}`, now.Add(time.Hour).Unix(), now.Add(24*time.Hour).Unix())
	observation, err := parseCodexUsageObservation([]byte(valid), now)
	if err != nil || observation.Plan != "pro" || observation.Primary.UsedPercent != 95 || observation.Primary.WindowMinutes != 300 || observation.Secondary.WindowMinutes != 10080 {
		t.Fatalf("valid usage rejected: %+v %v", observation, err)
	}
	for _, invalid := range []string{`{}`, `{"rate_limit":{}}`, strings.Replace(valid, `"used_percent":95`, `"used_percent":101`, 1), strings.Replace(valid, `"used_percent":95`, `"used_percent":null`, 1), strings.Replace(valid, fmt.Sprint(now.Add(time.Hour).Unix()), fmt.Sprint(now.Unix()), 1), valid + `{}`} {
		if _, err := parseCodexUsageObservation([]byte(invalid), now); err == nil {
			t.Fatalf("invalid usage accepted: %s", invalid)
		}
	}
}

func TestParseCodexResetCreditObservation(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	data := `{"credits":[{"status":"available","expires_at":"2030-01-03T00:00:00Z"},{"status":"available","is_supported_by_plan":false,"expires_at":"2030-01-01T00:01:00Z"},{"status":"consumed","expires_at":"2030-01-01T00:02:00Z"},{"status":"available","expires_at":null},{"status":"available","expires_at":"2029-01-01T00:00:00Z"},{"status":"available","is_supported_by_plan":true,"expires_at":"2030-01-02T00:00:00Z"}]}`
	observation, err := parseCodexResetCreditObservation([]byte(data), now)
	if err != nil || !observation.BankedResetExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("earliest usable expiry missing: %+v %v", observation, err)
	}
	for _, empty := range []string{`{"credits":[]}`, `{"credits":[{"status":"available","expires_at":null}]}`} {
		observation, err = parseCodexResetCreditObservation([]byte(empty), now)
		if err != nil || observation.BankedResetObservedAt.IsZero() || !observation.BankedResetExpiresAt.IsZero() {
			t.Fatal("valid empty/timeless credits failed")
		}
	}
	for _, invalid := range []string{`{}`, `{"credits":null}`, `{"credits":{}}`, `{"credits":[{"status":"available","expires_at":"invalid"}]}`, `{"credits":[{"status":"available"}]}`} {
		if _, err = parseCodexResetCreditObservation([]byte(invalid), now); err == nil {
			t.Fatalf("invalid credits accepted: %s", invalid)
		}
	}
}

func TestCodexObserverReadOnlyAndFailurePreservation(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	var fail atomic.Bool
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet {
			t.Error("probe used mutating method")
		}
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/usage" {
			fmt.Fprintf(w, `{"rate_limit":{"primary_window":{"used_percent":97,"reset_at":%d,"limit_window_seconds":18000}}}`, now.Add(time.Hour).Unix())
			return
		}
		fmt.Fprint(w, `{"credits":[{"status":"available","expires_at":"2030-01-02T00:00:00Z"}]}`)
	}))
	defer server.Close()
	manager := coreauth.NewManager(nil, nil, nil)
	auth, err := manager.Register(context.Background(), &coreauth.Auth{ID: "fake", Provider: "codex", Metadata: map[string]any{"access_token": "synthetic"}})
	if err != nil {
		t.Fatal(err)
	}
	observer := &codexRoutingObserver{manager: manager, request: func(ctx context.Context, auth *coreauth.Auth, req *http.Request) (*http.Response, error) {
		return server.Client().Do(req)
	}, now: func() time.Time { return now }, usageURL: server.URL + "/usage", creditsURL: server.URL + "/credits"}
	observer.observe(context.Background(), auth)
	current, _ := manager.GetByID(auth.ID)
	if calls.Load() != 2 || current.CodexRouting == nil || current.CodexRouting.Primary.UsedPercent != 97 || current.CodexRouting.BankedResetExpiresAt.IsZero() {
		t.Fatal("read-only probes did not update independent snapshots")
	}
	fail.Store(true)
	now = now.Add(time.Minute)
	observer.observe(context.Background(), auth)
	current, _ = manager.GetByID(auth.ID)
	if current.CodexRouting.Primary.UsedPercent != 97 || current.CodexRouting.BankedResetExpiresAt.IsZero() {
		t.Fatal("failed probe erased data")
	}
	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", codexProbeBodyLimit+1))
	}))
	defer oversized.Close()
	observer.request = func(ctx context.Context, auth *coreauth.Auth, req *http.Request) (*http.Response, error) {
		return oversized.Client().Do(req)
	}
	if _, err = observer.fetch(context.Background(), auth, oversized.URL); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestCodexObserverInitialPollConcurrencyAndCancellation(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	for i := 0; i < 12; i++ {
		_, err := manager.Register(context.Background(), &coreauth.Auth{ID: fmt.Sprint(i), Provider: "codex", Metadata: map[string]any{"access_token": "synthetic"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	started := make(chan struct{}, 12)
	var active, maximum atomic.Int32
	observer := &codexRoutingObserver{manager: manager, now: time.Now, usageURL: "https://example.test/usage", creditsURL: "https://example.test/credits", interval: time.Hour,
		request: func(ctx context.Context, auth *coreauth.Auth, req *http.Request) (*http.Response, error) {
			count := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); count > old; old = maximum.Load() {
				if maximum.CompareAndSwap(old, count) {
					break
				}
			}
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); observer.run(ctx) }()
	for i := 0; i < codexObserverWorkers; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("initial workers did not start")
		}
	}
	if maximum.Load() != codexObserverWorkers {
		t.Fatalf("concurrency=%d", maximum.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("observer did not cancel")
	}
	if active.Load() != 0 {
		t.Fatal("worker leaked")
	}
}

func TestCodexObserverDuplicateLifecycleStart(t *testing.T) {
	service := &Service{coreManager: coreauth.NewManager(nil, nil, nil)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.startCodexRoutingObserver(ctx)
	first := service.codexObserverDone
	service.startCodexRoutingObserver(ctx)
	if service.codexObserverDone != first {
		t.Fatal("duplicate loop started")
	}
	service.stopCodexRoutingObserver()
	select {
	case <-first:
	default:
		t.Fatal("stop did not join loop")
	}
	if service.codexObserverDone != nil {
		t.Fatal("stopped observer retained lifecycle state")
	}
}

func TestCodexObserverInFlightProbeCannotOverwriteAccountOrNewerWindow(t *testing.T) {
	for _, switchAccount := range []bool{false, true} {
		t.Run(fmt.Sprint(switchAccount), func(t *testing.T) {
			now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			manager := coreauth.NewManager(nil, nil, nil)
			auth, err := manager.Register(context.Background(), &coreauth.Auth{ID: "fake", Provider: "codex", Metadata: map[string]any{"access_token": "synthetic", "account_id": "first"}})
			if err != nil {
				t.Fatal(err)
			}
			started, release := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/credits" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				close(started)
				<-release
				fmt.Fprintf(w, `{"rate_limit":{"primary_window":{"used_percent":1,"reset_at":%d,"limit_window_seconds":18000}}}`, now.Add(time.Hour).Unix())
			}))
			defer server.Close()
			observer := &codexRoutingObserver{manager: manager, now: func() time.Time { return now }, usageURL: server.URL + "/usage", creditsURL: server.URL + "/credits", request: func(ctx context.Context, auth *coreauth.Auth, req *http.Request) (*http.Response, error) {
				return server.Client().Do(req)
			}}
			done := make(chan struct{})
			go func() { defer close(done); observer.observe(context.Background(), auth) }()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("probe did not start")
			}
			if switchAccount {
				replacement := auth.Clone()
				replacement.Metadata["account_id"] = "second"
				if _, err := manager.Update(context.Background(), replacement); err != nil {
					t.Fatal(err)
				}
			} else {
				manager.UpdateCodexRoutingObservation(auth, &coreauth.CodexRoutingObservation{ObservedAt: now.Add(time.Minute), Primary: &coreauth.CodexRoutingWindow{UsedPercent: 98}})
			}
			close(release)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("probe did not finish")
			}
			current, _ := manager.GetByID(auth.ID)
			if switchAccount {
				if current.CodexRouting != nil {
					t.Fatal("late probe contaminated replacement")
				}
			} else if current.CodexRouting.Primary.UsedPercent != 98 {
				t.Fatal("late probe overwrote newer low watermark")
			}
		})
	}
}

// The fake executor injects generated account identity at the same manager boundary
// as the production executor, while all traffic remains on the loopback provider.
type codexObserverProofExecutor struct {
	syncTestExecutor
	client *http.Client
}

func (e *codexObserverProofExecutor) HttpRequest(ctx context.Context, auth *coreauth.Auth, req *http.Request) (*http.Response, error) {
	req = req.Clone(ctx)
	req.Header.Set("X-Synthetic-Account", auth.ID)
	return e.client.Do(req)
}

func TestCodexObserverBackendRoutingProof(t *testing.T) {
	reference := time.Now().UTC().Truncate(time.Second)
	now := reference.Add(-time.Minute)
	var phase atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Error("observation must be read-only")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		account := r.Header.Get("X-Synthetic-Account")
		if r.Header.Get("ChatGPT-Account-ID") != account {
			t.Error("probe omitted account selection header")
		}
		if account == "" {
			t.Error("manager executor did not inject generated account identity")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bankExpiry := reference.Add(2 * time.Hour)
		weekly := reference.Add(48 * time.Hour)
		plan := "plus"
		used := 40
		if account == "plus-early-bank" {
			bankExpiry = reference.Add(time.Hour)
			if phase.Load() == 1 {
				used = 95
			}
		}
		if account == "plus-early-weekly" {
			weekly = reference.Add(24 * time.Hour)
		}
		if account == "pro-account" {
			plan = "pro"
			bankExpiry = reference.Add(30 * time.Minute)
			weekly = reference.Add(12 * time.Hour)
		}
		if phase.Load() == 3 && plan == "plus" {
			bankExpiry = reference.Add(time.Hour)
		}
		switch r.URL.Path {
		case "/usage":
			fmt.Fprintf(w, `{"plan_type":%q,"rate_limit":{"primary_window":{"used_percent":%d,"reset_at":%d,"limit_window_seconds":18000},"secondary_window":{"used_percent":20,"reset_at":%d,"limit_window_seconds":604800}}}`, plan, used, reference.Add(5*time.Hour).Unix(), weekly.Unix())
		case "/credits":
			fmt.Fprintf(w, `{"credits":[{"status":"available","is_supported_by_plan":true,"expires_at":%q}]}`, bankExpiry.Format(time.RFC3339))
		default:
			t.Error("unexpected probe endpoint")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&codexObserverProofExecutor{client: server.Client()})
	for _, id := range []string{"plus-early-bank", "plus-early-weekly", "pro-account"} {
		if _, err := manager.Register(context.Background(), &coreauth.Auth{ID: id, Provider: "codex", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic-token", "account_id": id}}); err != nil {
			t.Fatal(err)
		}
	}
	observer := &codexRoutingObserver{manager: manager, request: manager.HttpRequest, now: func() time.Time { return now }, usageURL: server.URL + "/usage", creditsURL: server.URL + "/credits"}
	selector := &coreauth.SubscriptionFirstSelector{}
	expectations := []struct{ wanted, verdict string }{
		{"plus-early-bank", "Plus tier wins before Pro; earliest usable bank expiry wins before earlier weekly reset"},
		{"plus-early-weekly", "95 percent used excludes the account with 5 percent remaining"},
		{"plus-early-bank", "fresh 40 percent usage restores eligibility and bank-expiry preference"},
		{"plus-early-weekly", "equal bank expiry falls through to earliest weekly reset"},
	}
	for i, expectation := range expectations {
		phase.Store(int32(i))
		now = now.Add(time.Second)
		for _, auth := range manager.List() {
			observer.observe(context.Background(), auth)
		}
		picked, err := selector.Pick(context.Background(), "codex", "", cliproxyexecutor.Options{}, manager.List())
		if err != nil || picked == nil || picked.ID != expectation.wanted {
			t.Fatalf("phase %d got %+v, error %v; wanted %s", i, picked, err, expectation.wanted)
		}
		t.Logf("backend proof phase %d: %s; selected generated account %s", i, expectation.verdict, picked.ID)
	}
}
