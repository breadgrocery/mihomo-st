package httpclient

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	C "github.com/metacubex/mihomo/constant"
)

const DefaultUserAgent = "clash.meta"

type Options struct {
	Timeout          time.Duration
	Transport        http.RoundTripper
	Headers          map[string]string
	DisableRedirects bool
}

func New(opts Options) *http.Client {
	transport := opts.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client := &http.Client{
		Timeout: opts.Timeout,
		Transport: headerTransport{
			base:    transport,
			headers: opts.Headers,
		},
	}
	if opts.DisableRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client
}

func NewProxied(proxy C.Proxy, opts Options) *http.Client {
	opts.Transport = NewProxyTransport(proxy)
	return New(opts)
}

func NewProxyTransport(proxy C.Proxy) http.RoundTripper {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			metadata := C.Metadata{NetWork: C.TCP}
			if err := metadata.SetRemoteAddress(addr); err != nil {
				return nil, err
			}
			return proxy.DialContext(ctx, &metadata)
		},
		MaxIdleConns:          1,
		DisableKeepAlives:     true,
		IdleConnTimeout:       10 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

func NewRequest(ctx context.Context, method, rawURL string, headers map[string]string, body *string) (*http.Request, error) {
	if method == "" {
		method = http.MethodGet
	}
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(*body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return req, nil
}

func MergeHeaders(layers ...map[string]string) map[string]string {
	merged := map[string]string{}
	keys := map[string]string{}
	for _, layer := range layers {
		for name, value := range layer {
			key := strings.ToLower(name)
			if previous, ok := keys[key]; ok && previous != name {
				delete(merged, previous)
			}
			keys[key] = name
			merged[name] = value
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	next := req.Clone(req.Context())
	applyHeaders(next.Header, t.headers)
	return t.base.RoundTrip(next)
}

func applyHeaders(header http.Header, headers map[string]string) {
	for name, value := range headers {
		if hasHeader(header, name) {
			continue
		}
		header.Set(name, value)
	}
	if !hasHeader(header, "User-Agent") {
		header.Set("User-Agent", DefaultUserAgent)
	}
}

func hasHeader(header http.Header, name string) bool {
	for existing := range header {
		if strings.EqualFold(existing, name) {
			return true
		}
	}
	return false
}
