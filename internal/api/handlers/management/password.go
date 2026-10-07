package management

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

const (
	minManagementPasswordChars = 8
	// bcrypt only uses the first 72 bytes of a password.
	maxManagementPasswordBytes = 72
)

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword replaces the dashboard management password stored as a bcrypt hash.
// A MANAGEMENT_PASSWORD environment secret is unaffected and keeps working.
func (h *Handler) ChangePassword(c *gin.Context) {
	var body changePasswordRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil || h.cfg.RemoteManagement.SecretKey == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "No dashboard password is set in the config file, so it cannot be changed here."})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(h.cfg.RemoteManagement.SecretKey), []byte(body.CurrentPassword)) != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "Current password is incorrect."})
		return
	}
	if msg := validateNewManagementPassword(body.CurrentPassword, body.NewPassword); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}

	hashed, errHash := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if errHash != nil {
		log.WithError(errHash).Error("management: failed to hash new management password")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update password"})
		return
	}

	previousHash := h.cfg.RemoteManagement.SecretKey
	h.setSecretHash(string(hashed))
	if !h.persistLocked(c) {
		h.setSecretHash(previousHash)
		return
	}
	log.Info("management: dashboard password changed")
}

// validateNewManagementPassword returns a user-facing message when the new password is not acceptable.
func validateNewManagementPassword(current, next string) string {
	if strings.TrimSpace(next) == "" {
		return "New password cannot be blank."
	}
	if utf8.RuneCountInString(next) < minManagementPasswordChars {
		return "New password must be at least 8 characters."
	}
	if len(next) > maxManagementPasswordBytes {
		return "New password is too long. Use 72 characters or fewer."
	}
	if next == current {
		return "New password must be different from the current password."
	}
	return ""
}

// setSecretHash replaces the stored hash so concurrent AuthenticateManagementKey calls never read a partial write.
// Callers must hold h.mu.
func (h *Handler) setSecretHash(hash string) {
	h.secretMu.Lock()
	h.cfg.RemoteManagement.SecretKey = hash
	h.secretMu.Unlock()
}
