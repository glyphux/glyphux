// Package adminui embeds the compiled admin shell (admin-ui/, React + TS +
// Vite + Tailwind per PRD §5.6 Surface 2) into the glyphuxd binary via
// go:embed, so the daemon ships as a single static binary with no Node
// runtime required in production. The embedded tree is admin-ui's `npm run
// build` output — see admin-ui/vite.config.ts, which builds straight into
// this package's dist/ subdirectory.
package adminui

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed all:dist
var embedded embed.FS

// Assets is the compiled admin SPA's static files, rooted so callers see
// index.html and assets/ directly rather than under a "dist/" prefix.
var Assets = mustSub(embedded, "dist")

func mustSub(fsys embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		// dist/ is embedded at compile time (admin-ui/vite.config.ts builds
		// directly into it) — a missing root here means the frontend was
		// never built, which is a build-time setup problem, not a request
		// glyphuxd can recover from at runtime.
		panic("internal/adminui: dist/ not embedded — run `npm run build` in admin-ui/ first: " + err.Error())
	}
	return sub
}

// Handler serves the admin SPA: real static files (index.html, assets/…) at
// their actual path, and index.html for everything else, so client-side
// routes (e.g. /content-types, /content/post/abc) resolve on a hard
// navigation or reload — the standard SPA-behind-a-server pattern. Callers
// mount it under whatever base path the SPA is served from and strip that
// prefix first (server.go strips "/admin"); this handler itself is
// prefix-agnostic.
func Handler() http.Handler {
	fileServer := http.FileServerFS(Assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			clean = "index.html"
		}
		info, err := fs.Stat(Assets, clean)
		if err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(clean, "assets/") {
			// A path under assets/ is always a real build artifact request
			// (a content-hashed JS/CSS chunk, a font, ...), never a
			// client-side route. Falling back to index.html here would hand
			// the browser an HTML document where it expected a JS module —
			// "Unexpected token '<'" — masking a genuine missing-file bug
			// (e.g. a stale cached index.html after a redeploy) as if the
			// asset had loaded. A real 404 lets the browser and any error
			// reporting see the actual failure.
			http.NotFound(w, r)
			return
		}
		// Not a real asset and not under assets/ — hand it to index.html so
		// client-side routing can take over. Served directly via
		// ServeContent (not by rewriting the request and delegating to
		// http.FileServer) because FileServer treats any request whose
		// resolved file is literally named "index.html" as a directory
		// index and 301-redirects it to a trailing slash — exactly wrong
		// for a deep SPA route like /content-types.
		serveIndex(w, r)
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	f, err := Assets.Open("index.html")
	if err != nil {
		http.Error(w, "admin UI not available", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "admin UI not available", http.StatusInternalServerError)
		return
	}
	// index.html is never safe to cache — it references content-hashed
	// asset filenames that change on every rebuild, so a stale cached
	// index.html would keep pointing at assets that no longer exist.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, rs)
}
