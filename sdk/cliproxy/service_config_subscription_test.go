package cliproxy

import (
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"testing"
	"time"
)

func TestSubscriptionFirstRoutingSelector(t *testing.T) {
	enabled := false
	cfg := &config.Config{}
	cfg.Routing.Strategy = "subscription-first"
	cfg.Routing.SubscriptionFirstMaxObservationAge = "2h"
	cfg.Routing.SubscriptionFirstPreferWeeklyReset = &enabled
	state := normalizedRoutingRuntimeState(cfg)
	selector, ok := newRoutingSelector(state).(*coreauth.SubscriptionFirstSelector)
	if !ok || selector.MaxObservationAge != 2*time.Hour || *selector.PreferWeeklyReset || selector.ReturnToPreferredTier {
		t.Fatal("subscription selector configuration not applied")
	}
	cfg.Routing.SubscriptionFirstReturnToPreferredTier = true
	returning := normalizedRoutingRuntimeState(cfg)
	if returning == state {
		t.Fatal("return-to-preferred-tier change would not rebuild the selector")
	}
	if selector, ok := newRoutingSelector(returning).(*coreauth.SubscriptionFirstSelector); !ok || !selector.ReturnToPreferredTier {
		t.Fatal("return-to-preferred-tier not applied")
	}
	cfg.Routing.SessionAffinity = true
	affinity, ok := newRoutingSelector(normalizedRoutingRuntimeState(cfg)).(*coreauth.SessionAffinitySelector)
	if !ok {
		t.Fatal("subscription strategy lost affinity wrapper")
	}
	affinity.Stop()
}
