package management

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const (
	claudeFiveHourUtilization = "Anthropic-Ratelimit-Unified-5h-Utilization"
	claudeFiveHourReset       = "Anthropic-Ratelimit-Unified-5h-Reset"
	claudeSevenDayUtilization = "Anthropic-Ratelimit-Unified-7d-Utilization"
	claudeSevenDayReset       = "Anthropic-Ratelimit-Unified-7d-Reset"
)

// claudeUsageResponse converts the latest Claude response headers into the
// usage shape used by the management UI. It is a passive observation: no
// upstream request is made here.
func claudeUsageResponse(quota coreauth.QuotaState) (gin.H, bool) {
	if quota.ObservedAt.IsZero() {
		return nil, false
	}

	response := gin.H{"observed_at": quota.ObservedAt}
	if window, ok := claudeUsageWindow(quota, claudeFiveHourUtilization, claudeFiveHourReset, 300); ok {
		response["primary"] = window
	}
	if window, ok := claudeUsageWindow(quota, claudeSevenDayUtilization, claudeSevenDayReset, 10080); ok {
		response["secondary"] = window
	}
	return response, len(response) > 1
}

func claudeUsageWindow(quota coreauth.QuotaState, utilizationKey, resetKey string, windowMinutes int) (gin.H, bool) {
	utilization, ok := claudeQuotaFloat(quota.Signals, utilizationKey)
	if !ok {
		return nil, false
	}
	reset, ok := claudeQuotaTime(quota.Signals, resetKey)
	if !ok {
		return nil, false
	}
	usedPercent := utilization * 100
	remainingPercent := (1 - utilization) * 100
	if usedPercent < 0 {
		usedPercent = 0
	}
	if remainingPercent < 0 {
		remainingPercent = 0
	}
	return gin.H{
		"used_percent":      usedPercent,
		"remaining_percent": remainingPercent,
		"window_minutes":    windowMinutes,
		"resets_at":         reset,
		"observed_at":       quota.ObservedAt,
	}, true
}

func claudeQuotaFloat(signals map[string]string, key string) (float64, bool) {
	raw, ok := claudeQuotaSignal(signals, key)
	if !ok {
		return 0, false
	}
	value, errParse := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if errParse != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false
	}
	return value, true
}

func claudeQuotaTime(signals map[string]string, key string) (time.Time, bool) {
	raw, ok := claudeQuotaSignal(signals, key)
	if !ok {
		return time.Time{}, false
	}
	raw = strings.TrimSpace(raw)
	if unix, errParse := strconv.ParseInt(raw, 10, 64); errParse == nil && unix > 0 {
		return time.Unix(unix, 0).UTC(), true
	}
	parsed, errParse := time.Parse(time.RFC3339, raw)
	if errParse != nil || parsed.IsZero() {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func claudeQuotaSignal(signals map[string]string, key string) (string, bool) {
	for name, value := range signals {
		if strings.EqualFold(name, key) {
			return value, true
		}
	}
	return "", false
}
