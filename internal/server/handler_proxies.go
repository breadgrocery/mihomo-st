package server

import (
	"net/http"
)

func (s *Server) handleProxies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		result, err := s.app.ListProxies()
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, proxyListResponseFromResult(result))
	case http.MethodPost:
		var req importProxiesRequestDTO
		if !readRequiredJSON(w, r, &req) {
			return
		}
		cmd, err := req.Command()
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		result, err := s.app.ImportProxies(r.Context(), cmd)
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, importProxiesResponseFromResult(result))
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleProxyExport(w http.ResponseWriter, r *http.Request) {
	result, err := s.app.ExportProxies()
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, proxyExportResponseFromResult(result))
}
