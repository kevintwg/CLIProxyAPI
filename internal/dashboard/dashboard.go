package dashboard

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

//go:embed web
var files embed.FS

var hashedAssetName = regexp.MustCompile(`-[A-Za-z0-9_-]{8}\.[^.]+$`)

// Handler serves the embedded dashboard without falling back to HTML for missing files.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveAsset(w, r, files)
	})
}

func serveAsset(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	assetPath, ok := strings.CutPrefix(r.URL.Path, "/dashboard/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if assetPath == "" {
		assetPath = "index.html"
	}
	if !fs.ValidPath(assetPath) {
		http.NotFound(w, r)
		return
	}
	content, errRead := fs.ReadFile(assets, "web/"+assetPath)
	if errRead != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasPrefix(assetPath, "assets/") && path.Ext(assetPath) != ".html" && hashedAssetName.MatchString(path.Base(assetPath)) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if contentType := mime.TypeByExtension(path.Ext(assetPath)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, path.Base(assetPath), time.Time{}, bytes.NewReader(content))
}
