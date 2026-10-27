// Package webui serves the browser-based web companion (agent control,
// status, settings) as a static bundle embedded in the agent binary — one
// binary, one port, no separate hosting step. The bundle is a Vite+React+
// TypeScript SPA built from web/ (`npm run build` in that directory,
// outDir "../dist") — rebuild that before `go build` if anything under
// web/src changes.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist
var distFS embed.FS

// Handler serves the web companion's static assets rooted at "/". The
// embed.FS gives every file a zero mtime, so http.FileServer never emits
// Last-Modified/ETag — with no cache validator, browsers fall back to
// heuristic caching and can serve a stale copy after a redeploy. Explicit
// no-store avoids that during active development of dist/index.html.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // dist/ is embedded at build time — this can't fail at runtime
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		// Defense-in-depth for the companion (PR-13). The generated index
		// loads one same-origin external module and the SPA has no inline
		// scripts, so script-src does not allow unsafe-inline. React style
		// props produce inline style attributes, so style-src retains it. The
		// bundle has no CDN or cross-origin asset dependency.
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-src 'none'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'; object-src 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// SPA fallback: the React app owns client-side routing (react-router,
		// history mode — /login, /pair, /app/files, ...), so any path that
		// isn't a real embedded file (a deep link, or a browser refresh on a
		// non-root route) must still serve index.html rather than 404 —
		// react-router then renders the matching route from the URL.
		if r.URL.Path != "/" {
			if _, err := fs.Stat(sub, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
