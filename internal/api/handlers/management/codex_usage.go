package management

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const codexUsageBodyLimit = 1 << 20

// FetchCredentialUsage manually refreshes current Codex usage and reset-credit observations.
func (h *Handler) FetchCredentialUsage(c *gin.Context) {
	var body credentialQuotaRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil || body.resolveAuthIndex() == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
		return
	}
	auth := h.authByIndex(body.resolveAuthIndex())
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth not found"})
		return
	}
	if !strings.EqualFold(auth.Provider, "codex") || auth.AuthKind() != coreauth.AuthKindOAuth {
		if strings.EqualFold(auth.Provider, "claude") {
			h.mu.Lock()
			host := h.pluginHost
			h.mu.Unlock()
			if host == nil || !host.HasQuotaProvider(auth.Provider) {
				if usage, ok := claudeUsageResponse(auth.Quota); ok {
					c.JSON(http.StatusOK, usage)
					return
				}
				c.JSON(http.StatusNotImplemented, gin.H{"error": "no Claude usage observation available; send a Claude request first"})
				return
			}
		}
		h.fetchCredentialQuota(c, auth, body)
		return
	}
	observation, errFetch := h.fetchCodexUsage(c, auth)
	if errFetch != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": errFetch.Error()})
		return
	}
	c.JSON(http.StatusOK, codexUsageResponse(observation))
}

// RedeemCredentialReset redeems one provider reset credit after the caller's confirmation.
func (h *Handler) RedeemCredentialReset(c *gin.Context) {
	var body credentialQuotaRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil || body.resolveAuthIndex() == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
		return
	}
	auth := h.authByIndex(body.resolveAuthIndex())
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth not found"})
		return
	}
	if !strings.EqualFold(auth.Provider, "codex") || auth.AuthKind() != coreauth.AuthKindOAuth {
		h.resetCredentialQuota(c, auth, body)
		return
	}
	creditsBody, errCredits := h.codexUpstreamJSON(c, auth, http.MethodGet, coreauth.CodexResetCreditsURL)
	if errCredits != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("could not verify reset credits: %v", errCredits)})
		return
	}
	credits, errParseCredits := coreauth.ParseCodexResetCreditObservation(creditsBody, time.Now().UTC())
	if errParseCredits != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": errParseCredits.Error()})
		return
	}
	if credits.BankedResetCount == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "no reset credits are available"})
		return
	}

	redeemBody, errMarshal := json.Marshal(map[string]string{"redeem_request_id": uuid.NewString()})
	if errMarshal != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not prepare reset redemption"})
		return
	}
	request, errRequest := h.authManager.NewHttpRequest(c.Request.Context(), auth, http.MethodPost, coreauth.CodexResetCreditConsumeURL, redeemBody, http.Header{"Accept": {"application/json"}, "Content-Type": {"application/json"}})
	if errRequest != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("could not prepare reset redemption: %v", errRequest)})
		return
	}
	if accountID := coreauth.CodexAccountID(auth); accountID != "" {
		request.Header.Set("ChatGPT-Account-ID", accountID)
	}
	response, errDo := h.authManager.HttpRequest(c.Request.Context(), auth, request)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if errDo != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("reset redemption failed: %v", errDo)})
		return
	}
	if response == nil || response.Body == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "reset redemption returned no response"})
		return
	}
	responseBody, errRead := io.ReadAll(io.LimitReader(response.Body, codexUsageBodyLimit+1))
	if errRead != nil || len(responseBody) > codexUsageBodyLimit {
		c.JSON(http.StatusBadGateway, gin.H{"error": "reset redemption returned an unreadable response"})
		return
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("reset redemption returned status %d", response.StatusCode)})
		return
	}
	var result struct {
		Code string `json:"code"`
	}
	if len(responseBody) == 0 || json.Unmarshal(responseBody, &result) != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "reset redemption returned invalid JSON"})
		return
	}
	code := strings.ToLower(strings.TrimSpace(result.Code))
	if code == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "reset redemption returned no completion code"})
		return
	}
	if code != "reset" && code != "already_redeemed" {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("reset redemption was not completed: %s", result.Code)})
		return
	}
	observation, errFetch := h.fetchCodexUsage(c, auth)
	if errFetch != nil {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Reset redeemed, but the updated usage could not be fetched."})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "usage": codexUsageResponse(observation)})
}

func (h *Handler) fetchCodexUsage(c *gin.Context, auth *coreauth.Auth) (*coreauth.CodexRoutingObservation, error) {
	usageBody, errUsage := h.codexUpstreamJSON(c, auth, http.MethodGet, coreauth.CodexUsageURL)
	if errUsage != nil {
		return nil, fmt.Errorf("usage fetch failed: %w", errUsage)
	}
	now := time.Now().UTC()
	usage, errParse := coreauth.ParseCodexUsageObservation(usageBody, now)
	if errParse != nil {
		return nil, errParse
	}
	creditsBody, errCredits := h.codexUpstreamJSON(c, auth, http.MethodGet, coreauth.CodexResetCreditsURL)
	if errCredits == nil {
		credits, errParseCredits := coreauth.ParseCodexResetCreditObservation(creditsBody, now)
		if errParseCredits == nil {
			usage.BankedResetObservedAt = credits.BankedResetObservedAt
			usage.BankedResetExpiresAt = credits.BankedResetExpiresAt
			usage.BankedResetCount = credits.BankedResetCount
		}
	}
	h.authManager.UpdateCodexRoutingObservation(auth, usage)
	return usage, nil
}

func (h *Handler) codexUpstreamJSON(c *gin.Context, auth *coreauth.Auth, method, target string) ([]byte, error) {
	request, errRequest := h.authManager.NewHttpRequest(c.Request.Context(), auth, method, target, nil, http.Header{"Accept": {"application/json"}})
	if errRequest != nil {
		return nil, errRequest
	}
	if accountID := coreauth.CodexAccountID(auth); accountID != "" {
		request.Header.Set("ChatGPT-Account-ID", accountID)
	}
	response, errDo := h.authManager.HttpRequest(c.Request.Context(), auth, request)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if errDo != nil {
		return nil, errDo
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("upstream returned no response")
	}
	body, errRead := io.ReadAll(io.LimitReader(response.Body, codexUsageBodyLimit+1))
	if errRead != nil || len(body) > codexUsageBodyLimit {
		return nil, fmt.Errorf("upstream returned an unreadable response")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("upstream returned status %d", response.StatusCode)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("upstream returned invalid JSON")
	}
	return body, nil
}

func codexUsageResponse(observation *coreauth.CodexRoutingObservation) gin.H {
	response := gin.H{}
	if observation == nil {
		return response
	}
	response["observed_at"] = observation.ObservedAt
	response["plan"] = observation.Plan
	if !observation.BankedResetObservedAt.IsZero() {
		response["banked_reset_count"] = observation.BankedResetCount
		response["banked_reset_observed_at"] = observation.BankedResetObservedAt
	}
	if !observation.BankedResetExpiresAt.IsZero() {
		response["banked_reset_expires_at"] = observation.BankedResetExpiresAt
	}
	response["primary"] = codexUsageWindowResponse(observation.Primary)
	response["secondary"] = codexUsageWindowResponse(observation.Secondary)
	return response
}

func codexUsageWindowResponse(window *coreauth.CodexRoutingWindow) gin.H {
	if window == nil {
		return nil
	}
	return gin.H{
		"used_percent":      window.UsedPercent,
		"remaining_percent": 100 - window.UsedPercent,
		"window_minutes":    window.WindowMinutes,
		"resets_at":         window.ResetsAt,
		"observed_at":       window.ObservedAt,
	}
}
