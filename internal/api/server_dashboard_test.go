package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func dashboardTestServer(cfg *config.Config) *Server {
	s := &Server{cfg: cfg, engine: gin.New()}
	s.engine.Use(s.homeHeartbeatMiddleware())
	s.registerDashboardRoutes()
	return s
}

func TestDashboardEmbeddedHTMLAndAssets(t *testing.T) {
	s := dashboardTestServer(&config.Config{})
	request := func(method, url string, want int) *httptest.ResponseRecorder {
		t.Helper()
		rr := httptest.NewRecorder()
		s.engine.ServeHTTP(rr, httptest.NewRequest(method, url, nil))
		if rr.Code != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, url, rr.Code, want, rr.Body.String())
		}
		return rr
	}
	rr := request(http.MethodGet, "/dashboard", http.StatusMovedPermanently)
	if rr.Header().Get("Location") != "/dashboard/" {
		t.Fatalf("unexpected redirect: %q", rr.Header().Get("Location"))
	}
	rr = request(http.MethodGet, "/dashboard/", http.StatusOK)
	if !strings.Contains(strings.ToLower(rr.Body.String()), "<!doctype html>") || !strings.Contains(rr.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("dashboard did not serve HTML: %s", rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Error("HTML must not be cached")
	}
	asset := regexp.MustCompile(`(?:src|href)="(/dashboard/assets/[^"?]+)"`).FindStringSubmatch(rr.Body.String())
	if len(asset) != 2 {
		t.Fatal("built dashboard HTML has no asset under /dashboard/assets/")
	}
	rr = request(http.MethodGet, asset[1], http.StatusOK)
	if rr.Body.Len() == 0 || !strings.Contains(rr.Header().Get("Cache-Control"), "immutable") {
		t.Error("built asset must have content and immutable caching")
	}
	if rr.Header().Get("Content-Type") == "" || strings.Contains(rr.Header().Get("Content-Type"), "text/html") {
		t.Error("built asset has an invalid content type")
	}
	for _, url := range []string{"/dashboard/", asset[1]} {
		if head := request(http.MethodHead, url, http.StatusOK); head.Body.Len() != 0 {
			t.Errorf("HEAD %s returned a body", url)
		}
	}
	request(http.MethodGet, "/dashboard/assets/missing.js", http.StatusNotFound)
	request(http.MethodGet, "/dashboard/missing-page", http.StatusNotFound)
	for _, url := range []string{"/dashboard", "/dashboard/", asset[1]} {
		rr = request(http.MethodPost, url, http.StatusMethodNotAllowed)
		if rr.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("POST %s missing Allow header", url)
		}
	}
}

func TestDashboardAvailabilityPolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *config.Config
	}{
		{"missing configuration", nil},
		{"home enabled", &config.Config{Home: config.HomeConfig{Enabled: true}}},
		{"panel disabled", &config.Config{RemoteManagement: config.RemoteManagement{DisableControlPanel: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := dashboardTestServer(tc.cfg)
			for _, url := range []string{"/dashboard", "/dashboard/", "/dashboard/assets/missing.js"} {
				rr := httptest.NewRecorder()
				s.engine.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, url, nil))
				if rr.Code != http.StatusNotFound {
					t.Errorf("%s: status=%d want=404", url, rr.Code)
				}
			}
		})
	}
}
