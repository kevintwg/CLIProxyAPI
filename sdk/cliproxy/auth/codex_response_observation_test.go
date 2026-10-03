package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestCodexResponseObservationAccountFence(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, change := range []string{"unchanged", "token", "account", "registration"} {
			name := change
			if streaming {
				name += "/stream"
			} else {
				name += "/execute"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				manager := NewManager(nil, &SubscriptionFirstSelector{}, nil)
				base := codexObservationAuth(t, manager)
				const model = "response-fence-model"
				registry.GetGlobalRegistry().RegisterClient(base.ID, "codex", []*registry.ModelInfo{{ID: model}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(base.ID) })
				replace := func() {
					if change == "unchanged" {
						return
					}
					replacement := base.Clone()
					if change == "token" {
						replacement.Metadata["access_token"] = "synthetic-replacement-token"
					}
					if change == "account" {
						replacement.Metadata["account_id"] = "synthetic-replacement-account"
					}
					var current *Auth
					var err error
					if change == "registration" {
						manager.Remove(ctx, base.ID)
						current, err = manager.Register(ctx, replacement)
					} else {
						current, err = manager.Update(ctx, replacement)
					}
					if err != nil {
						t.Fatal(err)
					}
					now := time.Now()
					if !manager.UpdateCodexRoutingObservation(current, &CodexRoutingObservation{ObservedAt: now, Primary: &CodexRoutingWindow{ObservedAt: now, UsedPercent: 99}}) {
						t.Fatal("replacement quota rejected")
					}
				}
				headers := http.Header{"X-Codex-Primary-Used-Percent": []string{"10"}}
				finish := make(chan struct{})
				calls := 0
				executor := &customStreamMockExecutor{
					identifier: "codex",
					mockCustomErrorExecutor: mockCustomErrorExecutor{executeFn: func(execCtx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
						calls++
						replace()
						logging.SetResponseHeaders(execCtx, headers)
						return cliproxyexecutor.Response{Payload: []byte(`{"ok":true}`)}, nil
					}},
					streamFn: func(execCtx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
						calls++
						logging.SetResponseHeaders(execCtx, headers)
						chunks := make(chan cliproxyexecutor.StreamChunk, 1)
						chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("data: {}\n\n")}
						go func() { <-finish; close(chunks) }()
						return &cliproxyexecutor.StreamResult{Headers: headers, Chunks: chunks}, nil
					},
				}
				manager.RegisterExecutor(executor)
				req := cliproxyexecutor.Request{Model: model}
				if streaming {
					stream, err := manager.ExecuteStream(ctx, []string{"codex"}, req, cliproxyexecutor.Options{Stream: true})
					if err != nil {
						close(finish)
						t.Fatal(err)
					}
					replace()
					close(finish)
					for range stream.Chunks {
					}
				} else {
					if _, err := manager.Execute(ctx, []string{"codex"}, req, cliproxyexecutor.Options{}); err != nil {
						t.Fatal(err)
					}
				}
				current, _ := manager.GetByID(base.ID)
				if change == "unchanged" {
					if current.CodexRouting == nil || current.CodexRouting.Primary.UsedPercent != 10 {
						t.Fatal("current response observation rejected")
					}
				} else {
					if current.CodexRouting == nil || current.CodexRouting.Primary.UsedPercent != 99 {
						t.Fatal("old response overwrote replacement quota")
					}
					if known, blocked := codexQuotaReserveState(current, time.Now()); !known || !blocked {
						t.Fatal("old response reopened replacement account")
					}
					if len(current.Quota.Signals) != 0 {
						t.Fatal("old response populated replacement passive signals")
					}
					before := calls
					if _, err := manager.Execute(ctx, []string{"codex"}, req, cliproxyexecutor.Options{}); err == nil {
						t.Fatal("replacement accepted a new exhausted request")
					}
					if calls != before {
						t.Fatal("replacement sent an exhausted upstream request")
					}
				}
			})
		}
	}
}
func TestCodexReserveSynchronizesWarmModelShards(t *testing.T) {
	for _, strategy := range []struct {
		name     string
		selector Selector
	}{
		{"round-robin", &RoundRobinSelector{}},
		{"fill-first", &FillFirstSelector{}},
		{"weighted", &WeightedRoundRobinSelector{}},
	} {
		t.Run(strategy.name, func(t *testing.T) {
			manager := NewManager(nil, strategy.selector, nil)
			base := codexObservationAuth(t, manager)
			const modelA = "reserve-shard-a"
			const modelB = "reserve-shard-b"
			registry.GetGlobalRegistry().RegisterClient(base.ID, "codex", []*registry.ModelInfo{{ID: modelA}, {ID: modelB}})
			t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(base.ID) })
			used := "10"
			calls := 0
			manager.RegisterExecutor(&mockCustomErrorExecutor{identifier: "codex", executeFn: func(ctx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
				calls++
				logging.SetResponseHeaders(ctx, http.Header{"X-Codex-Primary-Used-Percent": []string{used}})
				return cliproxyexecutor.Response{Payload: []byte(`{"ok":true}`)}, nil
			}})
			execute := func(model string) error {
				_, err := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
				return err
			}
			if err := execute(modelB); err != nil {
				t.Fatal(err)
			}
			used = "95"
			if err := execute(modelA); err != nil {
				t.Fatal(err)
			}
			before := calls
			if err := execute(modelB); err == nil {
				t.Fatal("warm model shard ignored reserve cutoff")
			}
			if calls != before {
				t.Fatal("warm model shard sent an exhausted upstream request")
			}
			current, _ := manager.GetByID(base.ID)
			now := time.Now()
			if !manager.UpdateCodexRoutingObservation(current, &CodexRoutingObservation{ObservedAt: now, Primary: &CodexRoutingWindow{ObservedAt: now, UsedPercent: 10}}) {
				t.Fatal("recovery rejected")
			}
			used = "10"
			if err := execute(modelB); err != nil {
				t.Fatalf("recovered warm model shard stayed blocked: %v", err)
			}
			if calls != before+1 {
				t.Fatal("recovery did not reach upstream")
			}
		})
	}
}

