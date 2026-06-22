package server

import "mihomo-st/internal/app"

type proxyHTTPRequestDTO struct {
	URL            *string           `json:"url"`
	Method         *string           `json:"method"`
	Headers        map[string]string `json:"headers"`
	Timeout        *int              `json:"timeout"`
	FollowRedirect *bool             `json:"follow-redirect"`
	Body           *string           `json:"body"`
}

func (r proxyHTTPRequestDTO) Command() (app.ProxyRequestCommand, error) {
	rawURL, err := requiredString("url", r.URL)
	if err != nil {
		return app.ProxyRequestCommand{}, err
	}
	method, err := optionalNonEmptyString("method", r.Method)
	if err != nil {
		return app.ProxyRequestCommand{}, err
	}
	timeout, err := optionalPositiveInt("timeout", r.Timeout)
	if err != nil {
		return app.ProxyRequestCommand{}, err
	}
	return app.ProxyRequestCommand{
		URL:            rawURL,
		Method:         method,
		Headers:        cloneHeaders(r.Headers),
		Timeout:        timeout,
		FollowRedirect: r.FollowRedirect,
		Body:           r.Body,
	}, nil
}
