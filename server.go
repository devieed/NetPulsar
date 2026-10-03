package main

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"netpulsar/internal/skin"
)

type assetHandler struct {
	frontend fs.FS
	skins    *skin.Catalog
	current  func() string
}

func (h assetHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := path.Clean(r.URL.Path)
	w.Header().Set("Cache-Control", "no-cache")
	switch {
	case name == "/" || name == "/index.html":
		h.file(w, r, h.frontend, "index.html")
	case name == "/bridge.js" || name == "/overlay.css":
		h.file(w, r, h.frontend, strings.TrimPrefix(name, "/"))
	case strings.HasPrefix(name, "/skin/"):
		h.skinFile(w, r, strings.TrimPrefix(name, "/skin/"))
	default:
		http.NotFound(w, r)
	}
}

func (h assetHandler) skinFile(w http.ResponseWriter, r *http.Request, name string) {
	id := h.skins.Resolve(h.current()).ID
	raw, err := h.skins.Read(id, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType(name))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (h assetHandler) file(w http.ResponseWriter, r *http.Request, tree fs.FS, name string) {
	raw, err := fs.ReadFile(tree, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType(name))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func contentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".json":
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