func TestCodexReplacementClearsPassiveObservation(t *testing.T) {
	for _, credentialCooldown := range []bool{false, true} {
		name := "without-cooldown"
		if credentialCooldown {
			name = "with-cooldown"
		}
		t.Run(name, func(t *testing.T) {
			ctx := logging.WithFreshResponseHeadersHolder(context.Background())
			manager := NewManager(nil, nil, nil)
			base := codexObservationAuth(t, manager)
			logging.SetResponseHeaders(ctx, http.Header{
				"X-Codex-Primary-Used-Percent":        []string{"95"},
				"X-Codex-Plan-Type":                   []string{"pro"},
				"X-Codex-Primary-Window-Minutes":      []string{"10080"},
				"X-Codex-Primary-Reset-After-Seconds": []string{"3600"},
			})
			result := Result{AuthID: base.ID, Provider: "codex", Model: "replacement-model", Success: true}
			if credentialCooldown {
				result.Success = false
				result.CredentialScope = true
				result.Error = &Error{HTTPStatus: http.StatusTooManyRequests, Message: "synthetic quota limit"}
				retry := time.Hour
				result.RetryAfter = &retry
			}
			manager.MarkResult(ctx, result)
			snapshot, _ := manager.GetByID(base.ID)
			if known, blocked := codexQuotaReserveState(snapshot, time.Now()); !known || !blocked {
				t.Fatal("seed cutoff not observed")
			}
			same, err := manager.Update(context.Background(), snapshot.Clone())
			if err != nil || len(same.Quota.Signals) == 0 {
				t.Fatal("same-account reload lost passive observations")
			}
			replacement := same.Clone()
			replacement.Metadata["account_id"] = "synthetic-replacement-account"
			replacement.Metadata["access_token"] = "synthetic-replacement-token"
			current, err := manager.Update(context.Background(), replacement)
			if err != nil {
				t.Fatal(err)
			}
			if current.CodexRouting != nil || len(current.Quota.Signals) != 0 || !current.Quota.ObservedAt.IsZero() {
				t.Fatal("replacement inherited passive observation")
			}
			for _, state := range current.ModelStates {
				if state != nil && (len(state.Quota.Signals) != 0 || !state.Quota.ObservedAt.IsZero()) {
					t.Fatal("replacement inherited model observation")
				}
			}
			if known, blocked := codexQuotaReserveState(current, time.Now()); known || blocked {
				t.Fatal("replacement inherited reserve cutoff")
			}
			profile := SubscriptionRoutingProfile(current, time.Now(), time.Hour)
			if profile.Plan != "" || profile.WeeklyResetAt != "" {
				t.Fatal("replacement inherited plan or reset observation")
			}
			if credentialCooldown && (!current.Quota.Exceeded || current.Quota.Reason != same.Quota.Reason || !current.Quota.NextRecoverAt.Equal(same.Quota.NextRecoverAt)) {
				t.Fatal("replacement changed existing cooldown policy")
			}
		})
	}
}
