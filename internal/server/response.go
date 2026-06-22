package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"mihomo-st/internal/app"
	"mihomo-st/internal/proxyconfig"
)

func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func writeBadRequest(w http.ResponseWriter, err error) {
	writeError(w, http.StatusBadRequest, err.Error())
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeNotFound(w http.ResponseWriter, message string) {
	writeError(w, http.StatusNotFound, message)
}

func writeAppError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrProxyNotFound):
		writeNotFound(w, err.Error())
	case errors.Is(err, proxyconfig.ErrRemoteBodyTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, proxyconfig.ErrRemoteStatus):
		writeError(w, http.StatusBadGateway, err.Error())
	case errors.Is(err, proxyconfig.ErrRemoteTimeout):
		writeError(w, http.StatusGatewayTimeout, err.Error())
	case errors.Is(err, app.ErrProxyRequestTimeout):
		writeError(w, http.StatusGatewayTimeout, err.Error())
	default:
		writeBadRequest(w, err)
	}
}

func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, errorResponse{
		Error: apiError{
			Code:    code,
			Status:  errorStatus(code),
			Message: message,
		},
	})
}

func errorStatus(code int) string {
	status := http.StatusText(code)
	if status == "" {
		status = "error"
	}
	return strings.ToUpper(strings.ReplaceAll(status, " ", "_"))
}
