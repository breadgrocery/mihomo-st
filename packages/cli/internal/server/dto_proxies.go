package server

import "mihomo-st/internal/app"

type importProxiesRequestDTO struct {
	Type           *string              `json:"type"`
	Payload        *string              `json:"payload"`
	Mode           *string              `json:"mode"`
	Headers        map[string]string    `json:"headers"`
	Timeout        *int                 `json:"timeout"`
	FollowRedirect *bool                `json:"follow-redirect"`
	ProxyServer    *proxyServerPatchDTO `json:"proxy-server"`
}

type proxyServerPatchDTO struct {
	Expand      *bool    `json:"expand"`
	Nameservers []string `json:"nameservers"`
	Timeout     *int     `json:"timeout"`
}

func (r importProxiesRequestDTO) Command() (app.ProxyImportCommand, error) {
	sourceType, err := requiredString("type", r.Type)
	if err != nil {
		return app.ProxyImportCommand{}, err
	}
	payload, err := requiredString("payload", r.Payload)
	if err != nil {
		return app.ProxyImportCommand{}, err
	}
	mode, err := optionalNonEmptyString("mode", r.Mode)
	if err != nil {
		return app.ProxyImportCommand{}, err
	}
	timeout, err := optionalPositiveInt("timeout", r.Timeout)
	if err != nil {
		return app.ProxyImportCommand{}, err
	}
	proxyServer, err := r.ProxyServer.Command()
	if err != nil {
		return app.ProxyImportCommand{}, err
	}

	cmd := app.ProxyImportCommand{
		Type:           sourceType,
		Payload:        payload,
		Headers:        cloneHeaders(r.Headers),
		Timeout:        timeout,
		FollowRedirect: r.FollowRedirect,
		ProxyServer:    proxyServer,
	}
	if mode != nil {
		cmd.Mode = *mode
	}
	return cmd, nil
}

func (r *proxyServerPatchDTO) Command() (*app.ProxyServerOverride, error) {
	if r == nil {
		return nil, nil
	}
	timeout, err := optionalPositiveInt("proxy-server.timeout", r.Timeout)
	if err != nil {
		return nil, err
	}
	return &app.ProxyServerOverride{
		Expand:      r.Expand,
		Nameservers: cloneStrings(r.Nameservers),
		Timeout:     timeout,
	}, nil
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	clone := make([]string, len(values))
	copy(clone, values)
	return clone
}
