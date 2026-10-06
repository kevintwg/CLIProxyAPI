package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestServeAsset(t *testing.T) {
	assets := fstest.MapFS{
		"web/index.html":                {Data: []byte("<!doctype html><title>Dashboard</title>")},
		"web/assets/index-AbCd1234.js":  {Data: []byte("console.log('dashboard')")},
		"web/assets/index-EfGh5678.css": {Data: []byte("body { color: blue; }")},
		"web/assets/plain.js":           {Data: []byte("console.log('plain')")},
		"web/assets/help-AbCd1234.html": {Data: []byte("<title>Help</title>")},
		"web/assets/logo.svg":           {Data: []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"/>")},
	}
	for _, tc := range []struct {
		name, method, url, contentType, cache string
		status                                int
	}{
		{"HTML", "GET", "/dashboard/", "text/html", "no-store", 200},
		{"HTML HEAD", "HEAD", "/dashboard/", "text/html", "no-store", 200},
		{"JavaScript", "GET", "/dashboard/assets/index-AbCd1234.js", "javascript", "public, max-age=31536000, immutable", 200},
		{"CSS", "GET", "/dashboard/assets/index-EfGh5678.css", "text/css", "public, max-age=31536000, immutable", 200},
		{"plain asset", "GET", "/dashboard/assets/plain.js", "javascript", "no-store", 200},
		{"HTML hash", "GET", "/dashboard/assets/help-AbCd1234.html", "text/html", "no-store", 200},
		{"SVG", "GET", "/dashboard/assets/logo.svg", "image/svg+xml", "no-store", 200},
		{"missing asset", "GET", "/dashboard/assets/missing.js", "", "", 404},
		{"unknown page", "GET", "/dashboard/missing", "", "", 404},
		{"directory", "GET", "/dashboard/assets/", "", "", 404},
		{"traversal", "GET", "/dashboard/../index.html", "", "", 404},
		{"outside dashboard", "GET", "/index.html", "", "", 404},
		{"POST", "POST", "/dashboard/", "", "", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			serveAsset(rr, httptest.NewRequest(tc.method, tc.url, nil), assets)
			if rr.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rr.Code, tc.status, rr.Body.String())
			}
			if tc.contentType != "" && !strings.Contains(rr.Header().Get("Content-Type"), tc.contentType) {
				t.Errorf("Content-Type=%q want %q", rr.Header().Get("Content-Type"), tc.contentType)
			}
			if got := rr.Header().Get("Cache-Control"); got != tc.cache {
				t.Errorf("Cache-Control=%q want=%q", got, tc.cache)
			}
			if tc.method == http.MethodHead && rr.Body.Len() != 0 {
				t.Error("HEAD returned a body")
			}
			if tc.status == http.StatusMethodNotAllowed && rr.Header().Get("Allow") != "GET, HEAD" {
				t.Error("missing allowed methods")
			}
		})
	}
}
