package server

import (
	"net/http"

	"mihomo-st/internal/digest"
)

var digestMapping = digest.Sum

func (s *Server) handleDigest(w http.ResponseWriter, r *http.Request) {
	var mapping map[string]any
	if !readRequiredJSON(w, r, &mapping) {
		return
	}
	if mapping == nil {
		writeError(w, http.StatusBadRequest, "request body must be a JSON object")
		return
	}
	sum, err := digestMapping(mapping)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	writeJSON(w, http.StatusOK, digestResponseDTO{Digest: sum})
}
