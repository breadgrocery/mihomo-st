package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleProxyDelayCollection(w http.ResponseWriter, r *http.Request) {
	var req delayCollectionRequestDTO
	if !readOptionalJSON(w, r, &req) {
		return
	}
	cmd, err := req.Command()
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	resp, err := s.app.DelayAll(r.Context(), cmd)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, delayCollectionResponseFromResult(resp))
}

func (s *Server) handleProxyDownloadCollection(w http.ResponseWriter, r *http.Request) {
	var req downloadCollectionRequestDTO
	if !readOptionalJSON(w, r, &req) {
		return
	}
	cmd, err := req.Command()
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	resp, err := s.app.DownloadAll(r.Context(), cmd)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, downloadCollectionResponseFromResult(resp))
}

func (s *Server) handleProxyDelay(w http.ResponseWriter, r *http.Request) {
	var req delayRequestDTO
	if !readOptionalJSON(w, r, &req) {
		return
	}
	cmd, err := req.Command()
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	resp, err := s.app.Delay(r.Context(), chi.URLParam(r, "digest"), cmd)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, delayResponseFromResult(resp))
}

func (s *Server) handleProxyDownload(w http.ResponseWriter, r *http.Request) {
	var req downloadRequestDTO
	if !readOptionalJSON(w, r, &req) {
		return
	}
	cmd, err := req.Command()
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	resp, err := s.app.Download(r.Context(), chi.URLParam(r, "digest"), cmd)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, downloadResponseFromResult(resp))
}
