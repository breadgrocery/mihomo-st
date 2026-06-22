package server

import (
	"encoding/json"
	"net/http"

	"mihomo-st/internal/config"
)

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, config.ToAPI(s.app.Config()))
	case http.MethodPatch:
		var values map[string]json.RawMessage
		if !readRequiredJSON(w, r, &values) {
			return
		}
		if values == nil {
			writeError(w, http.StatusBadRequest, "request body must be a JSON object")
			return
		}
		next, err := s.app.PatchConfig(values)
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		writeJSON(w, http.StatusOK, config.ToAPI(next))
	default:
		writeMethodNotAllowed(w)
	}
}
