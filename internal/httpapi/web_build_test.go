package httpapi_test

import (
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/whg517/rustdesk-hub/internal/httpapi"
	"github.com/whg517/rustdesk-hub/internal/hub"
)

func TestValidateWebBuild(t *testing.T) {
	dist := fstest.MapFS{}
	write := func(name, content string) {
		dist[name] = &fstest.MapFile{Data: []byte(content)}
	}
	requireInvalid := func() {
		t.Helper()
		if err := httpapi.ValidateWebBuild(dist); err == nil {
			t.Fatal("expected the incomplete web build to be rejected")
		}
	}

	requireInvalid()
	const entry = `<script type="module" src="/assets/index.js"></script>`
	const style = `<link href="/assets/main.css" rel="stylesheet">`
	const preload = `<link href="/assets/runtime.js" rel="modulepreload">`
	write("index.html", entry+style+preload)
	write(".vite/manifest.json", `{
		"index.html": {"file": "assets/index.js", "css": ["assets/main.css"], "imports": ["runtime"], "dynamicImports": ["src/Settings.tsx"]},
		"runtime": {"file": "assets/runtime.js"},
		"src/Settings.tsx": {"file": "assets/settings.js"}
	}`)
	write("assets/index.js", "export {}")
	write("assets/runtime.js", "export {}")
	write("assets/main.css", "body { margin: 0; }")
	requireInvalid()
	write("assets/settings.js", "export {}")
	if err := httpapi.ValidateWebBuild(dist); err != nil {
		t.Fatal(err)
	}
	write("assets/old.css", "body { margin: 5px; }")
	write("assets/old.js", "export {}")
	write("index.html", entry+`<link href="/assets/old.css" rel="stylesheet">`+preload)
	requireInvalid()
	write("index.html", entry+style+`<link href="/assets/old.js" rel="modulepreload">`)
	requireInvalid()
	write("index.html", entry+preload)
	requireInvalid()
	write("index.html", `<script type="module" src="/assets/index.js"></script>`)
	write(".vite/manifest.json", `{"index.html":{"file":"assets/index.js","dynamicImports":["absent"]}}`)
	requireInvalid()
}

func TestWebRoutesServeBundledFiles(t *testing.T) {
	store, err := hub.Open(filepath.Join(t.TempDir(), "hub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.InitOwner(t.Context(), "owner", password); err != nil {
		t.Fatal(err)
	}

	dist := fstest.MapFS{
		"index.html":           {Data: []byte(`<script type="module" src="/assets/index-abcd.js"></script>`)},
		"assets/index-abcd.js": {Data: []byte("export {}")},
		".vite/manifest.json":  {Data: []byte(`{"index.html":{"file":"assets/index-abcd.js"}}`)},
	}
	web := client{handler: httpapi.New(store, httpapi.Config{PublicURL: origin, WebFS: dist})}
	web.call(t, "GET", "/healthz", "", 200)
	home := web.call(t, "GET", "/", "", 200)
	if home.Body.String() != string(dist["index.html"].Data) || home.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("expected the bundled index with cache disabled")
	}
	asset := web.call(t, "GET", "/assets/index-abcd.js", "", 200)
	if asset.Body.String() != "export {}" || asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatal("expected the bundled hashed asset with immutable caching")
	}
	missing := web.call(t, "GET", "/assets/index-old.js", "", 404)
	if missing.Header().Get("Cache-Control") != "" {
		t.Fatal("missing assets must not be marked immutable")
	}
	web.call(t, "GET", "/.vite/manifest.json", "", 404)
}
