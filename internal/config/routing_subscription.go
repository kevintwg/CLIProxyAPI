package config

import (
	"fmt"
	"strings"
	"time"
)

func (r RoutingConfig) SubscriptionFirstObservationAge() time.Duration {
	age, err := time.ParseDuration(strings.TrimSpace(r.SubscriptionFirstMaxObservationAge))
	if err != nil || age <= 0 {
		return 30 * time.Minute
	}
	return age
}

func (r RoutingConfig) SubscriptionFirstWeeklyResetEnabled() bool {
	return r.SubscriptionFirstPreferWeeklyReset == nil || *r.SubscriptionFirstPreferWeeklyReset
}

func (r RoutingConfig) Validate() error {

	if raw := strings.TrimSpace(r.SubscriptionFirstMaxObservationAge); raw != "" {
		age, err := time.ParseDuration(raw)
		if err != nil || age <= 0 {
			return fmt.Errorf("subscription-first-max-observation-age must be a positive duration")
		}
	}
	return nil
}

func (r RoutingConfig) ValidateStrategy() error {
	switch strings.ToLower(strings.TrimSpace(r.Strategy)) {
	case "", "round-robin", "roundrobin", "rr", "weighted-round-robin", "weightedroundrobin", "wrr", "fill-first", "fillfirst", "ff", "subscription-first":
	default:
		return fmt.Errorf("unsupported routing strategy")
	}
	return nil
}
