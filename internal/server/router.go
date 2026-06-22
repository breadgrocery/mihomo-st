package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"mihomo-st/internal/app"
)

func New(runtime *app.Runtime) http.Handler {
	s := &Server{app: runtime}
	router := chi.NewRouter()
	router.NotFound(s.handleNotFound)
	router.MethodNotAllowed(s.handleMethodNotAllowed)
	router.Get("/version", s.handleVersion)
	router.Post("/digest", s.handleDigest)
	router.Get("/config", s.handleConfig)
	router.Patch("/config", s.handleConfig)
	router.Get("/proxies", s.handleProxies)
	router.Post("/proxies", s.handleProxies)
	router.Post("/proxies/delay", s.handleProxyDelayCollection)
	router.Post("/proxies/download", s.handleProxyDownloadCollection)
	router.Post("/proxies/{digest}/proxy", s.handleProxyRequest)
	router.Post("/proxies/{digest}/delay", s.handleProxyDelay)
	router.Post("/proxies/{digest}/download", s.handleProxyDownload)
	return router
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeNotFound(w, "not found")
}

func (s *Server) handleMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeMethodNotAllowed(w)
}
