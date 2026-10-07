package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"golang.org/x/crypto/bcrypt"
)

const testCurrentPassword = "old-password-1"

func newPasswordTestHandler(t *testing.T) *Handler {
	t.Helper()
	hashed, errHash := bcrypt.GenerateFromPassword([]byte(testCurrentPassword), bcrypt.MinCost)
	if errHash != nil {
		t.Fatalf("hash current password: %v", errHash)
	}
	cfg := &config.Config{}
	cfg.RemoteManagement.SecretKey = string(hashed)
	return &Handler{
		cfg:            cfg,
		configFilePath: writeTestConfigFile(t),
		failedAttempts: make(map[string]*attemptInfo),
	}
}

func putPassword(t *testing.T, h *Handler, current, next string) *httptest.ResponseRecorder {
	t.Helper()
	payload, errMarshal := json.Marshal(changePasswordRequest{CurrentPassword: current, NewPassword: next})
	if errMarshal != nil {
		t.Fatalf("marshal body: %v", errMarshal)
	}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v8/management/password", strings.NewReader(string(payload)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.ChangePassword(ctx)
	return rec
}

func TestChangePasswordReplacesStoredHash(t *testing.T) {
	h := newPasswordTestHandler(t)
	const newPassword = "brand-new-password"

	rec := putPassword(t, h, testCurrentPassword, newPassword)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	if allowed, status, msg := h.AuthenticateManagementKey("127.0.0.1", true, newPassword); !allowed {
		t.Fatalf("new password rejected: status=%d msg=%q", status, msg)
	}
	if allowed, _, _ := h.AuthenticateManagementKey("127.0.0.1", true, testCurrentPassword); allowed {
		t.Fatal("old password still accepted")
	}

	saved, errRead := os.ReadFile(h.configFilePath)
	if errRead != nil {
		t.Fatalf("read saved config: %v", errRead)
	}
	if strings.Contains(string(saved), newPassword) {
		t.Fatalf("config file contains plaintext password:\n%s", saved)
	}
	if !strings.Contains(string(saved), h.cfg.RemoteManagement.SecretKey) {
		t.Fatalf("config file does not contain the new bcrypt hash:\n%s", saved)
	}
	if bcrypt.CompareHashAndPassword([]byte(h.cfg.RemoteManagement.SecretKey), []byte(newPassword)) != nil {
		t.Fatal("stored hash does not match the new password")
	}
}

func TestChangePasswordRejectsWrongCurrentWithoutCountingFailedLogin(t *testing.T) {
	h := newPasswordTestHandler(t)
	before := h.cfg.RemoteManagement.SecretKey

	rec := putPassword(t, h, "not-the-password", "brand-new-password")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Current password is incorrect.") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if h.cfg.RemoteManagement.SecretKey != before {
		t.Fatal("hash changed after a wrong current password")
	}
	if len(h.failedAttempts) != 0 {
		t.Fatalf("failed login attempts recorded: %d", len(h.failedAttempts))
	}
}

func TestChangePasswordValidatesNewPassword(t *testing.T) {
	tests := []struct {
		name string
		next string
	}{
		{name: "blank", next: "          "},
		{name: "too short", next: "short7!"},
		{name: "too long", next: strings.Repeat("a", maxManagementPasswordBytes+1)},
		{name: "same as current", next: testCurrentPassword},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newPasswordTestHandler(t)
			before := h.cfg.RemoteManagement.SecretKey

			rec := putPassword(t, h, testCurrentPassword, tc.next)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if h.cfg.RemoteManagement.SecretKey != before {
				t.Fatal("hash changed after an invalid new password")
			}
		})
	}
}

func TestChangePasswordRequiresConfiguredHash(t *testing.T) {
	h := newPasswordTestHandler(t)
	h.cfg.RemoteManagement.SecretKey = ""

	rec := putPassword(t, h, testCurrentPassword, "brand-new-password")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}
