package server

import "mihomo-st/internal/app"

type Server struct {
	app *app.Runtime
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    int    `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
