package server

import (
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleProxyRequest(w http.ResponseWriter, r *http.Request) {
	var req proxyHTTPRequestDTO
	if !readRequiredJSON(w, r, &req) {
		return
	}
	cmd, err := req.Command()
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	resp, err := s.app.ProxyHTTP(r.Context(), chi.URLParam(r, "digest"), cmd)
	if err != nil {
		writeAppError(w, err)
		return
	}
	defer resp.Body.Close()

	copyUpstreamHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func copyUpstreamHeaders(dst, src http.Header) {
	blocked := map[string]struct{}{
		"connection":          {},
		"keep-alive":          {},
		"proxy-authenticate":  {},
		"proxy-authorization": {},
		"te":                  {},
		"trailer":             {},
		"transfer-encoding":   {},
		"upgrade":             {},
	}
	for _, value := range src.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			token = strings.TrimSpace(token)
			if token != "" {
				blocked[strings.ToLower(token)] = struct{}{}
			}
		}
	}
	for name, values := range src {
		if _, ok := blocked[strings.ToLower(name)]; ok {
			continue
		}
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}
