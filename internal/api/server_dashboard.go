package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/dashboard"
)

func (s *Server) registerDashboardRoutes() {
	assets := dashboard.Handler()
	serve := func(c *gin.Context) {
		cfg := s.cfg
		if cfg == nil || cfg.Home.Enabled || cfg.RemoteManagement.DisableControlPanel {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Header("Allow", "GET, HEAD")
			c.AbortWithStatus(http.StatusMethodNotAllowed)
			return
		}
		if c.Request.URL.Path == "/dashboard" {
			c.Header("Cache-Control", "no-store")
			c.Redirect(http.StatusMovedPermanently, "/dashboard/")
			return
		}
		assets.ServeHTTP(c.Writer, c.Request)
	}
	s.engine.Any("/dashboard", serve)
	s.engine.Any("/dashboard/*path", serve)
}
