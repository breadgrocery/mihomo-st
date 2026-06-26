package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
)

func TestNewWrapsTransportWithDefaultsAndOptions(t *testing.T) {
	seen := make(chan http.Header, 1)
	client := New(Options{
		Timeout: 250 * time.Millisecond,
		Headers: map[string]string{
			"Authorization": "Bearer configured",
		},
		Transport: captureTransport(func(req *http.Request) (*http.Response, error) {
			seen <- req.Header.Clone()
			return okResponse(req), nil
		}),
	})

	if client.Timeout != 250*time.Millisecond {
		t.Fatalf("client timeout = %s", client.Timeout)
	}
	resp, err := client.Get("https://example.test/resource")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	headers := <-seen
	if headers.Get("User-Agent") != DefaultUserAgent {
		t.Fatalf("default user-agent = %q", headers.Get("User-Agent"))
	}
	if headers.Get("Authorization") != "Bearer configured" {
		t.Fatalf("authorization = %q", headers.Get("Authorization"))
	}
}

func TestRequestHeadersWinOverConfiguredHeaders(t *testing.T) {
	client := New(Options{
		Headers: map[string]string{
			"Authorization": "Bearer option",
			"User-Agent":    "option-agent",
		},
		Transport: captureTransport(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") != "Bearer request" {
				t.Fatalf("authorization = %q", req.Header.Get("Authorization"))
			}
			if req.Header.Get("User-Agent") != "request-agent" {
				t.Fatalf("user-agent = %q", req.Header.Get("User-Agent"))
			}
			return okResponse(req), nil
		}),
	})

	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("authorization", "Bearer request")
	req.Header.Set("user-agent", "request-agent")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestNewRedirectPolicyDefaultsToFollowAndCanBeDisabled(t *testing.T) {
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	defer redirecting.Close()

	followResp, err := New(Options{}).Get(redirecting.URL + "/start")
	if err != nil {
		t.Fatal(err)
	}
	followResp.Body.Close()
	if followResp.Request.URL.Path != "/final" {
		t.Fatalf("followed request path = %s", followResp.Request.URL.Path)
	}

	noFollowResp, err := New(Options{DisableRedirects: true}).Get(redirecting.URL + "/start")
	if err != nil {
		t.Fatal(err)
	}
	noFollowResp.Body.Close()
	if noFollowResp.StatusCode != http.StatusFound || noFollowResp.Request.URL.Path != "/start" {
		t.Fatalf("disabled redirect response = status %d path %s", noFollowResp.StatusCode, noFollowResp.Request.URL.Path)
	}
}

func TestGlobalSkipCertVerifyAppliesToHTTPTransports(t *testing.T) {
	SetSkipCertVerify(true)
	t.Cleanup(func() { SetSkipCertVerify(false) })

	defaultClient := New(Options{})
	defaultTransport, ok := defaultClient.Transport.(headerTransport).base.(*http.Transport)
	if !ok {
		t.Fatalf("default base transport = %T", defaultClient.Transport.(headerTransport).base)
	}
	if defaultTransport == http.DefaultTransport {
		t.Fatal("default transport was not cloned before applying TLS config")
	}
	if defaultTransport.TLSClientConfig == nil || !defaultTransport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("default transport TLS config = %+v", defaultTransport.TLSClientConfig)
	}

	customBase := &http.Transport{}
	customClient := New(Options{Transport: customBase})
	customTransport, ok := customClient.Transport.(headerTransport).base.(*http.Transport)
	if !ok {
		t.Fatalf("custom base transport = %T", customClient.Transport.(headerTransport).base)
	}
	if customTransport == customBase {
		t.Fatal("custom transport was not cloned before applying TLS config")
	}
	if customTransport.TLSClientConfig == nil || !customTransport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("custom transport TLS config = %+v", customTransport.TLSClientConfig)
	}

	proxyTransport := NewProxyTransport(&dialCaptureProxy{}).(*http.Transport)
	if proxyTransport.TLSClientConfig == nil || !proxyTransport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("proxy transport TLS config = %+v", proxyTransport.TLSClientConfig)
	}
}

func TestMergeHeadersAppliesCaseInsensitiveLayerPrecedence(t *testing.T) {
	low := map[string]string{"User-Agent": "low-agent", "X-Test": "low", "X-Keep": "keep"}
	mid := map[string]string{"x-test": "mid"}
	high := map[string]string{"X-TEST": "high", "Authorization": "token"}

	got := MergeHeaders(low, nil, mid, high)
	if got["X-TEST"] != "high" || got["Authorization"] != "token" || got["User-Agent"] != "low-agent" || got["X-Keep"] != "keep" {
		t.Fatalf("merged headers = %+v", got)
	}
	if _, ok := got["X-Test"]; ok {
		t.Fatalf("lower-precedence header spelling survived: %+v", got)
	}
	if _, ok := got["x-test"]; ok {
		t.Fatalf("middle-precedence header spelling survived: %+v", got)
	}

	got["Authorization"] = "mutated"
	if high["Authorization"] != "token" {
		t.Fatalf("MergeHeaders returned source map storage: %+v", high)
	}
	if empty := MergeHeaders(nil, map[string]string{}); empty != nil {
		t.Fatalf("empty merge = %+v, want nil", empty)
	}
}

func TestNewRequestBuildsHTTPRequestsFromTextInputs(t *testing.T) {
	text := `{"ok":true}`
	req, err := NewRequest(context.Background(), "", "https://example.test/api", map[string]string{
		"Content-Type": "application/json",
		"X-Test":       "one",
	}, &text)
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodGet {
		t.Fatalf("default method = %s", req.Method)
	}
	if req.Header.Get("Content-Type") != "application/json" || req.Header.Get("X-Test") != "one" {
		t.Fatalf("request headers = %+v", req.Header)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != text {
		t.Fatalf("body = %q", body)
	}

	post, err := NewRequest(context.Background(), http.MethodPost, "https://example.test/post", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if post.Method != http.MethodPost || post.Body != nil {
		t.Fatalf("post request = method %s body %v", post.Method, post.Body)
	}
}

func TestNewRequestRejectsMalformedMethodOrURL(t *testing.T) {
	if _, err := NewRequest(context.Background(), "BAD METHOD", "https://example.test", nil, nil); err == nil {
		t.Fatal("invalid method was accepted")
	}
	if _, err := NewRequest(context.Background(), http.MethodGet, "http://%zz", nil, nil); err == nil {
		t.Fatal("invalid URL was accepted")
	}
}

func TestNewProxiedRoutesDialThroughMihomoProxy(t *testing.T) {
	proxy := &dialCaptureProxy{}
	client := NewProxied(proxy, Options{Timeout: time.Second})

	resp, err := client.Get("http://target.example/path")
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "dial stopped") {
		t.Fatalf("proxied request error = %v", err)
	}
	if proxy.remote != "target.example:80" {
		t.Fatalf("proxy saw remote address %q", proxy.remote)
	}
}

func TestZeroOptionsKeepHTTPClientDefaultsExceptHeaderTransport(t *testing.T) {
	client := New(Options{})
	if client.Timeout != 0 {
		t.Fatalf("zero option timeout = %s", client.Timeout)
	}
	if client.CheckRedirect != nil {
		t.Fatal("zero options should use net/http redirect policy")
	}
	if client.Transport == nil {
		t.Fatal("transport wrapper is required for default headers")
	}
}

type captureTransport func(*http.Request) (*http.Response, error)

func (f captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func okResponse(req *http.Request) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}
}

type dialCaptureProxy struct {
	remote string
}

func (p *dialCaptureProxy) Name() string { return "dial-capture" }
func (p *dialCaptureProxy) Type() C.AdapterType {
	return C.Direct
}
func (p *dialCaptureProxy) Addr() string { return "dial-capture" }
func (p *dialCaptureProxy) SupportUDP() bool {
	return false
}
func (p *dialCaptureProxy) ProxyInfo() C.ProxyInfo { return C.ProxyInfo{} }
func (p *dialCaptureProxy) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"name": p.Name()})
}
func (p *dialCaptureProxy) DialContext(ctx context.Context, metadata *C.Metadata) (C.Conn, error) {
	p.remote = metadata.RemoteAddress()
	return nil, errors.New("dial stopped")
}
func (p *dialCaptureProxy) ListenPacketContext(context.Context, *C.Metadata) (C.PacketConn, error) {
	return nil, C.ErrNotSupport
}
func (p *dialCaptureProxy) SupportUOT() bool { return false }
func (p *dialCaptureProxy) IsL3Protocol(*C.Metadata) bool {
	return false
}
func (p *dialCaptureProxy) Unwrap(*C.Metadata, bool) C.Proxy { return nil }
func (p *dialCaptureProxy) Close() error                     { return nil }
func (p *dialCaptureProxy) Adapter() C.ProxyAdapter          { return p }
func (p *dialCaptureProxy) AliveForTestUrl(string) bool      { return true }
func (p *dialCaptureProxy) DelayHistory() []C.DelayHistory   { return nil }
func (p *dialCaptureProxy) ExtraDelayHistories() map[string]C.ProxyState {
	return nil
}
func (p *dialCaptureProxy) LastDelayForTestUrl(string) uint16 { return 0 }
func (p *dialCaptureProxy) URLTest(context.Context, string, utils.IntRanges[uint16]) (uint16, error) {
	return 0, C.ErrNotSupport
}

var _ C.Proxy = (*dialCaptureProxy)(nil)
